# Workspace Files and Skills Injection

**Status:** Final

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

## Architecture

### Current file seeding flow

```
ConfigMap keys          merge.js            PVC destination
─────────────          ────────            ───────────────
operator.json    ──▶  deep-merge      ──▶  openclaw.json
openclaw.json    ──▶  (seed)
AGENTS.md        ──▶  seedIfMissing   ──▶  workspace/AGENTS.md
PLATFORM.md      ──▶  copyAlways      ──▶  workspace/skills/platform/SKILL.md
KUBERNETES.md    ──▶  copyAlways      ──▶  workspace/skills/kubernetes/SKILL.md
```

### Extended flow (this design)

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

### Go types

```go
// WorkspaceSpec configures workspace file seeding.
type WorkspaceSpec struct {
    // SkipBootstrap suppresses the OpenClaw first-run questionnaire.
    // Default: false.
    // +optional
    SkipBootstrap bool `json:"skipBootstrap,omitempty"`

    // Files maps workspace-relative paths to file content.
    // Each file is seeded once (seedIfMissing) — user edits via the
    // OpenClaw UI are preserved across restarts.
    // +optional
    Files map[string]string `json:"files,omitempty"`
}
```

`spec.skills` is `map[string]string` directly on `ClawSpec`:

```go
// Skills maps skill names to SKILL.md content. Each entry creates
// workspace/skills/<name>/SKILL.md, overwritten on every pod restart
// (operator-managed).
// +optional
Skills map[string]string `json:"skills,omitempty"`
```

---

## Implementation Plan

Single PR. Steps are ordered by dependency but ship together.

1. Add `WorkspaceSpec` to `api/v1alpha1/claw_types.go`; add
   `Workspace *WorkspaceSpec` and `Skills map[string]string` to `ClawSpec`;
   run `make manifests generate`
2. Add validation helpers — reject empty/absolute/traversal paths, `--` in
   names, conflicts with built-in skills (`platform`, `kubernetes`)
3. Add `injectWorkspaceFiles(objects, instance)` — validate, encode paths
   with `--`, write `_ws_<encoded-path>` keys into the gateway ConfigMap
4. Add `injectSkillFiles(objects, instance)` — validate, write `_skill_<name>`
   keys into the gateway ConfigMap
5. Add `injectSkipBootstrap(config, instance)` — set
   `agents.defaults.skipBootstrap: true` in operator.json when enabled
6. Wire steps 3-5 into `enrichConfigAndNetworkPolicy()` pipeline
7. Extend merge.js: iterate `_ws_*` keys (seedIfMissing) **before** static
   seeds; iterate `_skill_*` keys (copyAlways); guard existing static
   `seedIfMissing(AGENTS.md)` with an existence check
8. Unit tests for injection functions and validation
9. Integration tests (envtest — keys appear in ConfigMap after reconcile)
10. Update user guide with "Workspace Files" and "Skills" sections

**Note:** `stampGatewayConfigHash` already hashes all ConfigMap data keys —
new `_ws_`/`_skill_` keys trigger pod rollouts automatically with no changes.

---

## Interaction with existing features

| Feature | Interaction |
|---------|-------------|
| `spec.config.raw` | Independent — workspace files are PVC content, not openclaw.json |
| `spec.plugins` | Independent — plugins are npm packages; skills are markdown files |
| Existing `AGENTS.md` seed | User's `spec.workspace.files.AGENTS.md` overrides the built-in seed. merge.js processes `_ws_*` keys FIRST, then runs the static `seedIfMissing(AGENTS.md)`. Since the destination already exists after `_ws_AGENTS.md` is seeded, the static seed is a no-op. |
| Existing platform/kubernetes skills | Coexist — operator-injected skills use `PLATFORM.md`/`KUBERNETES.md` keys; user skills use `_skill_` prefix |
| Config hash / rollout | Automatic — any workspace/skill change triggers pod restart |

---

## Validation

The controller rejects the CR (condition = `Ready=False`) if:

- A workspace file path is empty, absolute, or contains `..` (directory traversal)
- A workspace file path conflicts with operator-managed paths (e.g., `skills/platform/SKILL.md`)
- A skill name is empty or contains `/` (names are directory components, not paths)
- A skill name conflicts with built-in operator skills (`platform`, `kubernetes`)
- A workspace file path or skill name contains `--` (reserved as the slash encoding
  delimiter — filenames with literal `--` are unsupported)

These checks run in the controller before writing ConfigMap keys, producing clear
status messages (e.g., `workspace file path "../../etc/passwd" is invalid: must not
contain ".."`)

---

## ConfigMap size budget

| Content | Approximate size |
|---------|-----------------|
| Existing keys (operator.json, merge.js, PLATFORM.md, etc.) | ~30KB |
| Typical workspace files (IDENTITY.md, AGENTS.md) | ~2-5KB |
| Typical skills (2-3 enterprise skills) | ~5-15KB |
| **Total** | **~40-50KB** |
| **Remaining budget** | **~950KB** |

Well within the 1MB ConfigMap limit for typical use cases.

---

## Example CR

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: demo-instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
  config:
    raw:
      agents:
        defaults:
          model:
            primary: google/gemini-2.5-pro
  workspace:
    skipBootstrap: true
    files:
      IDENTITY.md: |
        # Identity
        - Name: Demo User
        - Role: Enterprise Developer
        - Company: FantaCo
      AGENTS.md: |
        ## FantaCo Enterprise Assistant
        You are a FantaCo enterprise assistant with access to
        internal APIs and compliance guidelines.
  skills:
    quote-builder: |
      ---
      name: quote-builder
      description: Build customer quotes using the pricing API
      ---
      # Quote Builder
      Connect to the pricing MCP server and generate quotes...
  plugins:
    - "@openclaw/diagnostics-otel"
  metrics:
    enabled: true
```
