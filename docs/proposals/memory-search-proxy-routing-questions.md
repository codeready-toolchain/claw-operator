# Memory Search Proxy Routing — Design Questions

**Status:** Resolved — all decisions made
**Related:** [Design document](memory-search-proxy-routing-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

---

## Q1: Provider selection — which credential becomes the embedding provider?

Users can configure multiple credentials (e.g., Anthropic for chat + OpenAI for embeddings, or Google + OpenAI). The operator needs to decide which one to use for memory search embeddings.

### Option A: First embedding-capable credential wins

Scan `spec.credentials` in order. The first credential whose provider is in the `embeddingProviders` set becomes the memory search provider.

- **Pro:** Simple, deterministic, no new API surface
- **Pro:** Matches the existing "first credential is primary model" pattern used for model catalog
- **Con:** User can't control embedding provider independently of credential order without reordering

**Decision:** Option A — consistent with existing primary model selection pattern. Users who care can reorder credentials.

_Considered and rejected: Option B (hardcoded OpenAI preference — surprising behavior), Option C (new CRD field — premature API surface growth)_

---

## Q2: Config merge semantics — what happens when the user already has memorySearch in their raw config?

The user might specify `agents.defaults.memorySearch` in `spec.config.raw` for custom behavior (e.g., local embeddings, a specific model, or `enabled: false`).

### Option A: Skip injection if user has any memorySearch config

If the user's raw config already has `agents.defaults.memorySearch` (at any depth), the operator does not inject anything.

- **Pro:** Cleanest escape hatch — user is fully in control
- **Pro:** Matches the "don't fight the user" principle
- **Con:** User might set `memorySearch.extraPaths` but still want the operator to pick the provider/baseUrl

**Decision:** Option A — if the user specifies memorySearch in their raw config, they own it completely. Simple and safe escape hatch.

_Considered and rejected: Option B (always-win operator injection — user can't override), Option C (deep-merge with user override — complex, accident-prone)_

---

## Q3: Which OpenClaw embedding adapter ID to use?

OpenClaw's memory search has multiple adapter IDs: `openai`, `gemini`, `openai-compatible`, `mistral`, `voyage`, etc. When routing through our proxy, we need to decide what adapter to tell OpenClaw to use.

### Option B: Use provider-native adapter IDs with proxy baseUrl

Map each provider to its native adapter:
- OpenAI credential → `provider: "openai"`, rely on `models.providers.openai.apiKey` (already set as placeholder)
- Google credential → `provider: "gemini"`, rely on `models.providers.google.apiKey`

- **Pro:** OpenClaw handles model defaults, API format, and error handling natively for each provider
- **Pro:** No need to specify model — OpenClaw picks the provider's default embedding model
- **Pro:** Gemini's non-OpenAI-compatible embedding API works correctly
- **Con:** Requires knowing the mapping between our credential provider names and OpenClaw's adapter IDs
- **Con:** Must verify that OpenClaw's adapter reads apiKey from the models.providers.<id> config (which we already inject)

**Decision:** Option B — use provider-native adapter IDs. The `injectProviders` function already writes `models.providers.<id>` with baseUrl and apiKey, which OpenClaw's memory search reads from. We just set the provider name. Can expand to hybrid (Option C) later if custom providers need support.

_Considered and rejected: Option A (always openai-compatible — breaks Gemini's non-standard API), Option C (hybrid with openai-compatible fallback for custom — deferred, not needed initially)_

---

## Q4: Behavior when no embedding-capable provider is configured

The user might only have Anthropic (no embeddings) or xAI. What should the operator do?

### Option C: Inject `memorySearch.enabled: false` only if user has no raw memorySearch config

Disable only when there's no embedding provider AND the user hasn't configured memorySearch themselves.

- **Pro:** Safe default while preserving user escape hatch
- **Pro:** No runtime noise for the common "Anthropic-only" case
- **Con:** Slightly more complex conditional logic

**Decision:** Option C — disable memory search explicitly when no embedding provider exists and the user hasn't configured their own. Eliminates noisy runtime errors while preserving the Q2 escape hatch.

_Considered and rejected: Option A (do nothing — noisy errors confuse users), Option B (always disable — blocks custom provider escape hatch)_

---

## Q5: Should custom providers (spec.customProviders) be eligible for memory search?

Custom providers have a `baseUrl` and optionally an `api` field. They could potentially serve embeddings.

### Option C: No automatic selection, but allow via `spec.config.raw`

Custom providers are not auto-selected for memory search, but users can point to them via their raw config (which triggers the Q2 skip behavior).

- **Pro:** Safe default, user has full control
- **Pro:** Already works with Q2's "skip if user has memorySearch config" behavior
- **Con:** User has to manually configure it

**Decision:** Option C — custom providers are not auto-selected. Users with custom embedding endpoints configure via `spec.config.raw`, which triggers the Q2 escape hatch. Can expand to auto-selection (Option B) later if there's demand.

_Considered and rejected: Option A (same behavior but less explicit about escape hatch), Option B (auto-select OpenAI-compatible custom providers — risks false positives, deferred)_
