# ADR-0007: Declarative Channel Configuration

**Status:** Implemented
**Date:** 2026-05-06

## Overview

When a user adds a messaging channel credential (Telegram, Discord, Slack, WhatsApp) to the Claw CR, the operator declaratively configures the corresponding OpenClaw channel — no reliance on the AI assistant to run `openclaw channels add` at runtime.

The `channel` field on a credential entry acts as both:
1. A declaration that enables the channel in OpenClaw's config
2. A service-level hint that infers proxy defaults (domain, type, companion routes)

This mirrors the existing `provider` field pattern: `provider: google` infers LLM config, `channel: telegram` infers messaging config. The two are mutually exclusive — a credential cannot have both `provider` and `channel` set (validated by CEL).

## Design Principles

1. **Declarative over imperative** — channel lifecycle is driven by the Claw CR, not runtime CLI commands
2. **Mirror the `provider` pattern** — `channel` infers service defaults just like `provider` infers LLM defaults
3. **Explicit > implicit** — user opts into channel management via the `channel` field (no magic detection)
4. **Placeholder-token architecture** — channels use dummy tokens; the proxy replaces them transparently
5. **Pod rollout on change** — channel config changes `operator.json` → config hash changes → gateway rolls out fresh

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| Q1 | How to identify channel credentials | `channel` field on `CredentialSpec` with service-level inference | Explicit opt-in, mirrors `provider` pattern, dramatically simpler UX (3 fields vs 5+ for Telegram), auto-generates companion routes. Rejected: implicit pattern matching (fragile), separate `spec.channels` array (redundant) |
| Q2 | Channel-specific settings | Opaque `channelConfig` (raw JSON) deep-merged into the channel config block | Flexible, forward-compatible with new upstream features, no Go type maintenance burden. Rejected: minimal (no declarative security settings), typed fields per channel (ongoing maintenance) |
| Q3 | Multi-credential channels | Auto-generate companion proxy routes; `secretRef` becomes an array with `role` discriminator | One entry instead of three, no risk of forgetting companion domains, explicit roles for multi-secret channels. Rejected: group by name prefix (fragile naming convention) |
| Q4 | WhatsApp handling | Partial support — operator enables channel + domains, AI installs plugin, user does QR pairing | Consistent UX across channels; division of labor: operator (infra), AI (plugin install), user (QR pairing). Rejected: full exclusion (inconsistent), full plugin management (too much scope) |
| Q5 | PLATFORM.md AI instructions | Explain operator-managed channels, mention custom channels are user/AI-managed | AI must not run `openclaw channels add/remove` for operator-managed channels; custom channels still use the standard CLI workflow |

## Architecture

### Flow

```
User adds credential with channel: telegram
  → Operator infers proxy config (type, domain, pathToken)
  → Operator generates companion proxy routes (if needed)
  → Operator injects channels.telegram into operator.json
  → Config hash changes → gateway pod rolls out (fresh start)
  → init-config merges channels into PVC config
  → Gateway starts with channel pre-configured ✓
```

### CRD Changes

The `CredentialSpec` gains two new fields:

- **`channel`** (string, optional) — declares a messaging channel integration. Known values: `telegram`, `discord`, `slack`, `whatsapp`. When set, the operator enables the channel and infers proxy defaults.
- **`channelConfig`** (`runtime.RawExtension`, optional) — opaque JSON deep-merged into the channel's config block in `operator.json` for channel-specific settings (e.g., `dmPolicy`, `allowFrom`).

**`Type` becomes optional** — when `channel` is set, the operator infers `Type` from the channel defaults table. Explicit `type:` still overrides. Validation uses two structural CEL rules at admission (require `type` or `channel`; `provider`/`channel` mutually exclusive). Type-specific config checks move to the controller and report via the `CredentialsResolved` status condition.

**`SecretRef` changes from `*SecretRef` to `[]SecretRefEntry`** — a breaking API change to support multi-secret channels (e.g., Slack needs both `botToken` and `appToken`). Each entry has `name`, `key`, and an optional `role` discriminator.

### ChannelConfig Merge Semantics

The operator builds a base config block per channel (e.g., `{"enabled": true, "botToken": "placeholder"}`), then applies `channelConfig` on top:

- **Objects:** deep-merge (recursive key-level merge)
- **Arrays:** replaced wholesale (not concatenated)
- **Scalars:** overwritten by `channelConfig` value

**Protected keys** (`enabled`, token placeholders like `botToken`/`token`/`appToken`) are operator-managed and cannot appear in `channelConfig`. Attempts produce a validation error via the `CredentialsResolved` condition.

### Channel Defaults

| Channel | Inferred Type | Domain(s) | Companion Routes | Placeholder |
|---------|--------------|-----------|------------------|-------------|
| telegram | pathToken (prefix: `/bot`) | api.telegram.org | — | `"placeholder"` |
| discord | apiKey (header: `Authorization`, valuePrefix: `Bot `) | discord.com | gateway.discord.gg, cdn.discordapp.com | `"placeholder"` |
| slack | bearer | slack.com | .slack.com (WS) | `"xoxb-placeholder"` / `"xapp-placeholder"` |
| whatsapp | none | — | .whatsapp.com, .whatsapp.net | — |

### Config Injection

The operator injects into `operator.json`:

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "botToken": "placeholder"
    }
  },
  "plugins": {
    "entries": {
      "telegram": { "enabled": true }
    }
  }
}
```

For WhatsApp, the plugin entry is added but the AI handles actual npm installation.

### User-Facing Examples

```yaml
credentials:
  # Telegram — minimal, everything inferred
  - name: telegram
    channel: telegram
    secretRef:
      - name: telegram-bot-secret
        key: token

  # Discord — one secret, companion routes auto-generated
  - name: discord
    channel: discord
    secretRef:
      - name: discord-bot-secret
        key: token

  # Slack — two secrets with roles
  - name: slack
    channel: slack
    secretRef:
      - name: slack-secret
        key: bot-token
        role: botToken
      - name: slack-secret
        key: app-token
        role: appToken

  # WhatsApp — no secret needed (QR pairing)
  - name: whatsapp
    channel: whatsapp

  # Telegram with custom settings
  - name: telegram
    channel: telegram
    secretRef:
      - name: telegram-bot-secret
        key: token
    channelConfig:
      dmPolicy: allowlist
      allowFrom: [12345]

  # Explicit override — custom domain, still gets channel enablement
  - name: telegram
    channel: telegram
    type: pathToken
    domain: "telegram.internal.corp.com"
    pathToken:
      prefix: "/bot"
    secretRef:
      - name: telegram-bot-secret
        key: token
```

### PLATFORM.md Updates

The AI skill document explains:
1. Channels with `channel:` field in the CR are **operator-managed** — do NOT run `openclaw channels add/remove`
2. WhatsApp: AI installs the `@openclaw/whatsapp` plugin; user does QR pairing
3. Custom/unknown channels not in the CR: AI and user manage them directly via CLI
