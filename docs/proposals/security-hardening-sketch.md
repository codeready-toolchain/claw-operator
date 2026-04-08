# Security Hardening for OpenClaw Operator

**Status:** Sketch complete — ready for detailed design

## Problem

The OpenClaw operator deploys a personal AI assistant with access to LLM APIs and the user's Kubernetes namespace. Today, credential isolation (nginx proxy) and egress NetworkPolicies are in place, but several security gaps remain: the gateway has no managed authentication, the CRD carries a plaintext API key, ingress is unrestricted, and the proxy can't handle OAuth-based providers like Vertex AI.

On Dev Sandbox, each OpenClaw instance runs in a user's namespace with their permissions. A security failure here means either leaked LLM credentials (cost and privacy), unauthorized access to the assistant (abuse of the user's namespace permissions), or lateral movement from a compromised pod.

## Current Security Posture (must keep)

- **OpenClaw pod egress locked to proxy only** — the OpenClaw process can never reach the internet or the Kubernetes API directly, only through the proxy (TCP 8080) + DNS. This is a cornerstone security measure.
- Credential isolation via forward proxy (OpenClaw never sees raw API keys)
- Proxy pod egress limited to TCP 443 + DNS; proxy L7 blocks unknown paths (returns 403)
- Pod hardening: non-root (uid 65532), `readOnlyRootFilesystem`, `seccompProfile: RuntimeDefault`, all capabilities dropped, `automountServiceAccountToken: false`
- Edge TLS on the Route with HTTP-to-HTTPS redirect
- Server-side apply with field ownership tracking

## Decisions

All questions resolved — see [security-hardening-questions.md](security-hardening-questions.md) for full context, options considered, and rationale.

### 1. Gateway Authentication (Q1)

The operator auto-generates a cryptographically random gateway token during reconciliation, stores it in a Secret (`openclaw-secrets`), and injects it as `OPENCLAW_GATEWAY_TOKEN` into the gateway Deployment. The Secret name is written to the CR status (`status.gatewayTokenSecretRef`) so the UI can discover and retrieve it using the user's kube token.

### 2. Credential Provisioning (Q2)

A single unified `ClawCredential` CRD (short name `cc`) with a `type` discriminator field, organized by credential *shape*:

- **`apiKey`** — Gemini, Anthropic, Discord, Jira, MCP servers. Secret ref + domain + header name + optional value prefix.
- **`bearer`** — OpenAI, OpenRouter, GitHub, Slack, WhatsApp, MCP servers. Secret ref + domain.
- **`gcp`** — Vertex AI, GCP APIs. Secret ref (SA JSON) + project + location + domain.
- **`pathToken`** — Telegram. Secret ref + domain + URL path prefix.
- **`oauth2`** — Enterprise MCP servers, corporate APIs. Secret ref (client secret) + domain + token URL + scopes.
- **`kubernetes`** — Kubernetes API access. ServiceAccount name + domain (kube-apiserver).
- **`none`** — Internal/cluster services (proxy allowlist only). Domain only.

The OpenClaw CR carries no credential fields. The controller discovers `ClawCredential` CRs in the namespace by label (`openclaw.sandbox.redhat.com/instance: instance`). Adding a new service with an existing auth shape is purely declarative (new CR instance). Adding a new auth shape is a new type constant + optional config struct on the existing CRD — no new CRD or controller watch needed.

See [credential-examples.md](security-hardening-credential-examples.md) for complete YAML examples of each type.

### 3. Credential Injection — Go MITM Proxy (Q7, Q12)

Replace the nginx proxy with a purpose-built Go credential-injecting MITM forward proxy. OpenClaw uses standard `HTTP_PROXY`/`HTTPS_PROXY` environment variables with real `https://` API URLs. The proxy intercepts CONNECT tunnels, terminates TLS with an operator-managed CA, injects credentials based on the target domain, and forwards as HTTPS to upstream APIs. This is the same proven pattern used by all reference projects (paude-proxy, OpenShell, onecli). The operator manages the CA lifecycle: generates cert+key at first reconciliation, stores in a Secret, mounts into both pods. On OpenShift, the proxy's outbound trust store uses `config.openshift.io/inject-trusted-cabundle` for corporate proxy CA support.

```
OpenClaw ──CONNECT host:443──▶ Go Proxy (MITM) ──HTTPS──▶ LLM APIs / External Services
                      │
                      ├─ CONNECT + MITM (operator-managed CA, leaf certs per domain)
                      ├─ HTTP_PROXY/HTTPS_PROXY env vars on OpenClaw (real https:// URLs)
                      ├─ Config derived from credential CRDs
                      ├─ Domain → injector routing (CONNECT target host)
                      ├─ Pluggable injectors: apiKey, bearer, gcp, pathToken, oauth2, kubernetes, none
                      ├─ Strips ALL client auth headers before injection (defense in depth)
                      ├─ Default headers per credential (e.g., anthropic-version)
                      ├─ GCP token vending — intercepts oauth2 token requests for SDK compat
                      ├─ Injection failure → 502 (clear signal, not passthrough)
                      ├─ Credential values redacted in all log output
                      ├─ Domain matching: exact ("api.github.com") or suffix (".googleapis.com")
                      ├─ Health endpoint (/healthz)
                      └─ Unknown/disallowed domains → 403
```

Port paude-proxy's credential injection layer (~500 LOC, MIT-licensed) as a starting point. The proxy derives its routing configuration from the credential CRDs — the operator reads them and generates the proxy's config. Estimated ~800-1000 LOC for the full component.

### 4. Network Isolation (Q3)

Add an **ingress NetworkPolicy** restricting gateway access to the OpenShift router only. The existing egress policies (OpenClaw→proxy, proxy→443) remain unchanged. Additional access for monitoring, logging, etc. is handled by separate NetworkPolicies managed outside the operator — Kubernetes NetworkPolicies are additive (union of all matching rules), so they compose cleanly.

### 5. Container Hardening (Q5)

Two quick wins:
- **Init container security context** — add explicit `securityContext` matching the main container (required for Pod Security Admission restricted profile)
- **Route host placeholder** — operator injects the real Route hostname into the ConfigMap during reconciliation, fixing broken CORS `allowedOrigins`

Deferred: image digest pinning (needs CI automation), Route TLS config (cluster-level concern on OpenShift).

### 6. Operator RBAC (Q4)

Deferred until credential CRDs and proxy changes are implemented, since those alter the required permission set. Review and trim to least-privilege at that point.

### 7. Application-Layer Security (Q6)

Document the threat model (prompt injection, excessive agency) and recommended mitigations. Investigate how OpenClaw accesses Kubernetes (ServiceAccount? user's token?) and whether RBAC scoping for the assistant is feasible.

## Target Architecture

```
                    ┌─────────────────────────────────────────────┐
                    │               User's Namespace              │
                    │                                             │
  User ──HTTPS──▶ Route ──▶ OpenClaw Gateway (port 18789)         │
                    │           │                                 │
                    │    ┌──────┴──────┐                          │
                    │    │ Ingress NP: │                          │
                    │    │ router only │                          │
                    │    └──────┬──────┘                          │
                    │           │                                 │
                    │    ┌──────┴──────┐                          │
                    │    │ Egress NP:  │                          │
                    │    │ proxy only  │                          │
                    │    └──────┬──────┘                          │
                    │           ▼                                 │
                    │    Go MITM Credential Proxy (operator CA)   │
                    │    ┌──────────────────────────────┐         │
                    │    │ CONNECT + TLS termination    │         │
                    │    │ Config from credential CRDs  │         │
                    │    │ Strip ALL client auth headers│         │
                    │    │ Inject real credentials      │         │
                    │    │ Default headers per provider │         │
                    │    │ Injection failure → 502      │         │
                    │    │ GCP token vending endpoint   │         │
                    │    │ Credential redaction in logs │         │
                    │    │ Unknown domains → 403        │         │
                    │    └──────┬───────────────────────┘         │
                    │           │                                 │
                    │    ┌──────┴──────┐                          │
                    │    │ Egress NP:  │                          │
                    │    │ TCP 443+DNS │                          │
                    │    └──────┬──────┘                          │
                    │           │                                 │
                    └───────────┼─────────────────────────────────┘
                                ▼
                    LLM APIs, GitHub, Telegram, etc.


  CRDs in namespace:

  OpenClaw (instance)           ← no credential fields
  ClawCredential (gemini)       ← type: apiKey, secretRef + domain + header
  ClawCredential (anthropic)    ← type: apiKey, secretRef + domain + header
  ClawCredential (vertex-ai)    ← type: gcp, secretRef + project + location
  ClawCredential (github)       ← type: bearer, secretRef + domain
  ClawCredential (kube-api)     ← type: kubernetes, SA name + domain
  Secret (openclaw-secrets)     ← auto-generated gateway token
  Secret (openclaw-proxy-ca)    ← auto-generated MITM CA cert+key (operator-managed)
  Secret (llm-keys)             ← user-created, referenced by ClawCredential CRs
  Secret (gcp-sa)               ← user-created, referenced by ClawCredential CRs
```

## Out of Scope

- **Multi-tenancy within a single OpenClaw instance** — one instance per user per namespace
- **External CA trust** — the proxy's MITM CA is cluster-internal only, never externally trusted; corporate proxy CAs are handled via OpenShift's `inject-trusted-cabundle` annotation
- **Full agent sandboxing** (gVisor, Kata, custom seccomp) — disproportionate for a personal assistant
- **LLM-layer defenses** (prompt injection filters, output sanitization) — upstream OpenClaw concern
