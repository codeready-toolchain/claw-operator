# Dynamic Model Catalog

**Status:** Final

## Overview

Today the model picker list (`agents.defaults.models` in `openclaw.json`) is hardcoded in the ConfigMap seed. It always shows the same set of models regardless of which providers the user has actually configured via the Claw CR. This means:

- A Google-only deployment shows Claude models that fail when selected
- An Anthropic-only deployment shows Gemini models that fail when selected
- An OpenRouter deployment shows none of its supported models
- Model aliases lack version numbers, making them ambiguous across generations

This proposal dynamically generates the model catalog in `operator.json` based on the providers actually configured in `spec.credentials`, and includes proper version numbers in all aliases.

## Design Principles

1. **Only show what works** — the UI model picker should reflect the providers actually configured
2. **Operator-managed, not user-managed** — model catalogs belong in `operator.json` (rewritten every reconcile, operator keys win on merge) so they stay in sync with credentials
3. **Users can still customize** — users can add/rename models on the PVC via `openclaw config patch`; deep-merge preserves user keys that don't collide with operator keys
4. **Version numbers in aliases** — all aliases include the model version for clarity
5. **Primary model auto-selection** — the primary model is set from the first configured provider's first model; user's choice persists across restarts after first run

## Architecture

### Current flow

```
configmap.yaml (embedded)
  ├── operator.json    ← operator-managed: gateway, models.providers (dynamic)
  └── openclaw.json    ← user-owned seed: agents.defaults.models (HARDCODED)

merge.js at pod start:
  PVC openclaw.json = deepMerge(PVC openclaw.json, operator.json)
```

The problem: `agents.defaults.models` lives in `openclaw.json` (user-owned seed), so it's static. `operator.json` dynamically builds `models.providers` but doesn't touch `agents.defaults`.

### Proposed flow

```
configmap.yaml (embedded)
  ├── operator.json    ← operator-managed: gateway, models.providers (dynamic),
  │                      agents.defaults.models (dynamic),
  │                      agents.defaults.model.primary (dynamic)
  └── openclaw.json    ← user-owned seed: agents list, workspace path
                         (models section REMOVED from seed)

merge.js at pod start (with primary-preserving tweak):
  1. Save PVC's existing agents.defaults.model.primary (if set)
  2. PVC openclaw.json = deepMerge(PVC openclaw.json, operator.json)
  3. Restore saved primary (so user's choice survives restarts)
```

By moving `agents.defaults.models` and `agents.defaults.model.primary` into `operator.json`, they become operator-managed and dynamically generated from configured credentials. The `merge.js` tweak ensures the user's primary model choice persists after first run.

### New function: `injectModelCatalogIntoConfigMap`

A new injection function in the reconciliation pipeline, called after `injectProvidersIntoConfigMap`. It iterates over `instance.Spec.Credentials` directly (same source as `injectProvidersIntoConfigMap`) rather than parsing the JSON back from the ConfigMap:

1. Iterates over credentials with `provider` set, skipping `pathToken` (same filters as `injectProvidersIntoConfigMap`)
2. Derives the provider key using the same logic: `usesVertexSDK(cred)` → `cred.Provider + "-vertex"`, else `cred.Provider`
3. Derives the logical provider name from the key (strips `-vertex` suffix)
4. Looks up a hardcoded Go map of known models for that logical provider. Providers with no catalog entry (e.g., `openrouter`) are silently skipped — no models emitted
5. Emits model entries as `{providerKey}/{modelName}` with versioned aliases into `agents.defaults.models`
6. Sets `agents.defaults.model.primary` from the first credential's provider catalog (first model of the first provider that has a catalog entry)

### Provider-to-model mapping

A hardcoded Go map in `internal/controller/claw_models.go` defines known models per logical provider name. Model names are stored without prefix — the prefix is derived from the provider key at injection time.

```go
var modelCatalog = map[string][]modelEntry{
    "google": {
        {Name: "gemini-3-flash-preview", Alias: "Gemini 3 Flash"},
        {Name: "gemini-3.1-pro-preview", Alias: "Gemini 3.1 Pro"},
        {Name: "gemini-3.1-flash-lite-preview", Alias: "Gemini 3.1 Flash Lite"},
    },
    "anthropic": {
        {Name: "claude-opus-4-7", Alias: "Claude Opus 4.7"},
        {Name: "claude-opus-4-6", Alias: "Claude Opus 4.6"},
        {Name: "claude-sonnet-4-6", Alias: "Claude Sonnet 4.6"},
        {Name: "claude-haiku-4-5", Alias: "Claude Haiku 4.5"},
    },
    "openai": {
        {Name: "gpt-5.5", Alias: "GPT-5.5"},
        {Name: "gpt-5.4", Alias: "GPT-5.4"},
        {Name: "gpt-5.4-mini", Alias: "GPT-5.4 Mini"},
        {Name: "o3", Alias: "o3"},
        {Name: "o4-mini", Alias: "o4 Mini"},
    },
    "xai": {
        {Name: "grok-4.3", Alias: "Grok 4.3"},
        {Name: "grok-4.20-reasoning", Alias: "Grok 4.20"},
    },
}
```

Providers not in the catalog (e.g., `openrouter`) are silently skipped — no models are emitted for them. OpenRouter is a meta-provider that routes to many upstream models; its model list is too dynamic to hardcode. Users can add `openrouter/auto` or specific models via `openclaw config patch`.

This naturally handles both direct API and Vertex paths:
- Credential `type: apiKey, provider: anthropic` → provider key `anthropic` → emits `anthropic/claude-sonnet-4-6`
- Credential `type: gcp, provider: anthropic` → provider key `anthropic-vertex` → logical name `anthropic` → emits `anthropic-vertex/claude-sonnet-4-6`
- Both can coexist if a user configures both paths

### Primary model selection

The first credential with `provider` set in the CR's `credentials` array determines the primary model. The first model in that provider's catalog becomes `agents.defaults.model.primary`.

This gives users implicit control via ordering in the CR. If they list Anthropic first, Claude is the default. The primary is only set on first run — `merge.js` preserves the user's choice on subsequent restarts.

### The `openclaw.json` seed

The hardcoded models and primary model are removed. The seed retains only user-owned config:

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.openclaw/workspace"
    },
    "list": [
      {
        "id": "default",
        "name": "OpenClaw Assistant",
        "workspace": "~/.openclaw/workspace"
      }
    ]
  }
}
```

### Deep-merge implications

Since `operator.json` is deep-merged into the PVC `openclaw.json`:

- `agents.defaults.models` merges **into** the PVC's existing models. Since `models` is an object (not array), deep-merge adds/overwrites keys but **preserves** user-added model entries that don't collide with operator-managed ones.
- `agents.defaults.model.primary` would normally overwrite the PVC value (it's a string), but `merge.js` saves and restores the existing primary before/after merge. First run gets the operator default; subsequent restarts keep the user's choice.

### Comprehensive Integration Skill (`PLATFORM.md`)

The current `PROXY_SETUP.md` skill is refactored into a single comprehensive `PLATFORM.md` skill that covers the full integration picture. This ensures the assistant always has the context it needs regardless of whether the user is asking about models, messaging, networking, or custom domains.

**Why one skill:** The foundation knowledge (OpenShift security, proxy architecture, Claw CR as the config mechanism) is needed for every integration domain. Splitting into separate skills risks the assistant loading a domain skill without the foundation. One well-structured skill eliminates duplication and guarantees full context.

**Skill structure (critical — must be well-organized for efficient navigation):**

```
PLATFORM.md
├── Frontmatter (name, description)
├── 1. Platform Overview
│   ├── OpenShift security model (non-root, SCC, capabilities)
│   ├── Claw CR as the single configuration point
│   └── Important: oc apply replaces the entire credentials list
├── 2. Proxy Architecture
│   ├── How outbound traffic flows through the MITM proxy
│   ├── Domain allowlisting and credential injection
│   └── Pre-allowed domains (clawhub.ai, npm, etc.)
├── 3. LLM Providers & Models
│   ├── How providers map to models in the picker
│   ├── Models are operator-managed, updated when providers change
│   ├── User customization via openclaw config patch
│   └── Primary model is user-owned after first run
├── 4. Messaging Channels
│   ├── WhatsApp (QR pairing, domain allowlisting, plugin setup)
│   ├── Telegram (pathToken injection)
│   ├── Discord (apiKey with Bot prefix)
│   └── Slack (dual-token bearer with allowedPaths)
└── 5. Custom Domains
    └── type: none for arbitrary external services
```

**Changes from `PROXY_SETUP.md`:**
- Rename `PROXY_SETUP.md` → `PLATFORM.md` in ConfigMap, `merge.js`, and skill path (`skills/platform/SKILL.md`)
- Add new section 3 (LLM Providers & Models) with model management guidance
- Restructure existing content into the section layout above
- Update frontmatter name/description to reflect broader scope

**`KUBERNETES.md` stays separate** — it's dynamically generated with actual cluster/context details and only injected when kubernetes credentials are present.

## Implementation Plan

### Phase 1: Dynamic model catalog (core feature)

1. **Add model catalog map** — new Go file `internal/controller/claw_models.go` with provider-to-model mappings (keyed by logical provider name, model names without prefix) and a `modelEntry` struct type. Also add `xai` to `knownProviders` in `claw_credentials.go` (validation only — xAI uses `type: bearer` with explicit `domain: "api.x.ai"`, same pattern as OpenAI). Update the hardcoded error message in `resolveCredentials` that lists known providers
2. **Add `injectModelCatalogIntoConfigMap`** — new function following the same pattern as `injectProvidersIntoConfigMap`. Iterates over `instance.Spec.Credentials` directly, derives provider key (same logic as `injectProvidersIntoConfigMap`), strips `-vertex` suffix for logical provider name, looks up catalog, emits `{providerKey}/{modelName}` entries into `agents.defaults.models` in `operator.json`. Sets `agents.defaults.model.primary` from the first provider with a catalog entry
3. **Wire into reconciliation** — call after `injectProvidersIntoConfigMap` in the Phase 3 pipeline
4. **Update `openclaw.json` seed** — remove hardcoded models and primary model, keep only user-owned config (agents list, workspace)
5. **Update `merge.js`** — add primary-preserving logic: before deep-merge, save the PVC's existing `agents.defaults.model.primary` and restore it after merge if it was set
6. **Add tests** — unit tests for the new function, update existing integration tests

### Phase 2: Refactor `PROXY_SETUP.md` → `PLATFORM.md` (skill consolidation)

This is a critical step — the skill structure must be well-organized for the assistant to navigate efficiently.

7. **Write `PLATFORM.md` content** — restructure existing `PROXY_SETUP.md` into the section layout described above (Platform Overview, Proxy Architecture, LLM Providers & Models, Messaging Channels, Custom Domains). Add the new "LLM Providers & Models" section with model management guidance
8. **Update ConfigMap** — rename `PROXY_SETUP.md` key to `PLATFORM.md` in `configmap.yaml`
9. **Update `merge.js`** — change the `copyAlways` call to copy `PLATFORM.md` to `skills/platform/SKILL.md` instead of `skills/proxy/SKILL.md`
10. **Update controller references** — specifically: `claw_resource_controller_test.go` (test `"should have PROXY_SETUP.md skill content"` checks the ConfigMap key name and content assertions)

### Phase 3: Documentation

11. **Update `docs/provider-setup.md`** — add OpenAI and xAI provider setup instructions (Secret creation, Claw CR example with `type: bearer`). Review existing provider sections for accuracy given the new dynamic model catalog (e.g., users no longer need to manually configure model aliases — the operator handles it)
12. **Update CLAUDE.md** — document the new model catalog behavior, the `PLATFORM.md` skill, the merge.js primary-preserving logic, and the updated ConfigMap keys
