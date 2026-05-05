# Messaging Channel Credential Injection — Design Questions

**Status:** All decisions resolved
**Related:** [Design document](messaging-channel-credential-injection-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

## Q1: How should the proxy replace the placeholder token in the Telegram URL path?

Telegram Bot API URLs follow the pattern `/bot<TOKEN>/<METHOD>`. OpenClaw sends the full URL with its configured token (the placeholder). The proxy must find the placeholder and replace it with the real secret.

The key challenge: the proxy sees a path like `/botplaceholder-value/sendMessage` and must replace `placeholder-value` with the real token. How does it identify the boundary of the placeholder?

### Option A: Prefix-strip-and-prepend

Strip everything between the prefix (`/bot`) and the next `/`, then prepend the prefix + real token to the remainder.

Algorithm:
```
Path: /botPLACEHOLDER/sendMessage
1. Confirm path starts with "/bot"
2. Find next "/" after position 4 → position at "/sendMessage"
3. Result: /bot<REAL_TOKEN>/sendMessage
```

- **Pro:** Simple, deterministic. No need to know the placeholder value. Works with any placeholder.
- **Pro:** Doesn't require a new field — the proxy only needs `pathPrefix` (already exists) and the env var for the real token.
- **Pro:** Follows OpenShell's approach conceptually (substring replacement in path segment).
- **Con:** Assumes exactly one path segment after the prefix contains the token. If Telegram ever adds paths without the bot prefix, they'd be incorrectly rewritten. (But Telegram Bot API always uses `/bot<TOKEN>/...`.)
- **Con:** A bare request like `/bot` or `/bot/` without a method would need edge-case handling.

**Decision:** Option A — simplest, no extra config, leverages existing `pathPrefix`, stable Telegram URL structure.

_Considered and rejected: Option B (known-placeholder matching — extra config field, coordination burden), Option C (regex — overkill for this use case)._

## Q2: Should this be a new injector type or an extension of `path_token`?

The current `path_token` injector prepends. We need a replacement mode. Should we modify the existing injector or create a new one?

### Option A: Extend `path_token` with a mode flag

Add a `PathReplace bool` field to `Route`. When true, `PathTokenInjector` does replacement instead of prepend.

- **Pro:** No new injector type — keeps the injector count manageable.
- **Pro:** The CRD type `pathToken` already exists and makes sense for both modes.
- **Pro:** Shared construction logic (env var, prefix, headers).
- **Con:** `PathTokenInjector` becomes a dual-mode struct, slightly more complex.

**Decision:** Option A — minimal change, reuses existing `pathToken` CRD type and injector. Q6 subsequently made the mode flag moot: replacement is now the only `path_token` behavior, requiring no config field or flag at all.

_Considered and rejected: Option B (new injector type — code duplication, extra wiring), Option C (infer from proxy mode — tight coupling, fragile assumption)._

## Q3: Should the proxy require a specific placeholder format?

When the user configures OpenClaw with a dummy bot token for proxy injection, does the placeholder value matter?

### Option A: Accept any token — the proxy replaces blindly

The proxy doesn't care what token OpenClaw sends. It strips the segment after `/bot` and replaces it with the real token regardless.

- **Pro:** Simplest for users — they can use any dummy value like `placeholder`, `dummy`, `0:fake`.
- **Pro:** No coordination needed between OpenClaw config and proxy config.
- **Con:** If OpenClaw sends a real token by mistake (misconfiguration), the proxy silently replaces it — no safety check.

**Decision:** Option A — simplest for users, no coordination needed. Docs recommend `placeholder` as the conventional value.

_Considered and rejected: Option B (well-known placeholder format — extra coordination friction, no real security gain), Option C (warn on real-looking token — minor complexity for debatable value)._

## Q4: Does Discord require additional domains beyond `discord.com`?

Discord bots communicate through the Discord API, but also use Gateway WebSocket connections and CDN.

### Option B: `discord.com` + domain allowlists for WebSocket and CDN

Primary credential injection on `discord.com`, plus `type: none` passthrough for:
- `gateway.discord.gg` (WebSocket gateway)
- `cdn.discordapp.com` (media/attachments)
- `discord.gg` (invite links, if used)

- **Pro:** Full Discord functionality.
- **Con:** More entries in the Claw CR.

**Decision:** Option B — credential injection on `discord.com`, passthrough for WebSocket and CDN. Exact domain list to be validated during Phase 2 testing.

_Considered and rejected: Option A (`discord.com` only — WebSocket/CDN would break), Option C (broad suffix matches — overly permissive)._

## Q5: How should we handle Slack given the `xapp-` validation constraint?

Slack's Bolt library validates that the app token starts with `xapp-` at startup, before making any HTTP calls. Without modifying OpenClaw, we can't control what token value Bolt sees.

### Option D: Bolt-shaped placeholder tokens (`xapp-placeholder` / `xoxb-placeholder`)

Slack's Bolt SDK validates token shape at `App` construction (before HTTP calls): app tokens must match `^xapp-[A-Za-z0-9_-]+$`, bot tokens must match `^xoxb-[A-Za-z0-9_-]+$`. However, a simple placeholder like `xapp-placeholder` or `xoxb-placeholder` passes this regex.

The flow:
1. User configures OpenClaw with `appToken: "xapp-placeholder"`, `botToken: "xoxb-placeholder"`
2. Bolt accepts these at startup (valid shape)
3. Bolt sends `POST https://slack.com/api/apps.connections.open` with `Authorization: Bearer xapp-placeholder`
4. Our MITM proxy strips the header and injects the real `xapp-...` token via `bearer` injector
5. Same for bot token on REST API calls

- **Pro:** No upstream changes, no preload hacks, no entrypoint modification.
- **Pro:** Uses the same `bearer` injector that already works for header-based auth.
- **Pro:** Simple placeholder values that match Bolt's regex.
- **Con:** Risk that Bolt or OpenClaw does additional in-process validation beyond the startup regex that we haven't discovered (e.g., token length, checksum). Needs end-to-end testing.
- **Con:** If Slack tightens their token format validation in a future Bolt release, this approach breaks.

**Decision:** Option D — plan for Bolt-shaped placeholder tokens (`xapp-placeholder` / `xoxb-placeholder`) with `bearer` injection. If end-to-end testing reveals additional validation blockers, we'll revisit.

_Considered and rejected: Option A (defer entirely — premature given the simple placeholder approach looks viable), Option B (container entrypoint override — too fragile, borderline upstream change), Option C (bot token only — Socket Mode is needed for most Slack deployments)._

## Q6: Should replacement mode be inferred automatically or require an explicit CRD field?

Q2 decided to extend `path_token` with a mode flag. But on reflection: the prepend mode has no real-world use case. OpenClaw is the only client behind this proxy, and it always sends full URLs with the token in the path. No other API uses path-based token injection where the client sends bare paths. The gateway mode uses `PathPrefix` + `Upstream` for path routing — a separate mechanism entirely. The prepend mode was the original design based on an incorrect assumption.

**Decision:** Q6 is moot. Just change `path_token` to always do replacement — no boolean flag, no mode switch, no config field. The `PathTokenInjector.Inject` method changes from prepend to replace. Existing tests update to reflect the new (and only correct) behavior. This also simplifies Q2's decision: no new `Route` field needed.

_Considered and rejected: Option A (always-true flag — unnecessary indirection), Option B (CRD mode field — no user would ever pick "prepend"), Option C (infer from proxy mode — moot when there's only one behavior)._

## Q7: How should the proxy handle Slack's two tokens on the same domain?

Slack uses two different Bearer tokens on the same domain (`slack.com`): an app-level token (`xapp-...`) for the `apps.connections.open` Socket Mode handshake, and a bot token (`xoxb-...`) for all other REST API calls. The proxy's `injectors` map is `map[string]Injector` keyed by domain — a second route for `slack.com` silently overwrites the first.

**Decision:** Option C — path-based route discrimination using existing `allowedPaths` field. Two standard `bearer` credentials for `slack.com`, differentiated by `allowedPaths`. The proxy is extended to support multiple routes per domain — routes with `allowedPaths` are tried first, the route without `allowedPaths` is the catch-all. Zero new CRD fields, general-purpose, and self-documenting YAML.

_Considered and rejected: Option A (bot-token only — Socket Mode is essential for enterprise Slack), Option B (composite injector — Slack-specific type is inconsistent with the rest of the CRD), Option D (defer entirely — doesn't meet the security goal)._
