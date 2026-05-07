# Declarative Channel Configuration

**Status:** Final — all decisions resolved in [declarative-channels-questions.md](declarative-channels-questions.md)

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

The `CredentialSpec` gets these changes:

**`Type` becomes optional** — currently required; when `channel` is set, the operator infers `Type` from the channel defaults table. Explicit `type:` still overrides. Validation strategy: only two structural CEL rules at admission (require `type` or `channel`; `provider`/`channel` mutually exclusive). All type-specific config checks (e.g., "apiKey requires apiKey config") move to the controller and report via the `CredentialsResolved` status condition — matching the existing `resolveProviderDefaults` pattern.

**Two new fields:**

```go
// Channel declares this credential as a messaging channel integration.
// When set, the operator enables the channel in OpenClaw's config and
// infers proxy defaults (type, domain, injection config, companion routes).
// Known values: telegram, discord, slack, whatsapp.
// +optional
Channel string `json:"channel,omitempty"`

// ChannelConfig is opaque JSON merged into the channel's config block
// in operator.json. Use for channel-specific settings (dmPolicy, allowFrom, etc.).
// +kubebuilder:pruning:PreserveUnknownFields
// +optional
ChannelConfig *runtime.RawExtension `json:"channelConfig,omitempty"`
```

**`SecretRef` changes from `*SecretRef` (single pointer) to `[]SecretRefEntry` (array).**
This is a breaking API change — existing CRs using `secretRef: {name: ..., key: ...}` must migrate to the array syntax. The controller code (`claw_credentials.go`, `claw_proxy.go`) accesses `cred.SecretRef.Name`/`cred.SecretRef.Key` directly and needs refactoring to iterate or index the array.

New type with an optional `role` discriminator for multi-secret channels:

```go
// SecretRefEntry references a specific key in a Secret.
type SecretRefEntry struct {
    Name string `json:"name"`
    Key  string `json:"key"`
    // Role distinguishes multiple secrets for the same credential.
    // Required when multiple secretRef entries are present (e.g., Slack botToken/appToken).
    // +optional
    Role string `json:"role,omitempty"`
}
```

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

### Channel Defaults

When `channel` is set and no explicit type/domain/config is provided:

| Channel | Inferred Type | Domain(s) | Companion Routes | Placeholder |
|---------|--------------|-----------|------------------|-------------|
| telegram | pathToken (prefix: `/bot`) | api.telegram.org | — | `"placeholder"` |
| discord | apiKey (header: `Authorization`, valuePrefix: `Bot `) | discord.com | gateway.discord.gg, cdn.discordapp.com | `"placeholder"` |
| slack | bearer | slack.com | .slack.com (WS) | `"xoxb-placeholder"` / `"xapp-placeholder"` |
| whatsapp | none | — | .whatsapp.com, .whatsapp.net | — |

### Channel Config Injection

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

### PLATFORM.md Updates

The AI skill document is updated to explain:
1. Channels with `channel:` field in the CR are **operator-managed** — do NOT run `openclaw channels add/remove`
2. WhatsApp: AI installs the `@openclaw/whatsapp` plugin; user does QR pairing
3. Custom/unknown channels not in the CR: AI and user manage them directly via CLI

## Implementation Plan

### PR 1: Declarative channel injection

1. Update `api/v1alpha1/claw_types.go`:
   - Add `Channel` and `ChannelConfig` fields to `CredentialSpec`
   - Make `Type` optional (add `+optional`, `omitempty`)
   - Change `SecretRef` from `*SecretRef` to `[]SecretRefEntry`
   - Add two structural CEL rules: (1) require `type` or `channel`, (2) `provider` and `channel` mutually exclusive
   - Remove type-specific CEL rules (e.g., `self.type != 'apiKey' || has(self.apiKey)`) — these become controller-side errors reported via `CredentialsResolved` status condition (matches existing `resolveProviderDefaults` pattern)
   - Run `make manifests && make generate`
   - Refactor all `cred.SecretRef.Name`/`cred.SecretRef.Key` usages in controller (helper function recommended)

2. New file: `internal/controller/claw_channels.go`
   - Channel defaults table (domain, type, companions, placeholder tokens)
   - `resolveChannelDefaults(cred *CredentialSpec)` — populate missing fields from channel
   - `injectChannelsIntoConfigMap(objects, instance)` — inject channels + plugins into operator.json
   - `generateCompanionRoutes(cred CredentialSpec)` — return companion proxy routes for multi-domain channels

3. Wire into reconciler (`claw_resource_controller.go`):
   - Call `resolveChannelDefaults` during credential resolution
   - Call `injectChannelsIntoConfigMap` after `injectModelCatalogIntoConfigMap`
   - Include companion routes in proxy config generation

4. Tests:
   - Unit: `internal/controller/claw_channels_test.go` — table-driven tests for each channel type, inference with/without overrides, companion route generation, channelConfig passthrough
   - E2E: channel credential → verify operator.json contains channel config, pod starts with channel active

### PR 2: Documentation

1. Update `docs/provider-setup.md` — simplify channel examples to use `channel:` field
2. Update PLATFORM.md template in `configmap.yaml` — new AI instructions for operator-managed channels
3. Update `docs/architecture.md` if needed
