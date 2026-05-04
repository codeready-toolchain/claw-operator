# Config Merge Design — Open Questions

**Status:** Resolved — all decisions made
**Related:** [Design document](config-merge-design.md)

All questions resolved. Decisions recorded below.

---

## Q1: Init container image for the merge step

The current `init-config` container uses `mirror.gcr.io/library/busybox:1.37`
which has no JSON processing capability. The deep-merge step needs to parse and
manipulate JSON. What image should we use?

### Option A: Use the gateway image (`ghcr.io/openclaw/openclaw:slim`)

The same image already used for the main container. Has Node.js, so we can run
an inline merge script.

- **Pro:** No additional image pull — already cached from the main container
- **Pro:** Full Node.js available, can handle complex JSON (comments, edge cases)
- **Pro:** Matches what openclaw-operator does (uses the OpenClaw image for its init merge)
- **Con:** Much larger image for a simple init task (~500MB vs ~5MB for busybox)
- **Con:** Slower init container startup on cold pull
- **Con:** If the gateway image tag changes, init container must also be updated (single source of truth anyway)

**Decision:** Option A — gateway image is already cached, no extra pull cost, and matches the openclaw-operator pattern.

_Considered and rejected: Option B — alpine+jq (jq deep-merge is fragile, extra image to maintain), Option C — split init containers (unnecessary complexity for minimal benefit)._

---

## Q2: CRD field design for locked/immutable config mode

The user wants the ability to disable runtime config edits via the Claw CRD. How
should this be exposed?

### Option A: `spec.configMode` top-level enum (`merge` / `overwrite`)

Same enum values as the upstream openclaw-operator (`merge` / `overwrite`) but as
a top-level field on ClawSpec rather than nested under `spec.config`. Default is
`merge` (user changes survive restarts). `overwrite` makes the operator fully
overwrite `openclaw.json` on every pod start.

- **Pro:** Matches upstream naming — familiar to openclaw-operator users
- **Pro:** Top-level field — no unnecessary nesting (we have nothing else under `config`)
- **Pro:** Extensible enum if needed later
- **Pro:** Mechanism-descriptive: clear what actually happens

**Decision:** Option A — top-level `spec.configMode` enum with values `merge` (default) and `overwrite`, matching upstream naming but without unnecessary nesting.

_Considered and rejected: Option B — nested `spec.config.mutable` boolean (less extensible, implies other config sub-fields), Option C — nested `spec.config.mergeMode` (matches upstream path exactly but adds unnecessary nesting for our simpler CRD)._

---

## Q3: Migration strategy for existing `$include`-based PVC configs

~~Not applicable — the operator is in development phase and not yet deployed in
production. No existing PVC data needs migration.~~

**Decision:** Skipped — no migration needed. Operator is pre-release.

---

## Q4: Array merge behavior (replace vs concatenate)

When the operator's `operator.json` has an array value (e.g.,
`gateway.controlUi.allowedOrigins`) and the user's `openclaw.json` also has that
key, should arrays replace or concatenate?

### Option A: Operator arrays replace user arrays

Simple override: operator value wins completely for arrays.

- **Pro:** Predictable — operator is authoritative for its keys
- **Pro:** `allowedOrigins` must be exactly what the operator sets (Route-derived)
- **Pro:** `providers` must be exactly what the operator sets (credential-derived)

**Decision:** Option A — arrays replace. Operator-managed arrays are authoritative and must reflect current deployment state.

_Considered and rejected: Option B — concatenate (would accumulate stale values, operator can't remove entries)._

---

## Q5: Should `operator.json` still be written to PVC?

Currently the init container always copies `operator.json` to the PVC so OpenClaw
can resolve `$include`. With `$include` removed, `operator.json` is no longer
read by OpenClaw at runtime and the merge script reads from the ConfigMap volume
mount (`/config/operator.json`), not from PVC. No functional purpose remains.

**Decision:** Option B — stop writing `operator.json` to PVC. It serves no
functional purpose (merge reads from ConfigMap mount, OpenClaw doesn't reference
it). Cleaner PVC.

_Considered and rejected: Option A — keep writing for debugging (dead file, no functional value)._

---

## Q6: Adopt `models.mode: "merge"` in `operator.json`

OpenClaw supports `"models": {"mode": "merge"}` which deep-merges provider
definitions from multiple config sources instead of replacing. NemoClaw uses
this. Should we adopt it?

### Option B: Don't add it — operator providers fully replace

- **Pro:** Operator has complete control over provider definitions
- **Pro:** User-added providers can't work anyway — the MITM proxy blocks any
  domain not configured via the Claw CR's `credentials` array. The only path to
  a working provider is through the CR, which configures both the proxy route
  AND the provider entry in `operator.json`

**Decision:** Option B — don't add `models.mode: "merge"`. User-added providers
are non-functional because the proxy blocks unconfigured domains. All providers
must come through the Claw CR.

_Considered and rejected: Option A — add `models.mode: "merge"` (no practical benefit since proxy blocks user-added provider traffic)._
