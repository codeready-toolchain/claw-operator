# Security Hardening — Detailed Design

**Status:** Design complete — all decisions made. See [impl-questions](security-hardening-impl-questions.md) for full rationale on each decision.

**Context:** This document turns the [sketch](security-hardening-sketch.md) (high-level decisions) into an implementable design. The sketch's Q1–Q7 resolved the architectural questions; the [implementation questions](security-hardening-impl-questions.md) Q1–Q12 resolved all remaining design decisions, which are incorporated below.

---

## Overview

The OpenClaw operator deploys a personal AI assistant into a user's Kubernetes namespace. Today, the operator handles basic credential injection (single Gemini API key on the CRD), egress NetworkPolicies, and gateway token generation. This design extends the security posture across five areas:

1. **ClawCredential CRD** — unified `ClawCredential` CRD with type discriminator, replacing `spec.apiKey`
2. **Go credential proxy** — replaces the nginx proxy with a Go credential-injecting forward proxy driven by ClawCredential CRs
3. **Ingress NetworkPolicy** — restricts gateway access to the OpenShift router
4. **Container hardening** — init container security context, Route host injection into ConfigMap
5. **Status reporting** — `gatewayTokenSecretRef` and other observability fields on the OpenClaw CR

## Design Principles

- **Zero plaintext credentials in CRDs** — all secrets live in Kubernetes Secrets, referenced by name/key
- **Declarative extensibility** — adding a new service with an existing auth shape is a new CR instance, not a code change
- **Atomic resource management** — the Kustomize + SSA pattern is preserved; new resources integrate into the existing pipeline
- **Clean break, no legacy code** — the operator is pre-v1 with no production users; `spec.apiKey` is removed outright, credentials flow exclusively through ClawCredential CRs
- **Defense in depth** — NetworkPolicies (L4) + proxy allowlisting (L7) + two-layer auth header stripping (strip then inject) + credential redaction in logs + 502 on injection failure

---

## Architecture

### ClawCredential CRD

One new CRD (`ClawCredential`, short name `cc`) in the `openclaw.sandbox.redhat.com/v1alpha1` API group with a `type` discriminator:

| Type | Shape | Key fields |
|------|-------|------------|
| `apiKey` | Custom header with secret value | `secretRef`, `domain`, `apiKey.header`, `apiKey.valuePrefix`, `defaultHeaders` |
| `bearer` | `Authorization: Bearer <token>` | `secretRef`, `domain`, `defaultHeaders` |
| `gcp` | GCP SA JSON → OAuth2 token + token vending | `secretRef`, `domain`, `gcp.project`, `gcp.location` |
| `pathToken` | Token in URL path | `secretRef`, `domain`, `pathToken.prefix` |
| `oauth2` | Client credentials → token exchange | `secretRef`, `domain`, `oauth2.clientID`, `oauth2.tokenURL`, `oauth2.scopes` |
| `kubernetes` | ServiceAccount projected token | `domain`, `kubernetes.serviceAccountName` |
| `none` | Proxy allowlist (no auth) | `domain` |

**Domain format:** exact match (`api.github.com`) or suffix match (`.googleapis.com`, leading dot). See [credential-examples.md](security-hardening-credential-examples.md) for syntax details.

Each ClawCredential CR is labeled `openclaw.sandbox.redhat.com/instance: instance` so the controller can discover all credentials for a given OpenClaw instance.

**Controller architecture ([Q1](security-hardening-impl-questions.md)):** Single unified controller with one additional `Watches()` for `ClawCredential` that enqueues the parent `OpenClaw` CR. SSA applies are idempotent, so re-applying all resources on a credential change is harmless.

**Validation ([Q10](security-hardening-impl-questions.md)):** CEL validation rules on the CRD enforce that the correct type-specific sub-struct is present for each `type` value. The controller validates during reconciliation as defense-in-depth (e.g., checking that referenced Secrets exist).

**Phase 2 types ([Q11](security-hardening-impl-questions.md)):** `apiKey`, `bearer`, `gcp`, and `kubernetes` ship in Phase 2. All 7 types are registered in the CRD schema from the start; unsupported types (`pathToken`, `oauth2`, `none`) log a warning and set a condition.

**Type definitions** live in `api/v1alpha1/` alongside the existing `OpenClaw` types in a single `clawcredential_types.go` file. See [credential-examples.md](security-hardening-credential-examples.md) for the full Go types and YAML examples for each type.

### Go Credential Proxy

A credential-injecting MITM forward proxy replacing the nginx proxy. OpenClaw uses standard `HTTP_PROXY`/`HTTPS_PROXY` environment variables with real `https://` API URLs. The proxy intercepts CONNECT tunnels, terminates TLS with an operator-managed CA, injects credentials based on the target domain, and forwards as HTTPS to upstream APIs. This is the same proven architecture used by all reference projects (paude-proxy, OpenShell, onecli). See [Q12](security-hardening-impl-questions.md) for full rationale.

**Traffic flow ([Q12](security-hardening-impl-questions.md)):**

```
OpenClaw ──CONNECT host:443──▶ Go Proxy (MITM: TLS terminate, inject creds) ──HTTPS──▶ upstream API
```

1. The operator sets `HTTP_PROXY`/`HTTPS_PROXY` env vars on the OpenClaw Deployment pointing to the proxy Service
2. OpenClaw's HTTP clients (undici `EnvHttpProxyAgent`) send `CONNECT api.anthropic.com:443` to the proxy
3. The proxy accepts the CONNECT, generates a leaf cert for the target domain signed by its CA, TLS-terminates the client side
4. On the decrypted stream, the proxy reads HTTP requests and applies credential injection
5. NetworkPolicy prevents OpenClaw from reaching the internet directly (enforcement layer)

**CA management:** The operator generates a CA cert+key pair at first reconciliation, stores it in Secret `openclaw-proxy-ca`. The CA cert+key is mounted into the proxy pod (for signing leaf certs). The CA cert only is mounted into the OpenClaw pod with `NODE_EXTRA_CA_CERTS` pointing to it (additive — supplements system CAs). On OpenShift, the proxy's outbound trust store uses `config.openshift.io/inject-trusted-cabundle` to handle corporate proxy CAs transparently.

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

**Injector types** (one per `ClawCredential.spec.type`):
- `apiKey` — sets a configured header to the secret value (with optional value prefix); injects optional `defaultHeaders` (e.g., `anthropic-version: 2023-06-01`)
- `bearer` — sets `Authorization: Bearer <token>`
- `gcp` — loads SA JSON, obtains short-lived OAuth2 token via `golang.org/x/oauth2/google`, caches and auto-refreshes; includes **token vending** — intercepts `POST oauth2.googleapis.com/token` and returns a dummy token so Google SDK clients inside OpenClaw work with placeholder credentials (pattern from paude-proxy)
- `pathToken` — inserts the token into the URL path (e.g., Telegram `/bot<TOKEN>/...`)
- `oauth2` — exchanges client credentials for a short-lived access token, injects as Bearer
- `kubernetes` — reads projected ServiceAccount token from file (re-read on each request for rotation), injects as Bearer for kube-apiserver
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

**Configuration format ([Q3](security-hardening-impl-questions.md)):** JSON ConfigMap with hybrid secret delivery. API keys and tokens use environment variables; GCP SA JSON uses a mounted volume. The operator generates the JSON config from ClawCredential CRs. ConfigMap changes trigger a Deployment rollout via annotation hash. Example:

```json
{
  "routes": [
    {
      "domain": ".googleapis.com",
      "injector": "api_key",
      "header": "x-goog-api-key",
      "envVar": "GEMINI_API_KEY"
    },
    {
      "domain": "api.anthropic.com",
      "injector": "api_key",
      "header": "x-api-key",
      "envVar": "ANTHROPIC_API_KEY",
      "defaultHeaders": { "anthropic-version": "2023-06-01" }
    },
    {
      "domain": "api.github.com",
      "injector": "bearer",
      "envVar": "GITHUB_TOKEN"
    }
  ]
}
```

**Health endpoint:** `GET /healthz` returns 200.

**Image:** Custom Go binary, built as a distroless container image.

**Image build ([Q4](security-hardening-impl-questions.md)):** `Containerfile.proxy` in this repo, built with `podman build`, same CI pipeline as the operator. OLM bundle declares the proxy in `spec.relatedImages` (by digest, for disconnected installs) and injects `PROXY_IMAGE` env var on the operator Deployment. Both images built from every commit for version lockstep.

### Operator Changes

**Reconciliation flow changes:**

```
Reconcile(ctx, req)
 ↓
1. Fetch OpenClaw CR (unchanged)
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
5. discoverCredentials(ctx, instance)          ← NEW
 ├─ List all ClawCredential CRs in namespace by label
 ├─ Validate each (type-specific fields present, secretRef exists, domain non-empty)
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
9. resolveRouteHostname(ctx, instance)          ← NEW (Q8: two-pass)
 ├─ Read Route status.ingress[0].host
 ├─ If hostname available and changed, patch ConfigMap allowedOrigins
 ├─ If hostname not yet available, requeue (Ready=False)
 └─ On non-OpenShift (no Route CRD), skip
 ↓
10. updateStatus(ctx, instance)                 ← NEW
  ├─ Set gatewayTokenSecretRef
  └─ Set conditions (Ready, CredentialsResolved, ProxyConfigured)
  ↓
11. Return success
```

**`applyProxySecret` removal:** The existing `applyProxySecret()` method and `spec.apiKey` field are removed outright — no transition period. The operator has no production users yet (pre-v1), so there is no migration concern. Credentials flow exclusively through ClawCredential CRs → proxy config. See [Q5](security-hardening-impl-questions.md) for rationale.

**Status fields ([Q6](security-hardening-impl-questions.md)):**

```go
type OpenClawStatus struct {
    GatewayTokenSecretRef string             `json:"gatewayTokenSecretRef,omitempty"`
    Conditions            []metav1.Condition `json:"conditions,omitempty"`
}
```

Conditions following Kubernetes conventions:
- `Ready` — overall instance health (gates on Route hostname resolution, credentials, proxy config)
- `CredentialsResolved` — all ClawCredential CRs reference valid Secrets
- `ProxyConfigured` — proxy ConfigMap generated successfully

### Ingress NetworkPolicy

A new NetworkPolicy restricting ingress to the OpenClaw gateway pod:

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

**Router label ([Q7](security-hardening-impl-questions.md)):** `policy-group.network.openshift.io/ingress: ""` is the official OpenShift convention (documented 4.17–4.21). On vanilla Kubernetes, the operator skips this NetworkPolicy based on whether the Route CRD is registered (same pattern used for Route resources).

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

**Route host injection** — the operator reads the Route hostname and substitutes `OPENCLAW_ROUTE_HOST` in the ConfigMap's `allowedOrigins` before applying.

**Route hostname discovery ([Q8](security-hardening-impl-questions.md)):** Two-pass reconciliation. First pass applies all resources including the Route. Second pass reads `status.ingress[0].host` and patches the ConfigMap. The `Ready` condition remains `False` until the hostname is resolved and the ConfigMap is patched — consumers never see a ready instance with a broken CORS `allowedOrigins`.

---

## Implementation Plan

**Phasing ([Q9](security-hardening-impl-questions.md)):** Two main phases plus cleanup. Originally three, but Q11 requires `gcp` and `kubernetes` from launch (which need the Go proxy), merging the CRD and proxy phases.

### Phase 1: Quick wins (no new CRDs, no proxy rewrite)

1. **Init container security context** — single manifest change
2. **Route host injection** — operator reads Route hostname, patches ConfigMap `allowedOrigins`
3. **Ingress NetworkPolicy** — new manifest in `internal/assets/manifests/`
4. **Status fields** — add `gatewayTokenSecretRef` + conditions (`Ready`, `CredentialsResolved`, `ProxyConfigured`) to `OpenClawStatus`

### Phase 2: ClawCredential CRD + Go proxy

5. **Remove `spec.apiKey`** — clean break, no deprecation (no production users yet)
6. **Define credential CRD types** — `api/v1alpha1/` type definitions + CEL validation + `make manifests && make generate`
7. **Credential discovery** — controller logic to list ClawCredential CRs by label
8. **Proxy CA management** — `applyProxyCA()` generates/stores CA cert+key in Secret, mounts into proxy and OpenClaw pods (Q12)
9. **Proxy config generation** — operator builds proxy config JSON from discovered credentials
10. **Build the Go proxy** — CONNECT + MITM handler, leaf cert generation, `apiKey`/`bearer`/`gcp`/`kubernetes` injectors; port paude-proxy injection layer, add health endpoint, config-driven routing
11. **OpenClaw config generation** — operator generates `openclaw.json` from ClawCredential CRs with real `https://` base URLs, placeholder API keys, and `request.proxy.mode: "env-proxy"`; sets `HTTP_PROXY`/`HTTPS_PROXY` + `NODE_EXTRA_CA_CERTS` env vars on OpenClaw Deployment
12. **Proxy container image** — Containerfile, CI pipeline (podman), OLM bundle with `relatedImages` + `PROXY_IMAGE` env var
13. **Replace nginx manifests** — update `proxy-deployment.yaml`, `proxy-configmap.yaml`, remove nginx entrypoint
14. **Remove `applyProxySecret`** — credentials now flow through credential CRDs → proxy config

### Phase 3: Remaining types + cleanup

15. **Add remaining injectors** — `pathToken`, `oauth2`, `none` in the Go proxy
16. **RBAC audit** — trim operator ClusterRole to least-privilege
17. **Threat model documentation** — application-layer security analysis

---

## Decisions Summary

All implementation questions are resolved. See [security-hardening-impl-questions.md](security-hardening-impl-questions.md) for full rationale on each.

| # | Question | Decision |
|---|----------|----------|
| Q1 | Controller architecture | Single unified controller + one `Watches()` for `ClawCredential` |
| Q2 | Proxy source location | Same repo: `cmd/proxy/` + `internal/proxy/` |
| Q3 | Proxy config format | JSON ConfigMap + hybrid secrets (env vars + volume for GCP) |
| Q4 | Proxy image build | `Containerfile.proxy`, podman, OLM bundle with `relatedImages` |
| Q5 | `spec.apiKey` migration | Clean break — removed outright (no production users) |
| Q6 | Status fields | `gatewayTokenSecretRef` + conditions (`Ready`, `CredentialsResolved`, `ProxyConfigured`) |
| Q7 | Ingress NP router labels | `policy-group.network.openshift.io/ingress` + conditional skip on non-OpenShift |
| Q8 | Route hostname discovery | Two-pass reconciliation, `Ready=False` until resolved |
| Q9 | Implementation phasing | Phase 1 (quick wins) → Phase 2 (CRDs + Go proxy) → Phase 3 (remaining types + cleanup) |
| Q10 | CRD field validation | CEL validation rules + controller defense-in-depth |
| Q11 | Phase 2 credential types | `apiKey`, `bearer`, `gcp`, `kubernetes` (all 7 registered in schema) |
| Q12 | Proxy traffic routing | CONNECT + MITM with operator-managed CA; `HTTP_PROXY`/`HTTPS_PROXY` env vars |
