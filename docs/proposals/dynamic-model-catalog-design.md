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
// Order matters: the first model becomes the default primary when this provider
// is the first configured credential in the Claw CR. Lead with the best
// cost/performance model, not the most expensive.
var modelCatalog = map[string][]modelEntry{
    "google": {
        {Name: "gemini-3-flash-preview", Alias: "Gemini 3 Flash"},
        {Name: "gemini-3.1-pro-preview", Alias: "Gemini 3.1 Pro"},
        {Name: "gemini-3.1-flash-lite-preview", Alias: "Gemini 3.1 Flash Lite"},
    },
    "anthropic": {
        {Name: "claude-sonnet-4-6", Alias: "Claude Sonnet 4.6"},
        {Name: "claude-opus-4-7", Alias: "Claude Opus 4.7"},
        {Name: "claude-opus-4-6", Alias: "Claude Opus 4.6"},
        {Name: "claude-haiku-4-5", Alias: "Claude Haiku 4.5"},
    },
    "openai": {
        {Name: "gpt-5.4", Alias: "GPT-5.4"},
        {Name: "gpt-5.5", Alias: "GPT-5.5"},
        {Name: "gpt-5.4-mini", Alias: "GPT-5.4 Mini"},
        {Name: "o3", Alias: "o3"},
        {Name: "o4-mini", Alias: "o4 Mini"},
    },
    "xai": {
        {Name: "grok-4.20-beta-latest-reasoning", Alias: "Grok 4.20"},
        {Name: "grok-4.3", Alias: "Grok 4.3"},
    },
}
```

Providers not in the catalog (e.g., `openrouter`) are silently skipped — no models are emitted for them. OpenRouter is a meta-provider that routes to many upstream models; its model list is too dynamic to hardcode. Users can add `openrouter/auto` or specific models via `openclaw config patch`.

This naturally handles both direct API and Vertex paths:
- Credential `type: apiKey, provider: anthropic` → provider key `anthropic` → emits `anthropic/claude-sonnet-4-6`
- Credential `type: gcp, provider: anthropic` → provider key `anthropic-vertex` → logical name `anthropic` → emits `anthropic-vertex/claude-sonnet-4-6`
- Both can coexist if a user configures both paths

### Primary model selection

The first credential with `provider` set in the CR's `credentials` array determines the primary model. The first model in that provider's catalog becomes `agents.defaults.model.primary`. This means **catalog ordering matters** — each provider's first model should be the best cost/performance option (not the most expensive flagship).

This gives users implicit control via ordering in the CR. If they list Anthropic first, Claude Sonnet is the default. The primary is only set on first run — `merge.js` preserves the user's choice on subsequent restarts.

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

## Testing Strategy

### Current coverage gaps

Today's tests verify ConfigMap *shape* (keys exist, init container command is `node /config/merge.js`, env var is `merge`) but never run `merge.js` or validate the merged JSON output. This means:

- The deep-merge algorithm (`objects merge, arrays/primitives replace`) is untested
- Operator-wins-on-conflict semantics are only documented in ADR-0004, never asserted
- User key preservation across restarts is unverified
- The proposed primary-preserving tweak would be entirely untested without new coverage

### Test plan

All merge logic tests can be implemented as **Go unit tests** by extracting `merge.js` from the embedded ConfigMap and running it with `node` via `exec.Command`. No e2e changes are needed — the merge script is pure Node.js with `fs` operations that can be driven from a temp directory.

#### Layer 1: Go unit tests for `injectModelCatalogIntoConfigMap` (new, `claw_configmap_test.go`)

Pattern: same as `TestInjectProvidersIntoConfigMap` — build unstructured ConfigMap, call function, assert JSON.

| Test case | What it verifies |
|---|---|
| Single provider (google) emits correct model entries | `google/gemini-3-flash-preview` with alias, correct count |
| Multiple providers emit models for each | google + anthropic credentials → both provider model sets |
| Vertex credential emits `{provider}-vertex/` prefix | GCP anthropic → `anthropic-vertex/claude-sonnet-4-6` |
| Both direct and vertex anthropic coexist | apiKey + GCP → both `anthropic/` and `anthropic-vertex/` models |
| Primary set from first provider's first model | google first → `google/gemini-3-flash-preview` as primary |
| Primary set from first provider with catalog | openrouter first (no catalog), anthropic second → anthropic's first model |
| No providers → no models, no primary | passthrough-only credentials |
| pathToken credentials skipped | same as provider injection filter |
| Provider not in catalog (openrouter) silently skipped | no models emitted, no error |
| Preserves existing operator.json sections | gateway config untouched after injection |

#### Layer 2: `merge.js` unit tests (new, `claw_merge_test.go`)

These tests extract `merge.js` from the embedded ConfigMap and run it via `node` in a temp directory that simulates the PVC and ConfigMap mount. Each test:

1. Creates a temp directory with `/config/` (ConfigMap mount) and `/home/node/.openclaw/` (PVC)
2. Writes `merge.js`, `operator.json`, `openclaw.json`, `AGENTS.md`, and `PROXY_SETUP.md` to `/config/`
3. Optionally writes an existing `openclaw.json` to the PVC path (for restart scenarios)
4. Runs `node merge.js` with appropriate `CLAW_CONFIG_MODE` env
5. Reads and parses the resulting PVC `openclaw.json`

Skip condition: tests are skipped if `node` is not available (`exec.LookPath("node")`).

| Test case | Setup | What it verifies |
|---|---|---|
| **First run (merge mode, no PVC file)** | Empty PVC, operator.json has models + primary, seed has agents.list | Result = `deepMerge(seed, operator)`: has both agents.list (from seed) and models (from operator) |
| **Restart with existing PVC (merge mode)** | PVC has user-modified agents + extra keys, operator.json has models | Result = `deepMerge(PVC, operator)`: operator keys win, user's extra keys preserved |
| **Operator keys win on conflict** | PVC has `models.providers.google.baseUrl = "old"`, operator has `"new"` | Result has operator's `"new"` value |
| **User keys preserved (no collision)** | PVC has `plugins.entries.slack.enabled = true`, operator has no `plugins` | Result preserves `plugins.entries.slack.enabled` |
| **Arrays replaced, not merged** | PVC has `gateway.trustedProxies = ["1.1.1.1"]`, operator has `["10.0.0.0/8"]` | Result has only operator's array |
| **Overwrite mode ignores PVC** | PVC has user modifications, `CLAW_CONFIG_MODE=overwrite` | Result = `deepMerge(seed, operator)`: PVC user changes gone |
| **Invalid PVC JSON falls back to seed** | PVC has `{invalid json`, merge mode | Result = `deepMerge(seed, operator)` (fallback logged) |
| **Primary preserved on restart** | PVC has `agents.defaults.model.primary = "anthropic/claude-opus-4-7"`, operator has `primary = "google/gemini-3-flash-preview"` | Result keeps user's `"anthropic/claude-opus-4-7"` (primary-preserving tweak) |
| **Primary set on first run** | No PVC file, operator has `primary = "google/gemini-3-flash-preview"` | Result has `"google/gemini-3-flash-preview"` |
| **Primary preserved even in overwrite mode** | This is a design decision: in overwrite mode, primary is NOT preserved (full reset) | Result has operator's primary |
| **Model entries merge (object merge)** | PVC has `agents.defaults.models["custom/my-model"] = {alias: "Custom"}`, operator has google models | Result has both custom and google models |
| **Seed files seeded correctly** | Empty PVC | `AGENTS.md` seeded to workspace, `PROXY_SETUP.md` copied to skills |
| **Seed files not overwritten** | PVC already has modified `AGENTS.md` | `AGENTS.md` not replaced (`seedIfMissing`), skill files always replaced (`copyAlways`) |

#### Layer 3: Existing test updates

| File | Change |
|---|---|
| `claw_configmap_test.go` / `TestOpenClawDynamicProviders` | Add subtest: after reconcile with credentials, verify `operator.json` contains `agents.defaults.models` and `agents.defaults.model.primary` alongside `models.providers` |
| `claw_resource_controller_test.go` / `TestClawConfigMapController` | Update `"should have openclaw.json seed"` subtest: assert models and primary are **absent** from seed (moved to operator.json) |
| E2E `"should create claw-config ConfigMap with deep-merge config"` | Add assertion: `operator.json` contains `"agents"` section with `"defaults"` (model catalog). Update `openclaw.json` assertion: seed should NOT contain `"models"` key |

#### Why not full e2e for merge?

Running `merge.js` via `node` in Go unit tests gives us:
- **Real execution** of the actual merge script (not a re-implementation)
- **Fast feedback** (no Kind cluster needed)
- **All scenarios covered** (first run, restart, overwrite, primary preservation)
- **CI-friendly** (node is available in CI; skipped gracefully otherwise)

E2e tests would require waiting for pods to start, exec-ing in to read PVC files, and managing pod lifecycle — all for testing JavaScript merge logic that runs identically in a temp directory. The existing e2e tests adequately cover the *wiring* (init container command, env vars, ConfigMap shape), while the new Go+Node tests cover the *logic*.

## Implementation Plan

Each phase = one PR. Phases are ordered so each PR is independently shippable and reviewable.

### Phase 1: `merge.js` test coverage (safety net)

Adds Layer 2 tests (`claw_merge_test.go`) for existing `merge.js` behavior — zero code changes, pure test addition. This establishes a regression safety net before any merge logic changes land.

1. **Add `claw_merge_test.go`** — extract `merge.js` from the embedded ConfigMap and run it via `node` in temp directories. Cover the 9 existing-behavior test cases from Layer 2: first run, restart with existing PVC, operator keys win, user keys preserved, arrays replaced, overwrite mode, invalid JSON fallback, seed file behavior (both `seedIfMissing` and `copyAlways`)
2. **CI verification** — confirm `node` is available on `ubuntu-latest` runners (pre-installed; skip condition is a local-dev safety net only)

### Phase 2: Dynamic model catalog (core feature)

The main feature PR. Depends on Phase 1's safety net being merged first.

3. **Add model catalog map** — new Go file `internal/controller/claw_models.go` with provider-to-model mappings (keyed by logical provider name, model names without prefix) and a `modelEntry` struct type. Also add `xai` to `knownProviders` in `claw_credentials.go` (validation only — xAI uses `type: bearer` with explicit `domain: "api.x.ai"`, same pattern as OpenAI). Update the hardcoded error message in `resolveCredentials` that lists known providers
4. **Add `injectModelCatalogIntoConfigMap`** — new function following the same pattern as `injectProvidersIntoConfigMap`. Iterates over `instance.Spec.Credentials` directly, derives provider key (same logic as `injectProvidersIntoConfigMap`), strips `-vertex` suffix for logical provider name, looks up catalog, emits `{providerKey}/{modelName}` entries into `agents.defaults.models` in `operator.json`. Sets `agents.defaults.model.primary` from the first provider with a catalog entry
5. **Wire into reconciliation** — call after `injectProvidersIntoConfigMap` in the Phase 3 pipeline
6. **Update `openclaw.json` seed** — remove hardcoded models and primary model, keep only user-owned config (agents list, workspace)
7. **Update `merge.js`** — add primary-preserving logic: before deep-merge, save the PVC's existing `agents.defaults.model.primary` and restore it after merge if it was set
8. **Add tests** — Layer 1 unit tests for `injectModelCatalogIntoConfigMap` (10 cases in `claw_configmap_test.go`), new primary-preservation `merge.js` tests (4 cases added to `claw_merge_test.go`), and Layer 3 existing test updates (`TestOpenClawDynamicProviders`, `TestClawConfigMapController`, e2e assertions)
9. **Update `CLAUDE.md`** — document the new model catalog behavior, the merge.js primary-preserving logic, and the updated ConfigMap keys

### Phase 3: Skill refactor and documentation

Skill consolidation (`PROXY_SETUP.md` → `PLATFORM.md`) and provider setup docs. No dependency on Phase 2 ordering, but logically follows it since the new "LLM Providers & Models" section references the dynamic model catalog.

10. **Write `PLATFORM.md` content** — restructure existing `PROXY_SETUP.md` into the section layout described above (Platform Overview, Proxy Architecture, LLM Providers & Models, Messaging Channels, Custom Domains). Add the new "LLM Providers & Models" section with model management guidance
11. **Update ConfigMap** — rename `PROXY_SETUP.md` key to `PLATFORM.md` in `configmap.yaml`
12. **Update `merge.js`** — change the `copyAlways` call to copy `PLATFORM.md` to `skills/platform/SKILL.md` instead of `skills/proxy/SKILL.md`
13. **Update controller references and tests** — specifically: `claw_resource_controller_test.go` (test `"should have PROXY_SETUP.md skill content"` checks the ConfigMap key name and content assertions)
14. **Update `docs/provider-setup.md`** — add OpenAI and xAI provider setup instructions (Secret creation, Claw CR example with `type: bearer`). Review existing provider sections for accuracy given the new dynamic model catalog (e.g., users no longer need to manually configure model aliases — the operator handles it)
15. **Update `CLAUDE.md`** — document the `PLATFORM.md` skill (rename, new structure, new section)
