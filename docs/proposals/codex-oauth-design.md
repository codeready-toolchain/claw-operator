# Codex OAuth Provider — Design Document

**Status:** Draft — all design questions resolved, see [codex-oauth-questions.md](codex-oauth-questions.md)

**Date:** 2026-06-03

---

## Overview

OpenAI Codex supports two authentication paths: API keys (static `sk-...` tokens billed per-use on the Platform) and ChatGPT OAuth (short-lived JWTs included with a ChatGPT Plus/Pro/Team subscription). The operator already supports the API key path via the `openai` provider with a `bearer` credential type. This proposal adds support for the OAuth path.

The core challenge is that Codex OAuth tokens expire (~24 hours) and must be refreshed using a long-lived `refresh_token`. Our security model requires that real credentials never reach the gateway container — the MITM proxy handles all credential injection. This design applies the same pattern we use for GCP service account credentials: bootstrap material (the `refresh_token`) is mounted on the proxy as a file, the proxy mints short-lived access tokens internally, and the gateway only ever sees placeholder values.

### Codex OAuth at a glance

| Concern | Value |
|---------|-------|
| Token endpoint | `https://auth.openai.com/oauth/token` |
| API endpoint | `https://chatgpt.com/backend-api/codex/responses` |
| Auth header | `Authorization: Bearer <access_token>` |
| Extra headers | `chatgpt-account-id: <id>`, `originator: openclaw`, `OpenAI-Beta: responses=experimental` |
| Access token lifetime | ~24 hours (JWT) |
| Refresh grant | `grant_type=refresh_token`, `client_id=app_EMoamEEZ73f0CkXaXp7hrann` |
| Bootstrap file | `~/.codex/auth.json` (contains `refresh_token`, `account_id`) |

---

## Design Principles

1. **No real tokens in the gateway.** The gateway container must never hold OAuth access tokens, refresh tokens, or account identifiers. Only placeholder values are visible to the gateway process.

2. **Follow existing patterns.** The implementation mirrors `GCPInjector` (file-mounted bootstrap material, proxy-side token refresh, bearer injection). No new architectural concepts are introduced.

3. **Minimal CRD surface.** One new credential type with no additional config struct — only a `secretRef` to the auth.json file. Users who already understand GCP credentials will find the UX familiar.

4. **Graceful degradation.** When the refresh token is revoked or expired, the proxy returns clear errors and the operator sets a condition on the CR. The gateway continues to function for other providers.

---

## Architecture

### Data flow

```text
User workstation                          Kubernetes cluster
─────────────────                         ──────────────────

codex login --device-auth
      │
      ▼
~/.codex/auth.json ──► kubectl create secret ──► Secret "codex-auth"
                                                      │
                                                      │ (mounted as file)
                                                      ▼
                                               ┌─────────────┐
                                               │  claw-proxy  │
                                               │  container   │
                                               │              │
                                               │  reads auth.json
                                               │  extracts refresh_token + account_id
                                               │  calls auth.openai.com/oauth/token
                                               │  caches access_token (auto-refresh)
                                               │              │
                                               │  on request to chatgpt.com:
                                               │    strips placeholder auth
                                               │    injects Bearer + headers
                                               └──────┬──────┘
                                                      │
                                               ┌──────▼──────┐
                                               │   gateway    │
                                               │  container   │
                                               │              │
                                               │  sends requests with
                                               │  synthetic JWT placeholder
                                               │  (contains account_id only)
                                               │  to chatgpt.com via proxy
                                               └─────────────┘
```

### Comparison with GCP injector

| Aspect | GCP (`injector_gcp.go`) | Codex OAuth (proposed) |
|--------|------------------------|------------------------|
| Bootstrap material | SA key JSON or ADC JSON | Codex `auth.json` |
| Stored as | File on proxy container | File on proxy container |
| Token minting | `google.CredentialsFromJSON` → `TokenSource` | `oauth2.Config` with `RefreshToken` → `TokenSource` |
| Token lifetime | ~1 hour | ~24 hours |
| Injected headers | `Authorization: Bearer` | `Authorization: Bearer` + `chatgpt-account-id` + `originator` + `OpenAI-Beta` |
| Token vending | Intercepts `oauth2.googleapis.com/token` | None (OpenClaw is in API-key mode) |
| Proxy egress needed | `oauth2.googleapis.com` | `auth.openai.com`, `chatgpt.com` |

---

## OpenClaw Native Codex Support vs Proxy Approach

OpenClaw has built-in Codex support through two independent mechanisms:

1. **`codex` plugin / agent harness.** A plugin (`extensions/codex`) that spawns and manages a **Codex app-server binary** as a child process via stdio/websocket RPC. The app-server is essentially the Codex CLI agent runtime — it provides its own sandbox execution environment, native tool handling, approval flows, subagent orchestration, and code execution. When a model has `agentRuntime: { id: "codex" }`, OpenClaw delegates the entire agent turn to the app-server process rather than handling tool calls itself.

2. **`openai-chatgpt-responses` LLM API provider.** A direct HTTP provider (`src/llm/providers/openai-chatgpt-responses.ts`) that makes `fetch()` calls to `chatgpt.com/backend-api/codex/responses` with SSE/WebSocket streaming, retry logic, and response parsing. This is the wire format layer — it handles HTTP transport and response parsing but does not provide its own agent runtime. Tool calls returned by the model are executed by OpenClaw's own tool infrastructure (MCP servers, built-in tools, terminal, etc.).

Both mechanisms require real OAuth credentials at the gateway layer — the native flow reads tokens from `auth-profiles.json` on the PVC and the LLM provider reads an API key (actually the OAuth access token) from the model config. In both cases the gateway process holds the refresh token or access token in memory.

**Our approach uses the `openai-chatgpt-responses` wire format (mechanism 2) with proxy-side credential injection.** This means:

- **What we support:** Access to Codex models (GPT-5.5, GPT-5.4-mini, etc.) through OpenClaw's own agent runtime. OpenClaw sends prompts, receives tool call requests, and executes them using its own tools. Users get the full OpenClaw coding experience powered by Codex models.

- **What we don't support (yet):** The Codex app-server agent harness (mechanism 1). This would require the Codex app-server binary installed in the container and real OAuth tokens available to the gateway for app-server authentication. Since our security model prohibits real tokens in the gateway, and the operator's purpose is to run OpenClaw (not Codex CLI), this is an intentional trade-off for this iteration. A future `spec.codexHarness` section (similar to how we handle Kubernetes support today) could add this capability — installing the binary via init container, configuring the plugin and agent harness, and determining the appropriate security boundary for app-server auth.

The gateway sees only a synthetic placeholder API key and routes traffic through the MITM proxy. The proxy intercepts requests to `chatgpt.com`, strips the placeholder auth, and injects the real OAuth access token plus Codex-specific headers. This preserves the security boundary we maintain for every other provider.

### Synthetic JWT placeholder

OpenClaw's `openai-chatgpt-responses` provider extracts the `account_id` from the JWT payload of the API key **before making any HTTP request** (`extractOpenAICodexAccountId`). If the API key is not a valid JWT, the provider throws immediately — the request never reaches the proxy.

To satisfy this client-side validation, the **controller** generates a **synthetic JWT** as the placeholder API key during reconciliation. The controller reads `auth.json` from the user's Secret, extracts the `account_id`, and builds a minimal unsigned JWT containing that claim. This JWT is set as the provider's `apiKey` in the OpenClaw config. OpenClaw successfully extracts the account ID and builds the request headers. The proxy then replaces the entire `Authorization` header and all Codex-specific headers with values derived from the real OAuth token before forwarding to `chatgpt.com`.

The synthetic JWT:
- Contains only `{"sub":"placeholder","account_id":"<real_account_id>"}` — no secrets
- Uses the `none` algorithm (unsigned) — not usable against any real endpoint
- Is deterministic given the same `account_id` — stable across reconciles

This means the gateway holds the `account_id` (which is non-secret metadata, not an access credential) but never the access token or refresh token.

---

## Core Concepts

### Credential type: `codexOAuth`

A new `CredentialType` constant in the CRD (decided in Q1):

```go
CredentialTypeCodexOAuth CredentialType = "codexOAuth"
```

No additional config struct is needed. The `account_id` is parsed from auth.json at proxy startup (decided in Q6), so the credential type requires only a `secretRef` pointing to the auth.json file — no CRD-level configuration fields beyond what `CredentialSpec` already provides.

### Proxy injector: `CodexOAuthInjector`

A new file `internal/proxy/injector_codex_oauth.go` implementing the `Injector` interface. Structurally identical to `GCPInjector`:

1. **Init:** Reads auth.json from the mounted file path. Parses `refresh_token` and `account_id`. Fails fast with a clear error if either is missing.
2. **Token source:** Creates an `oauth2.Config` targeting `auth.openai.com/oauth/token` with the public client ID. Wraps the refresh token in an `oauth2.Token` and calls `Config.TokenSource()` to get an auto-refreshing `TokenSource`.
3. **Inject:** On each request, calls `TokenSource.Token()` (cached, auto-refreshes when near expiry), then sets:
   - `Authorization: Bearer <access_token>`
   - `chatgpt-account-id: <account_id>`
   - `originator: openclaw`
   - `OpenAI-Beta: responses=experimental`
   - Any `DefaultHeaders` from the route config

No token vending is implemented (decided in Q7). Since OpenClaw is configured in API-key mode (Q4), it won't attempt its own OAuth refresh. The proxy refreshes tokens via direct HTTP calls to `auth.openai.com`, bypassing its own route table.

### Proxy route config

New field on the `Route` struct in `internal/proxy/config.go`:

```go
type Route struct {
    // ... existing fields ...
    CodexAuthFilePath string `json:"codexAuthFilePath,omitempty"`
}
```

The `account_id` is parsed from the auth.json file at proxy startup (Q6), not passed through the route config.

And a corresponding new injector identifier in `internal/proxy/injector.go`:

```go
case "codex_oauth":
    return NewCodexOAuthInjector(route)
```

### Controller wiring

The operator generates proxy config and mounts credentials following the GCP pattern:

**Route generation** (`claw_proxy.go`): For a `codexOAuth` credential, the controller emits a proxy route targeting `chatgpt.com` with:
- `injector: "codex_oauth"`
- `codexAuthFilePath: "/etc/proxy/credentials/<name>/auth.json"`

The `account_id` is parsed from the auth.json file at proxy startup (Q6).

**Volume mounts** (`claw_proxy.go`): The Secret is mounted as a file on the proxy container at `/etc/proxy/credentials/<name>/auth.json`, identical to GCP's SA key mount pattern.

**Provider config** (`claw_resource_controller.go`): The controller injects an OpenClaw provider entry using provider name `openai-oauth` with wire format `api: "openai-chatgpt-responses"` (Q2). OpenClaw is configured in API-key mode with a synthetic JWT placeholder (Q4) — no `auth-profiles.json` is generated. The synthetic JWT contains the `account_id` from auth.json so that OpenClaw's `extractOpenAICodexAccountId` validation passes client-side (see "Synthetic JWT placeholder" above).

**Network policy**: No NetworkPolicy changes are needed. The existing `{instance}-proxy-egress` policy already allows all TCP/443 egress. The proxy's L7 route table is the real allowlist — `chatgpt.com` gets a route from the credential definition, and `auth.openai.com` is reached directly by the proxy's `oauth2.TokenSource` (bypassing the route table). This is consistent with how GCP credentials work.

---

## User Experience

### Setup flow

```bash
# 1. Authenticate with Codex CLI (one-time, on user's workstation)
codex login --device-auth

# 2. Create a Kubernetes Secret from the auth file
kubectl create secret generic codex-auth \
  --from-file=auth.json=$HOME/.codex/auth.json

# 3. Reference it in the Claw CR
```

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: codex
      type: codexOAuth
      provider: openai-oauth
      secretRef:
        - name: codex-auth
          key: auth.json
```

No additional config block is needed — the proxy parses `account_id` directly from the auth.json file (Q6).

### Status reporting

The operator reports Codex OAuth credential health via the existing `CredentialsResolved` condition. If the Secret is missing, the auth.json is malformed, or the account ID is absent, the condition transitions to `False` with a descriptive reason.

Token refresh failures at runtime are logged by the proxy but do not directly update the CR status (the proxy is a separate process). The gateway will see HTTP 401 errors from `chatgpt.com` which surface in the OpenClaw UI.

---

## Implementation Plan

Single PR — the feature is small enough to land as one cohesive change. Most of the work is adding cases to existing switch statements following established patterns (`GCPInjector`, `CredentialTypeGCP`).

**Proxy (`internal/proxy/`):**
1. Add `CodexAuthFilePath` field to `Route` struct in `config.go`
2. Create `internal/proxy/injector_codex_oauth.go` with `CodexOAuthInjector`:
   - Parse auth.json (validate `auth_mode`, `refresh_token`, `account_id`)
   - Create `oauth2.TokenSource` for auto-refresh via `auth.openai.com/oauth/token`
   - Inject `Authorization`, `chatgpt-account-id`, `originator`, `OpenAI-Beta` headers
3. Add `codex_oauth` case to `NewInjector` in `injector.go`
4. Add `chatgpt-account-id` and `OpenAI-Beta` to `authHeaders` stripping in `injector.go`
5. Write unit tests in `injector_codex_oauth_test.go`

**CRD (`api/v1alpha1/`):**
6. Add `CredentialTypeCodexOAuth` to `claw_types.go` (update kubebuilder enum marker)
7. Add `openai-oauth` to CEL type-inference rule so users can omit `type` when `provider: openai-oauth`
8. Run `make manifests generate`

**Provider registry (`internal/controller/`):**
9. Add `openai-oauth` entry to `knownProviders` with dedicated model catalog, `CredType: codexOAuth`, `Domain: "chatgpt.com"`, `API: "openai-chatgpt-responses"`
10. Update `resolveProviderDefaults` for the new type (set domain to `chatgpt.com`)

**Controller wiring (`internal/controller/`):**
11. Add `injectorCodexOAuth = "codex_oauth"` constant to `claw_proxy.go`
12. Add `CodexAuthFilePath` field to `proxyRoute` struct in `claw_proxy.go` (mirrors proxy `Route`)
13. Add route generation case in `buildCredentialRoute` for `codexOAuth` (set `CodexAuthFilePath`)
14. Add volume mount case in `configureProxyForCredentials` (file mount, like GCP's SA key)
15. Add validation case in `resolveCredentials` — parse auth.json from Secret, validate `auth_mode`, extract `tokens.account_id`, store on `resolvedCredential`
16. Update `injectProviders` to accept resolved credentials and generate a synthetic JWT `apiKey` for `codexOAuth` credentials using the parsed `account_id`

**Tests and docs:**
17. Write controller tests
18. Update `PLATFORM.md` skill in `configmap.yaml` to document `codexOAuth` credential type and `openai-oauth` provider
19. Update `docs/user-guide.md` with Codex OAuth setup instructions

---

## `auth.json` File Format

The Codex CLI stores credentials at `~/.codex/auth.json`. The proxy expects a subset of this file:

```json
{
  "auth_mode": "chatgpt",
  "tokens": {
    "access_token": "eyJ...",
    "refresh_token": "v1.MjE...",
    "account_id": "acct_abc123def"
  }
}
```

The proxy validates:
- `auth_mode` must be `"chatgpt"` (not API key mode)
- `tokens.refresh_token` must be non-empty
- `tokens.account_id` must be non-empty

The `access_token` is used as the initial token to avoid an immediate refresh on startup. If it is expired, the `oauth2.TokenSource` will refresh it transparently on the first request.

---

## Security Considerations

1. **Refresh token is the crown jewel.** It is equivalent to a session credential for the user's ChatGPT account. It is stored in a Kubernetes Secret and only mounted on the proxy container — never the gateway.

2. **Access tokens are short-lived.** Even if somehow leaked from the proxy's memory, they expire in ~24 hours.

3. **Account ID is not secret** but is sensitive metadata. It identifies the ChatGPT account and is injected as a header. It does not grant access on its own.

4. **Synthetic JWT prevents gateway-side credential access.** The gateway receives a synthetic JWT that contains only the `account_id` (non-secret metadata) — no access token, refresh token, or real signing key. OpenClaw's `extractOpenAICodexAccountId` succeeds, but the JWT is not usable against any real endpoint. The proxy replaces it with the real access token at the network layer.

5. **Proxy egress scope.** Unlike the GCP wildcard domain issue (`.googleapis.com` matches thousands of APIs), Codex OAuth is scoped to exactly two domains: `chatgpt.com` and `auth.openai.com`. The attack surface is narrow.

6. **Revocation.** If the user changes their ChatGPT password or revokes sessions, the refresh token becomes invalid. The proxy will log refresh failures and requests to `chatgpt.com` will fail with 401. The user must re-run `codex login` and update the Secret.

---

## Resolved Design Questions

All questions have been resolved. See [codex-oauth-questions.md](codex-oauth-questions.md) for full decision rationale.

| # | Question | Decision |
|---|----------|----------|
| Q1 | Credential type naming | New dedicated `codexOAuth` type |
| Q2 | Provider identity and wire format | `openai-oauth` with `openai-chatgpt-responses` wire format |
| Q3 | Relationship to existing `openai` credential | Fully independent — no companion behavior |
| Q4 | OpenClaw auth-profiles.json configuration | API-key mode with synthetic JWT placeholder, no auth-profiles.json |
| Q5 | Network policy for auth.openai.com | No NP changes needed — proxy egress already allows TCP/443 |
| Q6 | Account ID source | Parse from auth.json at proxy startup |
| Q7 | Token vending for auth.openai.com | None needed — OpenClaw is in API-key mode |
| Q8 | Model catalog for Codex OAuth | Dedicated catalog; users can extend via `spec.config.raw` |
| Q9 | Coexistence with API key OpenAI credential | Allow both simultaneously |
