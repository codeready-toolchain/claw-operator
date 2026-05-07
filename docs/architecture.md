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

Objects merge recursively (operator keys win), arrays and primitives from operator replace user values. This means operator-managed sections like `gateway.*` and `models.providers` are always current, while user-owned sections like `plugins.*` and `agents.list` survive restarts.

`channels.*` has split ownership: channels declared with the `channel:` field in the Claw CR are operator-managed (injected into `operator.json` with enabled state and placeholder tokens), while channels configured manually via CLI remain user-owned on the PVC. Because deep-merge is key-level, operator-managed channel entries (e.g., `channels.telegram`) overwrite user values for those keys, but user-managed channels (e.g., `channels.mycustom`) are preserved.

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

**Provider defaults**: known providers (google, anthropic, openai, openrouter, xai) have pre-configured domain and apiKey header defaults. Users only need to specify `provider` and `secretRef` — the operator infers the rest. Explicit values always take precedence as an escape hatch.

**Channel defaults**: known channels (telegram, discord, slack, whatsapp) have pre-configured domain, credential type, companion routes, and placeholder tokens. Users only need to specify `channel` and `secretRef` — the operator infers proxy config and injects channel enablement into `operator.json`. This mirrors the `provider` pattern for LLM credentials. Explicit values always take precedence as an escape hatch.

**Vertex AI path**: credentials with `type: gcp` and a non-google `provider` (e.g., anthropic) use the native Vertex AI SDK rather than gateway routing. The operator creates a stub ADC (Application Default Credentials) ConfigMap so google-auth-library can bootstrap, and the proxy intercepts token refresh requests to vend real tokens.

## Kustomize Manifest Organization

Two Kustomize components under `internal/assets/manifests/`:

- **`claw/`** — Gateway resources: ConfigMap, PVC, Deployment (with init containers), Service, Route, ingress NetworkPolicy
- **`claw-proxy/`** — Proxy resources: Deployment, Service, egress NetworkPolicies (claw→proxy, proxy→internet)

The proxy ConfigMap is intentionally excluded from Kustomize (file prefixed with `_`). It's applied directly by the controller because its content is generated dynamically from resolved credentials, not from a static template.

Both components share the `app.kubernetes.io/name: claw` label applied via their respective `kustomization.yaml` files.

