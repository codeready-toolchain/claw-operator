# ADR-0006: Dynamic Model Catalog

**Status:** Implemented
**Date:** 2026-05-06

## Overview

The model picker list (`agents.defaults.models` in `openclaw.json`) was hardcoded in the ConfigMap seed, always showing the same set of models regardless of which providers the user actually configured via the Claw CR. This meant a Google-only deployment showed Claude models that fail when selected, an Anthropic-only deployment showed Gemini models that fail, and OpenRouter deployments showed none of their supported models.

This ADR covers the design for dynamically generating the model catalog in `operator.json` based on the providers configured in `spec.credentials`, including proper version numbers in all aliases.

## Design Principles

1. **Only show what works** — the UI model picker reflects the providers actually configured
2. **Operator-managed, not user-managed** — model catalogs belong in `operator.json` (rewritten every reconcile, operator keys win on merge) so they stay in sync with credentials
3. **Users can still customize** — users can add/rename models on the PVC via `openclaw config patch`; deep-merge preserves user keys that don't collide with operator keys
4. **Version numbers in aliases** — all aliases include the model version for clarity
5. **Primary model auto-selection** — the primary model is set from the first configured provider's first model; user's choice persists across restarts after first run

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Where to inject the model catalog | `operator.json` with primary-preserving merge | Follows the established pattern: operator-managed config in `operator.json`, user-owned config in `openclaw.json` seed. Deep-merge preserves user-added model entries. Small `merge.js` tweak preserves user's primary choice after first run. |
| 2 | How to define models per provider | Hardcoded Go map | Simple, type-safe, testable. No new CRD fields or external dependencies. Operator upgrades naturally bring new catalogs. Users who need custom models use `openclaw config patch`. |
| 3 | Model ID format across provider types | Catalog keyed by logical provider name, prefix derived at injection time | Single catalog entry for Anthropic covers both direct and Vertex paths. Model names don't need duplication. Handles `anthropic` and `anthropic-vertex` coexisting from the same catalog. |
| 4 | Default primary model selection | First configured provider wins | User controls priority implicitly via credential ordering in the CR. Simple, no extra config needed. |
| 5 | What remains in `openclaw.json` seed | Agents structure with workspace and list only | Clean separation: seed has only user-owned config (agent list, workspace). No stale model references. |
| 6 | Primary model on restart | Preserve existing primary in `merge.js` | First run gets the operator default; subsequent restarts keep the user's choice. Overwrite mode does a full reset. |
| 7 | How to surface model knowledge to the assistant | One comprehensive `PLATFORM.md` skill replacing `PROXY_SETUP.md` | No duplication of foundation knowledge. Assistant always has the full picture. Single file to maintain. `KUBERNETES.md` stays separate (dynamically generated). |

## Architecture

### Data Flow

```
configmap.yaml (embedded)
  ├── operator.json    ← operator-managed: gateway, models.providers (dynamic),
  │                      agents.defaults.models (dynamic),
  │                      agents.defaults.model.primary (dynamic)
  └── openclaw.json    ← user-owned seed: agents list, workspace path
                         (models section removed from seed)

merge.js at pod start (with primary-preserving tweak):
  1. Save PVC's existing agents.defaults.model.primary (if set)
  2. PVC openclaw.json = deepMerge(PVC openclaw.json, operator.json)
  3. Restore saved primary (so user's choice survives restarts)
```

By moving `agents.defaults.models` and `agents.defaults.model.primary` into `operator.json`, they become operator-managed and dynamically generated from configured credentials.

### Model Catalog Injection

A dedicated injection function in the reconciliation pipeline, called after provider injection, performs the following:

1. Iterates over credentials with `provider` set, skipping `pathToken` (same filters as provider injection)
2. Derives the provider key: `usesVertexSDK(cred)` produces `{provider}-vertex`, otherwise `{provider}`
3. Strips the `-vertex` suffix to get the logical provider name
4. Looks up a hardcoded Go map of known models for that logical provider — providers with no catalog entry (e.g., `openrouter`) are silently skipped
5. Emits model entries as `{providerKey}/{modelName}` with versioned aliases into `agents.defaults.models`
6. Sets `agents.defaults.model.primary` from the first credential's provider catalog

### Provider-to-Model Mapping

A hardcoded Go map defines known models per logical provider name. Catalog ordering matters: each provider's first model should be the best cost/performance option (not the most expensive flagship), since it becomes the default primary when that provider is first in the credentials list.

Providers not in the catalog (e.g., `openrouter`) are silently skipped. OpenRouter is a meta-provider whose model list is too dynamic to hardcode. Users add specific models via `openclaw config patch`.

This naturally handles both direct API and Vertex paths:
- `type: apiKey, provider: anthropic` → provider key `anthropic` → emits `anthropic/claude-sonnet-4-6`
- `type: gcp, provider: anthropic` → provider key `anthropic-vertex` → emits `anthropic-vertex/claude-sonnet-4-6`
- Both coexist if a user configures both paths

### Primary Model Selection

The first credential with `provider` set determines the primary model. The first model in that provider's catalog becomes `agents.defaults.model.primary`. The primary is only set on first run — `merge.js` preserves the user's choice on subsequent restarts.

### Deep-Merge Implications

Since `operator.json` is deep-merged into the PVC `openclaw.json`:

- `agents.defaults.models` merges into the PVC's existing models. Since `models` is an object (not array), deep-merge adds/overwrites keys but preserves user-added model entries that don't collide with operator-managed ones.
- `agents.defaults.model.primary` would normally overwrite the PVC value (it's a string), but `merge.js` saves and restores the existing primary before/after merge.

### The `openclaw.json` Seed

With models moved to `operator.json`, the seed retains only user-owned config:

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

### Comprehensive Integration Skill (`PLATFORM.md`)

`PROXY_SETUP.md` is refactored into a single comprehensive `PLATFORM.md` skill covering the full integration picture. This ensures the assistant always has context regardless of whether the user is asking about models, messaging, networking, or custom domains.

**Skill structure:**

```
PLATFORM.md
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
│   ├── WhatsApp, Telegram, Discord, Slack
│   └── Per-channel domain allowlisting and credential patterns
└── 5. Custom Domains
    └── type: none for arbitrary external services
```

`KUBERNETES.md` stays separate — it's dynamically generated with actual cluster/context details and only injected when kubernetes credentials are present.

## Future Considerations

- As new LLM providers and models are released, the hardcoded catalog map will need periodic updates via operator releases
- OpenRouter and similar meta-providers may eventually warrant dynamic model discovery, but the current escape hatch (`openclaw config patch`) is sufficient
- The primary-preserving tweak in `merge.js` could be generalized to preserve other user-chosen defaults if the need arises
