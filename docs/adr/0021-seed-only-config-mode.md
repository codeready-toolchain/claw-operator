# ADR-0021: `seedOnly` Config Mode + `ClawOperatorConfig` Admin Gating

**Status:** Implemented
**Date:** 2026-07-06

## Overview

`spec.config.mergeMode` today offers two behaviors, both of which re-apply
operator-managed `openclaw.json` keys (`gateway.*`, `models.providers`,
`channels.*`, `mcp.servers`, model catalog, etc.) on **every** pod restart,
forever:

- `merge` (default): operator keys always win on collision; user-owned keys
  (agents list, non-declared plugins/channels, tools, cron, etc.) persist.
- `overwrite`: PVC state is ignored entirely; config is rebuilt from scratch
  every restart.

This addresses [GitHub issue #224](https://github.com/codeready-toolchain/claw-operator/issues/224),
which asks for a third mode where the operator seeds `openclaw.json` **once**
on first boot, then treats the file (plus MCP config) as belonging to the
user/agent from then on. A narrow set of infrastructure/security keys remain
operator-enforced regardless of mode, since those aren't safe to delegate —
everything else becomes durable user state instead of "whatever key the
operator doesn't currently recognize."

This matters for interactive/agentic workflows where the running OpenClaw
instance (or the user, via the UI/CLI) legitimately mutates its own
configuration over time — installing plugins, registering custom providers,
tuning MCP servers — and expects those changes to survive restarts.

Full design exploration, including the two-bucket ownership analysis and five
resolved open questions, lives in
[docs/proposals/user-owned-config-design.md](../proposals/user-owned-config-design.md)
and [docs/proposals/user-owned-config-questions.md](../proposals/user-owned-config-questions.md).
This ADR records the as-implemented decisions.

> **Naming disambiguation:** `seedOnly` (this ADR) is a `ConfigMode` value
> (`spec.config.mergeMode`) governing the *whole* `openclaw.json` file. It is
> unrelated to the pre-existing `SeedMode` type's `seedIfMissing` value
> (`spec.workspace.*.mode`), which governs *per-file* workspace seeding. The
> similar names are coincidental; see the doc-comments on `ConfigMode` and
> `SeedMode` in `api/v1alpha1/claw_types.go` for the cross-reference.

## Design Principles

1. **Opt-in, not default.** Existing `merge`/`overwrite` behavior is
   unchanged. A CR must explicitly request `seedOnly`.
2. **Security/network boundaries are never delegated.** Regardless of mode,
   the operator remains the sole authority over gateway bind/port, auth mode,
   credential injection (proxy), Secrets, NetworkPolicies, and RBAC.
3. **"Seed once" is a natural extension of existing behavior**, not a new
   concept — first-run seeding into an empty PVC already happens today. The
   new mode changes what happens on *subsequent* restarts only.
4. **No new convergence mechanism is needed.** The existing
   reconcile → recompute `operator.json` → hash → conditional rollout
   pipeline already delivers fresh operator-desired state to every pod on
   every relevant trigger. `seedOnly` only changes what `merge.js` *does*
   with that fresh state once it reaches the pod.
5. **Fail safe, not silent.** If a cluster admin disallows a mode, a Claw
   that requests it gets clear, visible feedback (`Ready: False`), not a
   silent fallback to a different mode.

## Ownership Boundary: The Two-Bucket Model

Several keys that look like ordinary "operator-declared app config"
(`models.providers`, credential-bearing `channels.*`/`mcp.servers` entries)
**cannot actually be operated by a user hand-editing the file**, in any mode
— e.g. a provider's `apiKey` is always the literal placeholder string the
proxy intercepts, and the pod's `NetworkPolicy` only permits egress the CR
declares. Treating these as "user-owned" would be a promise the architecture
can't keep.

This gives a precise test: **would hand-editing this field directly in
`openclaw.json`, with no CR change, actually produce a working
configuration?** If no (because it needs a Secret, NetworkPolicy rule, proxy
route, or Deployment env var only reconcile can create), the field is
**Bucket A** — always operator-managed, in every mode. If yes, it's **Bucket
B** — a genuine candidate for `seedOnly` freezing.

### Bucket A — always operator-managed

- **Pure infrastructure/security**, reasserted unconditionally by `merge.js`
  in every mode: `gateway.mode`/`.bind`/`.port`/`.controlUi.enabled`,
  `gateway.auth.mode`, `gateway.controlUi.dangerouslyDisableDeviceAuth`,
  `gateway.trustedProxies`, the Route-host entry in
  `gateway.controlUi.allowedOrigins` (append-only), `tools.web.search.*`,
  `agents.defaults.memorySearch.*`, `diagnostics.otel.{metrics,metricsEndpoint}`.
  Credential injection itself never touches `openclaw.json` at all — it
  happens transparently at the MITM proxy layer.
- **Credential/routing-critical sub-fields** of a CR-declared
  provider/channel/MCP-server entry — only these specific sub-fields are
  reasserted; everything else in the same entry is Bucket B:
  - `models.providers.<name>.{baseUrl,apiKey,api}`
  - `channels.<name>.{enabled,botToken,token,appToken}` (the existing
    `protectedChannelKeys` allowlist)
  - `mcp.servers.<name>` — **full-entry** replace, but only for servers using
    `envFrom`/`credentialRef` or reached via URL (proxy-routed). `merge.js`
    can't infer this from JSON shape alone (a `command`+`env` entry looks
    identical either way), so `injectMcpServers` (`claw_mcp.go`) writes a
    private `_seedOnlyMeta.mcpBucketAServers` marker into `operator.json` for
    `merge.js` to read — stripped before the file ever reaches the PVC, in
    every mode.

### Bucket B — user-manageable, gated by mode

Everything that passes the hand-edit test: `agents.defaults.model.primary`/
`.fallbacks`, `agents.defaults.models` (catalog aliases), a provider's local
`.models` array, a channel's non-Bucket-A fields (`dmPolicy`, `allowFrom`,
etc.), a command+inline-env MCP server's full entry, `agents.list`, `cron.*`,
non-declared channels/MCP servers/plugins, and skill docs.

### Reassertion + gap-fill mechanism

For each provider/channel/MCP-server key present in **both** the freshly
computed `operator.json` and the existing PVC file, `merge.js` overwrites
only the Bucket-A sub-fields above, leaving the rest of the entry untouched.
This is a **path-level** reassertion, not `merge` mode's whole-key
`deepMerge` — a materially more error-prone shape of logic, since
under-reasserting silently leaves a security field unprotected while
over-reasserting silently clobbers a user customization, and neither failure
surfaces until the *second-or-later* restart of an already-seeded instance.
See `internal/controller/claw_merge_test.go`'s `TestMergeJSSeedOnly` for the
exhaustive test matrix this required.

A second, shallow **gap-fill pass** adds any top-level key present in
`operator.json`'s Bucket-B collections (`models.providers`, `channels`,
`mcp.servers`, `agents.defaults.models`) but absent from the PVC file — this
is what makes "add a new provider/channel/MCP server to the CR" work
automatically, without requiring an opt-in step. It never recurses into an
already-existing entry's fields. The same pass fills
`agents.defaults.model.primary`/`.fallbacks` only if currently absent/empty,
mirroring the pre-existing `merge`-mode carve-out.

**Known accepted rough edges** (documented, not solved by this ADR):
- Removing a declared credential/channel/MCP server from the CR after
  seeding leaves an orphaned stub in the PVC file — its Bucket-A fields
  point at a proxy route/Secret that no longer exists and start failing at
  the network layer, but nothing deletes or reasserts it.
- Updating an **already-existing** Bucket-B entry's content (not adding a
  new one) has no automatic path — deferred to a future opt-in resync
  annotation (not implemented in this change).
- Bucket-B schema-breaking migrations (an image upgrade that changes the
  *shape* of an existing key) are explicitly out of scope.

## Cluster-Admin Gating: `ClawOperatorConfig`

No mechanism existed for a cluster admin to restrict which `mergeMode`
values a `Claw` CR author may use. This ADR adds `ClawOperatorConfig`, a new
CRD (`api/v1alpha1/clawoperatorconfig_types.go`) that a reconcile-time check
enforces:

- **Scope:** Namespaced, with the singleton instance (name
  `ClawOperatorConfigSingletonName` = `"cluster"`) required to live in the
  operator's own runtime namespace — resolved dynamically via the
  `WATCH_NAMESPACE` env var (Kubernetes downward API,
  `fieldRef: metadata.namespace`), never hardcoded, and never looked up in a
  tenant namespace.
- **Why Namespaced, not Cluster-scoped:** Both are equally safe from
  accidental tenant access; Namespaced wins on RBAC footprint (a
  `Role`/`RoleBinding` in one namespace vs. a `ClusterRole`), consistent with
  this project's minimal-privilege conventions.
- **Why not a webhook:** No webhook infrastructure exists anywhere in this
  operator today, and reconcile-time status-condition enforcement is the
  established pattern for equivalent self-service gating in the closest
  upstream project.
- **Fail-open by design:** if the singleton doesn't exist, or exists with an
  empty `allowedConfigModes`, every mode is allowed — preserving today's
  unrestricted behavior until an admin explicitly opts in to restricting it.

`ClawResourceReconciler.checkConfigModeAllowed`
(`internal/controller/claw_operator_config.go`) runs early in `Reconcile`,
before credential resolution. On denial, it reuses the existing `Ready`
condition — no new condition type — setting `Ready: False` with reason
`ConfigModeNotAllowed` and returning `nil` (not an error), so
controller-runtime doesn't apply exponential-backoff retries to what's a
stable policy mismatch, not a transient failure. Because halting reconcile
early never re-renders the Deployment, an already-running instance whose
mode becomes disallowed after the fact (an admin tightens policy
retroactively) is never restarted or downgraded — it keeps running
untouched, just with `Ready: False` until the mismatch is resolved.

`SetupWithManager` also watches `ClawOperatorConfig` and enqueues every
`Claw` CR in the cluster on change, so a policy edit is reflected promptly
instead of waiting for an unrelated reconcile trigger.

## Summary Table

| Row | Current `merge` | Current `overwrite` | `seedOnly` |
|---|---|---|---|
| **Bucket A** (infra/auth + declared-entry credential sub-fields) | Reasserted every restart, but at whole-entry granularity (resets Bucket-B fields of the same entry too) | Reasserted every restart as part of the full-file rebuild | Reasserted every restart, at sub-field granularity only — doesn't touch the entry's Bucket-B fields |
| **Bucket B — declared-entry fields** (`dmPolicy`, a provider's local `.models`, etc.) | Reset every restart as a side effect of whole-entry overwrite, except `agents.defaults.model.primary`/`.fallbacks` (pre-existing carve-out) | Reset every restart — full rebuild, PVC not read | Frozen once seeded; a brand-new CR-declared entry still gap-fills automatically; updating an existing entry needs a future opt-in resync |
| **Bucket B — freeform** (`agents.list`, `cron.*`, non-declared channels/MCP servers) | Preserved (side effect of `deepMerge` only touching known paths) | Wiped — full rebuild ignores the PVC file | Preserved — same outcome, now a designed guarantee |
| **Skill docs** (`PLATFORM.md`, `KUBERNETES.md`, `_skill_*`) | `copyAlways` — overwritten every restart | Same — overwritten every restart | `seedIfMissing` — seed once; user/agent edits or deletions persist |
| **Workspace files** (`AGENTS.md`, `SOUL.md`, `BOOTSTRAP.md`) | `seedIfMissing`, unaffected by `mergeMode` | Same — unaffected | No change — this mechanism never reads `CLAW_CONFIG_MODE` |

## Decisions

| # | Question | Decision |
|---|----------|----------|
| Q1 | Mechanism for cluster-admin gating | `ClawOperatorConfig`, a Namespaced singleton CRD in the operator's own runtime namespace (resolved via `WATCH_NAMESPACE`) |
| Q2 | How gating violations are surfaced | Reuse the existing `Ready` condition (no new condition type); halt reconcile without returning an error |
| Q3 | Ownership boundary | Two-bucket model, with sub-field-level reassertion for declared providers/channels/MCP servers |
| Q4 | Update/convergence mechanism | Bucket A: covered by the existing reconcile→hash→rollout pipeline, no new mechanism. Bucket B: automatic gap-fill for *new* entries; opt-in resync for *existing* entries is a deferred fast-follow; schema-breaking migrations are explicit out-of-scope future work |
| Q5 | Naming for the new `mergeMode` value | `seedOnly` — disambiguated from the unrelated `SeedMode`/`seedIfMissing` via doc-comments rather than a rename |
| Missing `ClawOperatorConfig` behavior | Fail-open — no singleton means every mode is allowed |

## Implementation Notes

- `api/v1alpha1/claw_types.go`: `ConfigMode` enum gains `seedOnly`;
  `ConditionReasonConfigModeNotAllowed` added.
- `api/v1alpha1/clawoperatorconfig_types.go`: new CRD, no status subresource
  (a misconfigured/missing singleton surfaces via the *Claw*'s own `Ready`
  condition, not this type's).
- `internal/controller/claw_operator_config.go`: `effectiveConfigMode` and
  `checkConfigModeAllowed` gating logic.
- `internal/controller/claw_mcp.go`: `injectMcpServers` writes the private
  `_seedOnlyMeta` marker; `mcpServerIsBucketA` decides membership
  (`command == ""` i.e. URL-based, or non-empty `EnvFrom`, or
  `CredentialRef` set).
- `internal/assets/manifests/claw/configmap.yaml`'s `merge.js`: new
  `seedOnly` branch (`reassertInfrastructureKeys`, `reassertBucketA`,
  `gapFillBucketB`), plus a `seedIfMissing` skill-doc helper. The
  `_seedOnlyMeta` marker is stripped immediately after being read, in every
  mode, so it never reaches the user-facing `openclaw.json`.
- `cmd/main.go` / `config/manager/manager.yaml`: `WATCH_NAMESPACE` wired via
  the downward API; the manager fails fast at startup if unset.
