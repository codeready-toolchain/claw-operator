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

### Option E: Typed credential CRDs (one CRD per shape)

Define a set of CRDs per credential *shape* (not per service): `APIKeyCredential`, `GCPCredential`, `BearerTokenCredential`, etc. Each CRD combines a Secret reference with shape-specific configuration.

- **Pro:** Tight CRD schema validation — each Kind has exactly the fields it needs
- **Pro:** Strong Go type safety — no nil checks on optional sub-structs
- **Con:** New auth shape = new CRD + new types file + `make manifests` + new controller watch + new RBAC entry
- **Con:** Real-world analysis reveals 7 distinct shapes needed (apiKey, bearer, gcp, pathToken, oauth2, kubernetes, none) — that's 7 CRDs to install, watch, and maintain
- **Con:** `NoAuthCredential` is an awkward name for something that holds no credentials (proxy allowlist entry)

### Option F: Unified `ClawCredential` CRD (one CRD, type discriminator)

A single `ClawCredential` CRD (short name `cc`) with a `type` enum field and optional shape-specific config sub-structs. The OpenClaw CR carries no credential fields — the controller discovers `ClawCredential` CRs in the namespace by label.

```yaml
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: ClawCredential
metadata:
  name: gemini
  labels:
    openclaw.sandbox.redhat.com/instance: instance
spec:
  type: apiKey
  secretRef:
    name: llm-keys
    key: GEMINI_API_KEY
  domain: "generativelanguage.googleapis.com"
  apiKey:
    header: "x-goog-api-key"
```

Supported types: `apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `kubernetes`, `none`.

- **Pro:** 1 CRD to install, 1 controller watch, 1 RBAC entry — regardless of how many shapes exist
- **Pro:** Adding a new shape is a new type constant + optional config struct on the existing CRD — no new CRD, no new watch
- **Pro:** `type: none` reads naturally (proxy allowlist entry)
- **Pro:** One `kubectl get credentials` shows all credentials
- **Con:** CRD schema validation is looser — optional sub-structs mean invalid combos are possible (e.g., `type: bearer` with `gcp` set)
- **Con:** Go types need nil checks on optional config blocks

**Precedent:** External Secrets Operator `SecretStore` (one Kind, typed `provider` field), Argo CD `Application`.

See [credential-examples.md](security-hardening-credential-examples.md) for complete YAML examples of every type.

**Decision:** Option F — unified `ClawCredential` CRD with a `type` discriminator. Originally chose Option E (dedicated CRDs per shape), but detailed analysis of real-world credential shapes (LLM providers, channels, MCP servers, Kubernetes API) revealed 7 distinct types needed. The cost-per-new-shape with dedicated CRDs (new Kind, types file, deepcopy, CRD YAML, watch, RBAC) outweighs the tighter schema validation. A single `ClawCredential` CRD keeps the operator simple (1 watch, 1 list call, 1 RBAC entry) and makes adding new shapes a struct-level change rather than a CRD-level one. The distinctive name avoids `kubectl get credentials` collisions with other operators.

_Considered and rejected: Option A — single Secret ref (can't co-locate credential config), Option B — list of typed Secret refs (no config alongside the ref), Option C — per-category fields (rigid, new category = schema change), Option D — single Secret with conventions (fragile naming, mixes API keys with SA JSON), Option E — typed CRDs per shape (7+ CRDs to install/watch/maintain, high cost-per-new-shape)_

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

Today the operator uses an **nginx forward proxy** with static `location` blocks per provider. Each block rewrites one path prefix to one upstream, injecting credentials via `proxy_set_header` from environment variables. This works for simple API-key providers but has limitations:

- **No Vertex AI / OAuth support**: Vertex AI requires short-lived OAuth2 access tokens obtained from a service account, with automatic refresh. Nginx cannot do this natively — it can only inject static values from env vars.
- **Static configuration**: adding a new provider means editing the nginx config template. The set of upstream hosts and auth header formats is baked into the ConfigMap.
- **No credential override guarantee**: nginx sets headers but doesn't strip client-supplied auth headers on the incoming side — a misconfigured or malicious OpenClaw process could potentially include its own credentials.

**How reference projects solve this:**

- **openshift-openclaw (PoC)**: same nginx pattern. Vertex AI partially sketched (SA JSON mounted, env vars set) but no actual Vertex route or OAuth token flow in nginx. Gemini only via static API key.
- **paude-proxy**: Go-based MITM forward proxy (CONNECT + CA-based TLS interception). Declarative JSON config maps env vars → injector types (`bearer`, `api_key`, `gcloud`) → domain patterns. For Google/Vertex, loads ADC (SA JSON), obtains short-lived OAuth2 tokens via Go's `oauth2/google` library with automatic refresh, injects `Authorization: Bearer <real-token>` on `*.googleapis.com`. A token vendor intercepts `POST oauth2.googleapis.com/token` returning dummy responses so the client's Google auth library is satisfied without seeing real credentials. Returns 502 on injection failure. Explicitly overrides all client-supplied auth headers. Supports exact, `.suffix`, and regex domain matching.
- **onecli**: Rust gateway with DB-driven injection rules per host/path. Supports Anthropic (API key or OAuth), generic header injection, and vault-backed credentials.
- **OpenShell**: Go inference router that strips incoming `authorization`/`x-api-key` headers (two-layer: strip at entry, strip again before injection) and re-applies route-specific auth. Supports bearer and custom header injection with default headers per route (e.g., `anthropic-version`). Redacts credentials in debug output. No Vertex/ADC.

**Decision:** Option B (revised after Q12 research) — replace nginx with a purpose-built Go credential-injecting MITM forward proxy. OpenClaw uses standard `HTTP_PROXY`/`HTTPS_PROXY` env vars with real `https://` API URLs. The proxy intercepts CONNECT tunnels, terminates TLS with an operator-managed CA, injects credentials, and forwards as HTTPS to upstream APIs. This is the same proven architecture used by all reference projects (paude-proxy, OpenShell, onecli).

Config-driven domain→injector routing (CONNECT target host determines the route), pluggable injectors (bearer, API key header, GCP ADC/OAuth2 with token vending), explicit auth header stripping on all requests (defense in depth — strip before inject, not just overwrite). Port paude-proxy's credential injection and CONNECT/MITM layer as a starting point rather than building from scratch. Add health endpoint, strict header policy, credential redaction in logs, and 502 on injection failure.

The proxy derives its routing configuration from the credential CRDs (Q2) — the operator reads the CRDs and generates the proxy's config, so adding a new service is purely declarative (new CR instance), not a code or config template change. See [Q12](security-hardening-impl-questions.md) for the full traffic routing decision and rationale.

**Why MITM (revised from original decision):** The original decision avoided CONNECT/MITM ("our inbound side is plain HTTP"), but deeper investigation ([Q12](security-hardening-impl-questions.md)) showed that all three reference projects use MITM because it allows OpenClaw to use real `https://` API URLs unchanged — standard SDK defaults work without patching. The alternatives (`http://` URLs, path-based routing, Host header overrides) are all fragile or tightly coupled. The MITM CA is ~30 LOC of Go (`crypto/x509`) and follows the same Secret management pattern the operator already uses. On OpenShift, `config.openshift.io/inject-trusted-cabundle` handles corporate proxy CAs for the outbound leg.

_Considered and rejected: Option A — nginx + sidecar for OAuth (band-aid, doesn't fix header stripping or static config), Option D — keep nginx and defer (blocks Vertex AI, accumulates technical debt). See [Q12](security-hardening-impl-questions.md) for the rejected non-MITM alternatives (plain HTTP proxy, path-based routing, Host header override)._
