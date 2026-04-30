# ADR-0001: Security Hardening

**Status:** Implemented (Phase 1 + Phase 2); Phase 3 deferred

**Date:** 2025-04-16

---

## Overview

The Claw Operator deploys a personal AI assistant into a user's Kubernetes namespace. This ADR covers the security posture across five areas:

1. **Inline credentials** — `spec.credentials[]` array on the `Claw` CRD with type discriminator
2. **Go credential proxy** — a hybrid proxy (gateway + MITM) driven by `spec.credentials`
3. **Ingress NetworkPolicy** — restricts gateway access to the OpenShift router
4. **Container hardening** — init container security context
5. **Status reporting** — `gatewayTokenSecretRef` and granular conditions (`Ready`, `CredentialsResolved`, `ProxyConfigured`)

## Design Principles

- **Zero plaintext credentials in CRDs** — all secrets live in Kubernetes Secrets, referenced by name/key
- **Declarative extensibility** — adding a new service with an existing auth shape is a new entry in `spec.credentials`, not a code change
- **Atomic resource management** — Kustomize + SSA pattern preserved; new resources integrate into the existing pipeline
- **Clean break, no legacy code** — the operator is pre-v1 with no production users; `spec.geminiAPIKey` replaced by `spec.credentials[]`
- **Defense in depth** — NetworkPolicies (L4) + proxy allowlisting (L7) + two-layer auth header stripping (strip then inject) + credential redaction in logs + 502 on injection failure

---

## Decisions

All questions resolved across sketch (Q1–Q7) and implementation questions (Q1–Q12).

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Gateway token management | Operator auto-generates 64-char hex token, stores in Secret `claw-gateway-token`, reports name in `status.gatewayTokenSecretRef` | Fully automated, zero friction, token never in CRD. Rejected: user-provided token (plaintext-in-CRD problem), manual override (fragile ownership detection) |
| Q2 | Credential provisioning | Inline `spec.credentials[]` array with `type` discriminator on the `Claw` CRD | Single resource, atomic updates, no label discovery, no extra Watch. Rejected: typed CRDs per shape (7+ CRDs), separate `ClawCredential` CRD (unnecessary complexity) |
| Q3 | Network policy tightening | Add ingress NetworkPolicy restricting gateway to OpenShift router; existing egress unchanged | Blocks lateral movement from in-cluster pods. Composable with monitoring NPs. Rejected: proxy egress IP/CIDR restriction (CDN IPs too dynamic), Cilium DNS-based (not universally available) |
| Q4 | Operator RBAC | Deferred until credential and proxy changes complete | Credential/proxy work alters required permissions; audit after stabilization |
| Q5 | Container hardening | Init container security context matching main container | Required for Pod Security Admission restricted profile. Deferred: image digest pinning, Route TLS config |
| Q6 | Application-layer security | Document threat model; investigate assistant RBAC scoping | |
| Q7 | Proxy architecture | MITM forward proxy built on `github.com/elazarl/goproxy` with `OPENCLAW_PROXY_ACTIVE=1` | OpenClaw 2026.4.27+ native managed proxy support enables pure MITM for all traffic. Provider baseUrls point to real upstreams; proxy injects credentials transparently |

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Controller architecture | Single unified controller; credentials from `spec.credentials[]` | No separate CRD watch needed. Secret watching via `findClawsReferencingSecret`. SSA applies are idempotent |
| Q2 | Proxy source location | Same repo: `cmd/proxy/` + `internal/proxy/` | ~800–1000 LOC; CRD types + proxy + manifests land in one PR. Rejected: separate repo (cross-repo pain), Go workspaces (build complexity) |
| Q3 | Proxy config format | JSON ConfigMap + hybrid secret delivery (env vars for simple creds, volume mounts for GCP SA JSON) | Operator never copies secret values — generates `secretKeyRef` and volume references on the proxy Deployment. Rejected: env-only (GCP JSON awkward), volume-only (aggregation complexity) |
| Q4 | Proxy image build | `Containerfile.proxy`, podman, OLM bundle with `relatedImages` + `PROXY_IMAGE` env var | Standard OLM operand image pattern. Both images built every commit for version lockstep |
| Q5 | `spec.geminiAPIKey` migration | Clean break — replaced by `spec.credentials[]` | Pre-v1, no production users. Rejected: two-phase deprecation (code for zero users), permanent shortcut (two ways forever) |
| Q6 | Status fields | `gatewayTokenSecretRef` + `Ready`, `CredentialsResolved`, `ProxyConfigured` conditions | Standard `kubectl wait --for=condition=Ready`. Rejected: per-credential summary (over-engineering) |
| Q7 | Ingress NP router labels | `policy-group.network.openshift.io/ingress: ""` | Official OpenShift 4.17+ convention, operator-managed label. Rejected: hardcoded namespace name (fragile) |
| Q8 | Route hostname discovery | Two-pass reconciliation; `Ready=False` until resolved | Auto-generated hostnames are the norm on Dev Sandbox. Rejected: computed hostname (fragile), user-specified field (operational concern in CRD) |
| Q9 | Implementation phasing | Phase 1 (quick wins + CRD rename) → Phase 2 (all 6 credential types + Go proxy) → Phase 3 (kubernetes type, deferred) | GCP requirement merged original Phases 2+3. Kubernetes type deferred due to SA token projection limitations |
| Q10 | Credential validation | CEL validation rules on `spec.credentials[]` items + controller defense-in-depth | Admission-time feedback, no webhook infrastructure. Requires Kubernetes 1.25+ |
| Q11 | Phase 2 credential types | `apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none` (6 types) | Covers all current use cases. `kubernetes` deferred to Phase 3 |
| Q12 | Proxy traffic routing | Pure MITM forward proxy (CONNECT, all traffic) via `HTTP_PROXY` + `OPENCLAW_PROXY_ACTIVE=1` | OpenClaw 2026.4.27+ supports `OPENCLAW_PROXY_ACTIVE` env for managed proxy environments; gateway mode (path-based reverse proxy) retained in proxy code but no longer used for provider routing |

---

## Architecture

### CRD: `Claw` (renamed from `OpenClaw`)

API group `claw.sandbox.redhat.com/v1alpha1`. Internal resource names use `claw-` prefix.

### Inline Credentials (`spec.credentials[]`)

Credentials are an array field with a `type` discriminator:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef: { name: llm-keys, key: GEMINI_API_KEY }
      provider: google     # generates provider entry in operator.json
    - name: github
      type: bearer
      secretRef: { name: platform-tokens, key: GITHUB_TOKEN }
      domain: api.github.com
      # no provider → MITM forward proxy only
```

| Type | Injection mechanism | Key fields |
|------|-------------------|------------|
| `apiKey` | Custom header with secret value | `secretRef`, `domain`, `apiKey.header`, `apiKey.valuePrefix`, `defaultHeaders`, `provider` |
| `bearer` | `Authorization: Bearer <token>` | `secretRef`, `domain`, `defaultHeaders`, `provider` |
| `gcp` | GCP SA JSON → OAuth2 token + token vending | `secretRef`, `domain`, `gcp.project`, `gcp.location`, `provider` |
| `pathToken` | Token in URL path | `secretRef`, `domain`, `pathToken.prefix` |
| `oauth2` | Client credentials → token exchange | `secretRef`, `domain`, `oauth2.clientID`, `oauth2.tokenURL`, `oauth2.scopes`, `provider` |
| `none` | Proxy allowlist (no auth) | `domain` |

**Provider field:** When set (e.g., `"google"`, `"anthropic"`), the controller configures gateway routing and dynamically generates the provider entry in `openclaw.json`. When omitted, the credential is used for MITM forward proxy only.

**Provider defaults:** For known providers (`google`, `anthropic`), the controller infers `domain` and `apiKey.header` via `resolveProviderDefaults()`. Explicit values are preserved as an escape hatch.

**CEL validation:** 5 rules enforce type-specific config presence at admission time. Controller validates during reconciliation as defense-in-depth.

**Domain format:** Exact match (`api.github.com`) or suffix match (`.googleapis.com`, leading dot). First match wins; exact before suffix.

### Hybrid Go Credential Proxy

A credential-injecting MITM forward proxy built on `github.com/elazarl/goproxy`:

```
# MITM forward proxy mode (all traffic):
Claw Gateway ──CONNECT host:443──▶ Go Proxy (MITM: TLS terminate, inject creds) ──HTTPS──▶ upstream API
  HTTP_PROXY=http://claw-proxy:8080
  OPENCLAW_PROXY_ACTIVE=1
  baseUrl: https://generativelanguage.googleapis.com/v1beta  (real upstream, proxy transparent)
```

All traffic from the gateway pod flows through the MITM proxy via `HTTP_PROXY`/`HTTPS_PROXY` env vars. OpenClaw's native managed proxy support (`OPENCLAW_PROXY_ACTIVE=1`) enables the SSRF guard to route requests through the env proxy. Provider `baseUrl` values point to real upstream URLs; the proxy intercepts CONNECT tunnels, generates leaf certs from the operator-managed CA, TLS-terminates, matches Host header against domain patterns, and injects credentials transparently.

**Injector types:**
- `apiKey` — configured header with optional value prefix + default headers
- `bearer` — `Authorization: Bearer <token>`
- `gcp` — SA JSON → OAuth2 token refresh + token vending (intercepts `oauth2.googleapis.com/token`)
- `pathToken` — token inserted into URL path
- `oauth2` — client credentials → token exchange → Bearer
- `none` — passthrough (allowlist entry)

**Security conventions:**
- Auth header stripping before injection (explicit `Del` on all known auth headers before `Set`)
- Credential redaction in all log output
- 502 on injection failure (not silent passthrough)
- 403 on unknown/disallowed domains

**CA management:** P-256 ECDSA CA cert+key generated at first reconciliation, stored in Secret `claw-proxy-ca`. CA cert+key mounted into proxy; CA cert only into gateway. Trust env vars: `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`, `GIT_SSL_CAINFO`. On OpenShift, `config.openshift.io/inject-trusted-cabundle` handles corporate proxy CAs.

**Secret delivery:** The operator never copies secret values. It generates Kubernetes-native references on the proxy Deployment:
- Env vars (`valueFrom.secretKeyRef`) for simple credentials (apiKey, bearer, pathToken, oauth2)
- Volume mounts for structured credentials (GCP SA JSON)

**Config format:** JSON ConfigMap (`claw-proxy-config`) with route definitions. Config hash annotation triggers rollout on changes. Secret ResourceVersion annotations trigger rollout on Secret data changes.

**Builtin passthrough domains:** `openrouter.ai` auto-injected as `none` route for model pricing API.

**Vertex SDK providers:** When `type: gcp` + non-Google `provider` (e.g., `anthropic`), the controller uses native Vertex AI SDK path:
- Provider key `<provider>-vertex` in `openclaw.json` with real Vertex AI URL
- Stub ADC ConfigMap for token refresh bootstrap
- Model entries auto-remapped (e.g., `anthropic/...` → `anthropic-vertex/...`)

### Ingress NetworkPolicy

Restricts gateway ingress to OpenShift router only:

```yaml
podSelector:
  matchLabels:
    app: claw
ingress:
  - from:
      - namespaceSelector:
          matchLabels:
            policy-group.network.openshift.io/ingress: ""
    ports:
      - port: 18789
        protocol: TCP
```

### Container Hardening

Init container (`init-config`) gets explicit `securityContext`: `runAsNonRoot`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, all capabilities dropped, `seccompProfile: RuntimeDefault`.

### Status

```yaml
status:
  gatewayTokenSecretRef: "claw-gateway-token"
  url: "https://..."
  conditions:
    - type: Ready                  # overall instance health
    - type: CredentialsResolved    # all secretRefs valid
    - type: ProxyConfigured        # proxy config generated
```

`Ready=False` until Route hostname resolved and ConfigMap patched with real `allowedOrigins`.

---

## Target Architecture

```
                    ┌──────────────────────────────────────────────────┐
                    │               User's Namespace                   │
                    │                                                  │
  User ──HTTPS──▶ Route ──▶ Claw Gateway (port 18789)                  │
                    │           │                                      │
                    │    ┌──────┴──────┐                               │
                    │    │ Ingress NP: │                               │
                    │    │ router only │                               │
                    │    └──────┬──────┘                               │
                    │           │                                      │
                    │    ┌──────┴──────┐                               │
                    │    │ Egress NP:  │                               │
                    │    │ proxy only  │                               │
                    │    └──┬───────┬──┘                               │
                    │       │       │                                  │
                    │  [Gateway]  [MITM]                               │
                    │      CONNECT (all traffic via HTTP_PROXY)        │
                    │           │                                      │
                    │           ▼                                      │
                    │    MITM Go Credential Proxy (goproxy)            │
                    │    ┌─────────────────────────────────────┐       │
                    │    │ MITM: CONNECT + TLS termination     │       │
                    │    │ Config from spec.credentials[]      │       │
                    │    │ Dynamic operator.json generation    │       │
                    │    │ Strip ALL client auth headers       │       │
                    │    │ Inject real credentials             │       │
                    │    │ Injection failure → 502             │       │
                    │    │ Unknown domains → 403               │       │
                    │    └──────┬──────────────────────────────┘       │
                    │           │                                      │
                    │    ┌──────┴──────┐                               │
                    │    │ Egress NP:  │                               │
                    │    │ TCP 443+DNS │                               │
                    │    └──────┬──────┘                               │
                    │           │                                      │
                    └───────────┼──────────────────────────────────────┘
                                ▼
                    LLM APIs, GitHub, Telegram, etc.


  Resources in namespace:

  Claw (instance)                      ← spec.credentials[] with provider field
  ConfigMap (claw-config)              ← openclaw.json with dynamic providers
  ConfigMap (claw-proxy-config)        ← proxy config JSON (routes, injectors)
  ConfigMap (claw-vertex-adc)          ← stub ADC for Vertex SDK (conditional)
  Secret (claw-gateway-token)          ← auto-generated gateway token
  Secret (claw-proxy-ca)               ← auto-generated MITM CA cert+key
  Secret (llm-keys, gcp-sa, ...)       ← user-created, referenced by credentials
```

---

## Future Considerations

- **`kubernetes` credential type (deferred to Phase 3)** — the assistant acting as a Kubernetes/OpenShift helper is a planned capability. This requires injecting a ServiceAccount token for the Kubernetes API through the proxy, similar to how LLM credentials are injected today. The key architectural constraint: **the Claw instance must not manage the same namespace where it runs.** Granting the assistant broad RBAC in its own namespace would expose the security infrastructure (proxy secrets, CA keys, NetworkPolicies) to tampering. The operator's security stack assumes the assistant cannot access operator-managed resources. Deploying the Claw infrastructure in a separate namespace from the assistant's workspace preserves this invariant.

---

## Out of Scope

- **Multi-tenancy within a single Claw instance** — one instance per user per namespace
- **External CA trust** — proxy's MITM CA is cluster-internal only; corporate proxy CAs handled via OpenShift's `inject-trusted-cabundle`
- **Full agent sandboxing** (gVisor, Kata) — disproportionate for a personal assistant
- **LLM-layer defenses** (prompt injection filters, output sanitization) — upstream OpenClaw concern
