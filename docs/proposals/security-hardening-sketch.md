# Security Hardening for OpenClaw Operator

**Status:** Sketch complete — ready for detailed design

## Problem

The OpenClaw operator deploys a personal AI assistant with access to LLM APIs and the user's Kubernetes namespace. Today, credential isolation (nginx proxy) and egress NetworkPolicies are in place, but several security gaps remain: the gateway has no managed authentication, the CRD carries a plaintext API key, ingress is unrestricted, and the proxy can't handle OAuth-based providers like Vertex AI.

On Dev Sandbox, each OpenClaw instance runs in a user's namespace with their permissions. A security failure here means either leaked LLM credentials (cost and privacy), unauthorized access to the assistant (abuse of the user's namespace permissions), or lateral movement from a compromised pod.

## Current Security Posture (must keep)

- **OpenClaw pod egress locked to proxy only** — the OpenClaw process can never reach the internet or the Kubernetes API directly, only through the proxy (TCP 8080) + DNS. This is a cornerstone security measure.
- Credential isolation via reverse proxy (OpenClaw never sees raw API keys)
- Proxy pod egress limited to TCP 443 + DNS; proxy L7 blocks unknown paths (returns 403)
- Pod hardening: non-root (uid 65532), `readOnlyRootFilesystem`, `seccompProfile: RuntimeDefault`, all capabilities dropped, `automountServiceAccountToken: false`
- Edge TLS on the Route with HTTP-to-HTTPS redirect
- Server-side apply with field ownership tracking

## Decisions

All questions resolved — see [security-hardening-questions.md](security-hardening-questions.md) for full context, options considered, and rationale.

### 1. Gateway Authentication (Q1)

The operator auto-generates a cryptographically random gateway token during reconciliation, stores it in a Secret (`openclaw-secrets`), and injects it as `OPENCLAW_GATEWAY_TOKEN` into the gateway Deployment. The Secret name is written to the CR status (`status.gatewayTokenSecretRef`) so the UI can discover and retrieve it using the user's kube token.

### 2. Credential Provisioning (Q2)

Typed credential CRDs using the `XYZCredential` naming pattern, organized by credential *shape*:

- **`APIKeyCredential`** — Gemini, Anthropic, OpenAI, OpenRouter, etc. Wraps a Secret ref + target domain + header name/format.
- **`GCPCredential`** — Vertex AI, GCP APIs. Wraps a Secret ref (SA JSON) + project + location + domain.
- **`BearerTokenCredential`** — GitHub, simple bearer services. Wraps a Secret ref + target domain.
- Additional CRDs as needed (e.g., `OAuth2Credential`, `ChannelCredential`).

The OpenClaw CR carries no credential fields. The controller discovers credential CRDs in the namespace by label (`openclaw.sandbox.redhat.com/instance: instance`). Adding a new service with an existing auth shape is purely declarative (new CR instance). Adding a new auth shape requires a new CRD + controller logic.

### 3. Credential Injection — Go Proxy (Q7)

Replace the nginx reverse proxy with a purpose-built Go reverse proxy:

```
OpenClaw ──HTTP──▶ Go Proxy ──HTTPS──▶ LLM APIs / External Services
                      │
                      ├─ Config derived from credential CRDs
                      ├─ Domain → injector routing
                      ├─ Pluggable injectors: bearer, API key header, GCP ADC/OAuth2
                      ├─ Strips ALL client auth headers, then injects real credentials
                      ├─ Health endpoint (/healthz)
                      └─ Unknown domains → 403
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
                    │    Go Credential Proxy                      │
                    │    ┌──────────────────────────────┐         │
                    │    │ Config from credential CRDs  │         │
                    │    │ Strip client auth headers    │         │
                    │    │ Inject real credentials      │         │
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
  APIKeyCredential (gemini)     ← secretRef + domain + header
  APIKeyCredential (anthropic)  ← secretRef + domain + header
  GCPCredential (vertex-ai)     ← secretRef + project + location
  BearerTokenCredential (github)← secretRef + domain
  Secret (openclaw-secrets)     ← auto-generated gateway token
  Secret (llm-keys)             ← user-created, referenced by credential CRDs
  Secret (gcp-sa)               ← user-created, referenced by GCPCredential
```

## Out of Scope

- **Multi-tenancy within a single OpenClaw instance** — one instance per user per namespace
- **End-to-end encryption (pod-to-pod TLS)** — typical OpenShift deployments trust the SDN
- **Full agent sandboxing** (gVisor, Kata, custom seccomp) — disproportionate for a personal assistant
- **LLM-layer defenses** (prompt injection filters, output sanitization) — upstream OpenClaw concern
