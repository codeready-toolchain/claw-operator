# Workspace and Skills — Design Questions

**Status:** Resolved — all decisions made
**Related:** [Design document](workspace-and-skills-design.md)

Each question has options with trade-offs and a recommendation. Go through them
one by one to form the design, then update the design document.

### Reference projects

| Project | Workspace API | Skills API | Delivery |
|---------|--------------|------------|----------|
| **Upstream openclaw-operator** | `spec.workspace.initialFiles` (inline map) + `configMapRef` | `spec.skills` ([]string: ClawHub/npm/pack:) | Separate `{name}-workspace` ConfigMap → init `[ -f ] \|\| cp` |
| **openclaw-installer** | Host dirs → ConfigMap → init `cp` | Host `skills/` dir → ConfigMap → init `cp -r` | ConfigMaps + init container |
| **NemoClaw** | `skipBootstrap` + entrypoint seeds templates | SSH `skill install` post-create | Image bake + entrypoint + SSH |
| **OpenShell** | `--upload .` / `sandbox cp` (no framework) | None (agent-runtime concern) | Upload/cp via SSH |

Key upstream choices:
- All workspace files are **seed-once** (no per-file strategy in CRD)
- `initialFiles` is an inline `map[string]string` (path → content)
- External ConfigMapRef is additive (lower priority than inline)
- Operator-injected files (`ENVIRONMENT.md`, `BOOTSTRAP.md`) always overwrite
  but are NOT declared in the CRD — they are implicit
- `skipBootstrap` is application config, not a CRD field
- Skills use a separate mechanism (init container for ClawHub/npm; GitHub
  resolution for `pack:` into workspace CM)

---

## Q1: How do users provide file content?

Users need to supply content for workspace files and skills. The content needs to
reach the gateway ConfigMap so merge.js can seed it to the PVC.

### Option A: Inline only (`content` field in CR)

```yaml
spec:
  workspace:
    files:
      IDENTITY.md: |
        # Identity
        Name: Demo User
      AGENTS.md: |
        ## Enterprise assistant
        ...
```

- **Pro:** Simplest UX — everything in one CR, no external resources to manage
- **Pro:** Works immediately, no need to pre-create ConfigMaps
- **Pro:** GitOps-friendly — full state in one resource
- **Pro:** Upstream uses `initialFiles` as inline map — proven pattern
- **Con:** CR size limit (1MB for etcd object). Large content could push limits
- **Con:** `oc get claw instance -o yaml` output gets noisy with embedded content
- **Con:** No sharing between instances — each CR carries its own copy

**Decision:** Option A — inline map only for v1. Simple, proven by upstream,
sufficient for our use cases (small markdown files). ConfigMapRef can be added
later as a non-breaking extension.

_Considered and rejected: Option B ConfigMapRef only (extra resource management,
RBAC complexity), Option C both (over-engineering for v1)._

---

## Q2: Workspace file ownership — seedIfMissing or copyAlways?

When a user specifies a workspace file like `IDENTITY.md`, should the operator
overwrite it on every pod restart or only seed it once?

### Option D: `seedIfMissing` default; `copyAlways` only for skills

- **Pro:** Simple mental model: workspace = user-owned, skills = operator-owned
- **Pro:** Matches existing operator patterns (AGENTS.md = seed, PLATFORM.md = always)
- **Pro:** Matches upstream (all workspace files are seed-once)
- **Pro:** No new CRD fields for strategy

**Decision:** Option D — fixed strategy per field type. `spec.workspace.files`
always uses `seedIfMissing` (user-owned after first seed). `spec.skills` always
uses `copyAlways` (operator-managed). Clear mental model, no per-file config.

_Considered and rejected: Option A seedIfMissing-only (same outcome but doesn't
address skills), Option B copyAlways (wipes user edits), Option C per-file
strategy (unnecessary CRD complexity)._

---

## Q3: Skills API shape — inline map, list of objects, or different mechanism?

Skills are always operator-managed (`copyAlways`). Each skill becomes
`workspace/skills/<name>/SKILL.md`. The question is the CRD shape.

### Option A: Inline map (name → content)

```yaml
spec:
  skills:
    quote-builder: |
      ---
      name: quote-builder
      description: Build customer quotes using the pricing API
      ---
      # Quote Builder
      ...
```

- **Pro:** Simplest — same shape as `spec.workspace.files` (map[string]string)
- **Pro:** Name is the map key, content is the value
- **Pro:** Skills are typically small markdown (1-5KB)

**Decision:** Option A — inline map. Map key = skill directory name, value =
SKILL.md content. Same shape as `spec.workspace.files`, minimal CRD surface.
Skills use `copyAlways` (per Q2), distinct from workspace `seedIfMissing`.

_Considered and rejected: Option B list of objects (unnecessary verbosity),
Option C reuse workspace.files (wrong ownership semantics — skills need
copyAlways)._

---

## Q4: How are workspace/skill files keyed in the ConfigMap?

The controller writes file content into the gateway ConfigMap. merge.js needs a
convention to distinguish workspace files from skills and from existing keys.

### Option B: Prefix convention with path encoding

Encode slashes as `--` (matching upstream's `SkillPackCMKey()`):

```
_ws_IDENTITY.md        → seedIfMissing → workspace/IDENTITY.md
_ws_docs--README.md    → seedIfMissing → workspace/docs/README.md
_skill_quote-builder   → copyAlways    → workspace/skills/quote-builder/SKILL.md
```

- **Pro:** Self-describing — no separate manifest needed
- **Pro:** Simpler merge.js (one pass, prefix filter)
- **Pro:** Matches upstream `--` path encoding convention
- **Pro:** Prefix determines strategy (`_ws_` = seedIfMissing, `_skill_` = copyAlways)

**Decision:** Option B — self-describing prefixed keys. `_ws_` prefix for
workspace files (seedIfMissing), `_skill_` prefix for skills (copyAlways).
Slashes in paths encoded as `--`. One-pass iteration in merge.js, no manifest.

_Considered and rejected: Option A JSON manifest (unnecessary two-step
indirection), Option C separate ConfigMap (over-engineering for 3-5 files)._

---

## Q5: Should `skipBootstrap` be a dedicated CR field?

OpenClaw prompts new users with a bootstrap questionnaire on first use. For
demos, this should be skipped. The config key is
`agents.defaults.skipBootstrap: true`.

### Option A: Dedicated field (`spec.workspace.skipBootstrap`)

```yaml
spec:
  workspace:
    skipBootstrap: true
```

- **Pro:** Discoverable in CRD docs and `oc explain`
- **Pro:** Clear intent — pairs naturally with workspace file seeding
- **Pro:** Upstream has a similar field (`spec.workspace.bootstrap.enabled`)

**Decision:** Option A — dedicated field on `spec.workspace`. Since we're
already adding `spec.workspace` as a new struct, including `skipBootstrap` there
is natural and discoverable. Users setting up workspace files almost always want
to skip bootstrap too — having it right next to `files` makes the connection
obvious.

_Considered and rejected: Option B spec.config.raw (less discoverable, users
won't know the config key), Option C auto-infer from IDENTITY.md (too magical,
non-obvious relationship)._

---

## Q6: Same ConfigMap or separate volume for workspace content?

**Moot — already decided by Q4.** The Q4 decision to use `_ws_`/`_skill_`
prefixed keys implies using the existing gateway ConfigMap. No separate
ConfigMap or volume needed.
