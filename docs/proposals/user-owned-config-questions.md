**Status:** Decided — ready for implementation planning / PR review
**Related:** [Design document](user-owned-config-design.md)

Each entry below records a decision and its reasoning, for PR review and
future reference.

## Q1: Mechanism for cluster-admin gating

Issue #224 requires that "cluster admins can control whether this mode is
exposed or allowed." No such mechanism exists anywhere in the operator today
— not for `mergeMode`, not for any other field. This is the biggest net-new
piece of infrastructure this feature would introduce.

### Option C: `ClawOperatorConfig` — Namespaced singleton CRD

A new CRD, **`ClawOperatorConfig`**, holding operator-wide admin policy
(initially just `allowedConfigModes`, extensible later). Modeled on the
Dev Sandbox platform's own `ToolchainConfig` pattern, which solves the
identical "admin sets policy, operator enforces it live" problem for
`host-operator`/`member-operator`.

**Naming:** `ClawOperatorConfig` — follows the existing `Claw`-prefixed
family (`Claw`, `ClawDevicePairingRequest`), and avoids confusion with the
existing `spec.config` (`ConfigSpec`) field on `Claw` (which is per-instance
application config, not operator policy). The full CRD identity
(`clawoperatorconfigs.claw.sandbox.redhat.com`) is globally unique regardless
of Kind name genericity, since CRD identity includes the API group.

**Scope:** Namespaced (matching `ToolchainConfig`'s own choice — its CRD is
defined with `scope: Namespaced`), with a singleton instance required to live
in the operator's own runtime namespace, looked up by a fixed name (e.g.
`cluster` or `default`). Only the config CR's *lookup* is namespace-pinned;
the existing cluster-wide watches on `Claw`/`ClawDevicePairingRequest` are
unaffected.

**Why Namespaced-and-self-referential over Cluster-scoped:** Both are
equally safe from tenant access via OLM's automatic aggregated
`admin`/`edit`/`view` ClusterRoles (which OLM generates for every CRD a CSV
owns, regardless of scope) — Kubernetes RBAC fundamentally cannot grant a
namespace-scoped `RoleBinding` access to a cluster-scoped resource, and Dev
Sandbox tenants only ever receive `RoleBinding`s (never
`ClusterRoleBinding`s) for their own Spaces. So a Cluster-scoped CRD is safe
by construction, while a Namespaced CRD is equally safe as long as the
instance only ever lives in a namespace tenants have no binding in (i.e.,
the operator's own namespace, never a tenant's `-dev`/`-claw` namespace).
Given that equivalence, Namespaced wins on **RBAC footprint**: it needs only
a `Role`/`RoleBinding` in the operator's own namespace, not a new
`ClusterRole`/`ClusterRoleBinding` grant for a new cluster-scoped type —
consistent with this project's existing minimal-privilege conventions
(non-root, dropped capabilities, RBAC hardening throughout, per
`CLAUDE.md`).

**Avoiding a hardcoded namespace:** The operator's own namespace is resolved
at runtime via a `WATCH_NAMESPACE` env var (matching the exact convention
already used by the Dev Sandbox platform's shared configuration-loading
library — required, fails fast if unset), populated via the Kubernetes
downward API rather than a literal value:

```yaml
env:
  - name: WATCH_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
```

This makes the mechanism work correctly no matter which namespace an admin
installs the operator into — the Kustomize `namespace:` transformer already
relocates the Deployment wherever it's overlaid, and the downward API
value automatically follows, with no separate value to keep in sync.

**Enforcement:** Reconcile-time, not webhook — see Q2.

_Considered and rejected: operator Deployment env var / global allowlist
(simplest, matches `PROXY_IMAGE`-style config, but requires a Deployment
restart to change policy and has no local precedent for CR-field policy in
this codebase); validating admission webhook (best UX — synchronous
rejection — but requires standing up currently-unused webhook
infrastructure, cert management, and new envtest webhook test harness, for
a capability neither this operator nor its closest upstream/platform
relatives currently need elsewhere)._

**Decision:** `ClawOperatorConfig` — a Namespaced singleton CRD, instance
required to live in the operator's own runtime namespace (resolved via
`WATCH_NAMESPACE`, populated by downward API), enforced at reconcile time.
Chosen for parity with the Dev Sandbox platform's own `ToolchainConfig`
precedent, lower RBAC footprint than a cluster-scoped alternative, and no
dependency on webhook infrastructure this operator doesn't otherwise need.

---

## Q2: How gating violations are surfaced

Q1 already settled the mechanism as reconcile-time (`ClawOperatorConfig`
read by the reconciler, no webhook), so the operator only learns about a
disallowed `mergeMode` after the CR is already persisted in etcd — a
validating-webhook-style synchronous rejection was already ruled out as a
consequence of Q1, not a live alternative here.

A more elaborate option was considered and rejected: splitting this into a
dedicated `ConfigModeAllowed` condition, kept separate from `Ready`, so that
an already-running instance whose mode is retroactively disallowed (an
admin tightens `ClawOperatorConfig` after the fact) wouldn't get flagged
`NotReady` for something it isn't actually broken by. This was rejected in
favor of simplicity: reusing the **existing** `Ready` condition, exactly
like every other reconcile-time validation failure in this operator
(credentials, MCP secrets, ConfigMap sources), rather than introducing a new
condition type and new "is this a new transition or steady-state drift"
tracking logic solely for this one field.

**Decision:** Reuse the existing `Ready` condition — no new condition type.
When the effective `mergeMode` isn't in `ClawOperatorConfig`'s
`allowedConfigModes`, set `status.conditions[Ready] = False` with a reason
(e.g. `ConfigModeNotAllowed`) and a clear message, and halt that
reconcile — the same pattern already used for bad credentials, invalid MCP
secrets, and missing ConfigMap sources
(`internal/controller/claw_resource_controller.go`, e.g. lines 484-500,
660-694).

Because this reuses the existing halt-on-validation-failure pattern, it
does **not** reintroduce the more severe risk raised earlier (an admin
policy change forcing a live downgrade + restart of an already-running
instance): halting reconcile early means the function returns before ever
re-rendering the Deployment's `CLAW_CONFIG_MODE` env var, so an
already-running Claw whose mode becomes disallowed keeps running completely
untouched — it does not get restarted, downgraded, or lose any PVC state.
The only accepted cost of the simpler design is cosmetic: such an instance's
`Ready` condition will read `False` even though the actual running pod is
functionally fine, until the CR author or admin resolves the mismatch
(admin re-allows the mode, or the CR author switches to an allowed one).

_Considered and rejected: dedicated `ConfigModeAllowed` condition, kept
separate from `Ready`, to avoid the cosmetic `NotReady` noise on policy
drift — added meaningful implementation complexity (transition-vs-drift
tracking) to solve a problem that turned out to be purely cosmetic once the
existing halt-on-failure semantics were confirmed to already prevent the
actually dangerous outcome (forced downgrade/restart); validating webhook
rejection — ruled out by Q1 (no webhook infrastructure)._

---

## Q3: Ownership boundary — what's always-managed vs. genuinely user-manageable?

This is the core question issue #224 raises. Issue #224's own "user-owned"
list (which includes MCP configuration) is directionally right but not
precise at the sub-field level — true for a locally-invoked MCP server's
behavior, but not for the credential wiring of one that needs a Secret. For
example, `models.providers` can't actually be operated by a user
hand-editing the file, in *any* mode — `apiKey` is always a hardcoded
placeholder string, and the pod's own `NetworkPolicy` permits egress only
to the `claw-proxy` pod, DNS, and explicit CR-declared exceptions (full
detail in the
[Q4 worked example](#worked-example-adding-a-new-provider-under-seedonly-mode)
below). The same reasoning applies to credential-bearing `channels.*` and
`mcp.servers` entries.

**The test:** not "does the operator currently generate this from a CR
field," but **"would hand-editing this field directly in `openclaw.json`,
with no CR change, actually produce a working configuration?"** If no,
it's always-managed, full stop, in every mode. If yes, it's a real
candidate for mode-gating.

**Decision:** A two-bucket model (full detail and code mapping in the
[design doc](user-owned-config-design.md#ownership-boundary)):

- **Bucket A — always-managed, every restart, every mode (including
  `seedOnly`):**
  - Pure infrastructure: `gateway.mode/bind/port`, `gateway.controlUi.enabled`
    (`enforceInfrastructureKeys`); `gateway.auth.mode`,
    `gateway.controlUi.dangerouslyDisableDeviceAuth` (`injectAuthMode`);
    `gateway.trustedProxies` (`enforceTrustedProxies`); the Route-host entry
    in `gateway.controlUi.allowedOrigins` (`injectRouteHost`); credential
    injection via the MITM proxy (never in the file at all);
    Secrets/NetworkPolicies/RBAC (outside the file entirely).
  - **Credential/routing-critical sub-fields only**, for entries the CR
    declares: `models.providers.<name>`'s existence, `.baseUrl`, `.apiKey`,
    `.api` (`injectProviders`); `channels.<name>`'s existence, `.enabled`,
    `.botToken`/`.token`/`.appToken` (`injectChannels`, reusing the
    existing `protectedChannelKeys` allowlist); `mcp.servers.<name>` in
    full, only when it uses `envFrom`/`credentialRef` or an
    otherwise-unallowlisted domain (`injectMcpServers`); web/memory search
    provider selection (`injectWebSearch`, `injectMemorySearch`); metrics
    sidecar wiring (`injectMetricsConfig`).
  - `spec.plugins` declarative installs behave the same way (Kubernetes-side,
    not `openclaw.json`; runs on every restart when plugins are declared,
    deterministically reinstalling the operator-managed subset without
    touching user-installed plugins).
- **Bucket B — user-manageable, gated by mode:** everything else in those
  same entries (a declared channel's `dmPolicy`/`allowFrom`, a declared
  provider's local `models` metadata array), plus content that was always
  intended as Bucket B: the primary/fallback chain (already "seed once" via
  the existing `configmap.yaml:92-105` carve-out, not `injectModelCatalog`)
  and the model catalog aliases themselves (currently reset every restart
  under `merge` mode per `docs/architecture.md`'s ownership table — made
  safe under `seedOnly` only by this proposal's new gap-fill pass, not by
  any existing protection); command-based MCP servers with no `envFrom`
  (addable by hand in any
  mode, no CR involved, since there's no credential/network gate at all);
  `agents.list`, `tools.*` beyond web search/fetch, `cron.*`,
  `diagnostics.*` beyond the bootstrap hook, non-declared
  channels/MCP servers/plugins; `update.checkOnStart`,
  `agents.defaults.skipOptionalBootstrapFiles`; skill docs
  (`PLATFORM.md`/`KUBERNETES.md`/`_skill_*`, currently `copyAlways` in
  every mode, switching to `seedIfMissing` under `seedOnly`); workspace
  files (already `seedIfMissing` in every mode).

Reasserting Bucket A requires a **path-level** mechanism, not a whole-key
one: for each provider/channel/MCP-server key present in both the fresh
`operator.json` and the existing PVC file, `merge.js` overwrites only the
specific Bucket-A sub-fields and leaves the rest of that entry (and any
entry not CR-declared at all) untouched. See the design doc's
[reassertion mechanism](user-owned-config-design.md#reassertion-mechanism-for-declared-entries)
section, including the accepted edge case of an orphaned entry when a
credential is later removed from the CR.

> **This is genuinely new logic** (not a reuse of `merge.js`'s existing
> whole-file `deepMerge` or fixed-path `enforce*` patterns), with silent,
> asymmetric failure modes — under-reasserting leaves a security field
> unprotected, over-reasserting silently destroys a user's customization —
> that only surface on the second-or-later restart of an already-seeded
> instance. See the design doc's implementation note and required test
> matrix in its
> [Implementation Plan](user-owned-config-design.md#implementation-plan);
> this must not ship with only happy-path test coverage.

Rationale for keeping Bucket A always-enforced even in `seedOnly`: these
are exactly the keys the issue itself calls "operator-owned regardless of
mode" — network/auth/service wiring, plus the sub-fields where drift is a
security or availability incident (locked-out gateway, exposed control UI,
a provider/channel that silently stops authenticating), not a customization
the mode is meant to enable. Narrowing it to the sub-field level is what
makes the mode actually deliver on "you own your app config" — a declared
channel's behavior or a hand-added local MCP server are things a user can
really use, unlike a provider block they can look at but never
functionally change.

---

## Q4: Update/convergence mechanism — how does new operator-desired state reach an already-seeded instance?

The follow-up question to Q3: once Bucket B is frozen after seeding, how
does a CR author's later change (new credential, new MCP server, new
webSearch config), an **operator upgrade**, or a **new OpenClaw image**
actually reach an already-running `seedOnly` instance? This matters
regardless of *why* the desired state changed — the mechanism needs to be
the same either way.

**The short answer: it already is the same mechanism, and no new plumbing
is needed for Bucket A.** Full detail in the design doc's
[Convergence / Update Mechanism](user-owned-config-design.md#convergence--update-mechanism)
section:

- `operator.json` is fully recomputed from current desired state on
  **every** reconcile, regardless of `mergeMode` — this doesn't change for
  `seedOnly`.
- Any change to it (for any reason: CR edit, credential rotation, operator
  upgrade changing a built-in default) changes `stampGatewayConfigHash`'s
  hash, which changes the pod template, which triggers a rollout. Image
  version bumps trigger a rollout independently, through the image field
  itself.
- Because Bucket A is applied unconditionally by `merge.js` on every
  restart in every mode, at the sub-field level for declared entries (Q3),
  **any** operator/image update that changes a Bucket-A default reaches
  every instance, in every mode, on the very next rollout — with zero new
  mechanism.
- Bucket B is where `seedOnly` mode's freeze is deliberate and by
  design — but the freeze applies **per entry, not per file**: a
  CR-declared provider/channel/MCP server/catalog model that doesn't exist
  yet in the PVC file gets added automatically on the next restart (there's
  nothing there to clobber), while an entry that already exists is left
  entirely alone even if the CR changes something about it. See the
  [automatic gap-fill](#automatic-gap-fill-for-newly-declared-entries)
  decision below — updating an *existing* entry still needs an opt-in
  resync.

### Worked example: adding a new provider under `seedOnly` mode

This is the single most likely real-world scenario, worth spelling out end
to end since it also clarifies a related point: a new provider can **never**
be added by hand-editing `openclaw.json` alone, in any mode — it isn't a
`seedOnly`-specific limitation.

- `injectProviders` (`claw_resource_controller.go:1260`) always writes the
  literal placeholder string `"ah-ah-ah-you-didnt-say-the-magic-word"` as
  `apiKey` — real credentials are never present in `openclaw.json`, in any
  mode. A hand-added provider block has no working credential to use.
- The OpenClaw pod's own `NetworkPolicy`
  (`internal/assets/manifests/claw-proxy/network-policies.yaml`, plus any
  runtime-injected `spec.network.additionalEgress`/in-cluster MCP bypass
  rules) permits egress only to the `claw-proxy` pod, DNS, and explicit
  CR-declared exceptions — a hand-added provider couldn't reach a new
  domain directly even with a real key pasted in.
- The proxy's own L7 route allowlist (`generateProxyConfig`,
  `claw_proxy.go:122`) is built purely from `spec.credentials` /
  `spec.mcpServers` / `spec.webSearch` on the **CR** — never from
  `openclaw.json` — so an unrecognized domain gets a 403 from the proxy
  itself regardless of mode.

So a new provider must always go through `spec.credentials` (or
`spec.customProviders`) on the CR. The `seedOnly`-specific question is what
happens *after* that CR edit, on an already-seeded instance:

1. Reconcile regenerates the credential Secret, the proxy's route/injector
   config, and `operator.json` — all Bucket A / K8s-level, unaffected by
   `mergeMode`. `stampGatewayConfigHash` changes, triggering a rollout.
2. The new pod's `merge.js` sees the PVC file already exists and, in
   `seedOnly` mode, skips the full Bucket-B `deepMerge` — but it still
   runs the [automatic gap-fill](#automatic-gap-fill-for-newly-declared-entries)
   pass, which adds any `models.providers` key present in `operator.json`
   but absent from the PVC file. Since this is a brand-new provider name,
   it's absent, so it gets added — **no annotation needed.**
3. Net effect: the new provider shows up in `openclaw.json` on the very
   next restart after the CR edit, fully proxy-backed and working, exactly
   as if it had been part of the original CR at seed time. The only case
   that still needs the opt-in resync annotation (below) is updating an
   **already-existing** provider's Bucket-B content (e.g. changing
   `spec.customProviders[].models` for a provider added on day one) —
   because that entry already has content in the file that might have been
   hand-customized, so it can't be touched without explicit opt-in. For
   that narrower case, the same zero-code workaround still applies:
   temporarily set `mergeMode: merge` for one restart, then switch back.

### Contrasting example: tuning a declared channel's behavior

Worth contrasting with the provider example above, since it shows Bucket B
isn't empty — it's the concrete capability `seedOnly` mode actually adds.
Say Telegram is already declared via `spec.credentials` and has been seeded
into `openclaw.json` as `channels.telegram` (with `dmPolicy: "open"` from
`knownChannels`'s `ConfigBase`, `claw_channels.go:65-69`).

- Under `merge` mode **today**, if the user (or agent) hand-edits
  `channels.telegram.dmPolicy` in the running file, it gets silently reset
  back to `"open"` on the very next restart — `injectChannels` writes the
  entire per-channel block into `operator.json`, and `merge.js`'s
  `deepMerge` replaces same-named scalar keys wholesale. The *only* way to
  persist a `dmPolicy` change today is through `spec.credentials[].channelConfig`
  on the CR (validated against `protectedChannelKeys`, then baked into
  `operator.json` itself).
- Under `seedOnly` mode, `channels.telegram.enabled`/`.botToken` (the
  protected, credential-bearing fields) still get reasserted every
  restart — but `dmPolicy` and `allowFrom` are left alone entirely once
  seeded, so a direct file edit persists across restarts. This is real,
  new capability, not just "the operator stopped touching a key it already
  couldn't make work" (as with providers) — it's letting the user do
  something previously only possible via a CR round-trip.

### Automatic gap-fill for newly-declared entries

This is not optional or deferred — it ships as part of the initial mode,
because without it the mode would fail the single most common real-world
workflow (adding a new credential/channel/MCP server after initial setup)
by default, requiring a manual step for something the operator can safely
automate.

**Decision:** In `seedOnly` mode's steady-state branch, `merge.js` applies
a **shallow, add-missing-keys-only** pass to each Bucket-B collection
(`models.providers`, `channels`, `mcp.servers`, `agents.defaults.models`):
for each key present in `operator.json`'s version of that collection but
absent from the existing PVC file, copy it in as-is; keys already present
in the PVC file are left completely untouched, whatever their content. This
is exactly the same gap-fill principle `injectModelCatalog`
(`claw_resource_controller.go:1359-1364`) already applies today at the Go
layer for the model catalog — this decision just extends the same pattern
to `merge.js`'s JS layer, and to the other Bucket-B collections.

This is safe by construction: a key that doesn't exist yet in the file has
nothing for a user/agent to have customized, so adding it can never
overwrite anything. It's the same reasoning that already justifies
`seedIfMissing` for workspace files — "create if absent" was never
considered a violation of "hands off," and this is that same idea applied
to JSON object keys within `openclaw.json` instead of whole files.

**Also decided (raised during design review):** the same pass extends to
two scalar fields alongside the four named collections —
`agents.defaults.model.primary` and `.fallbacks` — filled from
`operator.json` only if currently absent/empty in the PVC file, never
touched once either has any value. Without this, a CR that starts with no
catalog-eligible credential (e.g. only a `pathToken`-type one, which
`injectModelCatalog` skips) and has one added later would leave the agent
with no usable default model under `seedOnly`, even though the exact same
scenario already self-heals under `merge` mode today via the existing
`configmap.yaml:92-105` carve-out (which only *preserves* a saved value
when one exists — it never blocks `deepMerge` from filling an empty one).
Folding this into the same gap-fill pass costs no new mechanism and closes
the one case where `seedOnly` would otherwise regress behavior `merge`
mode already provides.

**Known accepted quirk:** because existence is keyed off the CR, not off
some independent "was this ever added" flag, a user who manually *deletes*
a still-CR-declared entry from the file (as opposed to removing the
credential from the CR) will see it silently reappear on the next restart —
the gap-fill pass has no way to distinguish "never existed" from
"deliberately removed by the user while the CR still asks for it." To
actually remove an entry, the credential must be removed from the CR (which
produces the [orphaned-stub behavior](user-owned-config-design.md#reassertion-mechanism-for-declared-entries)
instead — the entry stays, but stops being reasserted/functional). This is
a minor, documentable edge case, not a blocker.

### Opt-in resync for updating an existing entry

Gap-fill only handles genuinely *new* keys. It deliberately does **not**
help when a CR author changes something about an entry that's already
present in the file (e.g. edits `spec.customProviders[].models` for a
provider that was already there on day one, or a new OpenClaw image adds a
model to the hardcoded catalog for an already-configured provider) — that
entry already has content in the file that might have been hand-customized,
so touching it needs explicit opt-in.

### Option A: No built-in mechanism — manual edit or temporary mode-switch only
Users update an already-existing entry themselves, or use the
zero-code `mergeMode: merge` round-trip workaround described above.
- **Pro:** Zero new mechanism.
- **Con:** No explicit, discoverable, one-shot way to say "refresh this
  entry to match the CR" without either hand-editing JSON or a
  temporary global mode flip that also re-syncs *every* other entry, not
  just the one the CR author cares about.

### Option B: Explicit one-shot resync via annotation
A CR annotation (e.g. `claw.sandbox.redhat.com/resync-config: "true"`)
triggers `merge.js` to run one full Bucket-B `deepMerge(base, ops)` pass on
the next restart (same as `merge` mode would do for that bucket), then the
annotation is cleared and subsequent restarts return to hands-off behavior.
- **Pro:** Gives users an explicit, opt-in way to force-refresh
  already-existing entries (an updated skill doc, a customProvider's model
  list) without losing other accumulated customizations permanently —
  one-time convergence, not continuous enforcement.
- **Con:** New mechanism to build and document (annotation handling,
  clearing logic, interaction with the config-hash rollout-trigger
  annotation that already exists); still all-or-nothing across every
  entry, not scoped to just the one the CR author changed.

**Decision:** Option B, but as a fast-follow rather than a blocker for the
initial mode — gap-fill (above) already covers the common "I added
something new" case for the initial release; ship Option A's behavior for
the narrower "update an existing entry" case first, and add the resync
annotation once real usage surfaces how painful manual reconciliation
actually is. This is documented as a known gap upfront (per issue #224's
acceptance criterion about documentation), not silently discovered later.

### Explicit non-goal: Bucket-B schema-breaking migrations

A harder variant is out of scope for this proposal: a new OpenClaw image
that changes the *shape* of a Bucket-B key (e.g. a field becomes required,
or gets renamed), not just its value. Resync (above) can push a new value,
but can't distinguish "a value the operator would still choose" from "a
shape a user/agent deliberately customized on top of" — that's a real
schema-migration problem, not a config-merge problem.

This is deliberately **not** solved by this proposal, for two reasons: (1)
it already exists today, independent of `seedOnly` mode — `merge` mode's
deep-merge can't rename/restructure a key a user or a prior operator
version wrote either, and `overwrite` avoids it only by destroying
user-authored config entirely, which is why it isn't the default; (2)
issue #224 doesn't ask for it, and no concrete need for it exists yet.
`seedOnly` mode doesn't introduce this class of problem — it just removes
Bucket B's periodic re-apply, which today happens to paper over some shape
drift by luck rather than by design.

**Decision:** Explicitly document this as a known, out-of-scope limitation
of `seedOnly` mode (and, more broadly, of `merge` mode's deep-merge
approach) rather than attempting to design a migration mechanism now. If a
real need for versioned config migrations emerges later, it should be
scoped as its own proposal — it's a materially different problem (schema
evolution) from what this mode is solving (ownership).

---

## Q5: Naming for the new `mergeMode` value

### Option A: `userOwned`
- **Pro:** Directly matches issue #224's own terminology ("user-owned
  runtime config mode"), self-documenting.
- **Con:** Overclaims. The [ownership-boundary table](user-owned-config-design.md#ownership-boundary)
  shows Bucket A — gateway/auth/infra keys, plus the credential/routing
  sub-fields of any CR-declared provider/channel/MCP server — is never
  delegated to the user in *any* mode, including this one. A CR author
  could reasonably read "userOwned" as "the file is entirely mine now" and
  be surprised that a hand-edited `apiKey`/`botToken` keeps getting
  silently corrected on every restart. Also slightly awkward alongside
  `merge`/`overwrite`, which name *mechanism* rather than *ownership
  philosophy*.

### Option B: `seedOnly`
- **Pro:** Names the actual mechanism precisely, and matches what the Q3/Q4
  analysis confirmed: the file is seeded once, and from then on the
  operator touches only a narrow, well-defined, always-enforced subset
  (Bucket A) — everything else is genuinely hands-off. Consistent with
  `merge`/`overwrite`'s naming style — all three describe what the init
  container *does* to the file, not an outcome it promises. Doesn't
  overclaim: a reader still has to check the CRD description to learn
  what's enforced, which is honest given that something always still is.
- **Con:** Less immediately meaningful to a CR author skimming CRD docs
  without reading the description; loses the direct terminology match with
  issue #224's own wording.

### Option C: `once`
- **Pro:** Shortest, mechanism-focused.
- **Con:** Ambiguous — "once" could be read as "overwrite once then stop
  touching" or "merge once then stop," doesn't convey which.

**Decision:** Option B (`seedOnly`) — reversed from an earlier `userOwned`
leaning once the ownership-boundary analysis made concrete just how much of
the file remains permanently non-delegated (all of Bucket A, at the
sub-field level, forever). "Seed once, then hands off except for a fixed
enforced subset" is a materially more honest description of the mechanism
than "user owned," which reads as an unqualified transfer. Matching
`merge`/`overwrite`'s mechanism-first naming convention also makes the
three values read as a coherent family instead of two mechanism names and
one outcome name. The broader feature can still be described as
"user-owned config mode" in prose/docs/issue references — only the enum
literal itself changes. Low-stakes/low-cost to revisit during PR review
since it's a single enum value.

**Naming-adjacency concern raised during design review:** `api/v1alpha1/claw_types.go`
already has an unrelated `SeedMode` type (`+kubebuilder:validation:Enum=
overwrite;seedIfMissing`) governing per-source workspace-file seeding
(`WorkspaceSpec.InlineSource`/`ConfigMapSource`/`GitSource`, three-tier
item→source→global cascade, from the archived `workspace-sources` change).
A CR author would see `spec.workspace...mode: seedIfMissing` and
`spec.config.mergeMode: seedOnly` on the same CR — two different enums,
both about "seeding," governing two structurally different mechanisms
(per-file three-tier cascade with an explicit `overwrite` opt-out, vs.
whole-file-once with a fixed always-reasserted subset). This is a closer,
more relevant local precedent than the external `merge`/`overwrite` naming
discussion above credits, but wasn't evaluated in the original options
above.

**Decision (confirmed on review):** Keep `seedOnly`; do not rename to avoid
the adjacency. Every alternative that dodges "seed" (e.g. `once`, already
rejected above for ambiguity; `handsOff`, `frozen`) either reintroduces a
naming problem this analysis already ruled out or breaks the
`merge`/`overwrite`/`seedOnly` mechanism-first family Option B was chosen
to preserve — a real but narrow cost to avoid a confusion risk that's
itself narrow, since the two fields live in different parts of the spec
(`spec.config.mergeMode` vs. `spec.workspace.*.mode`) with no way to
mistakenly set one where the other belongs. The risk is "a careful reader
or reviewer is briefly confused," not "a CR author misconfigures their
CR," and that's fully addressed by cross-referencing documentation instead:
add a one-line doc-comment on each type pointing at the other (see the
[design doc's Implementation Plan](user-owned-config-design.md#implementation-plan)),
plus a short disambiguation callout in the new ADR and
`docs/architecture.md` update, since that's where both mechanisms are most
likely to appear side by side for a reviewer.
