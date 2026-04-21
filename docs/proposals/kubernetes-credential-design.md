# Kubernetes Credential Type — Design Document

**Status:** Final
**Decisions:** [kubernetes-credential-questions.md](kubernetes-credential-questions.md)

---

## Overview

The claw-operator's proxy currently supports six credential types (`apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none`) for injecting LLM and external API credentials. This design adds a seventh type — `kubernetes` — enabling the AI assistant to interact with Kubernetes API servers through the same credential-injecting proxy, with the same security properties: the assistant never sees raw tokens, all auth headers are stripped and replaced, and network policies prevent direct API server access.

The credential input is a **standard kubeconfig file**, giving the operator everything it needs in one artifact: server URLs, auth credentials, contexts, and namespaces. Multi-cluster, external clusters, and namespace awareness all come for free.

This is the generic claw-operator implementation. Platform-specific concerns (namespace isolation, ServiceAccount provisioning, RBAC policies) are delegated to the deploying platform (e.g., Dev Sandbox).

## Design Principles

- **Operator is platform-agnostic** — the operator accepts a kubeconfig in a Secret and processes it. It does not create ServiceAccounts, RoleBindings, or decide what RBAC the assistant gets. That's the platform's job.
- **Kubeconfig is the standard** — users and platforms already produce kubeconfigs. The operator consumes them natively rather than requiring manual token extraction.
- **Same security model as LLM credentials** — the proxy strips all client auth headers and injects the real token. The gateway pod has no direct route to the API server (blocked by NetworkPolicy). The assistant cannot present its own token or bypass injection.
- **Multi-cluster for free** — a single kubeconfig can contain multiple clusters with different tokens. The proxy injects the right credential per API server hostname.
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
             │
2. Validate: all users must use token auth (v1)
   └─ Reject kubeconfigs with client certs or exec-based auth (clear error message)
             │
3. Extract cluster server URLs
   └─ For each cluster: derive hostname + port for proxy route
   └─ Extract unique non-443 ports for NetworkPolicy patching
             │
4. Generate proxy routes
   └─ One route per cluster server hostname
   └─ Injector type: "kubernetes"
   └─ Kubeconfig file path for the proxy to read tokens from
             │
5. Generate sanitized kubeconfig
   └─ Same clusters, contexts, namespaces, CA data
   └─ All user tokens replaced with "proxy-managed-token"
   └─ Write to operator-managed ConfigMap (claw-kube-config)
             │
6. Patch deployments
   └─ Proxy: mount original kubeconfig Secret as volume
   └─ Gateway: mount sanitized kubeconfig ConfigMap, set KUBECONFIG env var
   └─ Gateway: inject AGENTS.md Kubernetes section
             │
7. Patch proxy egress NetworkPolicy
   └─ Add any non-443 ports from kubeconfig server URLs
```

### Traffic Flow

```
Claw Gateway (assistant runs kubectl)
    │
    │  KUBECONFIG=/etc/kube/config  (sanitized — dummy tokens)
    │  HTTP_PROXY=http://claw-proxy:8080
    │  → CONNECT api.cluster.example.com:6443
    │
    ▼
Claw Proxy (MITM mode)
    │
    │  1. Match hostname against kubeconfig cluster servers
    │  2. TLS-terminate (MITM CA)
    │  3. Strip all auth headers
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
- AGENTS.md section listing both contexts with their namespaces

### NetworkPolicy

The gateway pod's egress is restricted to the proxy only — it cannot reach any API server directly. This is the critical security property.

The proxy's egress NetworkPolicy currently allows port 443 to `0.0.0.0/0`. The operator dynamically adds any non-443 ports parsed from the kubeconfig's cluster server URLs (e.g., 6443). This follows the same in-memory kustomize object patching pattern as `injectRouteHostIntoConfigMap`.

### Token Refresh

The operator does not manage token refresh (consistent with all other credential types). If the platform updates the Secret contents (e.g., rotating the kubeconfig's tokens), `stampSecretVersionAnnotation` detects the Secret's `ResourceVersion` change and triggers a rolling restart of the proxy pod. The new pod reads the updated kubeconfig on startup. This reuses the same restart-on-Secret-change mechanism as all other credential types — no additional file-watching logic needed.

---

## Controller Changes

### CRD Types (`api/v1alpha1/claw_types.go`)

1. Add `CredentialTypeKubernetes CredentialType = "kubernetes"`
2. Add to the kubebuilder enum marker: `apiKey;bearer;gcp;pathToken;oauth2;none;kubernetes`
3. No new CEL validation needed — the existing rule already requires `secretRef` for all types except `none`, which covers `kubernetes`
4. No `KubernetesConfig` struct needed

### Provider Defaults (`resolveProviderDefaults`)

Add a `kubernetes` case — no defaults needed (domain is derived from the kubeconfig, not the `domain` field). Skip the domain-required check for `kubernetes` type since domains come from the kubeconfig.

### Credential Resolution Refactor (`validateCredentials` → `resolveCredentials`)

**Refactor:** Rename `validateCredentials` to `resolveCredentials`. It both validates credentials AND extracts parsed data needed by downstream functions. Returns `[]resolvedCredential` instead of just an error.

```go
type kubeconfigData struct {
    Clusters []kubeconfigCluster // server URL, hostname, port per cluster
    Contexts []kubeconfigContext // name, cluster, namespace, current flag
}

type resolvedCredential struct {
    clawv1alpha1.CredentialSpec
    KubeConfig *kubeconfigData // non-nil for kubernetes type only
}
```

The existing validation logic for all other types is unchanged — they just return `resolvedCredential` with a nil `KubeConfig` field.

For a `kubernetes` credential:
1. Read the referenced Secret and extract the kubeconfig data
2. Parse with `client-go/tools/clientcmd` (already a transitive dependency via controller-runtime)
3. Validate all users use token-based auth (reject client certs, exec, etc.)
4. Validate all cluster server URLs are parseable
5. Validate one-token-per-server: each unique cluster server URL must map to exactly one token. If multiple contexts reference the same cluster with different users/tokens, reject the kubeconfig with an error (the proxy's `hostname:port → token` map cannot represent this; the user must split into separate kubeconfigs or use the same user)
6. Populate `kubeconfigData` with extracted cluster/context metadata

The resolved data is then passed to all downstream functions: `generateProxyConfig`, `applySanitizedKubeconfig`, `injectKubePortsIntoNetworkPolicy`, `injectKubernetesIntoAgentsMd` — each consuming the pre-parsed `kubeconfigData` without re-reading Secrets or re-parsing YAML.

### Proxy Config Generation (`generateProxyConfig`)

**Signature change:** `generateProxyConfig(credentials []resolvedCredential)` instead of `[]CredentialSpec`.

For non-kubernetes types, the function accesses `cred.CredentialSpec` — minimal test churn (wrap existing `CredentialSpec` in `resolvedCredential` with nil `KubeConfig`).

For a `kubernetes` credential:
1. Read `cred.KubeConfig.Clusters` (already parsed and validated)
2. Emit a proxy route per cluster with domain set to `hostname:port` (e.g., `api.example.com:6443`) — including port enables `MatchRoute` to disambiguate clusters on the same hostname
3. The `domain` field on `CredentialSpec` is left empty — domains are derived from `kubeconfigData.Clusters`

### Deployment Patching (`configureProxyForCredentials`)

Add a `kubernetes` case:
- Mount the user's kubeconfig Secret as a volume on the proxy pod (similar to GCP SA JSON pattern)
- Mount path: `/etc/proxy/credentials/<cred-name>/kubeconfig`

### Gateway Deployment Patching (new: `configureClawDeploymentForKubernetes`)

When a `kubernetes` credential is present:
1. Mount the sanitized kubeconfig ConfigMap as a volume
2. Set `KUBECONFIG` env var pointing to the mount path

### Sanitized Kubeconfig Generation (new: `applySanitizedKubeconfig`)

**Reconciliation ordering:** Called in Phase 1 (alongside `applyGatewaySecret` and `applyProxyResources`), before Phase 2 (Route) and Phase 3 (remaining resources). The ConfigMap must exist before the gateway Deployment references it.

Takes the original kubeconfig bytes (available from `resolveCredentials` — the Secret was already read during resolution):
1. Walk all users, replace `token` with `"proxy-managed-token"`
2. Preserve all clusters, contexts, namespaces, CA data
3. Serialize to YAML
4. Create/update ConfigMap `claw-kube-config` with owner reference
5. Server-side apply

### NetworkPolicy Patching (new: `injectKubePortsIntoNetworkPolicy`)

1. Read unique ports from `resolvedCredential.KubeConfig.Clusters` (already parsed)
2. If any port != 443, find the `claw-proxy-egress` NetworkPolicy in the kustomize objects
3. Add the additional port(s) to the egress rules
4. Same in-memory patching pattern as `injectRouteHostIntoConfigMap`

**Implementation note:** Add a new constant `ClawProxyEgressNetworkPolicyName = "claw-proxy-egress"` alongside the existing `ClawIngressNetworkPolicyName`. The function finds the NP by name in the parsed kustomize objects and modifies it in-memory before apply, following the same pattern as `injectRouteHostIntoConfigMap`.

### AGENTS.md Injection (new: `injectKubernetesIntoAgentsMd`)

1. Read contexts from `resolvedCredential.KubeConfig.Contexts` (already parsed)
2. Build a Kubernetes section with context list and current-context
3. Append to the AGENTS.md content in the `claw-config` ConfigMap

---

## Proxy Changes

### New Injector: `KubernetesInjector` (`internal/proxy/injector_kubernetes.go`)

```go
type KubernetesInjector struct {
    kubeconfigPath string
    defaultHeaders map[string]string
    tokens         map[string]string  // server hostname:port → bearer token
}
```

- Parses the kubeconfig once on startup, builds a hostname→token map
- No file watching — token rotation is handled by pod restart via `stampSecretVersionAnnotation` (same as all other credential types)
- `Inject(req)`: look up the token for `req.URL.Host`, set `Authorization: Bearer <token>`
- Registered in `NewInjector()` factory as `"kubernetes"`

### Proxy Config Route Format

```json
{
  "domain": "kubernetes.default.svc",
  "injector": "kubernetes",
  "kubeconfigPath": "/etc/proxy/credentials/k8s-workspace/kubeconfig"
}
```

One route per cluster server hostname. All share the same kubeconfig file path (the injector maps hostnames to the right token internally).

**Note:** The proxy's `MatchRoute` currently strips the port from the `Host` header. For Kubernetes routes, the domain should include the port when it differs from 443 (e.g., `api.cluster.example.com:6443`) and `MatchRoute` must be updated to support port-aware matching. This prevents ambiguity when two clusters share the same hostname on different ports.

### Proxy Config Struct Changes

The `Route` struct in `internal/proxy/config.go` needs a new `KubeconfigPath` field. The controller-side `proxyRoute` struct (used in `generateProxyConfig`) needs a corresponding field so that the JSON config can carry the path from the controller to the proxy.

### Auth Header Stripping

The existing `StripAuthHeaders` function already removes `Authorization`, `X-Api-Key`, `X-Goog-Api-Key`, and `Proxy-Authorization`. This covers Kubernetes API auth headers. No changes needed.

---

## OpenClaw Configuration

### KUBECONFIG

The gateway pod gets:
- `KUBECONFIG=/etc/kube/config` env var
- The sanitized kubeconfig mounted at that path from the `claw-kube-config` ConfigMap

Standard `kubectl` and client-go discover the API server and context automatically.

**Prerequisite:** The OpenClaw container image must include `kubectl`. This is an OpenClaw image concern, not a claw-operator concern — the operator only creates the kubeconfig and mounts it.

### AGENTS.md

The operator injects a Kubernetes section into AGENTS.md:

```markdown
## Kubernetes Access

You have access to Kubernetes clusters via `kubectl`. Your KUBECONFIG is
pre-configured. Available contexts:
- `dev` (cluster: kubernetes.default.svc, namespace: alice-dev) [current]
- `staging` (cluster: api.staging.example.com, namespace: staging-apps)

Use `kubectl` commands to manage resources. The proxy handles authentication
transparently — do not attempt to manage tokens or kubeconfig yourself.
```

### No Provider Entry

The `kubernetes` credential does **not** add a provider entry to `openclaw.json`. It is MITM-only. No changes to `injectProvidersIntoConfigMap()`.

---

## New Resources

When a `kubernetes` credential is configured, the operator creates one additional resource:

| Resource | Purpose |
|----------|---------|
| ConfigMap `claw-kube-config` | Sanitized kubeconfig (dummy tokens, real clusters/contexts/namespaces) |

The original kubeconfig Secret is user-managed and mounted directly (not copied).

---

## Implementation Plan

### Phase 1: CRD + Controller + Proxy Injector

1. Add `CredentialTypeKubernetes` to CRD types + enum marker
2. Refactor `validateCredentials` → `resolveCredentials` returning `[]resolvedCredential` (all existing tests updated to use new signature)
3. Update `generateProxyConfig` to accept `[]resolvedCredential` (minimal test churn — wrap existing specs)
4. Add `kubernetes` case to `resolveProviderDefaults()` (skip domain requirement)
5. Add `kubernetes` case to `resolveCredentials()` (parse kubeconfig, validate token auth, populate `kubeconfigData`)
6. Add `kubernetes` case to `generateProxyConfig()` (routes per cluster from resolved data)
7. Add `kubernetes` case to `configureProxyForCredentials()` (volume mount)
8. Implement `applySanitizedKubeconfig()` (generate + apply ConfigMap)
9. Implement `configureClawDeploymentForKubernetes()` (mount kubeconfig, set KUBECONFIG)
10. Implement `injectKubePortsIntoNetworkPolicy()` (dynamic port patching)
11. Implement `injectKubernetesIntoAgentsMd()` (append context info)
12. Implement `KubernetesInjector` in `internal/proxy/injector_kubernetes.go`
13. Register in `NewInjector()` factory
14. Add `kubeconfigPath` field to proxy `Route` struct and controller-side `proxyRoute` struct
15. Update `MatchRoute` for port-aware matching — compare `hostname:port` when a route's domain includes a port (e.g., `api.example.com:6443`); preserve existing port-stripping behavior for LLM routes that use bare hostnames
16. Unit tests for `MatchRoute` port-aware matching (hostname:port collisions, e.g., `api.example.com:6443` vs `:8443`)
17. Run `make manifests generate`
18. Unit tests for all new code paths
19. Integration tests with envtest

### Phase 2: Future Enhancements

- Client certificate auth support in the proxy
- Status condition for token health (detect expired tokens)
- Kubernetes skill file (SKILL.md) for richer assistant behavior

---

## Decisions Summary

| # | Question | Decision |
|---|----------|----------|
| Q1 | Token provisioning | User-provided Secret only — operator mounts, doesn't provision |
| Q2 | Credential input format | Kubeconfig file — standard format, multi-cluster, namespace-aware |
| Q3 | Auth methods (v1) | Token-only — reject client certs and exec with clear error |
| Q4 | Egress NetworkPolicy | Dynamically add ports parsed from kubeconfig server URLs |
| Q5 | Proxy caching | Parse once on startup; token rotation handled by pod restart via `stampSecretVersionAnnotation` |
| Q6 | Sanitized kubeconfig delivery | Dedicated ConfigMap (`claw-kube-config`) |
| Q7 | AGENTS.md | Inject Kubernetes section with contexts/namespaces from kubeconfig |
