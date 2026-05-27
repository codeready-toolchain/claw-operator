# ADR-0015: Workspace Files and Skills Injection

**Status:** Implemented
**Date:** 2026-05-26

---

## Overview

Add `spec.workspace` and `spec.skills` to the Claw CR, enabling operators to
declaratively seed workspace files (IDENTITY.md, AGENTS.md overrides) and custom
skills (enterprise-specific SKILL.md files) onto the PVC without `oc exec`.

This eliminates the manual file-copy steps from demo and onboarding scripts,
and enables GitOps workflows for workspace content.

---

## Design Principles

1. **Extend existing mechanisms** — reuse the proven `merge.js` seeding patterns
   (`seedIfMissing`, `copyAlways`) rather than inventing new file delivery.

2. **Existing ConfigMap as transport** — files travel to the pod via the existing
   gateway ConfigMap using prefixed keys. No new volumes or resources.

3. **Clear ownership semantics** — workspace files are user-owned (seeded once,
   never touched again). Skills are operator-managed (overwritten every restart).

4. **Size-conscious** — ConfigMaps have a 1MB total limit. Inline content in the
   CR is appropriate for typical use cases (small markdown files). ConfigMapRef
   can be added later as an extension if needed.

5. **Backward compatible** — no workspace or skill seeding by default. Existing
   CRs produce identical behavior.

---

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | How do users provide file content? | Inline map only (`map[string]string` in the CR) | Simplest UX — everything in one CR, GitOps-friendly, proven by upstream's `initialFiles`. ConfigMapRef can be added later as a non-breaking extension. |
| Q2 | Workspace file ownership — seedIfMissing or copyAlways? | Fixed strategy per field type: `spec.workspace.files` → `seedIfMissing`, `spec.skills` → `copyAlways` | Simple mental model matching existing operator patterns (AGENTS.md = seed, PLATFORM.md = always) and upstream behavior. No per-file strategy config needed. |
| Q3 | Skills API shape? | Inline map (name → content) on `ClawSpec` | Same shape as workspace files, minimal CRD surface. Map key = skill directory name, value = SKILL.md content. |
| Q4 | How are files keyed in the ConfigMap? | Self-describing prefixed keys with `--` path encoding | `_ws_` prefix for workspace (seedIfMissing), `_skill_` prefix for skills (copyAlways). Slashes encoded as `--` matching upstream convention. One-pass iteration in merge.js. |
| Q5 | Should `skipBootstrap` be a dedicated CR field? | Yes — `spec.workspace.skipBootstrap` | Discoverable in CRD docs, pairs naturally with workspace file seeding. Users setting up workspace files almost always want to skip bootstrap. |
| Q6 | Same ConfigMap or separate volume? | Existing gateway ConfigMap (implied by Q4) | No separate ConfigMap or volume needed. |

---

## Architecture

### File seeding flow

```
ConfigMap keys              merge.js                PVC destination
─────────────              ────────                ───────────────
(existing keys)            (unchanged)             (unchanged)
_ws_IDENTITY.md      ──▶  seedIfMissing   ──▶  workspace/IDENTITY.md
_ws_AGENTS.md        ──▶  seedIfMissing   ──▶  workspace/AGENTS.md
_skill_quote-builder ──▶  copyAlways      ──▶  workspace/skills/quote-builder/SKILL.md
_skill_compliance    ──▶  copyAlways      ──▶  workspace/skills/compliance/SKILL.md
```

### Key conventions

- `_ws_<path>` → `seedIfMissing` to `workspace/<path>`
- `_skill_<name>` → `copyAlways` to `workspace/skills/<name>/SKILL.md`
- Slashes in paths encoded as `--` (e.g., `_ws_docs--README.md` → `workspace/docs/README.md`)

### Data flow

```
                   ┌─────────────────────────────────────────┐
                   │  Reconciler                              │
                   │                                          │
spec.workspace ──▶ │  injectWorkspaceFiles(objects, instance) │
spec.skills    ──▶ │  injectSkillFiles(objects, instance)     │
                   │                                          │
                   │  Writes _ws_/_skill_ keys into ConfigMap  │
                   └──────────────────────┬──────────────────┘
                                          │
                                          ▼
                   ┌──────────────────────────────────────────┐
                   │  init-config (merge.js)                   │
                   │                                           │
                   │  For each _ws_* key → seedIfMissing       │
                   │  For each _skill_* key → copyAlways       │
                   └──────────────────────────────────────────┘
```

---

## CRD Changes

### `spec.workspace`

```yaml
spec:
  workspace:
    # Skip the OpenClaw bootstrap questionnaire on first use.
    # Default: false.
    skipBootstrap: true

    # Files to seed into the workspace directory.
    # Map keys are file paths relative to workspace/.
    # Content is seeded once (seedIfMissing) — user edits are preserved.
    files:
      IDENTITY.md: |
        # Identity
        Name: Demo User
        Creature: An octopus
      AGENTS.md: |
        ## Enterprise assistant
        You are a FantaCo enterprise assistant...
```

### `spec.skills`

```yaml
spec:
  # Operator-managed skills. Map keys are skill directory names.
  # Content is always overwritten on pod restart (copyAlways).
  skills:
    quote-builder: |
      ---
      name: quote-builder
      description: Build customer quotes using the pricing API
      ---
      # Quote Builder
      Use the pricing MCP server to generate quotes...
    compliance: |
      ---
      name: compliance
      description: Corporate compliance guidelines
      ---
      # Compliance
      Always follow FantaCo policy...
```

---

## Validation

The controller rejects the CR (condition = `Ready=False`) if:

- A workspace file path is empty, absolute, or contains `..` (directory traversal)
- A workspace file path conflicts with operator-managed paths (e.g., `skills/platform/SKILL.md`)
- A skill name is empty, `.`, `..`, or contains `/`
- A skill name conflicts with built-in operator skills (`platform`, `kubernetes`)
- A workspace file path or skill name contains `--` (reserved as the slash encoding
  delimiter)

---

## Interaction with existing features

| Feature | Interaction |
|---------|-------------|
| `spec.config.raw` | Independent — workspace files are PVC content, not openclaw.json |
| `spec.plugins` | Independent — plugins are npm packages; skills are markdown files |
| Existing `AGENTS.md` seed | User's `spec.workspace.files.AGENTS.md` overrides the built-in seed. merge.js processes `_ws_*` keys first; the static `seedIfMissing(AGENTS.md)` becomes a no-op since the destination already exists. |
| Existing platform/kubernetes skills | Coexist — operator-injected skills use `PLATFORM.md`/`KUBERNETES.md` keys; user skills use `_skill_` prefix |
| Config hash / rollout | Automatic — any workspace/skill change triggers pod restart |

---

## ConfigMap size budget

| Content | Approximate size |
|---------|-----------------|
| Existing keys (operator.json, merge.js, PLATFORM.md, etc.) | ~30KB |
| Typical workspace files (IDENTITY.md, AGENTS.md) | ~2-5KB |
| Typical skills (2-3 enterprise skills) | ~5-15KB |
| **Total** | **~40-50KB** |
| **Remaining budget** | **~950KB** |

---

## Future considerations

- **ConfigMapRef extension** — for large file sets or shared content across
  instances, a `configMapRef` field could be added to both `spec.workspace` and
  `spec.skills` as a non-breaking extension.
