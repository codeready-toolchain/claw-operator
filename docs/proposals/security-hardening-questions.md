# Security Hardening — Open Questions

**Status:** All decisions made
**Related:** [Sketch document](security-hardening-sketch.md)

---

## Q1: How should the gateway token be managed?

The OpenClaw gateway requires `OPENCLAW_GATEWAY_TOKEN` for authentication. The PoC generated this in a bash script. The operator needs an automated, secure approach. Without this, anyone who can reach the Route can interact with the assistant using the user's namespace permissions.

### Option A: Operator generates a random token and stores it in a Secret

The operator generates a cryptographically random token during reconciliation (if the Secret doesn't exist), stores it in `openclaw-secrets`, and the Deployment mounts it as `OPENCLAW_GATEWAY_TOKEN`. The token is never exposed in the CRD. The UI retrieves it via `kubectl get secret` using the user's kube token.

- **Pro:** Fully automated — zero friction for the user, secure by default
- **Pro:** Token never appears in CRD, audit logs of CR changes, or etcd as a non-Secret resource
- **Con:** User can't easily set a custom token (would need to edit the Secret manually, and the operator would need to not overwrite it)

**Decision:** Option A — automatic generation is the safest default with zero friction. The operator writes the Secret name back to the CR status (e.g., `status.gatewayTokenSecretRef`) so consumers (UI, CLI) can discover where the token lives, then fetch it using the user's kube token. The actual token value never appears in the CRD — only the reference.

_Considered and rejected: Option B — user-provided token in CRD spec (same plaintext-in-CRD problem as apiKey, adds unnecessary friction), Option C — operator generates with manual override (fragile ownership-detection logic, premature flexibility)_

---

## Q2: How should users provide credentials to the operator?

This question is about the **user-facing API** — how credentials get into the cluster and how the CRD references them. How those credentials are consumed by the proxy is a separate concern (see Q7).

Today `OpenClaw.spec.apiKey` is a single plaintext string field on the CRD, managing only Gemini. In reality, the credential surface is much broader:

| Category | Examples | Credential shape |
|----------|----------|-----------------|
| **LLM providers** | Gemini, Anthropic, OpenAI, OpenRouter | API key (header format varies) |
| **Cloud AI** | Vertex AI, AWS Bedrock | Service account JSON / OAuth / SDK auth |
| **Channel integrations** | Telegram bot token, Slack bot/app/signing tokens, Discord token | Varied per channel |
| **Platform APIs** | GitHub token | Bearer token |
| **Future integrations** | Calendar, CRM, internal tools, ... | Unknown |

The design must handle this diversity without requiring CRD schema changes for every new integration. No secret data should ever appear in the CRD.

### Option E: Typed credential CRDs

Define a set of CRDs per credential *shape* (not per service). Each CRD combines a Secret reference with shape-specific configuration. The OpenClaw CR doesn't know about credentials at all — the controller discovers credential CRDs in the same namespace.

**CRDs by credential shape:**

```yaml
# API key credentials (Gemini, Anthropic, OpenAI, OpenRouter, etc.)
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: APIKeyCredential
metadata:
  name: gemini
  labels:
    openclaw.sandbox.redhat.com/instance: instance
spec:
  secretRef:
    name: my-llm-keys
    key: GEMINI_API_KEY
  domain: "generativelanguage.googleapis.com"
  header: "x-goog-api-key"              # or "x-api-key" for Anthropic, etc.
---
# GCP / Vertex AI credentials (service account JSON → OAuth2)
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: GCPCredential
metadata:
  name: vertex-ai
  labels:
    openclaw.sandbox.redhat.com/instance: instance
spec:
  secretRef:
    name: gcp-sa-secret
    key: sa-key.json
  project: my-project
  location: us-central1
  domain: "*.googleapis.com"
---
# Bearer token credentials (GitHub, etc.)
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: BearerTokenCredential
metadata:
  name: github
  labels:
    openclaw.sandbox.redhat.com/instance: instance
spec:
  secretRef:
    name: platform-tokens
    key: GITHUB_TOKEN
  domain: "api.github.com"
```

The OpenClaw CR stays clean — no credential fields at all:
```yaml
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: OpenClaw
metadata:
  name: instance
spec: {}
```

The controller discovers all `APIKeyCredential`, `GCPCredential`, `BearerTokenCredential`, etc. in the namespace (by label) and configures the proxy accordingly.

**Precedent:** Crossplane `ProviderConfig`, cert-manager `Issuer`, External Secrets Operator `SecretStore` — all use per-backend CRDs with Secret ref + typed config.

**Decision:** Option E — typed credential CRDs using the `XYZCredential` naming pattern (singular Kind per Kubernetes convention). CRDs are organized by credential *shape* (e.g., `APIKeyCredential`, `GCPCredential`, `BearerTokenCredential`), not per service. Each CRD combines a Secret reference with shape-specific configuration (domain, header format, project/location, etc.). The OpenClaw CR carries no credential fields — the controller discovers credential CRDs in the namespace by label. Adding a new service with an existing auth shape is data (new CR instance); adding a new auth shape is a new CRD + controller logic.

_Considered and rejected: Option A — single Secret ref (can't co-locate credential config; GCP is a special case), Option B — list of typed Secret refs (no config alongside the ref), Option C — per-category fields (rigid, new category = schema change), Option D — single Secret with conventions (fragile naming, mixes API keys with SA JSON)_

---

## Q3: How should network policies be tightened?

**Already implemented (must keep):**
- **OpenClaw pod egress** — restricted to proxy pod only (TCP 8080) + DNS. This is a key security measure: the OpenClaw process can never reach the internet or the Kubernetes API directly, only through the proxy.
- **Proxy pod egress** — TCP 443 to anywhere + DNS. L7 blocking in the proxy (unknown paths return 403) is the primary control; L4 is intentionally broad because LLM API IPs are dynamic/CDN-hosted.

**Gap:** No ingress restriction on the OpenClaw gateway pod — any in-cluster pod that can route to the Service can reach it.

### Option A: Add ingress NetworkPolicy for the gateway pod

Add an ingress policy allowing traffic to OpenClaw pods only from the OpenShift router (for user access via the Route). Proxy egress stays unchanged — the proxy itself handles L7 allowlisting.

Additional NetworkPolicies managed outside the operator (e.g., by cluster admins or monitoring operators) can open access for other needs like Prometheus scraping, logging agents, etc. Since Kubernetes NetworkPolicies are additive (union of all matching rules), these compose cleanly with the operator's policies.

- **Pro:** Addresses the highest-risk gap — blocks lateral movement from other in-cluster pods
- **Pro:** Pragmatic — doesn't assume specific CNI capabilities
- **Pro:** Composable — cluster-level monitoring/observability policies work alongside without conflict
- **Con:** Ingress policy is OpenShift-specific (router label selectors vary by cluster)

**Decision:** Option A — add ingress NetworkPolicy restricting gateway access to the OpenShift router. Proxy egress stays as-is with L7 blocking as defense-in-depth. Additional access (monitoring, etc.) is handled by separate NetworkPolicies outside the operator's scope.

_Considered and rejected: Option B — restrict proxy egress by IP/CIDR (LLM APIs use dynamic CDN IPs, brittle and high maintenance), Option C — DNS-based egress via Cilium (requires specific CNI, not universally available)_

---

## Q4: Operator RBAC

The operator currently has a `ClusterRole` with broad permissions. This will need a review to ensure least-privilege (only permissions the controller actually uses), but it's not a design question — it's a housekeeping task to do once the new credential CRDs and proxy architecture are implemented, since those changes will alter the required permission set.

**Decision:** Deferred. Review and trim operator RBAC after Q2 (credential CRDs) and Q7 (proxy architecture) are implemented.

---

## Q5: Which container/supply-chain hardening items should we prioritize?

Several minor gaps exist: init container security context, image digest pinning, Route TLS settings, and the `OPENCLAW_ROUTE_HOST` placeholder.

**Decision:** Two quick wins:

1. **Init container security context** — the busybox init container (`init-config`) has no explicit `securityContext`, relying on image defaults. The main container is fully hardened (non-root, read-only root FS, drop all caps, seccomp RuntimeDefault). The init container needs the same — required for Pod Security Admission restricted profile compliance.

2. **Route host placeholder** — the ConfigMap has `"allowedOrigins": ["https://OPENCLAW_ROUTE_HOST"]` which is never substituted with the real hostname, breaking CORS protection. The operator creates the Route and knows the hostname — it should inject it into the ConfigMap during reconciliation.

_Deferred: image digest pinning (good practice but needs CI automation first), Route TLS version/cipher config (OpenShift router enforces cluster-wide defaults)_

---

## Q6: Should the operator manage any application-layer security controls?

OpenClaw as an AI agent can interact with Kubernetes on behalf of the user. Prompt injection could cause it to take unexpected actions. This is mostly an upstream concern, but the operator controls the deployment environment.

**Decision:** Document the threat model and recommended mitigations. Investigate how OpenClaw accesses Kubernetes (ServiceAccount? user's token?) and whether RBAC scoping for the assistant is feasible — implement if it doesn't break core use cases.

_Considered and rejected: Option A — fully out of scope (users expect a "secure deployment" to address this), Option B — restrict Kubernetes permissions now (unclear how OpenClaw accesses the API, needs investigation first)_

---

## Q7: What proxy architecture should inject credentials into LLM requests?

This question is about the **credential injection mechanism** — how user-provided secrets (from Q2) are actually used when OpenClaw calls LLM APIs. This is separate from how users provide the secrets.

Today the operator uses an **nginx reverse proxy** with static `location` blocks per provider. Each block rewrites one path prefix to one upstream, injecting credentials via `proxy_set_header` from environment variables. This works for simple API-key providers but has limitations:

- **No Vertex AI / OAuth support**: Vertex AI requires short-lived OAuth2 access tokens obtained from a service account, with automatic refresh. Nginx cannot do this natively — it can only inject static values from env vars.
- **Static configuration**: adding a new provider means editing the nginx config template. The set of upstream hosts and auth header formats is baked into the ConfigMap.
- **No credential override guarantee**: nginx sets headers but doesn't strip client-supplied auth headers on the incoming side — a misconfigured or malicious OpenClaw process could potentially include its own credentials.

**How reference projects solve this:**

- **openshift-openclaw (PoC)**: same nginx pattern. Vertex AI partially sketched (SA JSON mounted, env vars set) but no actual Vertex route or OAuth token flow in nginx. Gemini only via static API key.
- **paude-proxy**: Go-based MITM forward proxy. Declarative JSON config maps env vars → injector types (`bearer`, `api_key`, `gcloud`) → domain patterns. For Google/Vertex, loads ADC (SA JSON), obtains short-lived OAuth2 tokens via Go's `oauth2/google` library with automatic refresh, injects `Authorization: Bearer <real-token>` on `*.googleapis.com`. A token vendor intercepts `POST oauth2.googleapis.com/token` returning dummy responses so the client's Google auth library is satisfied without seeing real credentials. Explicitly overrides all client-supplied auth headers.
- **onecli**: Rust gateway with DB-driven injection rules per host/path. Supports Anthropic (API key or OAuth), generic header injection, and vault-backed credentials.
- **OpenShell**: Go inference router that strips incoming `authorization`/`x-api-key` headers and re-applies route-specific auth. Supports bearer and custom header injection. No Vertex/ADC.

**Decision:** Option B — replace nginx with a purpose-built Go reverse proxy. Config-driven domain→injector routing, pluggable injectors (bearer, API key header, GCP ADC/OAuth2), explicit auth header stripping on all requests. Port paude-proxy's credential injection layer (~500 LOC, MIT-licensed, cleanly self-contained) as a starting point rather than building from scratch. Add health endpoint and strict header policy. Estimated ~800-1000 LOC total for a focused component that fits the operator's Go tech stack.

The proxy derives its routing configuration from the credential CRDs (Q2) — the operator reads the CRDs and generates the proxy's config, so adding a new service is purely declarative (new CR instance), not a code or config template change.

_Considered and rejected: Option A — nginx + sidecar for OAuth (band-aid, doesn't fix header stripping or static config), Option C — fork paude-proxy (its MITM/CONNECT/CA machinery is ~850 LOC we don't need; forking to delete half the codebase is worse than porting the valuable 500 LOC), Option D — keep nginx and defer (blocks Vertex AI, accumulates technical debt)_
