# Architecture Reference

Detailed architecture for the claw-operator. For a concise overview, see [CLAUDE.md](../CLAUDE.md).

## Three-Phase Reconciliation

The controller uses three phases because the Route host (populated asynchronously by the OpenShift router) must be injected into the gateway ConfigMap for CORS configuration. Applying everything in one pass would leave the ConfigMap with a placeholder value.

**Phase 1 — Gateway Secret + Credentials + Proxy Config**: Generate the gateway authentication token (preserved across reconciles), validate all credentials and referenced Secrets, build the proxy configuration, and apply proxy-specific resources (proxy ConfigMap, Vertex ADC ConfigMap, sanitized kubeconfig ConfigMap, proxy CA Secret). These are controller-managed resources applied directly, not through Kustomize.

**Phase 2 — Route + Host Resolution**: Apply only the Route resource from the Kustomize output, then read back its `.status.ingress[0].host`. If the router hasn't populated the host yet, requeue with 5s backoff. On vanilla Kubernetes (no Route CRD), fall back to `http://localhost:18789`.

**Phase 3 — ConfigMap Injection + Remaining Resources**: With the Route host now known, inject it (plus providers, model catalog, channel config, kubernetes skill, network policy ports, config hash) into the in-memory Kustomize objects, then server-side apply everything except the Route (already applied) and proxy ConfigMap (controller-managed).

This ordering ensures the gateway ConfigMap always has the correct CORS origin on first apply.

## Config Deep-Merge Design

The operator needs to manage certain config keys (gateway settings, CORS, providers, model catalog) while letting users keep their own customizations (plugins, channels, agent configs, cron). Rather than using config file includes or layered config, the operator uses a deep-merge at init time:

1. `operator.json` in the ConfigMap holds operator-managed settings — rewritten every reconcile
2. `openclaw.json` on the PVC holds the live config — modified by users and by OpenClaw itself
3. On pod start, `init-config` runs `merge.js` which deep-merges operator keys into the PVC config

Objects merge recursively (operator keys win), arrays and primitives from operator replace user values. This means operator-managed sections like `gateway.*` and `models.providers` are always current, while user-owned sections like `agents.list` and `tools.*` survive restarts. `plugins.*` and `channels.*` have split ownership — declared entries (from `channel:` credentials) are operator-managed, while everything else is user-owned.

Because deep-merge operates at the key level, operator-managed entries (e.g., `channels.telegram`, `plugins.entries.telegram`) overwrite user values for those specific keys, while user-managed entries (e.g., `channels.mycustom`) are preserved across restarts.

**Config ownership summary (merge mode):**

| Owner | Sections | Restart behavior |
|---|---|---|
| Operator | `gateway.*`, `models.providers`, `agents.defaults.models`, `channels.<declared>`, `plugins.entries.<declared>` | Overwritten every restart |
| Operator → User | `agents.defaults.model.primary` | Set on first run, then preserved |
| User | `agents.list`, `plugins.*` (non-declared), `channels.*` (non-declared), `tools.*`, `cron.*` | Preserved across restarts |

In `overwrite` mode, the PVC config is ignored and `operator.json` is merged into the seed `openclaw.json` from the ConfigMap. User edits are wiped on every restart.

## Multi-Instance Support

Resource names in the embedded Kustomize manifests use a `CLAW_INSTANCE_NAME` placeholder. At build time, the controller replaces this with the Claw CR name, so multiple Claw instances in the same namespace get distinct resource names. Instance labels (`claw.sandbox.redhat.com/instance`) are injected into all resource metadata, Deployment selectors, Service selectors, and NetworkPolicy selectors to ensure isolation between instances.

## Credential System Design

The proxy sits between the gateway and external APIs, injecting credentials into requests transparently. This design means the gateway never sees raw API keys — it talks to the proxy via HTTP_PROXY, and the proxy does TLS interception (MITM) to add auth headers.

**Why two CONNECT modes?** Most domains need MITM for credential injection, path filtering, or header injection. But some protocols (WhatsApp Noise handshake, certain WebSocket tunnels) break under TLS interception. Domains with `type: none` and no path/header restrictions use a direct CONNECT tunnel instead.

**Provider defaults**: The `knownProviders` registry in `claw_providers.go` centralizes all per-provider knowledge: credential defaults (domain, header), wire format API identifier, Vertex AI SDK config, companion relationships, and model catalog. For `google` and `anthropic`, users only need `provider` and `secretRef` — the operator infers domain and apiKey header. For `openai` and `xai`, domain must be explicit (they use `type: bearer`). Providers not in the registry (e.g., `openrouter`, custom providers) still work but require fully explicit configuration. Explicit values always take precedence as an escape hatch.

**Wire format API**: Each provider in `knownProviders` declares the OpenClaw wire format API it requires (e.g., `google-generative-ai` for Google, `anthropic-messages` for Anthropic, `openai-codex-responses` for the internal openai-codex companion). `buildProviderEntry()` bakes the correct `api` field into every `models.providers` entry at generation time. Providers using the OpenClaw default (`openai-completions`) — like `openai`, `xai`, and unknown providers — omit the field entirely.

**Channel defaults**: known channels (telegram, discord, slack, whatsapp) have pre-configured domain, credential type, companion routes, and placeholder tokens. Users only need to specify `channel` and `secretRef` — the operator infers proxy config and injects channel enablement into `operator.json`. This mirrors the `provider` pattern for LLM credentials. Explicit values always take precedence as an escape hatch.

**Vertex AI path**: credentials with `type: gcp` and a non-google `provider` (e.g., anthropic) use the native Vertex AI SDK rather than gateway routing. The operator creates a stub ADC (Application Default Credentials) ConfigMap so google-auth-library can bootstrap, and the proxy intercepts token refresh requests to vend real tokens. Providers that require an external OpenClaw plugin for the Vertex SDK path (declared via `VertexPlugin` in `knownProviders`) are auto-installed — `effectivePlugins()` merges these implicit plugins with explicit `spec.plugins` entries, deduplicating where both are declared.

**Companion providers**: OpenClaw sometimes routes models through internal provider names that differ from the user-facing provider. For example, GPT-5.x models (gpt-5.5, gpt-5.4, gpt-5.4-mini) are routed through an internal `openai-codex` provider rather than `openai`. The `knownProviders` registry in `claw_providers.go` declares these relationships via the `Companions` field: when `injectProviders` creates a provider entry, it also creates entries for any companions with the same `baseUrl` and placeholder API key (using `buildProviderEntry` to ensure the correct wire format API is set). This ensures the proxy handles credential injection for all models without requiring users to configure additional providers. Companion keys are checked for collisions with the same error path as primary providers.

## NetworkPolicy and Egress Rules

The operator creates three NetworkPolicies per instance: `{instance}-ingress` (gateway ingress from OpenShift routers), `{instance}-egress` (gateway egress to proxy + DNS), and `{instance}-proxy-egress` (proxy egress to HTTPS + DNS). These enforce a defense-in-depth posture where the gateway can only reach external services through the proxy.

**MCP auto-egress:** When `spec.mcpServers` declares HTTP MCP server URLs, the operator classifies each URL as in-cluster or external using Kubernetes DNS heuristics (bare hostname → same namespace, `svc.ns` or `svc.ns.svc.cluster.local` → cross namespace, anything else → external). When `spec.network.inClusterBypass` is `false` (default), in-cluster targets get egress rules on `{instance}-proxy-egress` (the proxy reaches them on behalf of the gateway). When `inClusterBypass` is `true`, in-cluster targets get gateway egress rules appended to `{instance}-egress` (podSelector for same-namespace, namespaceSelector with `kubernetes.io/metadata.name` for cross-namespace). External non-443 ports get added to the proxy egress rule in `{instance}-proxy-egress`.

**Escape hatch:** `spec.network.additionalEgress` appends raw `NetworkPolicyEgressRule` objects to `{instance}-egress` for anything the operator can't auto-detect (tracing, databases, webhooks).

**Kube API ports:** `injectKubePortsIntoNetworkPolicy` adds non-443 ports from kubernetes credentials to proxy egress, allowing the proxy to reach API servers on non-standard ports (e.g., 6443).

**Metrics ingress:** When metrics are enabled, `addMetricsIngressRule` opens the ingress NP for Prometheus scraping from OpenShift monitoring namespaces.

All NP mutations follow the same pattern: find the NP by kind+name in the `[]*unstructured.Unstructured` slice, read with `NestedSlice`, append rules, write back with `SetNestedSlice`. Rules are deterministically sorted to avoid unnecessary NP churn across reconcile loops.

## Kustomize Manifest Organization

Two Kustomize components under `internal/assets/manifests/`:

- **`claw/`** — Gateway resources: ConfigMap, PVC, Deployment (with init containers), Service, Route, ingress NetworkPolicy
- **`claw-proxy/`** — Proxy resources: Deployment, Service, egress NetworkPolicies (claw→proxy, proxy→internet)

The proxy ConfigMap is intentionally excluded from Kustomize (file prefixed with `_`). It's applied directly by the controller because its content is generated dynamically from resolved credentials, not from a static template.

Both components share the `app.kubernetes.io/name: claw` label applied via their respective `kustomization.yaml` files.

## Deployment Apply Strategy (CreateOrUpdate)

Deployments are applied via `controllerutil.CreateOrUpdate` rather than server-side apply (SSA). SSA with `Force: true` causes `generation` to increment on every patch — even when the desired state is identical — due to field-ownership transfers between `claw-operator` (Apply) and `kube-controller-manager` (Update). For the gateway Deployment (strategy: Recreate), this triggered unnecessary pod-killing rollouts; for RollingUpdate Deployments, it caused continuous reconciliation loops.

`CreateOrUpdate` avoids this by comparing before/after state with `DeepEqual` and skipping the Update when nothing changed. All other resources (ConfigMaps, Services, NetworkPolicies, Routes) continue to use SSA, which works correctly for resources without field-ownership conflicts.

A `NormalizeDeployment()` function pre-applies Kubernetes admission defaults (strategy fields, `terminationGracePeriodSeconds`, container `imagePullPolicy`, probe thresholds, etc.) onto the desired spec before comparison. Without this, the operator's desired spec (missing defaulted fields) would always differ from the API server's stored spec (with defaults populated), causing an Update on every reconcile.

The Kustomize-rendered unstructured Deployment is converted to a typed `*appsv1.Deployment` via `runtime.DefaultUnstructuredConverter.FromUnstructured()` at the apply boundary.

See [ADR-0018](adr/0018-centralized-provider-registry.md) for the full decision record.

