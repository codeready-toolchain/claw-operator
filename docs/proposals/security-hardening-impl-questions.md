# Security Hardening — Implementation Questions

**Status:** All decisions made (Q1–Q12)
**Related:** [Design document](security-hardening-design.md), [Sketch decisions](security-hardening-questions.md)

The [sketch questions](security-hardening-questions.md) resolved the high-level architectural decisions. This document covers the implementation-level questions that remain before coding begins. Go through them one by one to form the design, then update the design document.

---

## Q1: How should the controller discover and react to ClawCredential CR changes?

The operator needs to watch the new `ClawCredential` CRD in addition to the existing `OpenClaw` CRD. When any ClawCredential CR is created, updated, or deleted, the proxy configuration must be regenerated. The design choice affects controller complexity, reconciliation latency, and how many controllers are registered with the manager.

With the unified `ClawCredential` CRD (decided in the sketch), this is simpler than the original multi-CRD design — only one additional watch is needed.

### Option A: Single unified controller with cross-type watch

Keep the existing `OpenClawResourceReconciler` and add a `Watches()` for `ClawCredential` that enqueues the parent `OpenClaw` CR. When any ClawCredential changes, the main reconciler re-discovers all credentials and regenerates everything.

- **Pro:** Stays consistent with the current single-controller architecture
- **Pro:** All resource generation happens in one reconcile loop — easy to reason about ordering
- **Pro:** Only one additional watch needed (unified CRD benefit)
- **Pro:** ClawCredential changes automatically trigger a full reconciliation (proxy config + manifests)
- **Con:** A single credential change triggers reconciliation of *all* resources, not just the proxy config
- **Con:** Requires a mapping function (ClawCredential → OpenClaw CR) for the watch

**Decision:** Option A — single unified controller with one additional `Watches()` for `ClawCredential`. SSA applies are idempotent and cheap, so re-applying all resources on a credential change is harmless. One watch + mapping function is ~20 LOC. Preserves the single-controller simplicity.

_Considered and rejected: Option B — separate credential controller (ordering concerns, coordination complexity for marginal benefit), Option C — hash-based selective reconciliation (unnecessary optimization given SSA idempotency)_

---

## Q2: Where should the Go proxy source code live?

The Go proxy is a separate binary (separate `main` package). It needs its own Containerfile for the container image. The question is whether it lives in this repository or a separate one.

### Option A: Same repo, separate `cmd/` entry point

```
cmd/
  main.go                    ← operator (existing)
  proxy/
    main.go                  ← proxy binary
internal/
  proxy/                     ← proxy logic (injectors, config, server)
  controller/                ← operator controller (existing)
```

- **Pro:** Single repo for the whole system — PRs that change CRD types + proxy behavior + operator logic are atomic
- **Pro:** Shared Go module, shared CI pipeline, shared version
- **Pro:** Proxy can import `api/v1alpha1` types directly for config generation
- **Con:** Two binaries from one repo — the container build needs to produce two images (or a multi-stage build)
- **Con:** Proxy dependencies (e.g., `golang.org/x/oauth2`) are pulled into the operator's `go.mod` even though the operator doesn't use them

**Decision:** Option A — same repo, `cmd/proxy/main.go` + `internal/proxy/`. The proxy is ~800-1000 LOC, not large enough for a separate repo. CRD type changes, proxy config format, and manifests all land in one PR. The dependency cost is minimal (`golang.org/x/oauth2` is already an indirect dep via `k8s.io/client-go`).

_Considered and rejected: Option B — separate repository (cross-repo CRD type changes are painful, two CI pipelines), Option C — Go workspaces (build complexity, unfamiliar to Kubebuilder contributors)_

---

## Q3: What is the proxy configuration format and how is it delivered?

The operator reads credential CRDs and generates a configuration for the Go proxy. This config tells the proxy which domains map to which injectors and which secrets to use. The question is the format, structure, and delivery mechanism.

**Decision:** Option C — JSON ConfigMap with hybrid secret delivery. API keys and tokens use environment variables (simple, small). GCP SA JSON uses a mounted volume (large, structured). The config format indicates which delivery mechanism each route uses.

The operator generates a JSON config describing routes (domain → injector type → header name) and mounts it as a ConfigMap. Example:

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

The current proxy Deployment already uses this hybrid pattern (env vars for API keys, volume mount for `openclaw-gcp-credentials`). The proxy reads env vars at startup for simple credentials and loads the SA JSON file for GCP — both well-established Go patterns. ConfigMap changes trigger a Deployment rollout via annotation hash.

_Considered and rejected: Option A — env vars only (GCP SA JSON as env var is awkward, many env vars at scale), Option B — volume mounts only (operator must aggregate multiple user Secrets into one proxy Secret, adds reconciliation complexity and fsnotify/polling for hot-reload)_

---

## Q4: How is the proxy container image built and published?

The proxy is a Go binary that needs to be packaged as a container image and made available for the proxy Deployment to pull. The operator is distributed via OLM (Operator Lifecycle Manager) using operator-sdk, which affects how operand images like the proxy are packaged and referenced.

**Decision:** Option A — multi-stage Containerfile in this repo, same CI pipeline, with OLM bundle integration.

**Build:** Add `Containerfile.proxy` that builds the proxy binary and packages it into a distroless/static base image. The CI pipeline builds and pushes it alongside the operator image using `podman build`. The Makefile gets a `podman-build-proxy` target matching the existing `podman-build` pattern. Both images are built from every commit to stay in version lockstep.

**OLM packaging:** The proxy image is declared in the ClusterServiceVersion (CSV) in two places:

1. **`spec.relatedImages`** — lists all images the operator needs (operator + proxy) by digest. OLM and downstream tooling (`oc-mirror`) use this to discover and mirror images for disconnected/air-gapped installs. Without the proxy listed here, disconnected installs break.

2. **`PROXY_IMAGE` env var** on the operator Deployment — the operator binary does not hardcode the proxy image reference. Instead, the CSV injects it as an environment variable. The operator reads it at runtime and uses it in the proxy Deployment manifest. This makes the image relocatable across registries.

```yaml
# ClusterServiceVersion (generated by operator-sdk)
spec:
  install:
    spec:
      deployments:
        - name: openclaw-operator
          spec:
            template:
              spec:
                containers:
                  - name: manager
                    image: quay.io/example/openclaw-operator:v0.2.0
                    env:
                      - name: PROXY_IMAGE
                        value: quay.io/example/openclaw-proxy:v0.2.0
  relatedImages:
    - name: operator
      image: quay.io/example/openclaw-operator@sha256:abc...
    - name: proxy
      image: quay.io/example/openclaw-proxy@sha256:def...
```

**CI release flow:**
1. Build operator image → push → get digest
2. Build proxy image → push → get digest
3. `operator-sdk generate bundle` → update CSV with both digests in `relatedImages` + `PROXY_IMAGE` env var
4. Build bundle image → push
5. Update catalog index → push

This is the standard pattern for operators managing operand images (e.g., OpenShift GitOps injects the ArgoCD image the same way).

_Considered and rejected: Option B — separate Containerfile triggered independently (operator and proxy versions can diverge, two CI workflows to maintain, bundle generation must coordinate across repos)_

---

## Q5: How should the existing `spec.apiKey` field be handled during migration?

Today, `OpenClawSpec.APIKey` is a required field. The new `ClawCredential` CRD model has no credential fields on the OpenClaw CR. Changing a required field is a breaking API change.

**Decision:** Option B — clean break. Remove `apiKey` from the CRD spec immediately. Users create a `ClawCredential` CR (type `apiKey`) and a Secret instead. The operator is pre-v1 (`v1alpha1`) with no production users yet — there is no one to migrate. No dual code paths, no synthetic credential logic, no legacy support code. One way to configure credentials, period.

_Considered and rejected: Option A — two-phase deprecation (adds ~20 LOC of synthetic credential code and a dual code path that would exist only to support zero users), Option C — keep `apiKey` as permanent shortcut (contradicts the "no plaintext credentials in CRDs" principle, two ways to do the same thing forever)_

---

## Q6: What status fields and conditions should the OpenClaw CR report?

The current `OpenClawStatus` is empty. The sketch calls for `gatewayTokenSecretRef`. This is an opportunity to add structured observability.

**Decision:** Option B — secret ref + standard conditions.

```go
type OpenClawStatus struct {
    GatewayTokenSecretRef string             `json:"gatewayTokenSecretRef,omitempty"`
    Conditions            []metav1.Condition `json:"conditions,omitempty"`
}
```

Conditions following Kubernetes conventions:
- `Ready` — overall instance health
- `CredentialsResolved` — all ClawCredential CRs reference valid Secrets
- `ProxyConfigured` — proxy ConfigMap generated successfully

Standard Kubernetes pattern — works with `kubectl wait --for=condition=Ready`. `gatewayTokenSecretRef` addresses the UI need. Conditions like `CredentialsResolved` give a clear signal when credentials are misconfigured.

_Considered and rejected: Option A — just `gatewayTokenSecretRef` (no visibility into credential or proxy health), Option C — conditions + per-credential summary (over-engineering, status grows with credential count, can be added later if needed)_

---

## Q7: What labels identify the OpenShift router namespace for the ingress NetworkPolicy?

The ingress NetworkPolicy needs a `namespaceSelector` that matches the namespace where the OpenShift router runs.

**Decision:** Option A + C — use `policy-group.network.openshift.io/ingress: ""` as the namespace selector, with conditional skip on non-OpenShift clusters.

```yaml
namespaceSelector:
  matchLabels:
    policy-group.network.openshift.io/ingress: ""
```

This is the official, documented approach in OpenShift 4.17+ through 4.21. The label is Operator-managed and automatically applied to the `openshift-ingress` namespace — it survives namespace name changes and works across all OpenShift 4.x versions. **Important:** do not apply `network.openshift.io/policy-group: ingress` to custom namespaces — it is reserved for OpenShift networking functions and can cause connectivity issues.

On vanilla Kubernetes, the operator skips this NetworkPolicy based on whether the Route CRD is registered (same pattern already used for Route resources). If there's no Route CRD, there's no OpenShift router to allow ingress from.

_Considered and rejected: Option B — `kubernetes.io/metadata.name: openshift-ingress` (hardcodes namespace name, breaks if router is in a different namespace)_

---

## Q8: How does the operator discover the Route hostname for ConfigMap injection?

The OpenClaw ConfigMap has `"allowedOrigins": ["https://OPENCLAW_ROUTE_HOST"]`. The operator needs to replace this placeholder with the actual Route URL. The Route hostname is either explicitly set in `spec.host` or auto-generated by OpenShift after the Route is created.

**Decision:** Option A — two-pass reconciliation.

1. First pass: apply all resources including the Route (with no `spec.host`, letting OpenShift generate one)
2. Read back the Route to get `status.ingress[0].host`
3. Patch the ConfigMap with the real hostname

Auto-generated hostnames are the norm on Dev Sandbox. The two-pass approach is simple: apply resources, read Route status, patch ConfigMap if the hostname changed. If the Route isn't available yet (non-OpenShift), the ConfigMap keeps the placeholder, which is the current behavior.

**Ready condition gate:** The `Ready` condition (from Q6) must remain `False` until the Route hostname has been resolved and the ConfigMap has been patched with the real value. On first creation, the Route takes a few seconds to be admitted — the reconciler re-queues until `status.ingress[0].host` is populated. Only after the ConfigMap contains the real hostname (and all other resources are applied) does the operator set `Ready=True`. This ensures consumers (UI, health checks) never see a ready instance with a broken CORS `allowedOrigins`.

_Considered and rejected: Option B — compute hostname from cluster domain (assumes specific pattern, fragile with custom routing, requires cluster domain config), Option C — user-specified hostname field (adds CRD field for an operational concern, most Dev Sandbox users rely on auto-generated hostnames)_

---

## Q9: What is the implementation phasing strategy?

The sketch identifies 7 work areas. Some have dependencies; others are independent. The question is how to group them into implementable phases.

**Decision:** Option A (revised) — two phases. Originally three, but Q11 requires `gcp` and `kubernetes` from launch, which need the Go proxy. Phases 2 and 3 merge into a single phase.

**Phase 1** (independent, no API changes):
- Init container security context
- Ingress NetworkPolicy
- Route host injection into ConfigMap
- `gatewayTokenSecretRef` status field + conditions (`Ready`, `CredentialsResolved`, `ProxyConfigured`)

**Phase 2** (ClawCredential CRD + Go proxy + remove legacy):
- Remove `spec.apiKey` from OpenClaw CRD (clean break, Q5)
- Define `ClawCredential` CRD type (unified, with type discriminator)
- ClawCredential discovery in controller (single watch + list)
- Go proxy implementation (`apiKey`, `bearer`, `gcp`, `kubernetes` injectors)
- Container image build pipeline (podman) + OLM bundle integration (Q4)
- Replace nginx manifests
- Remove `applyProxySecret`

**Phase 3** (remaining types + cleanup):
- Add `pathToken`, `oauth2`, `none` injectors to the Go proxy
- RBAC audit — trim operator ClusterRole to least-privilege
- Threat model documentation — application-layer security analysis

Phase 1 delivers immediate security value with minimal risk. Phase 2 is larger than originally planned but ships everything needed for a complete credential management story (no intermediate nginx bridge state). This aligns with the Q5 decision (clean break, no legacy code). Phase 3 adds the remaining credential types and housekeeping.

_Considered and rejected: original three-phase split (Phase 2 without the Go proxy would block Vertex AI and Kubernetes, which are required from launch), Option C — feature flags (dual code paths, premature for pre-v1)_

---

## Q10: How should the unified ClawCredential CRD validate type-specific fields?

The unified `ClawCredential` CRD has optional sub-structs (`apiKey`, `gcp`, `pathToken`, `oauth2`, `kubernetes`) — only one should be set, matching the `type` field. Invalid combinations (e.g., `type: bearer` with a `gcp` block) need to be caught.

**Decision:** Option B — CEL validation rules on the CRD.

Use kubebuilder's CEL validation markers to enforce constraints at the schema level:

```go
// +kubebuilder:validation:XValidation:rule="self.type != 'apiKey' || has(self.apiKey)",message="apiKey config is required when type is apiKey"
// +kubebuilder:validation:XValidation:rule="self.type != 'gcp' || has(self.gcp)",message="gcp config is required when type is gcp"
```

Invalid CRs are rejected at admission time — immediate feedback on `kubectl apply`. No webhook infrastructure needed — CEL runs in the API server. Rules are declarative and live in the CRD schema. Combined with the `type` enum validation that kubebuilder already generates, this catches the most common mistakes at admission time. The controller still validates during reconciliation as defense-in-depth (e.g., checking that the referenced Secret actually exists). Requires Kubernetes 1.25+, available on all supported OpenShift versions.

_Considered and rejected: Option A — controller-level validation only (invalid CRs accepted by API server, user discovers errors only after reconciliation), Option C — validating webhook (certs, service, deployment add significant operational complexity, webhook unavailability blocks all ClawCredential CR operations)_

---

## Q11: Which credential types should be implemented in Phase 2 (initial release)?

The unified `ClawCredential` CRD defines 7 types. The question is which types to support initially.

**Decision:** Ship `apiKey`, `bearer`, `gcp`, and `kubernetes` in Phase 2. These cover the required use cases:

- **`apiKey`** — Gemini, Anthropic, Discord, Jira, MCP servers
- **`bearer`** — OpenAI, OpenRouter, GitHub, Slack, WhatsApp, MCP servers
- **`gcp`** — Vertex AI (requires Go proxy for OAuth2 token refresh + token vending)
- **`kubernetes`** — Kubernetes API access (requires Go proxy for projected SA token reading)

Register all 7 types in the CRD schema from the start (so the API surface is stable), but have the controller log a warning and set a condition for unsupported types (`pathToken`, `oauth2`, `none`). These are added in a later phase.

**Impact on phasing:** Since `gcp` and `kubernetes` require the Go proxy, the original Phase 2 (CRDs only) and Phase 3 (Go proxy) merge into a single phase. See updated Q9.

_Considered and rejected: Option A — `apiKey` and `bearer` only (blocks Vertex AI and Kubernetes, which are required from launch), original Option C — all except `kubernetes` (Kubernetes API access is a core requirement)_

---

## Q12: How does OpenClaw's traffic reach the proxy instead of going to third-party APIs directly?

The proxy needs to intercept OpenClaw's outbound HTTP(S) requests, inspect the target domain, inject credentials, and forward upstream. The routing mechanism determines how OpenClaw's traffic is directed to the proxy and how the proxy knows which upstream to target.

**Research:** Common pattern: `HTTP_PROXY`/`HTTPS_PROXY` environment variables → HTTP CONNECT tunneling → MITM TLS interception with an operator-managed CA. OpenClaw itself supports `request.proxy: { mode: "env-proxy" }` per provider, which activates undici's `EnvHttpProxyAgent`. Node.js native `fetch` does not honor `HTTP_PROXY` by default, but OpenClaw's guarded fetch paths use undici dispatchers explicitly.

**Decision:** Option A — CONNECT + MITM with operator-managed CA. The same proven pattern used by paude-proxy, OpenShell, and onecli.

**Mechanism:**

1. The operator sets `HTTP_PROXY=http://openclaw-proxy-svc:8080` and `HTTPS_PROXY=http://openclaw-proxy-svc:8080` on the OpenClaw Deployment
2. The operator generates a CA cert+key pair, stores it in a Secret (`openclaw-proxy-ca`)
3. The CA cert is mounted into the OpenClaw pod; `NODE_EXTRA_CA_CERTS` points to it (additive — supplements system CAs)
4. OpenClaw uses real `https://` API URLs (e.g., `https://generativelanguage.googleapis.com`) — the standard SDK defaults
5. The HTTP client (undici `EnvHttpProxyAgent`) sends `CONNECT generativelanguage.googleapis.com:443` to the proxy
6. The proxy accepts the CONNECT, generates a leaf cert for the target domain signed by its CA, TLS-terminates the client side
7. On the decrypted stream, the proxy reads the HTTP request, matches the Host header against configured domain patterns, strips auth headers, injects credentials
8. The proxy opens an HTTPS connection to the real upstream and forwards the request
9. NetworkPolicy prevents OpenClaw from reaching the internet directly (even if `HTTP_PROXY` were somehow bypassed)

**CA lifecycle:**
- Generated once at first reconciliation (~30 LOC using Go's `crypto/x509` + `crypto/ecdsa`)
- Stored in Secret `openclaw-proxy-ca` with owner reference
- CA cert+key mounted into proxy pod (for signing leaf certs)
- CA cert only mounted into OpenClaw pod + `NODE_EXTRA_CA_CERTS`
- Long validity (5 years) — cluster-internal only, not externally trusted
- Operator checks expiry during reconciliation, regenerates if nearing expiration
- On OpenShift, the proxy's outbound trust store uses `config.openshift.io/inject-trusted-cabundle` to handle corporate proxy CAs

**OpenClaw configuration changes:**
- `models.providers.*.baseUrl` uses real API URLs (e.g., `https://generativelanguage.googleapis.com/v1beta`) — no path-prefix routing
- `models.providers.*.apiKey` uses placeholder values (proxy replaces them)
- `models.providers.*.request.proxy.mode: "env-proxy"` activates undici's proxy agent for the guarded model fetch paths
- The operator generates this config from ClawCredential CRs

**Why MITM, not plain HTTP:**
- MITM lets OpenClaw use real `https://` URLs — standard SDK defaults work unchanged
- Without MITM, the client would need `http://` URLs (non-standard, some SDKs warn/reject) or path-based routing (tight coupling between proxy config and OpenClaw config)
- The CONNECT target host gives the proxy clean domain-based routing — no path prefixes, no custom headers, no coupling
- Proven by all reference projects; paude specifically ships patches for OpenClaw to work with this pattern

_Considered and rejected: Option B — `HTTP_PROXY` with `http://` URLs (no MITM, but non-standard `http://` URLs for API endpoints, some SDKs may reject, and the `env-proxy` path in OpenClaw is less exercised than standard HTTPS+CONNECT), Option C — path-based routing via base URL override (current PoC model, tight coupling between proxy and OpenClaw config, path prefix management complexity with dynamic credentials), Option D — Host header override (fragile, HTTP clients overwrite Host from URL authority)_
