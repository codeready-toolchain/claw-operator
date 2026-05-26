# Design: `spec.config` — User-Provided OpenClaw Configuration

**Status:** Final — all decisions resolved in
[spec-config-questions.md](spec-config-questions.md)

---

## Overview

Add a `spec.config` field to the Claw CRD that accepts arbitrary OpenClaw
configuration (model settings, CORS origins, diagnostics, agent defaults, etc.)
without requiring per-feature typed CRD fields.

This addresses multiple known gaps: model registration, CORS origins,
diagnostics/OTEL configuration, and future OpenClaw features — all through a
single architectural change.

The `spec.config.raw` + enrichment pipeline pattern is well-established in
similar Kubernetes operators and Helm charts for opinionated applications.

## Design Principles

1. **Typed fields for Kubernetes side effects** — credentials, auth, MCP
   servers, and anything that drives proxy config, NetworkPolicies, Secrets, or
   init container behavior stays as typed CRD fields.

2. **Raw config for OpenClaw application settings** — model preferences, CORS
   extras, diagnostics, agent defaults, session config, and any other
   `openclaw.json` key goes through `spec.config`.

3. **Backward compatible** — existing CRs without `spec.config` must produce
   identical behavior to today. The enrichment pipeline produces the same
   `operator.json` when no user config is provided.

4. **Operator infra works OOTB, user extends** — operator-managed
   infrastructure (gateway networking, proxy routing, auth, channel wiring)
   always works out of the box, regardless of what the user sets in
   `spec.config`. User config *adds to* operator infra, never silently
   disables it. This means:

   - **Always-win keys**: Driven by typed CRD fields (`spec.auth`,
     `spec.credentials`, `spec.mcpServers`, `spec.webSearch`). The operator
     sets these unconditionally — user config cannot override them.
   - **Append/merge keys**: Operator always provides its part, user *adds*
     on top. Example: CORS `allowedOrigins` always includes the Route host;
     user entries are appended. Model catalog always includes auto-discovered
     models from credentials; user entries are merged in and win on collision.
   - **User-only keys**: Keys the operator never touches (`diagnostics.*`,
     `session.*`, `logging.*`, etc.). User fully owns these.

5. **Merge semantics preserved** — `merge.js` and `spec.config.mergeMode`
   continue to control how `operator.json` is applied to the PVC at pod start.
   The change is in what goes into `operator.json`, not how `operator.json` is
   applied to the PVC.

## Architecture

### Current flow (no `spec.config`)

```
Static operator.json template in configmap.yaml
        ↓
Kustomize build (template replacement, label injection)
        ↓
Enrichment pipeline (7 operator.json injections, all unconditional)
        ↓
operator.json in ConfigMap (only operator-managed keys)
        ↓
Init: merge.js deepMerge(PVC, operator.json) → PVC openclaw.json
        ↓
OpenClaw reads PVC openclaw.json
```

### New flow (with `spec.config`)

```
Kustomize build (same as today)
        ↓
Extract operator.json from ConfigMap in Kustomize output
        ↓
Deep-merge user's spec.config.raw INTO operator.json template
  (user keys win over template defaults)
        ↓
Enrichment pipeline (three-tier behavior on the merged config)
        ↓
Write enriched operator.json back into ConfigMap
        ↓
Init: merge.js deepMerge(PVC, operator.json) → PVC openclaw.json
        ↓
OpenClaw reads PVC openclaw.json
```

The key change: after Kustomize produces the ConfigMap, `operator.json` is
extracted and the user's raw config is deep-merged into it (user wins on
collision). Then the enrichment pipeline applies the three-tier model —
always-win keys are set back unconditionally, append keys combine user and
operator values, user-only keys pass through untouched.

### ConfigMap structure (unchanged)

```yaml
data:
  operator.json: |
    { ... user config + operator enrichment ... }
  openclaw.json: |
    { ... seed: agents.list, workspace defaults ... }
  merge.js: |
    ...
  AGENTS.md: |
    ...
  PLATFORM.md: |
    ...
```

The two-file model (`operator.json` + `openclaw.json` seed) is preserved.
`operator.json` now contains both user and operator keys. The seed
`openclaw.json` continues to provide first-run defaults for `agents.list` and
`agents.defaults.workspace` — it is unchanged regardless of `spec.config`.

## CRD Changes

### New types

```go
// ConfigSpec defines user-provided OpenClaw configuration.
type ConfigSpec struct {
    // Raw is inline openclaw.json configuration as arbitrary JSON.
    // Keys set here are merged into operator.json before the enrichment
    // pipeline runs. User-set keys take precedence over operator defaults
    // for non-security-critical settings.
    // +kubebuilder:pruning:PreserveUnknownFields
    // +optional
    Raw *RawConfig `json:"raw,omitempty"`

    // MergeMode controls how operator config is applied on pod start.
    // "merge" (default) deep-merges operator settings into the existing
    // user config, preserving user-owned keys. "overwrite" fully replaces
    // the config on every pod start.
    // +optional
    // +kubebuilder:validation:Enum=merge;overwrite
    // +kubebuilder:default=merge
    MergeMode ConfigMode `json:"mergeMode,omitempty"`
}

// RawConfig holds arbitrary JSON configuration for openclaw.json.
// +kubebuilder:pruning:PreserveUnknownFields
type RawConfig struct {
    runtime.RawExtension `json:",inline"`
}
```

### Updated ClawSpec

`spec.configMode` moves into `spec.config.mergeMode`. The top-level
`configMode` field is removed.

```go
type ClawSpec struct {
    // Config provides user-supplied OpenClaw configuration and merge behavior.
    // +optional
    Config *ConfigSpec `json:"config,omitempty"`

    Auth        *AuthSpec                `json:"auth,omitempty"`
    Credentials []CredentialSpec         `json:"credentials,omitempty"`
    McpServers  map[string]McpServerSpec `json:"mcpServers,omitempty"`
    WebSearch   *WebSearchSpec           `json:"webSearch,omitempty"`
    WebFetch    *WebFetchSpec            `json:"webFetch,omitempty"`
    Idle        bool                     `json:"idle,omitempty"`
}
```

### Example CR

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: my-claw
spec:
  credentials:
    - name: google
      provider: google
      type: apiKey
      secretRef:
        - name: gemini-key
          key: api-key
  config:
    mergeMode: merge
    raw:
      agents:
        defaults:
          model:
            primary: google/gemini-3-flash-preview
          models:
            openrouter/qwen3-14b:
              alias: Qwen 3 14B
      gateway:
        controlUi:
          allowedOrigins:
            - "https://custom.example.com"
      diagnostics:
        otel:
          enabled: true
          endpoint: http://langfuse.observability.svc:3000/api/public/otel/v1/traces
          captureContent:
            inputMessages: true
            outputMessages: true
      plugins:
        entries:
          diagnostics-otel:
            enabled: true
```

## Enrichment Pipeline

### Current injection functions (all unconditional)

Seven functions modify `operator.json`. Additionally, `injectKubernetesSkill`
writes a `KUBERNETES.md` key to the ConfigMap, `injectKubePortsIntoNetworkPolicy`
modifies the NetworkPolicy, and `stampGatewayConfigHash` stamps a deployment
annotation — these three are unaffected by `spec.config`.

| # | Function | Keys managed | File |
|---|----------|--------------|------|
| 1 | `injectRouteHostIntoConfigMap` | `gateway.controlUi.allowedOrigins` | `claw_resource_controller.go` |
| 2 | `injectAuthModeIntoConfigMap` | `gateway.auth.*`, `gateway.controlUi.dangerouslyDisableDeviceAuth` | `claw_auth.go` |
| 3 | `injectProvidersIntoConfigMap` | `models.providers` | `claw_resource_controller.go` |
| 4 | `injectModelCatalogIntoConfigMap` | `agents.defaults.models`, `agents.defaults.model.primary` | `claw_resource_controller.go` |
| 5 | `injectChannelsIntoConfigMap` | `channels.*`, `plugins.entries.<channel>` | `claw_channels.go` |
| 6 | `injectMcpServersIntoConfigMap` | `mcp.servers` | `claw_mcp.go` |
| 7 | `injectWebSearchIntoConfigMap` | `tools.web.*`, `plugins.entries.<search>` | `claw_web_search.go` |

Note: `gateway.mode`, `gateway.bind`, `gateway.port`, `gateway.controlUi.enabled`,
and `gateway.trustedProxies` currently have **no enrichment function** — they
come only from the static template in `configmap.yaml`. Without `spec.config`
this is fine (no user config to conflict). With `spec.config`, the enrichment
pipeline must explicitly enforce these values (see proposed changes below).

### Three-tier enrichment behavior

| Key | Tier | Change needed |
|-----|------|---------------|
| `gateway.mode`, `gateway.bind`, `gateway.port` | Always-win | **New:** explicit set after merge (currently template-only) |
| `gateway.controlUi.enabled` | Always-win | **New:** explicit set after merge (currently template-only) |
| `gateway.auth.*` | Always-win | **Modify:** `injectAuthModeIntoConfigMap` must become unconditional — currently skips when auth is nil/token mode, leaving user-set values intact |
| `gateway.controlUi.dangerouslyDisableDeviceAuth` | Always-win | Covered by auth function above |
| `models.providers` | Always-win | No change — already unconditional |
| `channels.*` | Always-win | No change — already unconditional |
| `plugins.entries.<channel>` | Always-win | No change — operator entries are merged into `plugins.entries`, user-managed plugin entries for non-declared keys are preserved (see note below) |
| `plugins.entries.<search>` | Always-win | No change — same merge behavior as channels |
| `mcp.servers` | Always-win | No change — already unconditional |
| `tools.web.*` | Always-win | No change — already unconditional |
| `gateway.controlUi.allowedOrigins` | Append | **Modify:** `injectRouteHostIntoConfigMap` must change from string replacement of `OPENCLAW_ROUTE_HOST` placeholder to JSON-level array append |
| `gateway.trustedProxies` | Append | **New:** append RFC1918 ranges to user's list (currently template-only) |
| `agents.defaults.models` | Merge | **Modify:** `injectModelCatalogIntoConfigMap` must change from full overwrite to merge — catalog entries are added, user entries win on collision |
| `agents.defaults.model.primary` | Merge | **Modify:** user value wins over catalog default; PVC runtime choice wins over both (via `merge.js` primary preservation) |
| Everything else | User-only | No change — operator doesn't touch these keys |

**`plugins.entries` split ownership:** The `plugins.entries` object is shared.
Operator-declared entries (from channels and search) are always written by
their respective functions. User-managed entries (for non-declared plugins) are
preserved because the current functions merge into the existing entries map
rather than replacing it. This behavior must be maintained.

### Implementation patterns

**Always-win** — unconditionally set the value, overriding any user config:

```go
gateway := ensureNestedMap(config, "gateway")
gateway["mode"] = "local"
gateway["bind"] = "lan"
gateway["port"] = int64(18789)
ensureNestedMap(gateway, "controlUi")["enabled"] = true

// Auth: always set based on spec.auth, even when nil/default
gateway["auth"] = map[string]any{"mode": resolvedAuthMode}
```

**Append** — read existing user entries, append operator entries, deduplicate:

```go
origins := getStringSlice(config, "gateway", "controlUi", "allowedOrigins")
origins = appendIfMissing(origins, routeHost)
setNestedValue(config, origins, "gateway", "controlUi", "allowedOrigins")
```

**Merge** — deep-merge operator catalog into user's models; user wins on
collision:

```go
userModels := getUserModels(config)
for key, entry := range catalogModels {
    if _, exists := userModels[key]; !exists {
        userModels[key] = entry
    }
}
// Primary: user value from spec.config.raw wins over catalog default
if getUserPrimary(config) == "" {
    setPrimary(config, catalogDefault)
}
```

## Config Resolution Flow

```
1. Kustomize build (unchanged — produces ConfigMap with static template)

2. Extract operator.json from the ConfigMap in Kustomize output
   Parse as map[string]any

3. Resolve user config:
   - If spec.config.raw set → parse raw bytes as map[string]any
   - Else → empty {}

4. Deep-merge user config INTO operator.json template
   (user keys win over template defaults)

5. Run enrichment pipeline on the merged config
   (three-tier behavior: always-win / append / user-only)

6. Marshal result, write back as operator.json in the ConfigMap

7. At pod start: merge.js deepMerge(PVC, operator.json) as before
```

### Config precedence (three layers)

When `merge.js` runs at pod start, the final config has three layers of
precedence:

1. **PVC runtime state** (highest) — user changes via UI, `config.patch`,
   or plugin installs. Preserved by `merge.js` for keys not in `operator.json`.
   The user's runtime primary model choice wins over everything (via the
   `savedPrimary` logic in `merge.js`).
2. **`operator.json`** — contains operator-managed keys (always-win tier) plus
   user keys from `spec.config.raw` (append/merge/user-only tiers). Wins over
   PVC for matching keys (except primary model).
3. **`openclaw.json` seed** (lowest) — first-run defaults for `agents.list` and
   `agents.defaults.workspace`. Used only when no PVC file exists yet.

### Backward compatibility

When `spec.config` is nil (existing CRs):
- User config = `{}`
- Deep-merge into template = template unchanged
- Enrichment pipeline = same behavior as today (no user keys to skip for)
- Result = identical `operator.json` to current behavior

When `spec.config.mergeMode` is not set, it defaults to `merge` (same default
as the current `spec.configMode`).

### Nil-safety for `spec.config` access

Code that currently reads `instance.Spec.ConfigMode` must handle the case
where `spec.config` is nil (the common case for existing CRs):

```go
func getMergeMode(spec clawv1alpha1.ClawSpec) clawv1alpha1.ConfigMode {
    if spec.Config != nil && spec.Config.MergeMode != "" {
        return spec.Config.MergeMode
    }
    return clawv1alpha1.ConfigModeMerge
}
```

## Controller Changes

### `enrichConfigAndNetworkPolicy` refactor

The function currently operates on `[]*unstructured.Unstructured` (Kustomize
objects), finding the ConfigMap by name and parsing/re-parsing `operator.json`
in every injection function. With `spec.config`, the flow becomes:

1. **After Kustomize build**: extract `operator.json` from the ConfigMap,
   parse it, merge user config, run enrichment on a single `map[string]any`
2. **Write back**: marshal the enriched config and replace the `operator.json`
   key in the Kustomize output
3. **Skip per-function ConfigMap lookup**: each injection function operates on
   the shared `map[string]any` instead of hunting through Kustomize objects

This is a refactor of the injection functions' signatures **and** their logic.
The table below lists every function that needs changes:

### Function-by-function changes

| Function | Current behavior | Required change |
|----------|-----------------|-----------------|
| `injectRouteHostIntoConfigMap` | String replacement of `OPENCLAW_ROUTE_HOST` placeholder in raw JSON | Rewrite to JSON-level array append: read `allowedOrigins` array from config, append Route host if missing, write back. Must work when user has already set origins in `spec.config.raw` (placeholder may not be present). |
| `injectAuthModeIntoConfigMap` | Conditional — skips when `spec.auth` is nil and default pairing is used | Make unconditional: always write `gateway.auth.mode` (token or password) and `dangerouslyDisableDeviceAuth` (true or false) to enforce always-win. Currently, if user sets `gateway.auth.mode: "password"` in `spec.config.raw` but `spec.auth` is nil, the function skips and the user's value persists. |
| `injectProvidersIntoConfigMap` | Unconditional overwrite of `models.providers` | Signature change only (accept `map[string]any`). Logic unchanged. |
| `injectModelCatalogIntoConfigMap` | Unconditional overwrite of `agents.defaults.models` and `agents.defaults.model.primary` | Change to merge: iterate catalog entries, skip keys the user already set. For primary: only set catalog default when user didn't provide one in `spec.config.raw`. |
| `injectChannelsIntoConfigMap` | Unconditional write of `channels.*`, merges into `plugins.entries` | Signature change only. Must continue merging into existing `plugins.entries` (not replacing) to preserve user-managed plugin entries. |
| `injectMcpServersIntoConfigMap` | Unconditional write of `mcp.servers` | Signature change only. Logic unchanged. |
| `injectWebSearchIntoConfigMap` | Unconditional write of `tools.web.*`, merges into `plugins.entries` | Signature change only. Same `plugins.entries` merge behavior as channels. |
| **(new)** infrastructure keys | Not currently implemented — values come from static template only | New enrichment step: unconditionally set `gateway.mode`, `gateway.bind`, `gateway.port`, `gateway.controlUi.enabled` to their required values. Without this, user config could override these infrastructure constants. |
| **(new)** trusted proxies | Not currently implemented — RFC1918 ranges come from static template only | New enrichment step: read user's `gateway.trustedProxies` (if any), append RFC1918 ranges (`10.0.0.0/8`, `172.16.0.0/12`), deduplicate. |

The model catalog remains hardcoded in `claw_models.go` — `spec.config.raw`
provides the per-deployment escape hatch.

## Decisions Summary

All decisions from [spec-config-questions.md](spec-config-questions.md):

| # | Question | Decision |
|---|----------|----------|
| Q1 | Where does user config go? | Merged into `operator.json` |
| Q2 | `configMode` placement | Moves to `spec.config.mergeMode` |
| Q3 | Enrichment key policies | Three-tier: always-win / append-merge / user-only |
| Q4 | CORS enrichment | Append Route host to user's list |
| Q5 | Model catalog interaction | Merge — catalog always present, user wins on collision |
| Q6 | `configMapRef` | Not implemented; only `raw` for now |
| Q7 | Seed behavior | Unchanged — seed stays as first-run fallback |
| Q8 | Model catalog storage | Hardcoded in Go; `spec.config.raw` is the escape hatch |
