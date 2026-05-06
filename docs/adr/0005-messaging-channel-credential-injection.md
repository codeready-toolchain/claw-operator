# ADR-0005: Messaging Channel Credential Injection

**Status:** Implemented
**Date:** 2026-05-05

## Overview

Messaging channels (Telegram, Discord, Slack) use API tokens that OpenClaw manages itself — stored in `openclaw.json` and included in outgoing requests. The proxy only allowlists their domains (`type: none`) but does not manage the credentials, breaking the security model where real secrets stay on the proxy pod and never reach the gateway pod.

This design extends the proxy to intercept and replace placeholder credentials with real secrets for messaging channel traffic, bringing messaging channels into the same security posture as LLM provider credentials.

**Constraint:** Modifying OpenClaw upstream is not an option. All changes must be within the claw-operator and its proxy.

## Design Principles

1. **No upstream changes** — OpenClaw is treated as a black box.
2. **Defense in depth** — Real secrets never reach the gateway pod. The proxy strips client-supplied credentials and injects real ones.
3. **Fail closed** — If credential injection fails, the request is rejected (502), not forwarded with a placeholder.
4. **Backward compatible** — Existing `type: none` passthrough continues to work. Credential injection is opt-in via credential type configuration.
5. **Minimal proxy complexity** — Prefer targeted, well-tested changes over architectural overhauls.

## Channel Authentication Background

| Channel | Domain(s) | Auth Mechanism | Validation |
|---------|-----------|----------------|------------|
| Telegram | `api.telegram.org` | Bot token in URL path: `/bot<TOKEN>/method` | None — failures surface as HTTP 401 |
| Discord | `discord.com` + `gateway.discord.gg`, `cdn.discordapp.com` | `Authorization: Bot <TOKEN>` header | None known |
| Slack | `slack.com` + `*.slack.com` (WebSocket) | `Authorization: Bearer <TOKEN>` for both bot (`xoxb-...`) and app (`xapp-...`) tokens | Bolt validates token shape at startup: `^xapp-[A-Za-z0-9_-]+$` / `^xoxb-[A-Za-z0-9_-]+$` |

## Architecture

### Before (Secret on Gateway Pod)

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

### After (Secret on Proxy Pod Only)

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

**Telegram:** OpenClaw sends `/bot<PLACEHOLDER>/sendMessage`. The proxy finds the `/bot` prefix, strips the existing token segment, and inserts the real token.

**Discord:** OpenClaw sends `Authorization: Bot <PLACEHOLDER>`. The proxy strips the header and injects the real token via the `apiKey` injector with `Bot ` value prefix.

**Slack:** OpenClaw sends `Authorization: Bearer xoxb-placeholder`. The proxy strips the header and injects the real token via the `bearer` injector. Same for `xapp-placeholder` on Socket Mode auth calls.

## Core Concepts

### Path Token Replacement (Telegram)

The `PathTokenInjector` uses **prefix-strip-and-replace** semantics:

1. Confirm `req.URL.Path` starts with `pathPrefix` (e.g., `/bot`)
2. Find the next `/` after the prefix — this delimits the token segment
3. Build new path: `pathPrefix` + real token + remainder
4. If no `/` found after prefix, treat entire remainder as the token
5. If path doesn't start with prefix, reject with error (fail closed)

```
/botplaceholder/sendMessage   →  /bot<REAL_TOKEN>/sendMessage
/botplaceholder/getMe         →  /bot<REAL_TOKEN>/getMe
/botplaceholder               →  /bot<REAL_TOKEN>
/sendMessage                  →  error (no prefix match)
```

Replacement is the only behavior — no mode flag or configuration switch. The proxy accepts any placeholder value; users can configure any non-empty string. Documentation recommends `placeholder` as the conventional value.

### Multi-route per Domain (Slack)

Slack requires two tokens on the same domain (`slack.com`): an app-level token for Socket Mode handshake and a bot token for REST API calls. The proxy supports this via path-based route discrimination.

**Matching algorithm:**
1. Collect all routes matching the domain (exact and suffix)
2. If only one match, return it (common case)
3. If multiple matches, prefer the route with the longest `allowedPaths` entry matching the request path
4. Fall back to the route with no `allowedPaths` (catch-all)
5. If no catch-all exists, return `nil` (rejected)

**Route precedence (three tiers):** host:port exact > bare-host exact > suffix/wildcard. Within a tier, routes with `allowedPaths` are preferred over catch-all routes for the same domain.

**Injector storage:** Injectors are attached directly to routes (unexported field on `Route`), not stored in a separate domain-keyed map. This naturally supports multiple injectors per domain.

**CRD impact:** None. `allowedPaths` already exists on `CredentialSpec`. No new fields needed.

### Path Matching Semantics

`AllowedPaths` entries use two matching modes:
- Entries ending with `/` use **prefix** semantics (e.g., `/BerriAI/litellm/` matches `/BerriAI/litellm/main/file`)
- Entries without a trailing `/` require **exact** match (e.g., `/api/apps.connections.open` matches only that exact path)

Both request paths and entries are canonicalized via `path.Clean` before comparison to prevent traversal bypasses (e.g., `/api/../etc/passwd`, `//` normalization).

## Channel Configuration

### Telegram — `pathToken` Credential

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

### Discord — `apiKey` Credential + Passthrough Domains

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

### Slack — `bearer` Credentials with Path Discrimination

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

The `slack-app` credential handles only the Socket Mode handshake endpoint. The `slack-bot` credential is the catch-all for all other `slack.com` API calls. The `slack-ws` entry (suffix match `.slack.com`) passes through WebSocket connections without credential injection.

Bolt-shaped placeholders (`xoxb-placeholder`, `xapp-placeholder`) pass Bolt's startup regex validation.

## Summary of Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Path replacement algorithm | Prefix-strip-and-replace: find prefix, strip token segment, insert real token | Simplest approach; no extra config; leverages existing `pathPrefix`; stable Telegram URL structure |
| Q2 | New injector vs extend `path_token` | Change `path_token` in-place — replacement is the only behavior (Q6) | Prepend mode had no real-world use case; OpenClaw always sends full URLs with token in path |
| Q3 | Placeholder format | Accept any token, replace unconditionally. Docs recommend `placeholder` | No coordination needed between OpenClaw config and proxy config |
| Q4 | Discord domains | `discord.com` (injection) + `gateway.discord.gg`, `cdn.discordapp.com` (passthrough) | Full Discord functionality requires WebSocket gateway and CDN access |
| Q5 | Slack feasibility | Bolt-shaped placeholders (`xapp-placeholder` / `xoxb-placeholder`) with `bearer` injection | Passes Bolt's startup regex; no upstream changes; uses existing `bearer` injector |
| Q6 | Replacement mode trigger | Moot — replacement is the only `path_token` behavior, no flag needed | The old prepend behavior was based on an incorrect assumption; no user would choose it |
| Q7 | Slack dual-token same domain | Path-based route discrimination using existing `allowedPaths` field | Zero new CRD fields; general-purpose; self-documenting YAML; extends natural meaning of `allowedPaths` |
