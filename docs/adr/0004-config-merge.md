# ADR-0004: Drop `$include`, Deep-Merge Config at Init Time

**Status:** Implemented
**Date:** 2026-05-04

## Overview

OpenClaw's `$include` directive at the root of `openclaw.json` blocks all
runtime config writes — plugin installs, `config.patch` via the gateway tool,
and any automated mutation. This breaks WhatsApp setup and any other plugin that
needs to persist config.

The root cause is OpenClaw's flatten guard (`preserveUntouchedIncludes`): a
root-level `$include` makes `collectIncludeOwnedPaths` record path `[]`, and
`patchTouchesPath(patch, [])` returns true for **any** non-empty patch. Every
write is rejected with _"Config write would flatten $include-owned config at
\<root\>"_.

This ADR replaces the `$include` approach with init-time deep-merging, where
the init container merges operator-managed settings into the user's config file
on every pod start. This is the proven pattern used by every other OpenClaw
deployment tool (openclaw-operator, openclaw-installer, NemoClaw, paude).

## Design Principles

1. **OpenClaw sees a plain JSON file** — no `$include`, no write barriers. All
   standard config mutations (plugin install, `config.patch`, UI settings) work.

2. **Operator controls what it needs to** — gateway settings, CORS origins,
   and provider definitions are always overwritten. The operator is the source
   of truth for infrastructure config.

3. **User changes survive pod restarts** (in `merge` mode) — keys the operator
   doesn't touch (plugins, agent config, model preferences, channels) persist on
   the PVC across restarts.

4. **Lockdown available** — `spec.configMode: overwrite` disables runtime config
   mutability for managed/shared deployments where user edits are not desired.

5. **Minimal change surface** — the ConfigMap structure, controller injection
   logic, and Kustomize pipeline remain the same. Only the init container script
   and `openclaw.json` seed change.

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Init container image | Gateway image (`openclaw:slim`) | Already cached from main container — no extra pull cost. Provides Node.js for JSON processing. Matches openclaw-operator pattern. |
| 2 | CRD field design | Top-level `spec.configMode` enum (`merge`/`overwrite`) | Matches upstream naming, no unnecessary nesting, extensible, mechanism-descriptive. |
| 3 | Migration strategy | None | Operator is pre-release; no existing PVC data to migrate. |
| 4 | Array merge behavior | Operator arrays replace user arrays | Operator-managed arrays (e.g., `allowedOrigins`, `providers`) are authoritative and must reflect current deployment state. |
| 5 | Write `operator.json` to PVC | No — read from ConfigMap mount only | No functional purpose after removing `$include`. Merge script reads from `/config/` volume mount. |
| 6 | `models.mode: "merge"` | Not adopted | User-added providers are non-functional because the proxy blocks unconfigured domains. All providers must come through the Claw CR. |

## Architecture

### Init-time merge flow

```
ConfigMap                          PVC (openclaw.json)
┌─────────────────────┐
│ operator.json       │           (read from ConfigMap mount at /config/)
│ openclaw.json seed  │──if new──▶ seeded to PVC (plain JSON, no $include)
└─────────────────────┘
         │
    init container (gateway image, Node.js)
    deep-merges /config/operator.json
    INTO /home/node/.openclaw/openclaw.json
    (operator keys win, arrays replace)
         │
         ▼
┌──────────────────────────────────────┐
│ openclaw.json (merged, plain JSON)   │
│  gateway: {from operator.json}       │
│  models: {from operator.json}        │
│  agents: {user-owned, preserved}     │
│  plugins: {user-owned, preserved}    │
│  channels: {user-owned, preserved}   │
│  cron: {user-owned, preserved}       │
└──────────────────────────────────────┘
         │
  OpenClaw loads ──▶ plain JSON, no includes
         │
  Plugin install ──▶ WORKS (standard write path)
  config.patch   ──▶ WORKS
  WhatsApp setup ──▶ WORKS
```

### Config ownership

| Config section | Owner | Behavior on pod restart (merge mode) |
|---|---|---|
| `gateway.*` | Operator | **Overwritten** — CORS origins, auth mode, bind, port, trustedProxies |
| `models.providers` | Operator | **Overwritten** — tracks credentials in Claw CR |
| `agents.*` | User | **Preserved** — model aliases, primary model, agent list, workspace |
| `plugins.*` | User | **Preserved** — plugin installs, allow/deny lists |
| `channels.*` | User | **Preserved** — WhatsApp, Telegram, etc. |
| `tools.*` | User | **Preserved** |
| `cron.*` | User | **Preserved** — scheduled tasks |

### Deep merge semantics

- **Objects**: merge recursively; operator keys override matching user keys
- **Arrays**: operator value replaces user value (not concatenated)
- **Primitives**: operator value wins

### `spec.configMode` CRD field

A top-level enum on `ClawSpec` with values `merge` (default) and `overwrite`:

- **`merge`** (default): init container seeds `openclaw.json` if missing, then
  deep-merges `operator.json` into it. User-owned keys survive restarts. Plugin
  installs, channel configs, and agent customizations persist.

- **`overwrite`**: init container fully overwrites `openclaw.json` with the
  operator-managed config on every pod start. User edits are wiped. Suitable for
  managed deployments where the operator is the sole config authority.

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  configMode: merge  # or "overwrite"
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        name: gemini-api-key
        key: api-key
      provider: google
```

### Init container

Uses the gateway image (`ghcr.io/openclaw/openclaw`) — same base image reference
as the main container. Kustomize's `images` transformer pins both to the same
version tag, so bumping the tag updates both containers atomically. Already
cached on the node. Provides Node.js for the merge script.

The merge script is stored as a `merge.js` key in the `claw-config` ConfigMap
(already mounted at `/config`). The init container command is simply
`node /config/merge.js`. The operator regenerates the ConfigMap on every
reconcile, so script updates deploy automatically with operator upgrades.

### Files on PVC

| File | Written by | When |
|---|---|---|
| `openclaw.json` | init container | Every pod start (merge or overwrite) |
| `workspace/AGENTS.md` | init container | Seed if missing |
| `workspace/skills/proxy/SKILL.md` | init container | Always (operator-managed) |
| `workspace/skills/kubernetes/SKILL.md` | init container | Always when k8s credentials present |

Note: `operator.json` is **not** written to PVC. It's only read from the
ConfigMap volume mount at `/config/`.

### ConfigMap structure

```yaml
data:
  operator.json: |
    {
      "gateway": { ... },
      "models": { "providers": { ... } }
    }
  openclaw.json: |
    {
      "agents": {
        "defaults": {
          "model": { "primary": "google/gemini-3-flash-preview" },
          "models": { ... },
          "workspace": "~/.openclaw/workspace"
        },
        "list": [{ "id": "default", "name": "OpenClaw Assistant", ... }]
      }
    }
  merge.js: |
    ...
  AGENTS.md: |
    ...
  PROXY_SETUP.md: |
    ...
```

Key changes from the previous `$include` approach:
- `openclaw.json` no longer has `$include` or `gateway` section — purely
  user-owned seed content
- `merge.js` added for init-time deep-merge logic
- `operator.json` structure unchanged

### Controller changes

Minimal. The controller continues to build `operator.json` with gateway
settings, CORS, providers; inject Route host, providers, skills into the
ConfigMap; and stamp config hash for rollout detection.

The only controller addition is `configureClawDeploymentConfigMode`, which
injects a `CLAW_CONFIG_MODE` environment variable on the init container based
on `spec.configMode` (same pattern as existing env var injection on the proxy
deployment).
