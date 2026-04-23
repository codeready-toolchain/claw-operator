# ADR-0002: Config Preservation via $include Layering

**Status:** Implemented

**Date:** 2026-04-21

---

## Overview

The Claw Operator manages a `claw-config` ConfigMap containing OpenClaw's configuration (`openclaw.json`), agent system prompt (`AGENTS.md`), and optionally Kubernetes context documentation (`KUBERNETES.md`). Prior to this change, every reconciliation rebuilt the entire ConfigMap from an embedded template and applied it via server-side apply, unconditionally overwriting all keys. This destroyed any changes made by the user through the Control UI or by the OpenClaw application itself (e.g., model preferences, agent definitions, system prompt edits).

This ADR documents the decision to split configuration into operator-managed and user-owned layers using OpenClaw's native `$include` mechanism, combined with conditional seeding for workspace files.

## Design Principles

- **Operator owns infrastructure; users own application config** — gateway settings, CORS, proxy routing, and provider wiring are operator concerns. Model preferences, agent definitions, and system prompts are user concerns.
- **Leverage upstream `$include`** — OpenClaw natively supports `$include` directives with `deepMerge` semantics. Using this avoids custom merge logic entirely.
- **Seed-once for user files; always-overwrite for operator files** — user-owned files are copied to the PVC only if they don't already exist. Operator-owned files are overwritten every pod start.
- **No runtime merge scripts** — the init container uses simple shell conditionals (`[ -f ... ] || cp`), not JSON merge tools or Node.js scripts.

---

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Config separation strategy | Split into `operator.json` (operator-managed) and `openclaw.json` (user-owned with `$include`) | OpenClaw's `$include` + `deepMerge` handles composition natively. Write-back logic preserves `$include` directives (doesn't flatten). Rejected: custom JSON merge in init container (fragile, upstream `mergeMode: merge` requires Node.js), split ConfigMaps (OpenClaw expects a single config file), SSA partial ownership (can't own fields inside a JSON string value) |
| Q2 | Where agent defaults live | User-owned `openclaw.json` seed | Model aliases, primary model, agent list, and workspace paths are user preferences. The operator should not overwrite these on every reconcile. The seed provides sensible defaults that users can customize freely |
| Q3 | `AGENTS.md` handling | Conditional seed (copy only if missing on PVC) | Users and the OpenClaw application modify the system prompt at runtime. The operator provides initial content but must not clobber edits. Rejected: always-overwrite (destroys user edits), append-only (accumulates duplicates across reconciliations) |
| Q4 | Kubernetes context delivery | Separate `KUBERNETES.md` file, always overwritten | Kubernetes context info (clusters, contexts, namespaces) is derived from the CR's credentials and must stay in sync. A separate file avoids conflicting with user edits to `AGENTS.md`. Always-overwritten because the content is purely operator-derived |
| Q5 | Model remapping and filtering removal | Removed `remapVertexProviderModels` and `filterAgentDefaultsForProviders` | These functions modified the `agents.defaults` section (model aliases, primary model) which is now user-owned in `openclaw.json`. The operator no longer has access to or responsibility for this section. Users control their own model preferences via the seed or Control UI |
| Q6 | Init container seeding logic | Shell conditionals in busybox, no merge tools | `[ -f target ] || cp source target` is the simplest correct approach. No dependencies beyond a POSIX shell. The upstream community operator's `mergeMode: merge` uses a Node.js script — unnecessary complexity when `$include` handles composition at the application layer |

---

## Architecture

### File Ownership Model

```
claw-config ConfigMap (rebuilt every reconcile via SSA)
├── operator.json          ← operator-managed: gateway, providers, CORS, cron
├── openclaw.json          ← seed: user-owned config with $include directive
├── AGENTS.md              ← seed: initial system prompt
└── KUBERNETES.md          ← operator-managed (only when kubernetes credentials exist)
```

```
claw-home-pvc (persistent across pod restarts)
├── operator.json          ← always overwritten from ConfigMap
├── openclaw.json          ← seeded once, then user-owned
└── workspace/
    ├── AGENTS.md           ← seeded once, then user-owned
    └── KUBERNETES.md       ← always overwritten from ConfigMap (when present)
```

### Config Composition via $include

OpenClaw loads `openclaw.json` at startup. The `$include` directive pulls in `operator.json` and merges using `deepMerge`:

```
openclaw.json (user-owned)          operator.json (operator-managed)
┌──────────────────────────┐         ┌──────────────────────────────┐
│ "$include": "./operator" │────────▶│ gateway: { mode, bind, ... } │
│ agents:                  │         │ models.providers: { ... }    │
│   defaults:              │         │ cron: { enabled: false }     │
│     model: { primary }   │         └──────────────────────────────┘
│     models: { aliases }  │
│   list: [ ... ]          │              deepMerge semantics:
└──────────────────────────┘              - objects merge recursively
                                         - arrays concatenate
         Result: merged config           - sibling keys (openclaw.json)
         with both operator                win for primitive conflicts
         and user settings
```

The operator dynamically injects values into `operator.json` before it reaches the ConfigMap:

1. **Route host** — `OPENCLAW_ROUTE_HOST` placeholder replaced with the actual Route URL (or `http://localhost:18789` on vanilla Kubernetes)
2. **Providers** — `models.providers` populated from `spec.credentials[]` entries that have `provider` set

### Init Container Behavior

The `init-config` container runs on every pod start and implements the two-tier copy strategy:

```sh
# Always overwrite operator-managed files
cp /config/operator.json /home/node/.openclaw/operator.json

# Seed user-owned files only if missing
mkdir -p /home/node/.openclaw/workspace
[ -f /home/node/.openclaw/openclaw.json ] || cp /config/openclaw.json /home/node/.openclaw/openclaw.json
[ -f /home/node/.openclaw/workspace/AGENTS.md ] || cp /config/AGENTS.md /home/node/.openclaw/workspace/AGENTS.md

# Conditionally copy operator-managed workspace files
if [ -f /config/KUBERNETES.md ]; then
  cp /config/KUBERNETES.md /home/node/.openclaw/workspace/KUBERNETES.md
fi
```

### Reconciliation Flow (Config-Related Steps)

```
Reconcile
 ↓
buildKustomizedObjects()
 ├─ Kustomize builds all manifests (ConfigMap contains operator.json, openclaw.json seed, AGENTS.md seed)
 ↓
injectRouteHostIntoConfigMap(objects, routeHost)
 ├─ Replaces OPENCLAW_ROUTE_HOST in operator.json with actual Route URL
 ↓
injectProvidersIntoConfigMap(objects, credentials)
 ├─ Populates models.providers in operator.json from spec.credentials[]
 ├─ Only credentials with provider set generate entries
 ├─ Does NOT touch agents.defaults (lives in user-owned openclaw.json)
 ↓
injectKubernetesContextFile(objects, resolvedCreds)
 ├─ Adds KUBERNETES.md key to ConfigMap (if kubernetes credentials exist)
 ├─ No-op when no kubernetes credentials
 ↓
applyResources()
 ├─ Server-side apply ConfigMap (and all other resources)
 ↓
Pod starts → init-config runs → files land on PVC → OpenClaw loads openclaw.json with $include
```

### Alternatives Considered

**Upstream `mergeMode: merge`** — The community `openclaw-operator` implements a Node.js-based merge script that runs in the init container and performs structural JSON merging of `openclaw.json`. While functional, this adds a runtime dependency on Node.js in the init container and custom merge logic that must be maintained separately from OpenClaw's own config loading. The `$include` approach delegates composition entirely to OpenClaw.

**Server-Side Apply partial ownership** — SSA tracks field ownership at the Kubernetes API level, but a ConfigMap `data` key containing a JSON string is a single atomic field. SSA cannot own individual JSON keys within that string. Two controllers writing to the same `data["openclaw.json"]` key would conflict.

**Annotation-based merge hints** — Embedding merge directives in ConfigMap annotations (e.g., `claw.sandbox.redhat.com/merge-strategy: deep`) would require custom merge logic in the controller and would not survive a `kubectl apply` from a GitOps tool.

**Split ConfigMaps** — Using separate ConfigMaps for operator and user config would require changes to how OpenClaw discovers its configuration file, as it expects a single `openclaw.json` path.

---

## Future Considerations

- **User reset mechanism** — Users who want to restore the default `openclaw.json` or `AGENTS.md` seed can delete the file from the PVC; the next pod restart will re-seed it from the ConfigMap.
- **Config schema evolution** — If `operator.json` gains new top-level keys, `deepMerge` adds them transparently. If keys are removed, stale copies on the PVC will contain unused fields that OpenClaw ignores.
- **Conflict on `$include` removal** — If a user removes the `$include` directive from their `openclaw.json`, they lose operator-managed settings (gateway, providers). This is by design: the user has full ownership of their config file. Re-seeding (delete + restart) restores the `$include`.

---

## Out of Scope

- **Runtime conflict detection** — No optimistic concurrency or `baseHash` checking for user edits vs operator updates. The file ownership boundary (`operator.json` vs `openclaw.json`) eliminates the conflict surface.
- **Custom merge scripts in init containers** — Explicitly rejected in favor of `$include`.
- **Multi-instance config inheritance** — Only one Claw instance (`"instance"`) is supported per namespace.
- **Bidirectional sync** — The operator does not read back user changes from the PVC. Config flows one direction: operator → ConfigMap → PVC → OpenClaw.
