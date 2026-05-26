# Design Questions: `spec.config`

**Status:** Final — all 8 decisions resolved
**Related:** [Design document](spec-config-design.md)

Each question has options with trade-offs and a recommendation. Go through them
one by one to form the design, then update the design document.

---

## Q1: Where does user config get injected?

User-provided config from `spec.config.raw` needs to end up in the gateway's
ConfigMap. Currently the ConfigMap has two config files: `operator.json`
(operator-managed, enriched at reconcile time) and `openclaw.json` (seed for
first run). The question is where user config fits in this two-file model.

### Option A: Merge user config into `operator.json`

User config is deep-merged into `operator.json` before the enrichment pipeline
runs. `operator.json` then contains both user keys and operator keys.

- **Pro:** Minimal structural change — ConfigMap keeps two files, `merge.js`
  unchanged. User keys in `operator.json` "win" on restart (same as operator
  keys today), which is the correct behavior for CR-declared config.
- **Pro:** Enrichment functions can check "did the user set this key?" by
  inspecting the config map after the user merge but before their injection.
- **Con:** `operator.json` is no longer purely operator-managed — its name
  becomes slightly misleading. But this is cosmetic.

**Decision:** Option A — simplest change, correct precedence, keeps two-file
model.

_Considered and rejected: Option B — third file adds three-way merge complexity
for marginal benefit. Option C — user config in seed only applies on first run,
violating user expectations._

---

## Q2: Should `spec.configMode` move inside `spec.config`?

Currently `spec.configMode` is a top-level field on `ClawSpec`. With the new
`spec.config` struct, it could move inside to keep config concerns together.

### Option B: Move to `spec.config.mergeMode`

- **Pro:** Groups all config concerns under `spec.config`.
- **Pro:** Pre-release API (`v1alpha1`), no real users yet — breaking changes
  are free.

**Decision:** Option B — groups config concerns cleanly, no backward compat
cost.

_Considered and rejected: Option A — keeping top-level avoids churn but leaves
config concerns split for no reason. Option C — deprecation path unnecessary
with no real users._

---

## Q3: Which enrichment keys are "always operator wins" vs "user can override"?

The enrichment pipeline injects ~15 distinct keys into `operator.json`. With
user config support, each key needs a policy: does the operator always write it
(current behavior), or does it skip when the user already set it?

The overarching principle: **operator-managed infrastructure works OOTB no
matter what. User config adds to it, never silently disables it.** This leads
to a three-tier model rather than a simple always-win / skip-if-set split.

### Option B: Three-tier split by category

**Tier 1 — Always-win (operator sets unconditionally, user can't override):**

Keys driven by typed CRD fields. Operator is the sole source of truth.

- `gateway.mode`, `gateway.bind`, `gateway.port`, `gateway.controlUi.enabled`
  — infrastructure, must match pod networking
- `gateway.auth.*`, `gateway.controlUi.dangerouslyDisableDeviceAuth`
  — security, driven by `spec.auth`
- `models.providers` — proxy-dependent, driven by `spec.credentials`
- `channels.*`, `plugins.entries.<channel>` — driven by
  `spec.credentials[].channel`
- `mcp.servers` — driven by `spec.mcpServers`
- `tools.web.*`, `plugins.entries.<search>` — driven by `spec.webSearch`

**Tier 2 — Append/merge (operator always provides its part, user extends):**

Operator infra is always present. User entries are merged or appended on top.

- `gateway.controlUi.allowedOrigins` — operator always appends Route host;
  user entries are additional origins
- `gateway.trustedProxies` — operator always appends RFC1918 ranges; user
  entries are additional CIDRs
- `agents.defaults.models` — operator always merges catalog from credentials;
  user entries win on key collision (alias, params)
- `agents.defaults.model.primary` — operator provides catalog default; user
  value in `spec.config.raw` wins; runtime PVC choice wins over both

**Tier 3 — User-only (operator doesn't touch, user fully owns):**

Keys the operator never injects. User has full control via `spec.config.raw`.

- `diagnostics.*`, `session.*`, `logging.*`, `agents.list`,
  `plugins.*` (non-declared), `skills.*`, `ui.*`, `cron.*`, `hooks.*`,
  `browser.*`, `memory.*`, `talk.*`, `discovery.*`, `update.*`, etc.

**Decision:** Option B — three-tier split. Typed CRD field drives it =
always-win. Operator infra keys = append/merge. Everything else = user-only.

_Considered and rejected: Option A — too conservative, doesn't solve
diagnostics/session use cases. Option C — users could break proxy routing by
overriding `models.providers`._

---

## Q4: CORS `allowedOrigins` — skip entirely or append Route host?

When the user sets `gateway.controlUi.allowedOrigins` in `spec.config.raw`, the
operator normally injects the auto-detected Route host into this array. Should
the operator skip its injection (pure user control) or append the Route host to
the user's list?

### Option B: Append Route host to user's list

Operator always adds the auto-detected Route host, even when the user provided
their own origins.

- **Pro:** User's custom origins work AND the default Route works. No
  footgun.
- **Pro:** User only needs to list *additional* origins — the Route host is
  automatic.
- **Pro:** Consistent with the overarching principle: operator-managed infra
  works OOTB, user appends extra configuration.
- **Con:** Slightly more complex logic — must deduplicate, handle missing Route.

**Decision:** Option B — operator always appends Route host; user list provides
extra origins. Infra works OOTB, user extends it.

_Considered and rejected: Option A — skip-if-set risks breaking default Route
CORS silently. Option C — opt-out flag is over-engineered for a near-zero use
case._

---

## Q5: Model catalog — how does enrichment interact with user-set models?

The operator injects `agents.defaults.models` (model catalog from hardcoded
`modelCatalog`) and `agents.defaults.model.primary` into `operator.json`. When
the user sets these keys in `spec.config.raw`, how should the enrichment
behave?

### Option C: Merge catalogs, user-set primary wins over catalog default

Deep-merge the hardcoded catalog into the user's models. For the same model
key, user's entry wins on collision. Catalog models the user didn't mention
are added. If the user set `agents.defaults.model.primary` in
`spec.config.raw`, use that instead of the catalog's default primary.

- **Pro:** Consistent with the overarching principle: operator infra always
  works (catalog models are always present), user extends/customizes.
- **Pro:** Adding a new credential automatically adds its models even when
  `spec.config.raw` is set.
- **Pro:** User can customize specific models (rename aliases, set params)
  while keeping auto-discovery.
- **Pro:** Aligns with `merge.js`'s existing primary preservation logic.

Primary precedence (three layers):
1. **Runtime PVC choice** (highest) — user changes primary via UI/CLI,
   `merge.js` preserves it across restarts
2. **`spec.config.raw` primary** — user's CR-declared default, used on first
   run or when no runtime choice exists
3. **Catalog default** (lowest) — first model of first credential's catalog

**Decision:** Option C — catalog always merges in (infra works OOTB), user
entries win on collision, user-set primary wins over catalog default.

_Considered and rejected: Option A — skip-if-set breaks auto-discovery from
credentials. Option B — same as C but doesn't handle primary override._

---

## Q6: Do we need `configMapRef` in the initial implementation?

Some operators support both inline `spec.config.raw` and external
`spec.config.configMapRef`. Should we implement both from the start?

### Option B: Only `raw`, skip `configMapRef` entirely

- **Pro:** Simpler implementation. Inline raw covers 90% of use cases.
- **Pro:** No speculative API surface — add `configMapRef` later if needed.

**Decision:** Option B — only `raw`. No `configMapRef` field in the CRD at all.
Can be added later if there's real demand.

_Considered and rejected: Option A — extra complexity for GitOps/large configs
that aren't needed yet. Option C — poor DX, high friction for simple config._

---

## Q7: What happens to the `openclaw.json` seed when `spec.config.raw` is set?

The seed `openclaw.json` in the ConfigMap provides first-run defaults:
`agents.list` (default agent) and `agents.defaults.workspace`. When the user
provides `spec.config.raw`, should the seed change?

### Option A: Seed stays as-is

User raw config goes into `operator.json`. The seed `openclaw.json` remains
unchanged regardless of `spec.config`.

- **Pro:** Clean separation: seed = first-run defaults (default agent,
  workspace path), operator.json = current desired state (operator +
  user keys).
- **Pro:** If the user sets `agents.list` in `spec.config.raw`, it goes into
  `operator.json` and will overwrite the seed's `agents.list` on every
  restart. This is correct — the user explicitly declared their agent list.

**Decision:** Option A — seed stays unchanged, user config goes into
`operator.json`.

_Considered and rejected: Option B — replacing the seed breaks merge model
precedence and loses default agent if user omits `agents.list`. Option C —
merging into seed means user config only applies on first run._

---

## Q8: Should the model catalog remain hardcoded in Go?

The model catalog in `claw_models.go` maps provider names to known models. With
`spec.config.raw`, users can add/override models directly. Should the hardcoded
catalog change?

### Option A: Keep hardcoded catalog (status quo)

- **Pro:** Zero-config experience for supported providers. Add a Google
  credential → get Gemini models in the picker automatically.
- **Pro:** Hardcoded catalog provides OOTB defaults (consistent with our
  principle); `spec.config.raw` is the escape hatch. Users can always add
  any model via `spec.config.raw`:
  ```yaml
  spec:
    config:
      raw:
        agents:
          defaults:
            models:
              openrouter/qwen3-14b:
                alias: Qwen 3 14B
  ```
  Per Q5 decision, these merge with the catalog — catalog models are always
  present, user entries are added on top and win on collision.
- **Con:** Updating the OOTB catalog defaults (e.g., when Google releases a
  new model) requires an operator release. But individual deployments are
  never blocked — they add models via `spec.config.raw`.

**Decision:** Option A — keep hardcoded catalog for OOTB defaults,
`spec.config.raw` for per-deployment customization.

_Considered and rejected: Option B — ConfigMap-based catalog adds operational
complexity for ~15 entries that change infrequently. Option C — removing the
catalog breaks zero-config experience._
