# User-Owned OpenClaw Runtime Config Mode

**Status:** Draft — ownership boundary and convergence mechanism decided;
implementation not started

**Source:** [GitHub issue #224](https://github.com/codeready-toolchain/claw-operator/issues/224) — "opt-in user-owned OpenClaw runtime config mode"

## Overview

Today, `spec.config.mergeMode` offers two behaviors, both of which re-apply
operator-managed `openclaw.json` keys (`gateway.*`, `models.providers`,
`channels.*`, `mcp.servers`, model catalog, etc.) on **every** pod restart,
forever:

- `merge` (default): operator keys always win on collision; user-owned keys
  (agents list, non-declared plugins/channels, tools, cron, etc.) persist.
- `overwrite`: PVC state is ignored entirely; config is rebuilt from scratch
  every restart.

Issue #224 asks for a third mode where the operator seeds `openclaw.json`
**once** on first boot, and from then on treats the file (plus workspace,
skills, and MCP config) as belonging to the user/agent — declarative
`spec.plugins` installs are the one exception, remaining operator-managed and
reinstalled every restart since they're Kubernetes-side, deterministically-
convergent state rather than `openclaw.json` content. A narrow set of
infrastructure/security keys remain operator-enforced regardless of mode,
since those aren't safe to delegate — everything else becomes durable user
state instead of "whatever key the operator doesn't currently recognize."

This addresses interactive/agentic workflows where the running OpenClaw
instance (or the user, via the UI/CLI) legitimately mutates its own
configuration over time — installing plugins, registering custom providers,
tuning MCP servers, editing skills — and expects those changes to survive
restarts.

## Design Principles

1. **Opt-in, not default.** Existing `merge`/`overwrite` behavior is
   unchanged. A deployment must explicitly request the new mode.
2. **Security/network boundaries are never delegated.** Regardless of mode,
   the operator remains the sole authority over gateway bind/port, auth mode,
   credential injection (proxy), Secrets, NetworkPolicies, and RBAC. These are
   either not stored in `openclaw.json` at all, or are in a narrow always-win
   subset applied even in the new mode (see [Ownership Boundary](#ownership-boundary)).
3. **"Seed once" is a natural extension of existing behavior**, not a new
   concept — first-run seeding into an empty PVC already happens today
   (`internal/assets/manifests/claw/configmap.yaml:108-111`). The new mode
   changes what happens on *subsequent* restarts, not on first boot.
4. **No new convergence mechanism is needed.** The existing
   reconcile → recompute `operator.json` → hash → conditional rollout
   pipeline already delivers fresh operator-desired state to every pod on
   every relevant trigger (CR edit, credential rotation, operator upgrade,
   image upgrade). `seedOnly` mode only changes what `merge.js` *does* with
   that fresh state once it reaches the pod — see
   [Convergence / Update Mechanism](#convergence--update-mechanism).
5. **Fail safe, not silent.** If a cluster admin disallows this mode, a
   deployment that requests it gets clear, visible feedback (`Ready: False`),
   not a silent fallback to a different mode.

## Ownership Boundary

This is the authoritative answer to "what does the user own vs. what does
the operator always own," cross-checked against issue #224's own two lists
and against the current enrichment pipeline
(`enrichConfigAndNetworkPolicy`, `internal/controller/claw_resource_controller.go:713-780`).

Several keys that look like ordinary "operator-declared app config"
(`models.providers`, credential-bearing `channels.*`/`mcp.servers`
entries) **cannot actually be operated by a user hand-editing the file**,
in any mode, regardless of what `mergeMode` says — so treating them as
"user-owned" would be a promise the architecture can't keep. The
`models.providers` case is documented in full in
[Q4's worked example](user-owned-config-questions.md#worked-example-adding-a-new-provider-under-seedonly-mode):
`apiKey` is always the literal placeholder string
`"ah-ah-ah-you-didnt-say-the-magic-word"`
(`claw_resource_controller.go:1260`), the pod's `NetworkPolicy` permits
egress only to the `claw-proxy` pod, DNS, and any explicitly CR-declared
exceptions (`spec.network.additionalEgress`, in-cluster MCP bypass rules) —
never to an arbitrary domain a hand-edited `openclaw.json` might reference —
and the proxy's own L7 route allowlist is built purely from the CR, never
from the file.

This generalizes to a precise test: **the right question for any given key
isn't "does the operator currently generate this from a CR field," but
"would hand-editing this field directly in `openclaw.json`, with no CR
change, actually produce a working configuration?"** If the answer is
no — because it needs a Secret, a NetworkPolicy rule, a proxy route, or a
Deployment env var that only reconcile can create — the field belongs in
the always-managed bucket, full stop, in every mode. If the answer is yes,
it's a genuine candidate for mode-gating. This gives a **two-bucket
model**, with the credential/routing-critical sub-fields of
provider/channel/MCP entries in the first bucket and everything else in
the second.

Issue #224 states:

> Operator-owned regardless of mode: gateway bind address, port, auth wiring,
> and service configuration · credential injection/proxy configuration ·
> mounted secrets · Kubernetes/OpenShift permissions · NetworkPolicies and
> egress allowlists · other cluster-admin-controlled infrastructure
> boundaries
>
> User-owned in this mode: `openclaw.json` · workspace files · skills ·
> plugins · **MCP configuration** · other OpenClaw runtime state persisted
> on the PVC

The issue's own list is directionally right but not precise at the
sub-field level — it says MCP configuration is user-owned, and that's true
for a locally-invoked MCP server's behavior, but not for the credential
wiring of one that needs a Kubernetes Secret. The two-bucket model below is
a more faithful implementation of the issue's actual intent (let users own
what they can *actually* own) than a literal per-key reading would be.

### Bucket A — Always operator-managed, in every mode (including `seedOnly`)

**Pure infrastructure/security** — unconditional, applied by `merge.js` on
every restart, in every mode:

| Keys | Function | Why it can't be delegated |
|---|---|---|
| `gateway.mode`, `gateway.bind`, `gateway.port`, `gateway.controlUi.enabled` | `enforceInfrastructureKeys` (`claw_config.go:128`) | Must match pod networking; a wrong value breaks or exposes the gateway |
| `gateway.auth.mode`, `gateway.controlUi.dangerouslyDisableDeviceAuth` | `injectAuthMode` (`claw_auth.go:71`) | Security posture — auth mode and device-pairing enforcement |
| `gateway.trustedProxies` | `enforceTrustedProxies` (`claw_config.go:145`) | Required RFC1918 ranges for correct client-IP handling behind the Route/Service |
| `gateway.controlUi.allowedOrigins` — **Route-host entry only** | `injectRouteHost` (`claw_resource_controller.go:1218`) | The Route host is a dynamic infrastructure fact (can change if the Route is recreated); append-only, the rest of the array is Bucket B |
| Credential injection (provider API keys/tokens) | MITM proxy (`internal/proxy/`), not `openclaw.json` | Never written to the file at all — injected transparently at the network layer |
| Kubernetes Secrets, NetworkPolicies, egress allowlists, RBAC | Separate K8s objects, applied via SSA | Outside `openclaw.json` entirely; unaffected by `mergeMode` in any mode today |

**Credential/routing-critical sub-fields of operator-declared entries** —
this is the corrected part. Only these specific sub-fields of a
CR-declared provider/channel/MCP-server/search entry are always reasserted;
everything else in the same entry is Bucket B (below):

| Keys | Function | Why it can't be delegated |
|---|---|---|
| `models.providers.<name>` — the entry's existence, `.baseUrl`, `.apiKey`, `.api` | `injectProviders` (`claw_resource_controller.go:1232`) | Placeholder credential + proxy-generated `baseUrl`; see the [Q4 worked example](user-owned-config-questions.md#worked-example-adding-a-new-provider-under-seedonly-mode) |
| `channels.<name>` — the entry's existence, `.enabled`, `.botToken`, `.token`, `.appToken` | `injectChannels` (`claw_channels.go:227`), reusing the existing `protectedChannelKeys` allowlist (`claw_channels.go:49`) | Same placeholder-credential pattern (`ConfigBase` in `claw_channels.go` uses literal `"placeholder"`/`"xoxb-placeholder"` strings) |
| `mcp.servers.<name>` — full entry, only for servers declared with `envFrom`/`credentialRef`, or a domain not already proxy-allowlisted | `injectMcpServers` (`claw_mcp.go:65`), `configureGatewayForMcpServers` (`claw_deployment.go:498`) | `envFrom` requires a Deployment-level env var only settable via reconcile; an undeclared domain gets a 403 from the proxy's L7 allowlist |
| `tools.web.search.enabled`, `.provider`, its search plugin entry | `injectWebSearch` (`claw_web_search.go:156`) | Provider selection references a credentialed, proxy-wired provider |
| `agents.defaults.memorySearch.provider`, `.enabled` | `injectMemorySearch` (`claw_memory_search.go:46`) | Same reasoning — references a credentialed embedding adapter |
| Metrics/diagnostics sidecar wiring | `injectMetricsConfig` (`claw_metrics.go:184`) | Wiring to the metrics sidecar container, not an app-level preference |

`spec.plugins` declarative installs (`configurePluginsInitContainer`,
`claw_plugins.go`) behave the same way for the purposes of this design even
though they're Kubernetes-side, not `openclaw.json` content: whenever any
plugins are declared (`spec.plugins` plus implicit Vertex-SDK plugins), the
init container runs `openclaw plugins install` on every pod start, in every
mode. The install script removes and reinstalls the operator-managed
extension directories each run (deterministic convergence, not a pure
additive no-op), but it doesn't touch or uninstall plugins a user installed
by hand outside of `spec.plugins`.

### Bucket B — User-manageable, gated by mode

Everything that passes the hand-edit test — safe to freeze/seed-once under
`seedOnly` because there's no structural reason it would stop working:

| Keys | Notes |
|---|---|
| `agents.defaults.model.primary`/`.fallbacks` | Already "seed once, hands off" **today**, under `merge` mode too — the hardcoded PVC-read carve-out at `configmap.yaml:92-105` (see Note 2 below), not `injectModelCatalog` |
| `agents.defaults.models` (catalog aliases) | References already-wired providers only. **Not currently protected** — `injectModelCatalog` (`claw_resource_controller.go:1299-1381`) only fills gaps *when composing `operator.json`* (Go layer), but any catalog key present in `operator.json` still gets reset to the catalog default on every restart under today's `merge` mode via `deepMerge`, matching `docs/architecture.md`'s existing ownership table (`agents.defaults.models` listed as "Operator... Overwritten every restart"). Safety under `seedOnly` comes entirely from the *new* gap-fill pass this proposal introduces, not from any existing protection |
| `models.providers.<name>.models` | Local metadata array, always empty from the operator (`buildProviderEntry`); safe for a user/agent to populate for an already-declared provider |
| `channels.<name>.*` — everything except the Bucket-A sub-fields above (e.g. `dmPolicy`, `allowFrom`) | Pure OpenClaw-side behavior for an already-declared channel |
| `mcp.servers.<name>` — full entry, only for servers declared with `command` + inline `env` only (no `envFrom`) | No network or credential gate — also genuinely addable by hand directly in the file, in any mode, without any CR change at all, as long as the domain (if any) is already proxy-allowlisted |
| `agents.list`, `tools.*` beyond web search/fetch, `cron.*`, `diagnostics.*` beyond the bootstrap hook, any channel/MCP server/plugin entry not declared in the CR | Freeform, never touched by any `injectX` function today, in any mode |
| `update.checkOnStart`, `agents.defaults.skipOptionalBootstrapFiles` | Operator product defaults, not security-relevant |
| Skill docs (`PLATFORM.md`, `KUBERNETES.md`, `_skill_*`) | See [Non-JSON Artifacts](#non-json-artifacts-workspace-skills-plugins) |
| Workspace files | Already `seedIfMissing` in every mode |

### Reassertion mechanism for declared entries

Bucket A's per-entry sub-fields need a **path-level** reassertion, not a
whole-key one — reusing the pattern the codebase already has for
`protectedChannelKeys` (currently CR-validation-only) and extending it to
apply on the running file too, even under `seedOnly`. Concretely, for each
provider/channel/MCP-server key present in **both** the freshly-computed
`operator.json` and the existing PVC file, `merge.js` overwrites only the
specific Bucket-A sub-fields listed above and leaves the rest of that entry
untouched; entries present only in the PVC file (user/agent-added, not
CR-declared) are never touched at all, in any mode.

> **Implementation note — do not skip this when implementing.** This
> path-level reassertion is genuinely new logic, not a reuse of an existing
> `merge.js` pattern: today, `merge.js` only ever does a whole-file
> `deepMerge` (`merge` mode) or fixed, statically-known paths
> (`enforceInfrastructureKeys` and friends). This mechanism instead has to
> walk dynamically-named, CR-declared entries and touch only specific
> sub-fields within each — a materially more error-prone shape of logic.
> The failure modes are both silent and asymmetric: under-reasserting
> quietly leaves a security/auth field unprotected (e.g. a stale `apiKey`
> or `botToken` nobody notices is wrong until the provider/channel stops
> authenticating), while over-reasserting quietly clobbers a Bucket-B field
> a user/agent deliberately customized — defeating the entire point of the
> mode without any error being raised. Neither failure surfaces on first
> boot (already well-covered by existing seed tests); both only show up on
> the *second-or-later* restart of an already-seeded `seedOnly` instance,
> which is exactly the scenario least likely to get manual QA attention.
> This mechanism needs deliberately exhaustive unit test coverage before
> merging — see the expanded test matrix in the
> [Implementation Plan](#implementation-plan)'s testing step. Do not ship
> this with only happy-path coverage.

**Edge case — removing a declared credential under `seedOnly`:** if a CR
author removes a credential/channel/MCP server from the CR after an
instance has already seeded under `seedOnly`, `operator.json` stops
containing that entry, so nothing reasserts it — but `merge.js` also
doesn't delete it from the PVC file (deletion-on-drift isn't something the
mode does for anything else either). The entry becomes an orphaned stub:
its Bucket-A fields (now pointing at a proxy route/Secret that no longer
exists) will start failing at the network layer, while its Bucket-B fields
remain intact but inert. This is a minor, known rough edge consistent with
the mode's overall "no automatic cleanup" philosophy — worth documenting,
not worth solving now (a resync mechanism with removal semantics could
address it later if it becomes a real problem).

### Automatic gap-fill for newly-declared entries

Bucket B's freeze applies **per entry, not per file** — this is what makes
"add a new provider/channel/MCP server to the CR" work without requiring
any opt-in step, covering the most common real-world `seedOnly` workflow
out of the box. See [Q4](user-owned-config-questions.md#automatic-gap-fill-for-newly-declared-entries)
for the full decision; summarized here because it directly extends the
reassertion mechanism above.

In `seedOnly` mode's steady-state branch, in addition to the Bucket-A
sub-field reassertion, `merge.js` applies a **shallow, add-missing-keys-only**
pass to each Bucket-B collection (`models.providers`, `channels`,
`mcp.servers`, `agents.defaults.models`): keys present in `operator.json`
but absent from the PVC file are copied in as-is; keys already present in
the PVC file are left completely untouched. This is the exact same
gap-fill principle `injectModelCatalog` (`claw_resource_controller.go:1359-1364`)
already applies today at the Go layer — this just extends the pattern to
`merge.js`'s JS layer and to the other Bucket-B collections. It's safe by
construction: a key that doesn't exist yet has nothing for a user/agent to
have customized, so adding it can never overwrite anything — the same
reasoning that already justifies `seedIfMissing` for workspace files,
applied to JSON object keys instead of whole files.

The same pass also covers two scalar fields alongside the four named
collections: `agents.defaults.model.primary` and `.fallbacks`. If either is
currently absent/empty in the PVC file and `operator.json` now has a
computed value (e.g. a catalog-eligible credential was added after an
initial seed with none), it's copied in; if either already has any value —
catalog-derived or hand-picked — it's left untouched, exactly like every
other Bucket-B field once set. This mirrors what the existing
`configmap.yaml:92-105` carve-out already does for `merge` mode today (it
only *preserves* a saved value when one exists — it never blocks
`deepMerge` from filling an empty one), so `seedOnly` isn't introducing new
behavior for this field, just matching a behavior that already ships.

This is a **shallow** pass (top-level keys of each collection only) —
critically different from the deep, recursive `deepMerge` that
`merge`/`overwrite` modes use. It must not recurse into an already-existing
entry's fields; doing so would reintroduce exactly the "silently clobbers a
Bucket-B customization" failure mode the [implementation note](#reassertion-mechanism-for-declared-entries)
above warns about, just via a different code path. This distinction (add
missing top-level keys vs. deep-merge existing ones) needs its own explicit
test coverage — see the test matrix in the
[Implementation Plan](#implementation-plan).

**Known accepted quirk:** existence is keyed off the CR, not an independent
"was this ever added" flag, so a user who manually *deletes* a
still-CR-declared entry from the file will see it silently reappear on the
next restart — gap-fill can't distinguish "never existed" from
"deliberately removed while still CR-declared." Actually removing an entry
requires removing the credential from the CR (which produces the orphaned-
stub behavior above instead). Minor, documentable, not a blocker.

**Decided:** `agents.defaults.model.primary`/`.fallbacks` are gap-filled by
the same shallow pass, not left as an accepted edge case — see above. This
was raised and resolved during design review: leaving it undecided would
have meant a CR author's "add a new credential" workflow (the single most
common `seedOnly` scenario per the [worked example](user-owned-config-questions.md#worked-example-adding-a-new-provider-under-seedonly-mode))
could silently leave the agent with no usable default model, with no
in-mode recovery short of the Q4 resync workaround — a materially worse
outcome than the four-collection gap-fill sitting right next to it in the
same pass, for what's conceptually the same trigger.

### Summary table

The two-bucket model is deliberately simple, but "current behavior" isn't
uniform within Bucket B, and non-JSON artifacts (skill docs vs. workspace
files) already follow two genuinely different mechanisms today — collapsing
either distinction into one row would misstate what actually happens. The
table below is the precise, code-verified version; see the row notes for
the specific function/line backing each cell.

| Row | Contents | Current `merge` | Current `overwrite` | Proposed `seedOnly` |
|---|---|---|---|---|
| **A — Always-managed** | Infra/auth keys (`gateway.*`), plus credential/routing sub-fields of declared providers/channels/MCP servers/webSearch/memorySearch | Reasserted every restart, but at **whole-entry** granularity — resets the entry's Bucket-B fields too, as a side effect (note 1) | Reasserted every restart as part of the full-file rebuild | Reasserted every restart, at **sub-field** granularity only (the new [reassertion mechanism](#reassertion-mechanism-for-declared-entries)) — doesn't touch the entry's Bucket-B fields |
| **B — declared-entry fields** | Non-critical fields of a CR-declared provider/channel/MCP server (e.g. `dmPolicy`, `allowFrom`, a provider's local `.models` array) | **Reset every restart** as a side effect of whole-entry overwrite (note 1) — the actual gap issue #224 reports — except the one hardcoded exception, `agents.defaults.model.primary`/`.fallbacks` (note 2), which already survives today | **Reset every restart** — the full file is rebuilt from the static seed + `operator.json`; the PVC file isn't even read (`configmap.yaml:108-111`) | **Frozen** once seeded; a brand-new CR-declared entry still gap-fills in automatically (note 3); updating an *existing* entry's content needs the opt-in resync (fast-follow) |
| **B — freeform** | Content the operator never writes into `operator.json` at all (`agents.list`, `cron.*`, non-declared channels/MCP servers/plugins) | **Preserved** — a side effect of `deepMerge` only touching key paths present in `operator.json`, not a designed guarantee | **Wiped** — the one row where `overwrite` genuinely diverges from `merge` for Bucket-B content, since the full rebuild ignores the PVC file entirely | **Preserved** — same outcome as today, now a designed guarantee rather than a side effect |
| **Skill docs** (`PLATFORM.md`, `KUBERNETES.md`, `_skill_*`) | Operator-authored reference docs copied into the workspace | `copyAlways` — **overwritten every restart**, unconditionally (no mode check at all, `configmap.yaml:113-133`) | Same — `copyAlways`, overwritten every restart | Switches to `seedIfMissing` — **seed once**; user/agent edits or deletions persist |
| **Workspace files** (`AGENTS.md`, `SOUL.md`, `BOOTSTRAP.md`, `spec.workspace` sources) | User-facing bootstrap/identity/memory files | `seedIfMissing` for builtins, already — via the separate `init-seed` container, **not gated by `CLAW_CONFIG_MODE`/`mergeMode` at all** | Same — unaffected by `mergeMode`; `overwrite` has zero effect on this mechanism | **No change** — this mechanism never reads `CLAW_CONFIG_MODE`, so `seedOnly` doesn't touch it either |

**Note 1 — `merge` mode's whole-entry reassertion:** `injectChannels`/
`injectMcpServers`/`injectWebSearch` currently write the *entire* per-entry
config block into `operator.json`, and `merge.js`'s `deepMerge` replaces
same-named keys wholesale down to scalar leaves — so under `merge` mode
today, even a Bucket-B sub-field of a *declared* entry (e.g. a declared
Telegram channel's `dmPolicy`) gets silently reset to the operator's
default on every restart, unless set through the CR's `channelConfig`
escape hatch. This is a pre-existing `merge`-mode characteristic, out of
scope to change here, and is the concrete behavior issue #224 is reacting
to.

**Note 2 — the pre-existing model-selection carve-out:** `merge.js` already
special-cases `agents.defaults.model.primary`/`.fallbacks`
(`configmap.yaml:92-105`): it reads the *currently saved* value from the
PVC file before merging, then re-applies it on top of the merge result,
regardless of what `operator.json` sets. This means one specific Bucket-B
field already behaves like "seed once, then hands off" under `merge` mode
today — a useful, real precedent for `seedOnly` generalizing the same idea
to the rest of Bucket B, rather than inventing a wholly new pattern.

**Note 3 — gap-fill:** see [Automatic gap-fill](#automatic-gap-fill-for-newly-declared-entries).

Net effect: correctly-implemented `seedOnly` mode offers genuinely new
capability beyond what `merge`/`overwrite` offer today — the ability to
hand-tune a declared channel's (or MCP server's) behavior directly in the
file and have it stick, something not currently possible in any mode.

## Convergence / Update Mechanism

The second design question this proposal must answer: when operator-desired
state changes — a CR edit, a rotated credential, an **operator upgrade**
that changes a default, or a **new OpenClaw image** that needs a different
key — how does that reach an already-running, already-seeded `seedOnly`
instance? The answer reuses the existing pipeline; no new mechanism is
introduced.

### The existing pipeline (unchanged by this proposal)

1. **Every reconcile**, regardless of `mergeMode`, `enrichConfigAndNetworkPolicy`
   fully recomputes `operator.json` in the instance's ConfigMap from the
   current CR spec, resolved credentials, and the operator's own built-in
   defaults (`claw_resource_controller.go:713-780`). This is cheap — it never
   touches the PVC — and always reflects the operator's current "desired
   state," independent of what mode governs the PVC file.
2. `stampGatewayConfigHash` (`claw_deployment.go:705`) hashes that ConfigMap
   data and stamps it onto the Deployment's pod template annotations. Any
   change to `operator.json` — for any reason — changes the hash, which
   changes the pod template, which triggers a normal Kubernetes rollout.
3. **Image upgrades** (operator upgrade with a new `OPENCLAW_IMAGE`, or a CR
   author bumping an image reference) change the pod template's image field
   directly, which independently triggers a rollout through the same
   Deployment mechanism — no extra plumbing needed.
4. On every rollout, the new pod's `init-config` container runs `merge.js`
   with the **freshly recomputed** `operator.json` as input. What `merge.js`
   does with it is the only thing that varies by `mergeMode`.

This means **"reconcile is invoked, desired state changes, a rollout
delivers it to a fresh pod" already works identically no matter what
triggered the change** (CR edit, credential rotation, operator upgrade, or
image upgrade) — this is one mechanism, not several.

### How each bucket responds

- **Bucket A (always-managed):** `merge.js` applies this subset
  unconditionally on every restart, in every mode, including `seedOnly`
  (see the [reassertion mechanism](#reassertion-mechanism-for-declared-entries)
  above). Combined with the pipeline above, any operator/image update that
  changes a Bucket-A default (e.g. a new `gateway.auth` field the next
  OpenClaw version requires, or a new credential's proxy route) is picked
  up automatically on the very next rollout, for every instance, regardless
  of mode. **No gap, no new mechanism required.**
- **Bucket B (user-manageable):** Under `merge`/`overwrite`, today's
  behavior stands (see the "note on `merge` mode today" above — often
  reapplied wholesale in practice) so operator/image updates already
  propagate automatically. Under `seedOnly`, the freeze is deliberate but
  applies **per entry, not per file**: a brand-new CR-declared entry (new
  provider, new channel, new MCP server) is added automatically via
  [gap-fill](#automatic-gap-fill-for-newly-declared-entries) on the very
  next restart — no opt-in needed, since there's nothing existing to
  clobber. Only changes to an **already-existing** entry's Bucket-B content
  (e.g. a new catalog model for a provider added on day one) require the
  opt-in resync mechanism (see [Q4](user-owned-config-questions.md#q4)) —
  a deliberate, documented trade-off, not a bug to route around.

### Explicit non-goal: Bucket-B schema-breaking migrations

There is a harder variant of the Bucket-B problem this proposal does
**not** solve: a new OpenClaw image that changes the *shape* of an existing
Bucket-B key (not just its value) — e.g. a provider's `models` array
entries gain a required field. Resync (above) can push a value, but can't
tell the difference between "a value the operator would still choose" and
"a shape a user/agent deliberately customized on top of."

This is genuinely a distinct, harder problem than the mode itself, and it
already exists today independent of `seedOnly` mode — `merge` mode's
deep-merge already can't rename or restructure a key that the user or a
prior operator version wrote, and `overwrite` would blow away any
user-authored config entirely, which is why it isn't the default either.
`seedOnly` mode doesn't make this problem worse in kind — it just removes
the safety net Bucket-B's periodic re-apply currently provides for the
subset of keys the operator itself declares. This is called out explicitly
as **known, out-of-scope future work**, not something this proposal needs
to solve to ship the mode (see [Q4](user-owned-config-questions.md#q4) for
the full reasoning and what to document instead).

## Non-JSON Artifacts: Workspace, Skills, Plugins

`merge.js` uses two strategies for non-`openclaw.json` files:

- `seedIfMissing` — workspace bootstrap files (`AGENTS.md`, `SOUL.md`,
  `_ws_*` keys) — already "seed once" semantics in every mode today.
- `copyAlways` — operator-managed skill docs (`PLATFORM.md`, `KUBERNETES.md`,
  `_skill_*` keys) — overwritten every restart today
  (`internal/assets/manifests/claw/configmap.yaml:68, 114, 122, 129`).

**Decided:** Under `seedOnly` mode, skill docs switch to `seedIfMissing` —
consistent with issue #224 explicitly listing "skills" as user-owned, and
with Bucket B treatment generally. The staleness risk (a skill doc
describing a proxy route that later changes) is accepted and covered by the
same resync mechanism as Bucket B ([Q4](user-owned-config-questions.md#q4)).

**Decided:** `spec.plugins` declarative installs keep running every restart
in `seedOnly` mode too (Bucket-A-like — see above), since it's
Kubernetes-side, deterministically-convergent, typed CRD state, not
`openclaw.json` content, and a CR author adding a required plugin later is a
more common workflow than a user wanting to uninstall an operator-declared
one.

## Cluster-Admin Gating

No mechanism exists today for a cluster admin to restrict which
`mergeMode` values a `Claw` CR author may use — any user who can create a
`Claw` CR can already set `mergeMode` to any value in the CRD enum. This gap
isn't unique to the new mode (it's equally true of `overwrite` today), but
issue #224 makes it an explicit acceptance criterion for the new mode.

**Decided (Q1):** A new CRD, **`ClawOperatorConfig`**, modeled on the Dev
Sandbox platform's own `ToolchainConfig` pattern — admins edit a singleton
instance to set `allowedConfigModes`, which the reconciler checks on every
`Claw` reconcile.

- **Scope:** Namespaced (like `ToolchainConfig`), with the singleton
  instance required to live in the operator's **own runtime namespace**
  (not a tenant namespace). The namespace is resolved dynamically via a
  `WATCH_NAMESPACE` env var populated by the Kubernetes downward API
  (`fieldRef: metadata.namespace`) — never hardcoded — so it works
  regardless of which namespace an admin installs the operator into.
- **Why not Cluster-scoped:** Both scopes are equally safe from
  accidental tenant access (OLM's auto-generated aggregated
  `admin`/`edit`/`view` ClusterRoles for owned CRDs can't grant tenant
  `RoleBinding`s access to a cluster-scoped resource, and a Namespaced
  instance confined to the operator's own namespace is never reachable by
  a tenant `RoleBinding` either). Namespaced wins on RBAC footprint: a
  `Role`/`RoleBinding` in one namespace, versus a new
  `ClusterRole`/`ClusterRoleBinding` grant — consistent with this
  project's minimal-privilege conventions.
- **Why not a Deployment env var:** No local precedent for CR-field policy
  via env var, and it requires a Deployment restart to change policy — a
  step down from how this platform (and `ToolchainConfig` specifically)
  normally handles admin-editable settings.
- **Why not a validating webhook:** No webhook infrastructure exists
  anywhere in this operator today (`cmd/main.go` provisions an empty
  webhook server with no registered handlers; Kustomize webhook patches
  are commented out), and neither the closest upstream project
  (`openclaw-operator`, whose own `OpenClawSelfConfig` self-service gating
  is enforced entirely at reconcile time via status conditions) nor the
  Dev Sandbox platform's own `ToolchainConfig` needs one for equivalent
  policy enforcement.

See [questions document](user-owned-config-questions.md), Q1 for full
detail.

**Decided (Q2):** Gating violations reuse the existing `Ready` condition —
no new condition type. When the effective `mergeMode` isn't in
`ClawOperatorConfig`'s `allowedConfigModes`, the reconciler sets
`Ready: False` (reason `ConfigModeNotAllowed`) and halts that reconcile,
exactly like every other validation failure (bad credentials, invalid MCP
secrets, missing ConfigMap sources). Because halting reconcile early never
re-renders the Deployment, an already-running instance whose mode becomes
disallowed after the fact (an admin tightens policy retroactively) is never
restarted or downgraded — it keeps running untouched, just with a `Ready`
condition that reads `False` until the mismatch is resolved. See
[questions document](user-owned-config-questions.md), Q2 for the full
trade-off discussion (a dedicated non-`Ready` condition was considered and
rejected as unnecessary complexity once this was confirmed).

## Core Concepts

- **Seed vs. steady-state.** The existing `merge.js` first-run branch
  (`deepMerge(seed, ops)`, written once) already *is* the seeding behavior
  this mode needs. The only new logic is: after seeding, skip Bucket B's
  steady-state merge (and the `copyAlways` skill copy) entirely instead of
  running it every restart — Bucket A keeps running unconditionally, at the
  sub-field level for declared entries (see
  [Convergence / Update Mechanism](#convergence--update-mechanism) and the
  [reassertion mechanism](#reassertion-mechanism-for-declared-entries)).
- **Resync.** `seedOnly` mode's Bucket-B freeze is per-entry: a CR author
  adding a brand-new credential/MCP server/webSearch config gets it applied
  automatically via gap-fill on the next restart (see
  [Automatic gap-fill](#automatic-gap-fill-for-newly-declared-entries)).
  Only *updating* an entry that already exists has no automatic path — a
  deliberate trade-off of the mode, addressed by an opt-in resync
  annotation — see [Q4](user-owned-config-questions.md#q4).
- **Mode transitions.** A CR author can change `mergeMode` at any time
  (e.g., `seedOnly` → `merge`). Since mode is read fresh from the CR on
  every pod start (`CLAW_CONFIG_MODE` env var,
  `internal/controller/claw_deployment.go:427-431`), switching modes takes
  effect on the next restart using whatever the target mode's normal
  first-boot-vs-restart logic dictates against the existing PVC file as the
  base. No special transition code should be needed as long as `merge.js`'s
  existing-file detection (`fs.existsSync(pvcPath)`) continues to be the
  single source of truth for "is this a fresh seed or not."

## Implementation Plan

1. **API changes** (`api/v1alpha1/claw_types.go`)
   - Add a new `ConfigMode` constant, `seedOnly` (see
     [Q5](user-owned-config-questions.md#q5) for naming rationale), to the
     enum and kubebuilder marker
     (`+kubebuilder:validation:Enum=merge;overwrite;seedOnly`).
   - Add a one-line doc-comment cross-reference disambiguating `seedOnly`
     from the existing, unrelated `SeedMode`/`seedIfMissing` type (workspace
     file seeding) on both types — see [Q5](user-owned-config-questions.md#q5).
   - New `ClawOperatorConfig` type (Namespaced, singleton) holding
     `spec.allowedConfigModes []ConfigMode` (Q1).
   - Run `make manifests` and `make generate` to regenerate the CRD YAML and
     deepcopy code (per repo convention after any API type change).

2. **Controller wiring** (`internal/controller/claw_deployment.go`,
   `internal/controller/claw_resource_controller.go`)
   - `configureClawDeploymentConfigMode` passes the new mode value through to
     `CLAW_CONFIG_MODE` unchanged.
   - Add a check in the reconciler comparing the effective `mergeMode`
     against `ClawOperatorConfig.spec.allowedConfigModes`; when disallowed,
     set `Ready: False` (reason `ConfigModeNotAllowed`) and halt that
     reconcile, matching the existing validation-failure pattern (Q2).
   - No changes needed to `enrichConfigAndNetworkPolicy` itself —
     `operator.json` is always fully recomputed regardless of mode; only
     `merge.js` needs mode-awareness (below).

3. **`merge.js` changes** (`internal/assets/manifests/claw/configmap.yaml`)
   - Add a new branch: when `mode === "seedOnly"` and the PVC file already
     exists, skip the `deepMerge(base, ops)` step for `openclaw.json`
     entirely, then run two passes instead:
     1. A **targeted reassertion pass**: for each
        provider/channel/MCP-server/webSearch/memorySearch key present in
        both `operator.json` and the existing PVC file, overwrite only the
        Bucket-A sub-fields listed in the
        [Ownership Boundary](#ownership-boundary) table (e.g.
        `models.providers.<name>.{baseUrl,apiKey,api}`,
        `channels.<name>.{enabled,botToken,token,appToken}` via the
        existing `protectedChannelKeys` list) — leaving the rest of each
        entry untouched.
     2. A **shallow gap-fill pass** (see
        [Automatic gap-fill](#automatic-gap-fill-for-newly-declared-entries)):
        for each of `models.providers`, `channels`, `mcp.servers`,
        `agents.defaults.models`, copy in any top-level key present in
        `operator.json` but absent from the PVC file, without recursing
        into or touching keys that already exist. **This must not become a
        deep merge** — that would silently reintroduce the exact
        Bucket-B-clobbering failure mode the reassertion pass above is
        designed to avoid. The same pass also fills
        `agents.defaults.model.primary`/`.fallbacks` from `operator.json`
        only if currently absent/empty in the PVC file, matching the
        existing `merge`-mode carve-out's fill-if-empty behavior — never
        touched once either has any value.
     Also always reapply the pure-infrastructure subset unconditionally
     (same function/logic already used for `merge`/`overwrite`'s
     always-win keys).
   - Switch skill-doc `copyAlways` calls to `seedIfMissing` when
     `mode === "seedOnly"`.
   - First-boot behavior (no existing PVC file) is unchanged from today's
     `merge`-mode first-run branch for all modes.
   - Implement the resync annotation handling for *updating* already-
     existing entries (Q4 fast-follow) as a follow-up PR — this is separate
     from gap-fill above, which is not deferred and ships with the initial
     mode.

4. **Testing** (`internal/controller/claw_merge_test.go`) — **the path-level
   reassertion mechanism (previous step) is the highest-risk new logic in
   this proposal and must not ship with only happy-path coverage.** At
   minimum, using the existing `runMergeJS` harness
   (`mergeTestSetup{configMode: "seedOnly", pvcJSON: ...}`), cover every
   cell of this matrix — not just one representative case per row:

   | Scenario | Expected result |
   |---|---|
   | First boot, no existing PVC file | Seeds correctly (unchanged from today's `merge`-mode first-run behavior) |
   | Declared provider's `baseUrl`/`apiKey`/`api` hand-corrupted in the PVC file | Corrected back to the operator's value on restart |
   | Declared channel's `botToken`/`enabled` hand-corrupted | Corrected back on restart |
   | Declared credentialed MCP server (`envFrom`) hand-corrupted | Corrected back on restart |
   | Declared channel's `dmPolicy`/`allowFrom` hand-edited | **Preserved** — not reset to the operator default |
   | Declared provider's `models` array hand-populated | **Preserved** |
   | Hand-added command-based MCP server (not CR-declared at all) | **Preserved**, and the reassertion pass doesn't error or touch it |
   | New provider/channel/MCP server added to the CR after seeding (key absent from PVC file) | Gap-fill adds it automatically on the next restart, fully formed, no annotation needed |
   | Existing provider/channel/MCP server's CR-side config changes after seeding (key already present in PVC file) | Gap-fill does **not** touch it — its Bucket-B content stays exactly as it was, even though `operator.json` now differs |
   | Gap-fill interaction: one new entry added to the CR at the same time an existing sibling entry has hand-customized Bucket-B content | New entry appears; existing sibling's customization is untouched — no cross-contamination between gap-fill and reassertion passes |
   | Model catalog / `agents.defaults.model.primary`/`fallbacks` hand-edited | **Preserved** (gap-fill semantics, unchanged) |
   | `agents.defaults.model.primary`/`fallbacks` absent/empty in the PVC file (e.g. seeded with only a `pathToken` credential), then a catalog-eligible credential is added to the CR | Gap-fill adds `primary`/`fallbacks` automatically on the next restart, same as the four named collections |
   | Multiple declared entries of the same kind (e.g. two providers), only one hand-corrupted | Only the corrupted one changes; no cross-contamination between entries |
   | `gateway.auth.*`, `gateway.bind`/`port`/`mode`, Route-host entry hand-corrupted | Corrected on every restart (pure infra subset, unchanged behavior) |
   | Credential/channel/MCP server removed from the CR after seeding (orphaned entry) | Stub remains in the PVC file, untouched — not deleted, not reasserted, reconcile doesn't error |
   | Declared entry present in `operator.json` but entirely missing from the existing PVC file (e.g. added seed-time-only field) | Reassertion pass adds it without erroring, doesn't require the entry to already exist |
   | Skill docs (`PLATFORM.md`, `KUBERNETES.md`) | Seed-only (`seedIfMissing`) — user edits/deletions persist |
   - New reconcile-level tests for admin gating (`Ready: False` +
     `ConfigModeNotAllowed` reason when disallowed).

5. **Documentation**
   - New ADR (following the `docs/adr/NNNN-*.md` numbering convention,
     mirroring the style of ADR-0004 and ADR-0013). Include a short callout
     disambiguating `seedOnly` (`ConfigMode`, whole-file, `spec.config.mergeMode`)
     from the existing, unrelated `seedIfMissing` (`SeedMode`, per-file,
     `spec.workspace.*.mode`) — see [Q5](user-owned-config-questions.md#q5).
   - `docs/architecture.md` config ownership table gets a third column, or a
     new subsection, documenting `seedOnly` mode and the two-bucket model,
     including the same `seedOnly`/`seedIfMissing` disambiguation callout.
   - `CLAUDE.md` likely doesn't need updates (it references
     `docs/architecture.md` rather than duplicating detail).

## Open Questions

See [user-owned-config-questions.md](user-owned-config-questions.md) for
full detail. All are decided; the document records the reasoning for
future reference and PR review discussion:

1. **Q1** — Mechanism for cluster-admin gating. **Decided:**
   `ClawOperatorConfig`, a Namespaced singleton CRD in the operator's own
   runtime namespace (resolved via `WATCH_NAMESPACE` downward-API env var).
2. **Q2** — How gating violations are surfaced. **Decided:** reuse the
   existing `Ready` condition (no new condition type), halting reconcile
   the same way as other validation failures.
3. **Q3** — Ownership boundary: which keys are always-managed (Bucket A) vs.
   user-manageable (Bucket B)? **Decided:** the two-bucket model above, with
   a sub-field-level reassertion mechanism for declared providers/
   channels/MCP servers, cross-checked against issue #224's own two lists
   and the constraint that a key can only be "user-owned" if hand-editing
   it can actually work.
4. **Q4** — Update/convergence mechanism: how do operator/image upgrades and
   new CR-declared Bucket-B items reach an already-seeded instance?
   **Decided:** Bucket A is already covered by the existing
   reconcile→hash→rollout pipeline with no new mechanism; Bucket B needs an
   opt-in resync annotation (fast-follow); true schema-breaking migrations
   are explicit, documented, out-of-scope future work.
5. **Q5** — Naming for the new `mergeMode` value. **Decided:** `seedOnly`,
   confirmed even after weighing its adjacency to the existing, unrelated
   `SeedMode`/`seedIfMissing` type already in `api/v1alpha1/claw_types.go`
   (workspace-file seeding) — mitigated via cross-referencing doc-comments
   and an ADR/architecture-doc callout rather than a rename. See
   [questions doc, Q5](user-owned-config-questions.md#q5) for the full
   reasoning.
