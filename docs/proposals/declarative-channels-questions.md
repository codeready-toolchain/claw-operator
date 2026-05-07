# Declarative Channel Configuration — Design Questions

**Status:** Resolved — all decisions made
**Related:** [Design document](declarative-channels-design.md)

## Q1: How should the operator identify which credentials are messaging channels?

The operator needs to know "this credential entry means Telegram" to inject `channels.telegram` into `operator.json`. Two approaches: infer from existing fields, or add a new CRD field.

### Option B (chosen): Add `channel` field to CredentialSpec with service-level inference

Add an optional `channel` field (e.g., `channel: telegram`) to `CredentialSpec`. When present, the operator:
1. Enables the channel in `operator.json`
2. Infers proxy defaults (type, domain, pathToken/apiKey config) — same pattern as `provider` for LLMs
3. Auto-generates companion credentials (e.g., discord-gateway, discord-cdn)

Simple path — everything inferred from `channel`:
```yaml
credentials:
  - name: telegram
    channel: telegram
    secretRef:
      - name: telegram-bot-secret
        key: token
```

Explicit override — user controls proxy config, still gets channel enablement:
```yaml
credentials:
  - name: telegram
    channel: telegram
    type: pathToken
    domain: "telegram.internal.corp.com"  # custom domain override
    pathToken:
      prefix: "/bot"
    secretRef:
      - name: telegram-bot-secret
        key: token
```

Proxy-only — no channel field, explicit low-level control:
```yaml
credentials:
  - name: telegram-custom
    type: pathToken
    domain: "api.telegram.org"
    pathToken:
      prefix: "/bot"
    secretRef:
      - name: telegram-bot-secret
        key: token
```

- **Pro:** Explicit — no magic, user clearly opts in
- **Pro:** Mirrors `provider` field design — `provider` infers LLM config, `channel` infers messaging config
- **Pro:** Dramatically simpler UX (3 fields vs 5+ for Telegram, 3 vs 12+ for Discord)
- **Pro:** Auto-generates companion entries (Discord gateway/CDN, Slack WS)
- **Pro:** Escape hatch preserved — can have proxy routing without channel enablement
- **Pro:** Explicit fields always override inference (full control when needed)

**Decision:** Option B with service-level inference — the `channel` field acts as both "enable this channel" and "infer proxy defaults for this service", mirroring how `provider` already works for LLMs. Backwards compatible: existing CRs without `channel` keep working unchanged.

_Considered and rejected: Option A (implicit pattern matching — fragile, no escape hatch), Option C (separate `spec.channels` array — redundant, breaks "one entry = one integration" model)_

---

## Q2: Should we support channel-specific settings in the CRD?

Channels can have settings beyond just "enabled + token" — for example, Telegram supports `dmPolicy` (allowlist/open), `allowFrom` (user IDs), `requireMention` (in groups). Should the operator expose these?

### Option C (chosen): Opaque channelConfig (raw JSON)

Allow an opaque `channelConfig` field that passes through to the channel's config block in `operator.json`:

```yaml
channel: telegram
channelConfig:
  dmPolicy: allowlist
  allowFrom: [12345]
```

- **Pro:** Flexible, no need to model every channel's config in Go types
- **Pro:** Forward-compatible with new OpenClaw channel features
- **Pro:** Users get full declarative control from day one without waiting for typed fields

**Decision:** Option C — opaque raw JSON for channel-specific settings. Flexible, forward-compatible, and avoids needing to chase upstream OpenClaw's channel config schema in our Go types.

_Considered and rejected: Option A (minimal, just enable — leaves no declarative path for security settings), Option B (typed fields per channel — ongoing maintenance burden tracking upstream changes)_

---

## Q3: How should multi-credential channels be handled?

Discord requires 3 proxy entries (bot token + gateway WS + CDN), Slack requires 3 (bot + app + WS). With the `channel` field from Q1, the user writes a single credential entry — should the operator auto-generate companion proxy routes?

### Option A (chosen): Auto-generate companion entries

The operator knows that `channel: discord` needs `gateway.discord.gg` and `cdn.discordapp.com` allowlisted. It generates these proxy routes internally — the user never writes them.

For multi-secret channels (Slack), `secretRef` becomes an array with `role` discriminator:

```yaml
credentials:
  # Telegram — one secret
  - name: telegram
    channel: telegram
    secretRef:
      - name: telegram-bot-secret
        key: token

  # Discord — one secret, companions auto-generated
  - name: discord
    channel: discord
    secretRef:
      - name: discord-bot-secret
        key: token

  # Slack — two secrets, roles distinguish them
  - name: slack
    channel: slack
    secretRef:
      - name: slack-secret
        key: bot-token
        role: botToken
      - name: slack-secret
        key: app-token
        role: appToken
```

`role` is optional when only one secretRef is defined (Telegram, Discord). Required when the channel needs multiple secrets (Slack).

- **Pro:** Dramatically simpler UX — one entry instead of three
- **Pro:** No risk of forgetting companion domains
- **Pro:** Uniform structure — no channel-specific nested structs
- **Pro:** Explicit roles for multi-secret channels, no conventions to remember

**Decision:** Option A — auto-generate companion proxy routes. `secretRef` becomes an array; multi-secret channels use `role` to distinguish entries. The `channel` field gives the operator all it needs to generate proxy routes internally.

_Considered and rejected: Option B (group by name prefix — fragile naming convention)_

---

## Q4: How should WhatsApp be handled?

WhatsApp is fundamentally different from Telegram/Discord/Slack:
- No API key or bot token — uses phone-based QR pairing (user interaction required)
- Requires an npm plugin (`@openclaw/whatsapp`) that only loads on a full pod restart
- Credential entries are just domain allowlisting (`type: none`)
- Session state lives on the PVC (persistent across restarts)

### Option B (chosen): Partial support — operator enables channel + domains, AI installs plugin, user pairs

`channel: whatsapp` causes the operator to:
1. Auto-generate domain allowlists (`.whatsapp.com`, `.whatsapp.net`)
2. Inject `channels.whatsapp: { enabled: true }` and plugin enablement into `operator.json`

The AI handles plugin installation (`@openclaw/whatsapp` via npm). The user completes QR pairing manually when prompted.

```yaml
- name: whatsapp
  channel: whatsapp
  # No secretRef needed — WhatsApp uses QR pairing, not API keys
```

- **Pro:** Consistent — all channels with `channel:` field get operator management
- **Pro:** Domain allowlisting and channel/plugin enabling are automated
- **Pro:** Division of labor: operator (infra), AI (plugin install), user (QR pairing)

**Decision:** Option B — partial declarative support. Operator handles domains + channel enablement. AI installs the plugin. User does QR pairing.

_Considered and rejected: Option A (exclude — inconsistent UX across channels), Option C (full plugin management in operator — too much scope for this proposal)_

---

## Q5: What should PLATFORM.md tell the AI about channels?

The PLATFORM.md skill (injected into the OpenClaw workspace) currently instructs the AI to run `openclaw channels add` after a credential is configured. With declarative channels, this instruction becomes wrong.

### Decision: B+C — explain operator-managed channels, mention custom channels are user/AI-managed

Update PLATFORM.md to:
1. Explain that channels declared in the Claw CR (`channel: telegram/discord/slack/whatsapp`) are **operator-managed** — the AI must NOT run `openclaw channels add/remove` for these. They are configured automatically on pod start.
2. For WhatsApp specifically: AI installs the `@openclaw/whatsapp` plugin when asked, and guides user through QR pairing.
3. For custom/unknown channels not in the CR: AI and user are on their own — standard `openclaw channels add` workflow applies. If the user wants operator management, guide them to update the Claw CR with the `channel:` field.

_Considered and rejected: Option A (remove all instructions — AI can't help at all), standalone Option B (doesn't cover custom channels), standalone Option C (didn't clearly explain what the operator manages)_
