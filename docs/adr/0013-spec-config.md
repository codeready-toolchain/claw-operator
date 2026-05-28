# ADR-0013: `spec.config` — User-Provided OpenClaw Configuration

**Status:** Implemented
**Date:** 2026-05-26

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
   disables it.

5. **Merge semantics preserved** — `merge.js` and `spec.config.mergeMode`
   continue to control how `operator.json` is applied to the PVC at pod start.
   The change is in what goes into `operator.json`, not how `operator.json` is
   applied to the PVC.

## Architecture

### Config resolution flow

```
Kustomize build (unchanged — produces ConfigMap with static template)
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

The two-file model (`operator.json` + `openclaw.json` seed) is preserved.
`operator.json` now contains both user and operator keys. The seed
`openclaw.json` continues to provide first-run defaults for `agents.list` and
`agents.defaults.workspace` — it is unchanged regardless of `spec.config`.

### Config precedence (three layers)

When `merge.js` runs at pod start, the final config has three layers:

1. **PVC runtime state** (highest) — user changes via UI, `config.patch`,
   or plugin installs. Preserved by `merge.js` for keys not in `operator.json`.
   The user's runtime primary model choice wins over everything.
2. **`operator.json`** — contains operator-managed keys (always-win tier) plus
   user keys from `spec.config.raw` (append/merge/user-only tiers). Wins over
   PVC for matching keys (except primary model).
3. **`openclaw.json` seed** (lowest) — first-run defaults for `agents.list` and
   `agents.defaults.workspace`. Used only when no PVC file exists yet.

## CRD Changes

`spec.configMode` moves into `spec.config.mergeMode`. The top-level
`configMode` field is removed.

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
            primary: google/gemini-3.5-flash
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
      plugins:
        entries:
          diagnostics-otel:
            enabled: true
```

## Three-Tier Enrichment

### Tier 1 — Always-win

Keys driven by typed CRD fields. Operator sets unconditionally, user cannot
override.

| Key | Driven by |
|-----|-----------|
| `gateway.mode`, `gateway.bind`, `gateway.port`, `gateway.controlUi.enabled` | Infrastructure — must match pod networking |
| `gateway.auth.*`, `gateway.controlUi.dangerouslyDisableDeviceAuth` | `spec.auth` |
| `models.providers` | `spec.credentials` |
| `channels.*`, `plugins.entries.<channel>` | `spec.credentials[].channel` |
| `mcp.servers` | `spec.mcpServers` |
| `tools.web.*`, `plugins.entries.<search>` | `spec.webSearch` |

### Tier 2 — Append/merge

Operator infra is always present. User entries are merged or appended on top.

| Key | Behavior |
|-----|----------|
| `gateway.controlUi.allowedOrigins` | Operator always appends Route host; user entries are additional origins |
| `gateway.trustedProxies` | Operator always appends RFC1918 ranges; user entries are additional CIDRs |
| `agents.defaults.models` | Operator merges catalog from credentials; user entries win on key collision |
| `agents.defaults.model.primary` | Operator provides catalog default; user value wins; runtime PVC choice wins over both |

### Tier 3 — User-only

Keys the operator never touches. User has full control via `spec.config.raw`.

`diagnostics.*`, `session.*`, `logging.*`, `agents.list`,
`plugins.*` (non-declared), `skills.*`, `ui.*`, `cron.*`, `hooks.*`,
`browser.*`, `memory.*`, `talk.*`, `discovery.*`, `update.*`, etc.

### `plugins.entries` split ownership

The `plugins.entries` object is shared. Operator-declared entries (from channels
and search) are always written by their respective enrichment functions.
User-managed entries (for non-declared plugins) are preserved because the
enrichment functions merge into the existing entries map rather than replacing it.

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| Q1 | Where does user config go? | Merged into `operator.json` | Minimal structural change, correct precedence, keeps two-file model. A third file would add three-way merge complexity for marginal benefit. |
| Q2 | `configMode` placement | Moves to `spec.config.mergeMode` | Groups config concerns cleanly. Pre-release API (`v1alpha1`) — no backward compat cost. |
| Q3 | Enrichment key policies | Three-tier: always-win / append-merge / user-only | Typed CRD field drives it = always-win. Operator infra keys = append/merge. Everything else = user-only. Users cannot silently break proxy routing or auth. |
| Q4 | CORS enrichment | Append Route host to user's list | Operator infra works OOTB. Skip-if-set risks breaking default Route CORS silently. |
| Q5 | Model catalog interaction | Merge — catalog always present, user wins on collision | Catalog provides OOTB defaults. User entries override on collision. Adding a credential automatically adds its models even when `spec.config.raw` is set. |
| Q6 | `configMapRef` | Not implemented; only `raw` for now | Simpler. Inline raw covers 90% of use cases. Can add `configMapRef` later if there's real demand. |
| Q7 | Seed behavior | Unchanged — seed stays as first-run fallback | Clean separation: seed = first-run defaults, operator.json = current desired state. |
| Q8 | Model catalog storage | Hardcoded in Go; `spec.config.raw` is the escape hatch | Zero-config experience for supported providers. Individual deployments are never blocked — they add models via `spec.config.raw`. |

## Backward Compatibility

When `spec.config` is nil (existing CRs):
- User config = `{}`
- Deep-merge into template = template unchanged
- Enrichment pipeline = same behavior as today (no user keys to skip for)
- Result = identical `operator.json` to current behavior

When `spec.config.mergeMode` is not set, it defaults to `merge` (same default
as the former `spec.configMode`).

## Future Considerations

- **`configMapRef`**: If users need to manage large configs outside the CR
  (GitOps workflows, multi-environment config), a `spec.config.configMapRef`
  field can be added alongside `raw`.
- **Model catalog externalization**: The hardcoded catalog could move to a
  ConfigMap or CRD if update frequency increases beyond what operator releases
  can keep up with.
