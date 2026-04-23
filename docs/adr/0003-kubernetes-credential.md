# ADR-0003: Kubernetes Credential Type

**Status:** Implemented (Phase 1); Phase 2 deferred

**Date:** 2026-04-22

---

## Overview

The claw-operator's proxy supports credential types (`apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none`) for injecting LLM and external API credentials. This adds a seventh type — `kubernetes` — enabling the AI assistant to interact with Kubernetes API servers through the same credential-injecting proxy, with the same security properties: the assistant never sees raw tokens, all auth headers are stripped and replaced, and network policies prevent direct API server access.

The credential input is a **standard kubeconfig file**, giving the operator everything it needs in one artifact: server URLs, auth credentials, contexts, and namespaces. Multi-cluster, external clusters, and namespace awareness all come for free.

This is the generic claw-operator implementation. Platform-specific concerns (namespace isolation, ServiceAccount provisioning, RBAC policies) are delegated to the deploying platform (e.g., Dev Sandbox).

## Design Principles

- **Operator is platform-agnostic** — the operator accepts a kubeconfig in a Secret and processes it. It does not create ServiceAccounts, RoleBindings, or decide what RBAC the assistant gets. That's the platform's job.
- **Kubeconfig is the standard** — users and platforms already produce kubeconfigs. The operator consumes them natively rather than requiring manual token extraction.
- **Same security model as LLM credentials** — the proxy strips all client auth headers and injects the real token. The gateway pod has no direct route to the API server (blocked by NetworkPolicy). The assistant cannot present its own token or bypass injection.
- **Multi-cluster for free** — a single kubeconfig can contain multiple clusters with different tokens. The proxy injects the right credential per API server `hostname:port`.
- **Fits existing credential patterns** — reuses the `spec.credentials[]` array, proxy injector interface, and controller patching pipeline. No new controllers or CRDs.

---

## Architecture

### CRD Extension

A new credential type `kubernetes`:

```yaml
credentials:
  - name: k8s-workspace
    type: kubernetes
    secretRef:
      name: my-kubeconfig
      key: kubeconfig           # a standard kubeconfig YAML file
```

No `KubernetesConfig` struct is needed — the kubeconfig file IS the configuration. The operator parses it to extract everything: server URLs, auth tokens, contexts, and default namespaces.

### Operator Processing Pipeline

When the operator encounters a `kubernetes` credential during reconciliation:

```
1. Read the Secret, extract the kubeconfig YAML
   └─ client-go/tools/clientcmd parses it

2. Validate: all users must use token auth (v1)
   └─ Reject kubeconfigs with client certs or exec-based auth (clear error message)

3. Extract cluster server URLs
   └─ For each cluster: derive hostname + port for proxy route
   └─ Extract unique non-443 ports for NetworkPolicy patching

4. Generate proxy routes
   └─ One route per cluster server hostname:port
   └─ Injector type: "kubernetes"
   └─ Kubeconfig file path for the proxy to read tokens from

5. Generate sanitized kubeconfig
   └─ Same clusters, contexts, namespaces, CA data
   └─ All user tokens replaced with "proxy-managed-token"
   └─ Write to operator-managed ConfigMap (claw-kube-config)

6. Patch deployments
   └─ Proxy: mount original kubeconfig Secret as volume
   └─ Gateway: mount sanitized kubeconfig ConfigMap, set KUBECONFIG env var
   └─ Gateway: inject Kubernetes skill for OpenClaw auto-discovery

7. Patch proxy egress NetworkPolicy
   └─ Add any non-443 ports from kubeconfig server URLs
```

### Traffic Flow

```
Claw Gateway (assistant runs kubectl/oc)
    │
    │  KUBECONFIG=/etc/kube/config  (sanitized — dummy tokens)
    │  HTTP_PROXY=http://claw-proxy:8080
    │  → CONNECT api.cluster.example.com:6443
    │
    ▼
Claw Proxy (MITM mode)
    │
    │  1. Match hostname:port against kubeconfig cluster servers
    │  2. TLS-terminate (MITM CA)
    │  3. Strip all auth headers (including Impersonate-*)
    │  4. Read original kubeconfig, find token for this cluster
    │  5. Inject Authorization: Bearer <real-token>
    │  6. Re-encrypt to upstream
    │
    ▼
Kubernetes API server
    │
    │  RBAC check against injected SA token
    │
    ▼
Target namespace resources
```

The Kubernetes API is accessed through the proxy's existing **MITM forward proxy mode** (CONNECT tunneling). This is the natural fit because:
- kubectl and client libraries use standard HTTPS to the API server
- `HTTP_PROXY` / `HTTPS_PROXY` env vars are already set on the gateway pod
- MITM mode handles CONNECT → TLS terminate → inject → forward, same as GitHub/npm/etc.
- No `provider` field needed — this isn't an LLM provider for `openclaw.json`

### Multi-Cluster Example

```yaml
# User provides this kubeconfig in a Secret
apiVersion: v1
kind: Config
clusters:
  - name: dev-cluster
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority-data: ...
  - name: staging-cluster
    cluster:
      server: https://api.staging.example.com:6443
      certificate-authority-data: ...
contexts:
  - name: dev
    context:
      cluster: dev-cluster
      user: dev-sa
      namespace: alice-dev
  - name: staging
    context:
      cluster: staging-cluster
      user: staging-sa
      namespace: staging-apps
current-context: dev
users:
  - name: dev-sa
    user:
      token: <real-token-1>
  - name: staging-sa
    user:
      token: <real-token-2>
```

The operator generates:
- Proxy route for `kubernetes.default.svc:443` → inject `<real-token-1>`
- Proxy route for `api.staging.example.com:6443` → inject `<real-token-2>`
- Proxy egress NetworkPolicy patched to allow port 6443 (in addition to existing 443)
- Sanitized kubeconfig ConfigMap with dummy tokens
- OpenClaw skill listing both contexts with their namespaces

### NetworkPolicy

The gateway pod's egress is restricted to the proxy only — it cannot reach any API server directly. This is the critical security property.

The proxy's egress NetworkPolicy allows port 443 to `0.0.0.0/0` (no destination constraint — LLM APIs sit behind CDNs with dynamic IPs). The operator dynamically adds any non-443 ports parsed from the kubeconfig's cluster server URLs (e.g., 6443) to the same scope.

**Residual risk:** Adding ports to `0.0.0.0/0` is a marginal expansion on an already wide-open baseline. The real L7 security boundary is the proxy itself — only configured routes are forwarded, everything else returns 403. Platforms that need tighter network controls can layer CNI-specific policies (e.g., Cilium FQDN-based egress) on top.

### Upstream TLS CA Trust

Kubernetes API servers typically use cluster-specific CAs not in the system trust store. The controller passes each kubeconfig cluster's `certificate-authority-data` through the proxy config as a base64-encoded PEM `caCert` field on the route. At startup, `NewServer()` merges all route CA certs into the transport's root CA pool alongside system CAs.

### Token Refresh

The operator does not manage token refresh (consistent with all other credential types). If the platform updates the Secret contents (e.g., rotating tokens), `stampSecretVersionAnnotation` detects the Secret's `ResourceVersion` change and triggers a rolling restart of the proxy pod. The new pod reads the updated kubeconfig on startup. No additional file-watching logic needed.

### Credential Resolution Refactor

The implementation refactored `validateCredentials` to `resolveCredentials`, returning `[]resolvedCredential` instead of just an error. Each resolved credential wraps the original `CredentialSpec` with optional parsed `kubeconfigData` (non-nil for kubernetes type only). This pre-parsed data is consumed by all downstream functions without re-reading Secrets or re-parsing YAML.

### Port-Aware Route Matching

The proxy's `MatchRoute` was extended to support port-qualified domains. Routes with a port in their domain (e.g., `api.example.com:6443`) match the full `host:port` string. Bare hostname routes preserve the existing port-stripping behavior for LLM API domains.

### Gateway Tooling

The gateway pod receives `kubectl` and `oc` via an init container that copies them from a configurable image (`KUBECTL_IMAGE` env var, default `quay.io/openshift/origin-cli:4.21`) into a shared emptyDir volume at `/opt/kube-tools`. The `PATH` env var is prepended accordingly.

### Assistant Awareness

Kubernetes context information is delivered as an OpenClaw workspace skill at `skills/kubernetes/SKILL.md` for auto-discovery. The skill content is dynamically generated from the kubeconfig's contexts and always overwritten on pod start. See [ADR-0002](0002-config-preservation.md) for the config delivery mechanism.

---

## New Resources

When a `kubernetes` credential is configured, the operator creates one additional resource:

| Resource | Purpose |
|----------|---------|
| ConfigMap `claw-kube-config` | Sanitized kubeconfig (dummy tokens, real clusters/contexts/namespaces) |

The original kubeconfig Secret is user-managed and mounted directly (not copied).

---

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Token provisioning | User-provided Secret only — operator mounts, doesn't provision | Simplest approach — no new RBAC, no token refresh logic, no cross-namespace concerns. Consistent with operator's role. Token lifecycle is the platform's concern |
| Q2 | Credential input format | Kubeconfig file in a Secret | Standard Kubernetes credential format. Multi-cluster, namespace-aware, and context-aware for free. `client-go/tools/clientcmd` parsing is battle-tested. No separate `KubernetesConfig` struct needed |
| Q3 | Auth methods (v1) | Token-only — reject client certs and exec with clear error | Simplest implementation, covers the primary use case (SA tokens). Client certificates are architecturally different from header injection (mTLS vs. Bearer). Exec plugins are a security concern in the proxy pod |
| Q4 | Egress NetworkPolicy | Dynamically add ports parsed from kubeconfig server URLs | The operator already parses kubeconfig to extract server URLs. Adding ports to the egress NetworkPolicy follows the same in-memory kustomize patching pattern used elsewhere. External clusters on non-443 ports work automatically |
| Q5 | Proxy caching | Parse once on startup; token rotation handled by pod restart | No file watching, no mutex. Consistent with all other credential types. `stampSecretVersionAnnotation` triggers rolling restart on Secret change |
| Q6 | Sanitized kubeconfig delivery | Dedicated ConfigMap (`claw-kube-config`) | Contains no real credentials — ConfigMap is the right primitive. The operator already manages multiple ConfigMaps |
| Q7 | Assistant awareness | OpenClaw skill file with contexts/namespaces from kubeconfig | Initially designed as AGENTS.md injection, then a separate KUBERNETES.md file, and finally settled on an OpenClaw workspace skill (`skills/kubernetes/SKILL.md`) for auto-discovery. The skill is always-overwritten because the content is operator-derived. See [ADR-0002](0002-config-preservation.md) for delivery mechanism |

---

## Phase 2: Future Enhancements

- **Client certificate auth** — support `client-certificate-data` / `client-key-data` in kubeconfigs. Architecturally different from header injection (requires per-route TLS transport configuration in the proxy)
- **Token health status condition** — decode JWT tokens to detect expiration and surface it as a status condition with `RequeueAfter` for proactive re-reconciliation
