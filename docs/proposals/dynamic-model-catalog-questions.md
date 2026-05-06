**Status:** All decisions made
**Related:** [Design document](dynamic-model-catalog-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

## Q1: Where should the dynamic model catalog be injected?

The model catalog (`agents.defaults.models` and `agents.defaults.model.primary`) needs to land in the final `openclaw.json` on the PVC. There are two places the operator can write it during reconciliation.

### Option A: Inject into `operator.json` with primary-preserving merge

Write `agents.defaults.models` and `agents.defaults.model.primary` into `operator.json` during reconciliation. The init container's `merge.js` then deep-merges this into the PVC `openclaw.json` at pod start. Add a small `merge.js` enhancement: before merging, if the PVC already has a `primary` model set, preserve it (don't let the operator value overwrite the user's choice).

- **Pro:** Follows the established pattern — operator-managed config goes in `operator.json`, user-owned config goes in `openclaw.json` seed.
- **Pro:** Deep-merge means user-added model entries survive restarts.
- **Pro:** User's primary model choice persists across restarts. First run gets a sensible operator default.
- **Con:** Small `merge.js` change needed (preserve existing primary before merge).

**Decision:** Option A — inject into `operator.json` with a `merge.js` tweak to preserve the user's primary model choice if already set on the PVC.

_Considered and rejected: Option B — inject into `openclaw.json` seed (adding a provider later wouldn't update the model list without PVC wipe)._

## Q2: How should the model catalog be defined per provider?

The operator needs to know which model IDs and aliases to emit for each provider. This catalog will evolve as new models are released.

### Option A: Hardcoded Go map

A Go map in a dedicated file (`claw_models.go`) maps provider keys to slices of `{ID, Alias}` structs. Updated via code changes and operator releases.

- **Pro:** Simple, type-safe, testable. No new CRD fields or external dependencies.
- **Pro:** Operator upgrades naturally bring new model catalogs.
- **Con:** Adding a new model requires a code change, rebuild, and redeployment of the operator.

**Decision:** Option A — hardcoded Go map. Users who need custom models can add them via `openclaw config patch` (survives restarts in merge mode).

_Considered and rejected: Option B — CRD field (too much CRD complexity, users can already customize via PVC), Option C — hybrid (marginal benefit over A)._

## Q3: How should model IDs be formatted across provider types?

OpenClaw model IDs use the format `provider-key/model-name`. The provider key in the model ID must match the provider key in `models.providers` for routing to work. The complication: the same provider (e.g., Anthropic) can appear under different keys depending on the credential type:

- Direct API key: provider key = `anthropic` → model IDs = `anthropic/claude-sonnet-4-6`
- Vertex AI SDK: provider key = `anthropic-vertex` → model IDs = `anthropic-vertex/claude-sonnet-4-6`
- Google direct: provider key = `google` → model IDs = `google/gemini-3-flash-preview`
- Google via Vertex: provider key = `google` → model IDs = `google/gemini-3-flash-preview` (same key)

### Option B: Model catalog keyed by logical provider name, with prefix derived at injection time

The catalog maps logical names (`anthropic`, `google`) to model names (without prefix). At injection time, the code iterates over actual provider keys from the providers map, derives the logical name (strip `-vertex` suffix), looks up the catalog, and emits models as `{providerKey}/{modelName}`. This naturally supports having both direct API and Vertex paths for the same provider simultaneously.

- **Pro:** Single catalog entry for Anthropic covers both direct and Vertex paths.
- **Pro:** Easier to maintain — model names don't need duplication.
- **Con:** Slightly more complex injection logic to derive `{providerKey}/{modelName}`.

**Decision:** Option B — catalog keyed by logical provider name. Prefix derived from provider key at injection time. Handles both `anthropic` and `anthropic-vertex` coexisting from the same catalog entry.

_Considered and rejected: Option A — keyed by provider key (requires duplicating model lists for direct vs Vertex paths of the same provider)._

## Q4: How should the default primary model be selected?

When multiple providers are configured, the operator needs to pick one model as `agents.defaults.model.primary`.

### Option B: First configured provider wins

Use the first credential with `provider` set in the CR's `credentials` array. Pick the first model from that provider's catalog as the primary.

- **Pro:** Simple — no extra logic or config. User controls priority implicitly via ordering in the CR.
- **Pro:** Users who list Anthropic first probably want Claude as default — this honors that intent.

**Decision:** Option B — first configured provider's first model becomes the primary. Simple and gives users implicit control.

_Considered and rejected: Option A — fixed preference order (opinionated, ignores user intent), Option C — CRD field (unnecessary complexity for a first-run default)._

## Q5: What should remain in the `openclaw.json` seed after removing models?

Currently the seed contains `agents.defaults` (model, models, workspace) and `agents.list`. With models moving to `operator.json`, the question is what stays.

### Option A: Keep agents structure with workspace and list only

Seed retains only user-owned config: agent list, workspace path. Models and primary model removed (now in `operator.json`).

- **Pro:** Clean separation — the seed only has user-owned config that the operator never touches.
- **Pro:** No risk of stale model references in the seed.

**Decision:** Option A — seed keeps only user-owned config (agents list, workspace).

_Considered and rejected: Option B — remove seed entirely (would make agents list and workspace operator-managed, losing user customizations)._

## Q6: Should the operator overwrite the primary model on every restart?

Resolved by Q1. The operator injects `agents.defaults.model.primary` into `operator.json`, but `merge.js` preserves the PVC's existing primary if one is already set. First run gets the operator's default; subsequent restarts keep the user's choice.

**Decision:** Preserve existing primary in `merge.js` (decided as part of Q1).

## Q7: How should model management knowledge be surfaced to the OpenClaw assistant?

The assistant needs to understand that models are operator-managed and tied to providers in the Claw CR. Without this, it might try to directly edit `openclaw.json` or give incorrect guidance when users ask about adding/changing models.

### Option D: One comprehensive integration skill (`PLATFORM.md`)

Refactor `PROXY_SETUP.md` into a single comprehensive skill (`PLATFORM.md`) that covers the full integration picture: OpenShift security model, proxy architecture, Claw CR as the single configuration point, LLM providers/models, and messaging channels. `KUBERNETES.md` stays separate because it's dynamically generated with actual cluster/context details.

The skill must be well-structured with clear sections so the assistant can quickly find relevant guidance regardless of whether the user is asking about models, messaging, or networking.

- **Pro:** No duplication of foundation knowledge (proxy, Claw CR, security model).
- **Pro:** Assistant always has the full picture when any integration question comes up.
- **Pro:** Single file to maintain for all static integration content.
- **Con:** Longer file (~350-400 lines), but well-structured with clear sections.

**Decision:** Option D — one comprehensive `PLATFORM.md` skill replacing `PROXY_SETUP.md`. Covers foundation (OpenShift, proxy, Claw CR) + all static integration domains (models/providers, messaging). `KUBERNETES.md` remains separate (dynamic). Proper structure is critical — the skill needs clear sections and headings so the assistant can navigate it efficiently.

_Considered and rejected: Option A — extend PROXY_SETUP.md (naming becomes misleading), Option B — separate MODEL_SETUP.md (foundation duplication, assistant might miss context), Option C — AGENTS.md (user-owned, never updated)._
