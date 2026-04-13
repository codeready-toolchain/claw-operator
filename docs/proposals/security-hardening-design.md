# Security Hardening — Detailed Design

**Status:** Design complete — all decisions made. See [impl-questions](security-hardening-impl-questions.md) for full rationale on each decision.

**Context:** This document turns the [sketch](security-hardening-sketch.md) (high-level decisions) into an implementable design. The sketch's Q1–Q7 resolved the architectural questions; the [implementation questions](security-hardening-impl-questions.md) Q1–Q12 resolved all remaining design decisions, which are incorporated below.

---

## Overview

The OpenClaw operator deploys a personal AI assistant into a user's Kubernetes namespace. Today, the operator handles basic credential injection (single Gemini Secret reference on the CRD via `spec.geminiAPIKey`), egress NetworkPolicies, gateway token generation, and Route hostname injection. This design extends the security posture across five areas:

1. **Inline credentials** — `spec.credentials[]` array on the `Claw` CRD (renamed from `OpenClaw`) with type discriminator, replacing `spec.geminiAPIKey`
2. **Go credential proxy** — replaces the nginx proxy with a Go credential-injecting forward proxy driven by `spec.credentials`
3. **Ingress NetworkPolicy** — restricts gateway access to the OpenShift router
4. **Container hardening** — init container security context (Route host injection already implemented)
5. **Status reporting** — `gatewayTokenSecretRef` and granular conditions (replacing the existing `Available` condition)

## Design Principles

- **Zero plaintext credentials in CRDs** — all secrets live in Kubernetes Secrets, referenced by name/key
- **Declarative extensibility** — adding a new service with an existing auth shape is a new entry in `spec.credentials`, not a code change
- **Atomic resource management** — the Kustomize + SSA pattern is preserved; new resources integrate into the existing pipeline
- **Clean break, no legacy code** — the operator is pre-v1 with no production users; `spec.geminiAPIKey` is replaced by `spec.credentials[]`
- **Defense in depth** — NetworkPolicies (L4) + proxy allowlisting (L7) + two-layer auth header stripping (strip then inject) + credential redaction in logs + 502 on injection failure

---

## Architecture

### CRD Rename: OpenClaw → Claw

The CRD Kind is renamed from `OpenClaw` to `Claw` to support potential future distributions (e.g., NemoClaw). The API group remains `openclaw.sandbox.redhat.com/v1alpha1` (the operator project identity). Internal resource names (`openclaw-gateway-token`, `openclaw-config`, etc.) are unchanged.

### Inline Credentials (`spec.credentials[]`)

Credentials are an array field on the `Claw` CRD spec, each with a `type` discriminator. No separate CRD — everything is in one resource:

```yaml
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef: { name: llm-keys, key: GEMINI_API_KEY }
      domain: ".googleapis.com"
      apiKey: { header: x-goog-api-key }
    - name: github
      type: bearer
      secretRef: { name: platform-tokens, key: GITHUB_TOKEN }
      domain: api.github.com
```

| Type | Shape | Key fields |
|------|-------|------------|
| `apiKey` | Custom header with secret value | `secretRef`, `domain`, `apiKey.header`, `apiKey.valuePrefix`, `defaultHeaders` |
| `bearer` | `Authorization: Bearer <token>` | `secretRef`, `domain`, `defaultHeaders` |
| `gcp` | GCP SA JSON → OAuth2 token + token vending | `secretRef`, `domain`, `gcp.project`, `gcp.location` |
| `pathToken` | Token in URL path | `secretRef`, `domain`, `pathToken.prefix` |
| `oauth2` | Client credentials → token exchange | `secretRef`, `domain`, `oauth2.clientID`, `oauth2.tokenURL`, `oauth2.scopes` |
| `none` | Proxy allowlist (no auth) | `domain` |

**Domain format:** exact match (`api.github.com`) or suffix match (`.googleapis.com`, leading dot). See [credential-examples.md](security-hardening-credential-examples.md) for syntax details.

**Controller architecture ([Q1](security-hardening-impl-questions.md)):** Single unified controller. Credentials are read directly from `instance.Spec.Credentials` — no label-based discovery, no additional `Watches()` for a separate CRD. Secret watching still uses the existing `findOpenClawsReferencingSecret` pattern (updated to scan `spec.credentials[*].secretRef`).

**Validation ([Q10](security-hardening-impl-questions.md)):** CEL validation rules on the `credentials` array items enforce that the correct type-specific sub-struct is present for each `type` value. The controller validates during reconciliation as defense-in-depth (e.g., checking that referenced Secrets exist).

**Phase 2 types ([Q11](security-hardening-impl-questions.md)):** `apiKey`, `bearer`, and `gcp` ship in Phase 2. The remaining types (`pathToken`, `oauth2`, `none`) were added subsequently. The `kubernetes` type was deferred to Phase 3 pending a design decision on SA token projection vs user-managed secrets.

**Type definitions** live in `api/v1alpha1/openclaw_types.go` alongside the existing `Claw` types. The `CredentialSpec` struct is used as the array element type for `ClawSpec.Credentials`. See [credential-examples.md](security-hardening-credential-examples.md) for the full Go types and YAML examples for each type.

### Go Credential Proxy

A credential-injecting MITM forward proxy replacing the nginx proxy. The Claw gateway uses standard `HTTP_PROXY`/`HTTPS_PROXY` environment variables with real `https://` API URLs. The proxy intercepts CONNECT tunnels, terminates TLS with an operator-managed CA, injects credentials based on the target domain, and forwards as HTTPS to upstream APIs. This is the same proven architecture used by all reference projects (paude-proxy, OpenShell, onecli). See [Q12](security-hardening-impl-questions.md) for full rationale.

**Traffic flow ([Q12](security-hardening-impl-questions.md)):**

```
Claw Gateway ──CONNECT host:443──▶ Go Proxy (MITM: TLS terminate, inject creds) ──HTTPS──▶ upstream API
```

1. The operator sets `HTTP_PROXY`/`HTTPS_PROXY` env vars on the gateway Deployment pointing to the proxy Service
2. The gateway's HTTP clients (undici `EnvHttpProxyAgent`) send `CONNECT api.anthropic.com:443` to the proxy
3. The proxy accepts the CONNECT, generates a leaf cert for the target domain signed by its CA, TLS-terminates the client side
4. On the decrypted stream, the proxy reads HTTP requests and applies credential injection
5. NetworkPolicy prevents the gateway from reaching the internet directly (enforcement layer)

**CA management:** The operator generates a CA cert+key pair at first reconciliation, stores it in Secret `openclaw-proxy-ca`. The CA cert+key is mounted into the proxy pod (for signing leaf certs). The CA cert only is mounted into the gateway pod with `NODE_EXTRA_CA_CERTS` pointing to it (additive — supplements system CAs). On OpenShift, the proxy's outbound trust store uses `config.openshift.io/inject-trusted-cabundle` to handle corporate proxy CAs transparently.

**Source location ([Q2](security-hardening-impl-questions.md)):** Same repo — `cmd/proxy/main.go` + `internal/proxy/`. Proxy can import `api/v1alpha1` types directly. CRD type changes, proxy config format, and manifests land in one PR.

**Core request flow (per CONNECT tunnel):**
1. Read config (JSON) from a mounted ConfigMap
2. Accept `CONNECT host:port` from client, check domain against allowlist
3. Generate leaf cert for target domain, TLS-terminate client side
4. On decrypted HTTP: match `Host` header against configured domain patterns (exact match or `.suffix` wildcard — e.g., `.googleapis.com` matches `aiplatform.googleapis.com`)
5. **Strip all client-supplied auth headers** (`Authorization`, `x-api-key`, `x-goog-api-key`, etc.) — defense in depth, not just overwrite (pattern from OpenShell's two-layer stripping)
6. Look up the injector for the matched domain
7. Inject real credentials and any default headers for the provider
8. Open HTTPS connection to real upstream, forward request
9. If route matches but injection **fails** (e.g., GCP token refresh error) → **502** with `"credential injection failed"` (pattern from paude-proxy — clear signal vs. confusing 401 from upstream)
10. Unknown/disallowed domains → **403**

**Injector types** (one per credential entry's `type`):
- `apiKey` — sets a configured header to the secret value (with optional value prefix); injects optional `defaultHeaders` (e.g., `anthropic-version: 2023-06-01`)
- `bearer` — sets `Authorization: Bearer <token>`
- `gcp` — loads SA JSON, obtains short-lived OAuth2 token via `golang.org/x/oauth2/google`, caches and auto-refreshes; includes **token vending** — intercepts `POST oauth2.googleapis.com/token` and returns a dummy token so Google SDK clients inside OpenClaw work with placeholder credentials (pattern from paude-proxy)
- `pathToken` — inserts the token into the URL path (e.g., Telegram `/bot<TOKEN>/...`)
- `oauth2` — exchanges client credentials for a short-lived access token, injects as Bearer
- `none` — no credential injection, just forwards (allowlist entry)

**Domain matching:**
- Exact: `api.github.com` — matches only that host
- Suffix: `.googleapis.com` — matches `aiplatform.googleapis.com`, `generativelanguage.googleapis.com`, etc. (leading `.` indicates suffix match, following paude-proxy's convention)
- First match wins; routes are checked in config order
- The operator emits exact-match routes before suffix-match routes in the generated config JSON to ensure predictable matching

**Security conventions:**
- **Credential redaction in logs** — all log output redacts secret values as `[REDACTED]` (pattern from OpenShell)
- **Auth header stripping before injection** — explicit `Del` on all known auth headers before `Set`, not relying on overwrite semantics alone
- **502 on injection failure** — matched route + failed injection → 502, not silent passthrough

**Configuration format ([Q3](security-hardening-impl-questions.md)):** JSON ConfigMap with hybrid secret delivery. API keys and tokens use environment variables; GCP SA JSON uses a mounted volume. The operator generates the JSON config from `spec.credentials` entries. ConfigMap changes trigger a Deployment rollout via annotation hash. Example:

```json
{
  "routes": [
    {
      "domain": ".googleapis.com",
      "injector": "api_key",
      "header": "x-goog-api-key",
      "envVar": "CRED_GEMINI"
    },
    {
      "domain": "api.anthropic.com",
      "injector": "api_key",
      "header": "x-api-key",
      "envVar": "CRED_ANTHROPIC",
      "defaultHeaders": { "anthropic-version": "2023-06-01" }
    },
    {
      "domain": "api.github.com",
      "injector": "bearer",
      "envVar": "CRED_GITHUB"
    }
  ]
}
```

**Secret delivery — how credentials reach the proxy:**

The operator never copies secret values. It reads each credential entry's `secretRef` and generates Kubernetes-native references on the proxy Deployment that the kubelet resolves at pod startup:

1. **Env vars for simple credentials** (API keys, bearer tokens) — the operator adds a `valueFrom.secretKeyRef` per credential entry, pointing to the user's own Secret:

    ```yaml
    # Generated from spec.credentials[name=gemini] (secretRef: llm-keys/GEMINI_API_KEY)
    env:
      - name: CRED_GEMINI
        valueFrom:
          secretKeyRef:
            name: llm-keys
            key: GEMINI_API_KEY
    ```

    The config JSON references the env var name (`"envVar": "CRED_GEMINI"`), not the secret value. The proxy reads `os.Getenv("CRED_GEMINI")` at startup.

2. **Volume mounts for large/structured credentials** (GCP SA JSON) — the operator adds a Secret volume mount:

    ```yaml
    volumes:
      - name: cred-vertex-ai
        secret:
          secretName: gcp-sa-secret
          items:
            - key: sa-key.json
              path: sa-key.json
    volumeMounts:
      - name: cred-vertex-ai
        mountPath: /etc/proxy/credentials/vertex-ai
        readOnly: true
    ```

    The config JSON references the file path (`"saFilePath": "/etc/proxy/credentials/vertex-ai/sa-key.json"`). The proxy reads the file at startup and refreshes OAuth2 tokens from the SA JSON.

The env var names (e.g., `CRED_GEMINI`) are operator-generated from the credential entry's `name` field, ensuring uniqueness. ConfigMap and Deployment changes are applied atomically via SSA; a config hash annotation on the Deployment triggers a rollout when credentials change.

```
User creates:
  Claw (instance) with spec.credentials[]  ──each entry references──▶  Secret (llm-keys, gcp-sa, ...)
       │
Operator reads spec.credentials, generates:
  ├─ ConfigMap (proxy config JSON)     ← domain/injector/envVar mappings (no secrets)
  └─ Proxy Deployment                  ← env vars with secretKeyRef → user's Secrets
       │                                  volume mounts → user's Secrets (for GCP)
       │
Proxy at runtime:
  ├─ Reads config JSON from ConfigMap mount
  ├─ Reads credential values from env vars (populated by kubelet from user's Secrets)
  ├─ Reads GCP SA JSON from volume mount (populated by kubelet from user's Secret)
  └─ On each request: match domain → look up injector → read credential → inject header
```

The actual secret bytes never pass through the operator or the ConfigMap.

**Health endpoint:** `GET /healthz` returns 200.

**Image:** Custom Go binary, built as a distroless container image.

**Image build ([Q4](security-hardening-impl-questions.md)):** `Containerfile.proxy` in this repo, built with `podman build`, same CI pipeline as the operator. OLM bundle declares the proxy in `spec.relatedImages` (by digest, for disconnected installs) and injects `PROXY_IMAGE` env var on the operator Deployment. Both images built from every commit for version lockstep.

### Operator Changes

**Reconciliation flow changes:**

```
Reconcile(ctx, req)
 ↓
1. Fetch Claw CR (unchanged, new Kind name)
 ↓
2. Filter by name "instance" (unchanged)
 ↓
3. applyGatewaySecret (unchanged)
 ↓
4. applyProxyCA(ctx, instance)                ← NEW (Q12)
 ├─ Check if openclaw-proxy-ca Secret exists with valid CA
 ├─ If missing or nearing expiry, generate CA cert+key (crypto/x509 + crypto/ecdsa)
 ├─ Store in Secret openclaw-proxy-ca (cert + key)
 ├─ Set owner reference
 └─ SSA apply
 ↓
5. validateCredentials(instance.Spec.Credentials)  ← NEW (reads directly from CR spec)
 ├─ Validate each entry (type-specific fields present, secretRef exists, domain non-empty)
 └─ Return aggregated credential config
 ↓
6. generateProxyConfig(credentials)            ← NEW
 ├─ Build proxy config JSON from credential list
 └─ Return ConfigMap data
 ↓
7. applyProxyConfigMap(ctx, instance, config)   ← NEW
 ├─ Create/update ConfigMap with generated proxy config
 └─ SSA apply
 ↓
8. applyKustomizedResources (updated manifests)
 ↓
9. resolveRouteHostname(ctx, instance)          ← EXISTS (two-pass already implemented: applyRouteOnly → getRouteURL → injectRouteHostIntoConfigMap)
 ├─ Read Route status.ingress[0].host
 ├─ If hostname available and changed, patch ConfigMap allowedOrigins
 ├─ If hostname not yet available, requeue (Ready=False)
 └─ Route CRD is always present (OpenShift-only)
 ↓
10. updateStatus(ctx, instance)                 ← EXISTS (extend with gatewayTokenSecretRef + new conditions)
  ├─ Set gatewayTokenSecretRef
  └─ Set conditions (Ready, CredentialsResolved, ProxyConfigured) — replacing existing Available condition
  ↓
11. Return success
```

**Gemini credential wiring removal:** The existing `configureProxyDeployment()` method (which patches the proxy Deployment's `GEMINI_API_KEY` env var to reference the user's Secret via `secretKeyRef`) and `stampSecretVersionAnnotation()` (which triggers rollouts on Secret changes) are removed, along with `spec.geminiAPIKey` on the Claw CRD. Credentials flow exclusively through `spec.credentials[]` → proxy config. See [Q5](security-hardening-impl-questions.md) for rationale.

**Status fields ([Q6](security-hardening-impl-questions.md)):**

The current status already has `Conditions` (with an `Available` condition) and `URL`. The design extends it with `GatewayTokenSecretRef` and replaces the single `Available` condition with more granular conditions:

```go
type ClawStatus struct {
    GatewayTokenSecretRef string             `json:"gatewayTokenSecretRef,omitempty"`
    URL                   string             `json:"url,omitempty"`
    Conditions            []metav1.Condition `json:"conditions,omitempty"`
}
```

Conditions following Kubernetes conventions (replacing `Available`):
- `Ready` — overall instance health (gates on Route hostname resolution, credentials, proxy config)
- `CredentialsResolved` — all `spec.credentials` entries reference valid Secrets
- `ProxyConfigured` — proxy ConfigMap generated successfully

### Ingress NetworkPolicy

A new NetworkPolicy restricting ingress to the gateway pod:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: openclaw-ingress
spec:
  podSelector:
    matchLabels:
      app: openclaw
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              # OpenShift router namespace label
              policy-group.network.openshift.io/ingress: ""
      ports:
        - port: 18789
          protocol: TCP
```

**Router label ([Q7](security-hardening-impl-questions.md)):** `policy-group.network.openshift.io/ingress: ""` is the official OpenShift convention (documented 4.17–4.21). The operator targets OpenShift exclusively; Route and router-specific NetworkPolicy resources are always applied.

This manifest is added to `internal/assets/manifests/` and included in `kustomization.yaml`.

### Container Hardening

**Init container security context** — add to the `init-config` container in `deployment.yaml`:

```yaml
securityContext:
  runAsNonRoot: true
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
  seccompProfile:
    type: RuntimeDefault
```

The init container writes to `/home/node/.openclaw` (PVC-mounted), so `readOnlyRootFilesystem: true` is safe.

**Route host injection** — already implemented. The operator reads the Route hostname and substitutes `OPENCLAW_ROUTE_HOST` in the ConfigMap's `allowedOrigins` before applying, using `injectRouteHostIntoConfigMap()`.

**Route hostname discovery ([Q8](security-hardening-impl-questions.md)):** Two-pass reconciliation is already implemented. First pass applies the Route via `applyRouteOnly()`. Then `getRouteURL()` reads `status.ingress[0].host` (requeueing after 5 s if not yet available). Finally `injectRouteHostIntoConfigMap()` patches the ConfigMap. The `Ready` condition remains `False` until the hostname is resolved and the ConfigMap is patched — consumers never see a ready instance with a broken CORS `allowedOrigins`. (The operator targets OpenShift; Route CRD is always present.)

---

## Implementation Plan

**Phasing ([Q9](security-hardening-impl-questions.md)):** Two main phases plus cleanup. Originally three, but Q11 requires `gcp` from launch (which needs the Go proxy), merging the CRD and proxy phases. The `kubernetes` type was deferred to Phase 3 after discovering that projected SA token volumes don't support user-specified ServiceAccounts.

### Phase 1: Quick wins (includes CRD schema changes: CRD rename and new status field, no proxy rewrite) - DONE

1. **Rename CRD** — `OpenClaw` → `Claw` (Kind, Go types, constants, manifests, tests)
2. **Init container security context** — single manifest change
3. ~~**Route host injection**~~ — already implemented (`injectRouteHostIntoConfigMap` with two-pass reconciliation via `applyRouteOnly` → `getRouteURL`)
4. **Ingress NetworkPolicy** — new manifest in `internal/assets/manifests/`
5. **Status fields** — add `gatewayTokenSecretRef`, rename `Available` condition to `Ready` (same semantics for now; `CredentialsResolved` and `ProxyConfigured` added in Phase 2 when those subsystems exist)

### Phase 2: Inline credentials + Go proxy - DONE

6. **Define `spec.credentials[]` types and replace `spec.geminiAPIKey`** — `CredentialSpec`, `CredentialType`, sub-config structs in `api/v1alpha1/` + CEL validation + `make manifests && make generate`; clean break, no deprecation (no production users)
7. **Credential validation** — controller reads `instance.Spec.Credentials` directly, validates Secrets exist
8. **Proxy CA management** — `applyProxyCA()` generates/stores CA cert+key in Secret, mounts into proxy and gateway pods (Q12)
9. **Proxy config generation** — operator builds proxy config JSON from `spec.credentials`
10. **Build the Go proxy** — CONNECT + MITM handler, leaf cert generation, `apiKey`/`bearer`/`gcp`/`pathToken`/`oauth2`/`none` injectors; port paude-proxy injection layer, add health endpoint, config-driven routing
11. **Gateway config generation** — operator generates `openclaw.json` from `spec.credentials` with real `https://` base URLs, placeholder API keys, and `request.proxy.mode: "env-proxy"`; sets `HTTP_PROXY`/`HTTPS_PROXY` + `NODE_EXTRA_CA_CERTS` env vars on gateway Deployment
12. **Proxy container image** — Containerfile, CI pipeline (podman), OLM bundle with `relatedImages` + `PROXY_IMAGE` env var
13. **Replace nginx manifests** — update `proxy-deployment.yaml`, `proxy-configmap.yaml`, remove nginx entrypoint and `configureProxyDeployment()`/`stampSecretVersionAnnotation()` code
14. **Status conditions** — add `CredentialsResolved` and `ProxyConfigured` conditions (subsystems now exist)

### Phase 3: Kubernetes credential type + cleanup

15. **`kubernetes` credential type** — deferred from Phase 2. Requires a design decision on how to inject ServiceAccount tokens for the Kubernetes API: projected SA token volumes only provide the proxy pod's own SA token, not a user-specified one. Options include using the TokenRequest API to mint tokens for arbitrary SAs, accepting a user-managed Secret containing a kubeconfig/token, or another approach. Will be designed and implemented when there is a concrete use case.
16. **RBAC audit** — trim operator ClusterRole to least-privilege
17. **Threat model documentation** — application-layer security analysis

---

## Decisions Summary

All implementation questions are resolved. See [security-hardening-impl-questions.md](security-hardening-impl-questions.md) for full rationale on each.

| # | Question | Decision |
|---|----------|----------|
| Q1 | Controller architecture | Single unified controller; credentials read from `spec.credentials[]` (no separate CRD watch) |
| Q2 | Proxy source location | Same repo: `cmd/proxy/` + `internal/proxy/` |
| Q3 | Proxy config format | JSON ConfigMap + hybrid secrets (env vars + volume for GCP) |
| Q4 | Proxy image build | `Containerfile.proxy`, podman, OLM bundle with `relatedImages` |
| Q5 | `spec.geminiAPIKey` migration | Clean break — replaced by `spec.credentials[]` (no production users) |
| Q6 | Status fields | `gatewayTokenSecretRef` + `Ready` (Phase 1); `CredentialsResolved`, `ProxyConfigured` (Phase 2) |
| Q7 | Ingress NP router labels | `policy-group.network.openshift.io/ingress` (OpenShift-only, always applied) |
| Q8 | Route hostname discovery | Two-pass reconciliation, `Ready=False` until resolved |
| Q9 | Implementation phasing | Phase 1 (quick wins + CRD rename) → Phase 2 (inline credentials + Go proxy) → Phase 3 (remaining types + cleanup) |
| Q10 | Credential validation | CEL validation rules on `spec.credentials[]` items + controller defense-in-depth |
| Q11 | Phase 2 credential types | `apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none` (6 types implemented; `kubernetes` deferred to Phase 3) |
| Q12 | Proxy traffic routing | CONNECT + MITM with operator-managed CA; `HTTP_PROXY`/`HTTPS_PROXY` env vars |
