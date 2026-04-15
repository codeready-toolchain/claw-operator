# Security Hardening for Claw Operator

**Status:** Sketch complete — ready for detailed design

## Problem

The Claw Operator deploys a personal AI assistant into a user's Kubernetes namespace. Today, credential isolation (nginx proxy), egress NetworkPolicies, gateway token authentication, and Route host injection are in place. Several security gaps remain: the CRD carries a single hardcoded Gemini Secret reference (`spec.geminiAPIKey`), ingress is unrestricted, and the proxy can't handle OAuth-based providers like Vertex AI.

**CRD rename:** The CRD Kind is renamed from `OpenClaw` to `Claw` to support potential future distributions (e.g., NemoClaw). The API group remains `claw.sandbox.redhat.com/v1alpha1`.

On Dev Sandbox, each OpenClaw instance runs in a user's namespace with their permissions. A security failure here means either leaked LLM credentials (cost and privacy), unauthorized access to the assistant (abuse of the user's namespace permissions), or lateral movement from a compromised pod.

## Current Security Posture (must keep)

- **Gateway pod egress locked to proxy only** — the gateway process can never reach the internet or the Kubernetes API directly, only through the proxy (TCP 8080) + DNS. This is a cornerstone security measure.
- Credential isolation via forward proxy (the gateway never sees raw API keys)
- Proxy pod egress limited to TCP 443 + DNS; proxy L7 blocks unknown paths (returns 403)
- Pod hardening: non-root (uid 65532), `readOnlyRootFilesystem`, `seccompProfile: RuntimeDefault`, all capabilities dropped, `automountServiceAccountToken: false`
- Edge TLS on the Route with HTTP-to-HTTPS redirect
- Server-side apply with field ownership tracking
- **Gateway token authentication** — auto-generated cryptographic token in `openclaw-gateway-token`, injected into gateway Deployment
- **Route host injection** — two-pass reconciliation injects real Route hostname into ConfigMap `allowedOrigins`
- **Secret-based credential reference** — `spec.geminiAPIKey` is a `SecretRef` (not plaintext), proxy Deployment patched with `secretKeyRef`

## Decisions

All questions resolved — see [security-hardening-questions.md](security-hardening-questions.md) for full context, options considered, and rationale.

### 1. Gateway Authentication (Q1) — already implemented

The operator already auto-generates a cryptographically random gateway token during reconciliation via `applyGatewaySecret()`, stores it in a Secret (`openclaw-gateway-token`), and injects it as `OPENCLAW_GATEWAY_TOKEN` into the gateway Deployment. The full URL (including token) is available via `status.url`. Remaining: add `status.gatewayTokenSecretRef` so the UI can discover the Secret by name.

### 2. Credential Provisioning (Q2)

Credentials are an inline `spec.credentials[]` array on the `Claw` CRD with a `type` discriminator field, organized by credential *shape*:

- **`apiKey`** — Gemini, Anthropic, Discord, Jira, MCP servers. Secret ref + domain + header name + optional value prefix.
- **`bearer`** — OpenAI, OpenRouter, GitHub, Slack, WhatsApp, MCP servers. Secret ref + domain.
- **`gcp`** — Vertex AI, GCP APIs. Secret ref (SA JSON) + project + location + domain.
- **`pathToken`** — Telegram. Secret ref + domain + URL path prefix.
- **`oauth2`** — Enterprise MCP servers, corporate APIs. Secret ref (client secret) + domain + token URL + scopes.
- **`kubernetes`** — Kubernetes API access. ServiceAccount name + domain (kube-apiserver).
- **`none`** — Internal/cluster services (proxy allowlist only). Domain only.

Adding a new service with an existing auth shape is a new entry in `spec.credentials` — purely declarative. Adding a new auth shape is a new type constant + optional config struct on the existing types — no new CRD needed.

See [credential-examples.md](security-hardening-credential-examples.md) for complete YAML examples of each type.

### 3. Credential Injection — Hybrid Go Proxy (Q7, Q12)

Replace the nginx proxy with a purpose-built Go credential-injecting proxy operating in **two modes**, built on `github.com/elazarl/goproxy`:

1. **Gateway mode (API gateway):** For LLM provider calls where the credential has `provider` set. OpenClaw is configured with `baseUrl` pointing to `http://claw-proxy:8080/<cred-name>/...`. The proxy strips the path prefix, injects credentials, and forwards to the upstream LLM API over HTTPS. This avoids the Google ADK JS SDK `thought_signature` bug that occurs with MITM/`env-proxy` on streamed responses.

2. **MITM forward proxy mode:** For general egress traffic (web_fetch, npm, OTEL, etc.). The gateway uses `HTTP_PROXY`/`HTTPS_PROXY` env vars. The proxy intercepts CONNECT tunnels, terminates TLS with an operator-managed CA, injects credentials for allowed domains, and forwards as HTTPS.

The operator manages the CA lifecycle: generates cert+key at first reconciliation, stores in a Secret, mounts into both pods. On OpenShift, the proxy's outbound trust store uses `config.openshift.io/inject-trusted-cabundle` for corporate proxy CA support.

```
# Gateway mode (LLM providers with provider set):
Claw Gateway ──HTTP──▶ Go Proxy (strip prefix, inject creds) ──HTTPS──▶ LLM APIs
  baseUrl: http://claw-proxy:8080/gemini/v1beta

# MITM forward proxy mode (general egress):
Claw Gateway ──CONNECT host:443──▶ Go Proxy (MITM) ──HTTPS──▶ External Services
  HTTP_PROXY=http://claw-proxy:8080
                      │
                      ├─ Built on github.com/elazarl/goproxy
                      ├─ Gateway mode: path-prefix routing for LLM providers
                      ├─ MITM mode: CONNECT + TLS termination (operator-managed CA)
                      ├─ Config derived from spec.credentials[] (with provider field)
                      ├─ Dynamic openclaw.json generation from credentials with provider set
                      ├─ Pluggable injectors: apiKey, bearer, gcp, pathToken, oauth2, none
                      ├─ Strips ALL client auth headers before injection (defense in depth)
                      ├─ Default headers per credential (e.g., anthropic-version)
                      ├─ GCP token vending — intercepts oauth2 token requests for SDK compat
                      ├─ Injection failure → 502 (clear signal, not passthrough)
                      ├─ Credential values redacted in all log output
                      ├─ Domain matching: exact ("api.github.com") or suffix (".googleapis.com")
                      ├─ NO_PROXY=localhost,127.0.0.1,claw-proxy (prevents circular proxying)
                      ├─ Health endpoint (/healthz)
                      └─ Unknown/disallowed domains → 403
```

The proxy derives its routing configuration from `spec.credentials` — the operator reads the credential entries and generates both the proxy's config JSON and the OpenClaw `openclaw.json` dynamically.

### 4. Network Isolation (Q3)

Add an **ingress NetworkPolicy** restricting gateway access to the OpenShift router only. The existing egress policies (gateway→proxy, proxy→443) remain unchanged. Additional access for monitoring, logging, etc. is handled by separate NetworkPolicies managed outside the operator — Kubernetes NetworkPolicies are additive (union of all matching rules), so they compose cleanly.

### 5. Container Hardening (Q5)

One remaining quick win:
- **Init container security context** — add explicit `securityContext` matching the main container (required for Pod Security Admission restricted profile)
- ~~**Route host placeholder**~~ — already implemented (`injectRouteHostIntoConfigMap` with two-pass reconciliation)

Deferred: image digest pinning (needs CI automation), Route TLS config (cluster-level concern on OpenShift).

### 6. Operator RBAC (Q4)

Deferred until inline credentials and proxy changes are implemented, since those alter the required permission set. Review and trim to least-privilege at that point.

### 7. Application-Layer Security (Q6)

Document the threat model (prompt injection, excessive agency) and recommended mitigations. Investigate how OpenClaw accesses Kubernetes (ServiceAccount? user's token?) and whether RBAC scoping for the assistant is feasible.

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
                    │   baseUrl    CONNECT                             │
                    │       │       │                                  │
                    │       ▼       ▼                                  │
                    │    Hybrid Go Credential Proxy (goproxy)          │
                    │    ┌────────────────────────────────────┐        │
                    │    │ Gateway mode: path-prefix routing  │        │
                    │    │   for LLM providers (provider set) │        │
                    │    │ MITM mode: CONNECT + TLS termination│       │
                    │    │   for general egress (HTTP_PROXY)  │        │
                    │    │ Config from spec.credentials[]     │        │
                    │    │ Dynamic openclaw.json generation   │        │
                    │    │ Strip ALL client auth headers      │        │
                    │    │ Inject real credentials            │        │
                    │    │ Default headers per provider       │        │
                    │    │ Injection failure → 502            │        │
                    │    │ GCP token vending endpoint         │        │
                    │    │ Credential redaction in logs       │        │
                    │    │ Unknown domains → 403              │        │
                    │    └──────┬─────────────────────────────┘        │
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

  Claw (instance)                      ← spec.credentials[] with provider field for gateway routing
  ConfigMap (claw-config)              ← openclaw.json with dynamically generated providers
  ConfigMap (claw-proxy-config)        ← proxy config JSON (routes, injectors, gateway/MITM)
  Secret (claw-gateway-token)          ← auto-generated gateway token
  Secret (claw-proxy-ca)               ← auto-generated MITM CA cert+key (operator-managed)
  Secret (llm-keys)                    ← user-created, referenced by spec.credentials entries
  Secret (gcp-sa)                      ← user-created, referenced by spec.credentials entries
```

## Out of Scope

- **Multi-tenancy within a single Claw instance** — one instance per user per namespace
- **External CA trust** — the proxy's MITM CA is cluster-internal only, never externally trusted; corporate proxy CAs are handled via OpenShift's `inject-trusted-cabundle` annotation
- **Full agent sandboxing** (gVisor, Kata, custom seccomp) — disproportionate for a personal assistant
- **LLM-layer defenses** (prompt injection filters, output sanitization) — upstream OpenClaw concern
