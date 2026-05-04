# Drop `$include`, Deep-Merge Config at Init Time

**Status:** Final

## Overview

OpenClaw's `$include` directive at the root of `openclaw.json` blocks all runtime
config writes — plugin installs, `config.patch` via the gateway tool, and any
automated mutation. This breaks WhatsApp setup and any other plugin that needs to
persist config.

The root cause is OpenClaw's flatten guard (`preserveUntouchedIncludes`): a
root-level `$include` makes `collectIncludeOwnedPaths` record path `[]`, and
`patchTouchesPath(patch, [])` returns true for **any** non-empty patch. Every
write is rejected with _"Config write would flatten $include-owned config at
\<root\>"_.

This design replaces the `$include` approach with init-time deep-merging, where
the init container merges operator-managed settings into the user's config file
on every pod start. This is the proven pattern used by every other OpenClaw
deployment tool (openclaw-operator, openclaw-installer, NemoClaw, paude).

## Design Principles

1. **OpenClaw sees a plain JSON file** — no `$include`, no write barriers. All
   standard config mutations (plugin install, `config.patch`, UI settings) work.

2. **Operator controls what it needs to** — gateway settings, CORS origins,
   provider definitions, and cron are always overwritten. The operator is the
   source of truth for infrastructure config.

3. **User changes survive pod restarts** (in `merge` mode) — keys the operator
   doesn't touch (plugins, agent config, model preferences, channels) persist on
   the PVC across restarts.

4. **Lockdown available** — `spec.configMode: overwrite` disables runtime config
   mutability for managed/shared deployments where user edits are not desired.

5. **Minimal change surface** — the ConfigMap structure, controller injection
   logic, and Kustomize pipeline remain the same. Only the init container script
   and `openclaw.json` seed change.

## Architecture / How It Works

### New flow

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
│  cron: {from operator.json}          │
└──────────────────────────────────────┘
         │
  OpenClaw loads ──▶ plain JSON, no includes
         │
  Plugin install ──▶ WORKS (standard write path)
  config.patch   ──▶ WORKS
  WhatsApp setup ──▶ WORKS
```

### What gets merged and who wins

| Config section | Owner | Behavior on pod restart (merge mode) |
|---|---|---|
| `gateway.*` | Operator | **Overwritten** — CORS origins, auth mode, bind, port, trustedProxies |
| `models.providers` | Operator | **Overwritten** — tracks credentials in Claw CR |
| `cron` | Operator | **Overwritten** |
| `agents.*` | User | **Preserved** — model aliases, primary model, agent list, workspace |
| `plugins.*` | User | **Preserved** — plugin installs, allow/deny lists |
| `channels.*` | User | **Preserved** — WhatsApp, Telegram, etc. |
| `tools.*` | User | **Preserved** |

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
  managed deployments (Dev Sandbox shared instances) where the operator is the
  sole config authority.

### Init container

Uses the gateway image (`ghcr.io/openclaw/openclaw:slim`) — same image as the
main container, already cached. Provides Node.js for the merge script. Replaces
the current busybox-based `init-config` container.

**Merge mode script** (pseudocode):
```javascript
const base = JSON.parse(fs.readFileSync("/home/node/.openclaw/openclaw.json"));
const ops = JSON.parse(fs.readFileSync("/config/operator.json"));

function deepMerge(target, source) {
  const result = { ...target };
  for (const key of Object.keys(source)) {
    if (source[key] && typeof source[key] === "object" && !Array.isArray(source[key])
        && result[key] && typeof result[key] === "object" && !Array.isArray(result[key])) {
      result[key] = deepMerge(result[key], source[key]);
    } else {
      result[key] = source[key];
    }
  }
  return result;
}

fs.writeFileSync("/home/node/.openclaw/openclaw.json",
  JSON.stringify(deepMerge(base, ops), null, 2));
```

**Overwrite mode script** (pseudocode):
```javascript
const seed = JSON.parse(fs.readFileSync("/config/openclaw.json"));
const ops = JSON.parse(fs.readFileSync("/config/operator.json"));
// Always start from seed (ignore PVC state) — user edits don't survive
fs.writeFileSync("/home/node/.openclaw/openclaw.json",
  JSON.stringify(deepMerge(seed, ops), null, 2));
```

The key difference: merge mode reads the **existing PVC file** as the base (preserving
user changes), while overwrite mode reads the **ConfigMap seed** as the base (resetting
to operator-defined state every restart).

### Files on PVC

| File | Written by | When |
|---|---|---|
| `openclaw.json` | init container | Every pod start (merge or overwrite) |
| `workspace/AGENTS.md` | init container | Seed if missing |
| `workspace/skills/proxy/SKILL.md` | init container | Always (operator-managed) |
| `workspace/skills/kubernetes/SKILL.md` | init container | Always when k8s credentials present |

Note: `operator.json` is **not** written to PVC. It's only read from the
ConfigMap volume mount at `/config/`.

### ConfigMap structure (updated)

```yaml
data:
  operator.json: |
    {
      "gateway": { ... },
      "models": { "providers": { ... } },
      "cron": { "enabled": false }
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
  AGENTS.md: |
    ...
  PROXY_SETUP.md: |
    ...
```

Key changes from current:
- `openclaw.json` no longer has `$include` or `gateway` section — purely
  user-owned seed content
- `operator.json` gains no new fields (structure unchanged)

### Controller changes

Minimal. The controller continues to:
- Build `operator.json` with gateway settings, CORS, providers, cron
- Inject Route host, providers, skills into the ConfigMap
- Stamp config hash for rollout detection

The only controller change is making the init container script conditional on
`spec.configMode`. The controller injects a `CLAW_CONFIG_MODE` environment
variable on the init container (same pattern as existing env var injection on
the proxy deployment via `configureProxyDeployment`).

## Implementation Plan

### Phase 1: Drop `$include` and implement merge

1. Update `configmap.yaml`: remove `$include` and `gateway` section from
   `openclaw.json` seed
2. Update `deployment.yaml`: change `init-config` to gateway image, implement
   merge/overwrite script with mode detection
3. Update tests: assert no `$include` in `openclaw.json` seed, verify ConfigMap
   content

### Phase 2: Add `spec.configMode` to CRD

4. Add `ConfigMode` field to `ClawSpec` in `claw_types.go` with enum validation
5. Run `make manifests && make generate`
6. Update controller to inject `CLAW_CONFIG_MODE` env var on init container
   (new function `configureClawDeploymentConfigMode` following existing patterns)
7. Init script already branches on `$CLAW_CONFIG_MODE` from Phase 1
8. Add tests for both modes (merge preserves user keys, overwrite resets)

### Phase 3: Update documentation

9. Update `CLAUDE.md` with new config approach
10. Update `PROXY_SETUP.md` if needed
