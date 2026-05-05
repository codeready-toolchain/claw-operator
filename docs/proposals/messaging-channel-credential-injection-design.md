# Messaging Channel Credential Injection

**Status:** Final

## Overview

Messaging channels (Telegram, Discord, Slack) use API tokens that OpenClaw currently manages itself — stored in `openclaw.json` and included in outgoing requests. The proxy only allowlists their domains (`type: none`) but does not manage the credentials, breaking our security model where real secrets should stay on the proxy pod and never be accessible to the gateway pod.

This design extends the proxy to intercept and replace placeholder credentials with real secrets for messaging channel traffic, bringing messaging channels into the same security posture as LLM provider credentials.

**Constraint:** Modifying OpenClaw upstream is not an option. All changes must be within the claw-operator and its proxy.

## Design Principles

1. **No upstream changes** — OpenClaw is treated as a black box. We cannot modify its source code, add configuration hooks, or require specific OpenClaw versions.
2. **Defense in depth** — Real secrets never reach the gateway pod. The proxy strips client-supplied credentials and injects real ones.
3. **Fail closed** — If credential injection fails, the request is rejected (502), not forwarded with a placeholder.
4. **Backward compatible (free)** — Existing `type: none` passthrough continues to work. Credential injection is opt-in via credential type configuration. Not a design constraint at this stage, just a side effect of the approach.
5. **Minimal proxy complexity** — Prefer targeted, well-tested changes to the existing proxy over architectural overhauls.

## Background: How Each Channel Authenticates

### Telegram Bot API

- **Domain:** `api.telegram.org`
- **Auth mechanism:** Bot token embedded in URL path: `POST /bot<TOKEN>/sendMessage`
- **URL construction (grammY):** `${apiRoot}/bot${token}/${method}` where `apiRoot` defaults to `https://api.telegram.org`
- **Config fields:** `channels.telegram.botToken`, `channels.telegram.tokenFile`, or `TELEGRAM_BOT_TOKEN` env var
- **Local validation:** None — grammY does not validate token format before making API calls. Failures surface as HTTP 401 from Telegram.

### Discord Bot API

- **Domain:** `discord.com` (REST API), plus `gateway.discord.gg` (WebSocket), `cdn.discordapp.com` (media)
- **Auth mechanism:** `Authorization: Bot <TOKEN>` header
- **Config field:** `channels.discord.botToken` or `DISCORD_BOT_TOKEN`
- **Local validation:** None known for the bot token itself.

### Slack (Bolt)

- **Domain:** `slack.com`
- **Auth mechanism:** `Authorization: Bearer <TOKEN>` header for both bot token (`xoxb-...`) and app token (`xapp-...`)
- **Local validation:** Bolt validates token **shape** at startup: app tokens must match `^xapp-[A-Za-z0-9_-]+$`, bot tokens must match `^xoxb-[A-Za-z0-9_-]+$`. However, Bolt-shaped placeholders like `xapp-placeholder` and `xoxb-placeholder` pass this regex.

## Architecture

### Current State

```
Gateway Pod                    Proxy Pod                     Upstream
┌──────────────┐              ┌──────────────┐
│ OpenClaw     │──CONNECT────▶│ MITM Proxy   │──────────────▶ api.telegram.org
│              │  (full URL   │              │  (passthrough  /bot<REAL_TOKEN>/...
│ botToken:    │   with real  │ type: none   │   or direct
│  <REAL>      │   token in   │              │   tunnel)
│              │   path)      │              │
└──────────────┘              └──────────────┘
```

**Problem:** The real bot token is stored in `openclaw.json` on the gateway pod's PVC and visible in the URL path flowing through the proxy. Any code running on the gateway pod can read it.

### Proposed State

```
Gateway Pod                    Proxy Pod                     Upstream
┌──────────────┐              ┌──────────────┐
│ OpenClaw     │──CONNECT────▶│ MITM Proxy   │──────────────▶ api.telegram.org
│              │  /bot<PLCH>/ │              │  /bot<REAL>/    /bot<REAL_TOKEN>/...
│ botToken:    │  sendMessage │ path_token   │  sendMessage
│  <PLACEHOLDER│              │ (replace)    │
│              │              │              │
└──────────────┘              └──────────────┘
```

**Telegram:** OpenClaw sends `/bot<PLACEHOLDER>/sendMessage`. The proxy finds the prefix `/bot`, strips the existing token segment, and inserts the real token. Forwarded as `/bot<REAL_TOKEN>/sendMessage`.

**Discord:** OpenClaw sends `Authorization: Bot <PLACEHOLDER>`. The proxy strips the `Authorization` header and injects `Authorization: Bot <REAL_TOKEN>` via the existing `apiKey` injector.

**Slack:** OpenClaw sends `Authorization: Bearer xoxb-placeholder`. The proxy strips the header and injects the real `xoxb-...` token via the existing `bearer` injector. Same for `xapp-placeholder` on Socket Mode auth calls.

### Request Flow (Telegram — MITM path)

1. User configures OpenClaw with `botToken: "placeholder"` (via `openclaw channels add --channel telegram --token placeholder`)
2. grammY constructs: `POST https://api.telegram.org/botplaceholder/sendMessage`
3. Gateway's undici sends CONNECT to proxy
4. Proxy: `MatchRoute("api.telegram.org")` → finds `path_token` route → `ConnectMitm`
5. Proxy decrypts TLS, sees `POST /botplaceholder/sendMessage`
6. Proxy: `StripAuthHeaders(req)` (no-op for path-based auth)
7. Proxy: `PathTokenInjector.Inject(req)` → finds `/bot` prefix at path start, strips `placeholder` (everything between prefix and next `/`), inserts real token
8. Proxy: forwards `POST /bot<REAL_TOKEN>/sendMessage` to `api.telegram.org`

## Core Concepts

### Path Token Replacement

The `PathTokenInjector` is changed from **prepend** to **replace** semantics. The old prepend behavior had no real-world use case — OpenClaw (the only client behind this proxy) always sends full URLs with the token already in the path.

**Algorithm (prefix-strip-and-replace):**
1. Confirm `req.URL.Path` starts with `pathPrefix` (e.g., `/bot`)
2. Find the next `/` after the prefix length → this delimits the token segment
3. Build new path: `pathPrefix` + real token + remainder
4. If no `/` found after prefix, treat entire remainder as the token (edge case: bare `/botplaceholder` with no method)
5. If path doesn't start with prefix, reject with error (fail closed)

```
Input:  /botplaceholder/sendMessage   →  /bot<REAL_TOKEN>/sendMessage
Input:  /botplaceholder/getMe         →  /bot<REAL_TOKEN>/getMe
Input:  /botplaceholder               →  /bot<REAL_TOKEN>
Input:  /sendMessage                  →  error (no prefix match)
```

No `PathReplace` flag or mode switch — replacement is the only behavior. The proxy config JSON and `Route` struct are unchanged.

### Placeholder Token Value

The proxy accepts any placeholder value. Users can configure OpenClaw with any non-empty string as the bot token (e.g., `placeholder`, `dummy`, `0:fake`). Documentation recommends `placeholder` as the conventional value.

No coordination between the OpenClaw config and proxy config is needed — the proxy strips whatever it finds and inserts the real token.

### Multi-route per Domain

The proxy currently stores injectors in a `map[string]Injector` keyed by domain and uses `MatchRoute` (first domain match wins) to look up routes. This limits each domain to one injector. Slack requires two — an app-token injector for the Socket Mode handshake and a bot-token injector for everything else.

**Solution:** Extend route matching to use `allowedPaths` as a route discriminator when multiple routes share a domain.

**Matching algorithm (updated `MatchRoute`):**
1. Collect all routes matching the domain (exact and suffix, same as today)
2. If only one match, return it (common case — no behavior change)
3. If multiple matches, prefer the route whose `allowedPaths` matches the request path
4. If no `allowedPaths` route matches, fall back to the route with no `allowedPaths` (catch-all)
5. If no catch-all exists, return `nil` (no route — rejected)

**Injector storage change:** Replace `map[string]Injector` with injectors attached directly to routes (e.g., unexported `injector` field on `Route`, or indexed by route position). The injector is resolved from the matched route, not from a separate domain-keyed map.

**CRD impact:** None. `allowedPaths` already exists on `CredentialSpec`. Its current meaning ("restrict which paths the proxy permits") naturally extends to "this credential handles these paths." No new fields.

**Config ordering:** Routes with `allowedPaths` (specific) must appear before routes without (catch-all) for the same domain. The controller already emits exact-match routes before suffix-match routes; within exact matches, it should emit `allowedPaths` routes first.

### Channel-Specific Configuration

#### Telegram — `pathToken` Credential

```yaml
- name: telegram
  type: pathToken
  secretRef:
    name: telegram-bot-secret
    key: token
  domain: "api.telegram.org"
  pathToken:
    prefix: "/bot"
```

OpenClaw config: `channels.telegram.botToken: "placeholder"` (or `openclaw channels add --channel telegram --token placeholder`).

#### Discord — `apiKey` Credential + Passthrough Domains

```yaml
- name: discord
  type: apiKey
  secretRef:
    name: discord-bot-secret
    key: token
  domain: "discord.com"
  apiKey:
    header: Authorization
    valuePrefix: "Bot "
- name: discord-gateway
  type: none
  domain: "gateway.discord.gg"
- name: discord-cdn
  type: none
  domain: "cdn.discordapp.com"
```

OpenClaw config: `channels.discord.botToken: "placeholder"`.

No proxy changes needed — the existing `apiKey` injector with `header: Authorization`, `valuePrefix: "Bot "` handles Discord. `StripAuthHeaders` removes the client's `Authorization` header, then the injector sets the real one. Additional domains (`gateway.discord.gg`, `cdn.discordapp.com`) are passthrough for WebSocket and media. Exact domain list to be validated during testing.

#### Slack — `bearer` Credentials with Path Discrimination

Slack requires two tokens on the same domain (`slack.com`): an app-level token (`xapp-...`) for Socket Mode handshake and a bot token (`xoxb-...`) for REST API calls. The proxy uses `allowedPaths` to route requests to the correct injector.

```yaml
- name: slack-app
  type: bearer
  secretRef:
    name: slack-secret
    key: app-token
  domain: "slack.com"
  allowedPaths: ["/api/apps.connections.open"]
- name: slack-bot
  type: bearer
  secretRef:
    name: slack-secret
    key: bot-token
  domain: "slack.com"
- name: slack-ws
  type: none
  domain: ".slack.com"
```

OpenClaw config: `channels.slack.botToken: "xoxb-placeholder"`, `channels.slack.appToken: "xapp-placeholder"`.

Bolt-shaped placeholders (`xoxb-placeholder`, `xapp-placeholder`) pass Bolt's startup regex validation. The `slack-app` credential has `allowedPaths` restricting it to the Socket Mode handshake endpoint; the `slack-bot` credential is the catch-all for all other `slack.com` traffic. The `slack-ws` entry (suffix match `.slack.com`) passes through WebSocket connections to `wss-primary.slack.com` without credential injection. If end-to-end testing reveals additional validation blockers beyond the startup regex, we'll revisit.

**Proxy change required:** The current proxy maps one injector per domain (`map[string]Injector`), so two routes for `slack.com` silently overwrite each other. PR 2 extends route matching to support **path-based discrimination** when multiple routes share a domain. See [Multi-route per domain](#multi-route-per-domain) above and the PR 2 implementation plan.

## Implementation Plan

### PR1: Telegram + Discord

Telegram requires a proxy change (pathToken replacement). Discord uses existing injectors unchanged. Grouped into one PR — small proxy diff, shared documentation scope.

1. **Proxy change** (`internal/proxy/injector_pathtoken.go`):
   - Change `PathTokenInjector.Inject` from prepend to prefix-strip-and-replace
   - Confirm path starts with `pathPrefix`, find next `/` to delimit token, replace
   - Fail closed: if path doesn't match expected prefix, return error → proxy returns 502
2. **Proxy tests** (`internal/proxy/injector_test.go`):
   - Update `TestPathTokenInjector` to send full paths (`/botplaceholder/sendMessage`) instead of bare paths (`/sendMessage`)
   - Add test cases: path with prefix match, path without prefix (error), edge case bare token without method
3. **Controller tests** (`internal/controller/claw_proxy_test.go`):
   - Existing `pathToken` test cases should continue to pass (config generation unchanged)
   - Add test case for Discord-style `apiKey` credential with `Authorization` header and `Bot ` prefix
4. **Documentation:**
   - Add Telegram and Discord sections to `PROXY_SETUP.md` skill in `internal/assets/manifests/claw/configmap.yaml`
   - Add Telegram and Discord sections to `docs/provider-setup.md`
   - Update skill frontmatter description to mention messaging channels
5. **End-to-end testing:** Verify both channels work with placeholder tokens

### PR2: Slack

Extends the proxy's routing model to support multiple routes per domain with path-based discrimination. Architecturally distinct from PR1 — modifies how route matching and injector storage work.

1. **Proxy change — injector storage** (`internal/proxy/server.go`):
   - Replace `injectors map[string]Injector` with injectors attached to routes (e.g., unexported field on `Route` set during `NewServer`, or a `[]routeInjector` indexed by route position)
   - Update injector lookup in `DoFunc` and gateway handler to resolve from matched route instead of domain map
2. **Proxy change — route matching** (`internal/proxy/config.go`):
   - Extend `MatchRoute(host, path)` to accept request path
   - When multiple routes match the same domain, prefer the route whose `allowedPaths` matches the request path; fall back to the route with no `allowedPaths`
   - Update all `MatchRoute` call sites to pass the request path
3. **Proxy tests** (`internal/proxy/injector_test.go`, `internal/proxy/server_test.go`):
   - Test multi-route matching: two routes for same domain, one with `allowedPaths`, one catch-all
   - Test that the correct injector is selected based on request path
   - Test fallback when no `allowedPaths` route matches
4. **Controller change** (`internal/controller/claw_proxy.go`):
   - Ensure routes with `allowedPaths` are emitted before catch-all routes for the same domain
5. **End-to-end testing:** Verify OpenClaw with `xoxb-placeholder` / `xapp-placeholder` + proxy `bearer` injection works for Slack
6. **Documentation:** Add Slack section to `PROXY_SETUP.md` skill and `docs/provider-setup.md`
7. **Fallback:** If Bolt validation or other blockers surface beyond the dual-token routing, defer Slack to `type: none` passthrough and document as known limitation

## Summary of Decisions

All decisions recorded in [messaging-channel-credential-injection-questions.md](messaging-channel-credential-injection-questions.md):

| # | Question | Decision |
|---|----------|----------|
| Q1 | Path replacement algorithm | Prefix-strip-and-replace: find prefix, strip token segment, insert real token |
| Q2 | New injector vs extend `path_token` | Change `path_token` in-place — no mode flag needed (Q6 made this moot) |
| Q3 | Placeholder format | Accept any token, replace unconditionally. Docs recommend `placeholder` |
| Q4 | Discord domains | `discord.com` (credential injection) + `gateway.discord.gg`, `cdn.discordapp.com` (passthrough) |
| Q5 | Slack feasibility | Plan for Bolt-shaped placeholders (`xapp-placeholder` / `xoxb-placeholder`); revisit if testing reveals blockers |
| Q6 | Replacement mode trigger | Moot — replacement is the only `path_token` behavior, no flag needed |
| Q7 | Slack dual-token same domain | Path-based route discrimination using existing `allowedPaths` field. No new CRD fields — proxy extended to support multiple routes per domain |
