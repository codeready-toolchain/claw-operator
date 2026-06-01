# ADR-0018: Centralized Provider Registry & Deployment Apply Strategy

**Status:** Implemented
**Date:** 2026-05-31

---

## Problem

Two independent issues were addressed together:

### 1. Scattered provider knowledge

Per-provider knowledge was scattered across four independent maps in the controller:

| Map | Purpose | File |
|-----|---------|------|
| `knownAPIKeyProviders` | Domain + header defaults for apiKey credentials | `claw_providers.go` |
| `companionProviders` | Internal provider name mappings (e.g., openai → openai-codex) | `claw_providers.go` |
| `vertexProviderAPIMapping` | Wire format API for Vertex AI SDK path | `claw_providers.go` |
| `modelCatalog` | Model names and aliases per provider | `claw_models.go` |

Adding a new first-class provider required updating up to four maps in two files — easy to miss one and produce subtle bugs. The wire format API identifier (`api` field in `models.providers`) was not tracked anywhere, leading to a bug where non-OpenAI providers (Google, Anthropic) used the wrong wire format (`openai-completions` instead of their native API).

Additionally, `injectProviders()` built provider config entries as plain maps and then relied on downstream code to mutate them with the correct `api` field — a "build then mutate" pattern that made it easy to forget the mutation step, which is exactly what caused the wire format bug.

### 2. SSA field-ownership loop on Deployments

The operator used server-side apply (SSA) with `Force: true` for all resources, including Deployments. This caused an infinite rollout loop where Deployment `metadata.generation` incremented ~1/second even when the desired spec was identical to the live spec.

**Root cause:** SSA Force + `kube-controller-manager`'s traditional Updates create a field-ownership tug-of-war. When SSA Force takes ownership of a field from another manager, the API server increments `generation` even if the field value hasn't changed. Combined with the controller's `Owns(&appsv1.Deployment{})` watch, this created a feedback loop:

1. Operator SSA-Force → takes ownership of a field → generation++
2. Watch fires → new reconcile → SSA-Force again → repeat

**Contributing cause:** The proxy config sort was non-deterministic (`sort.Slice` with a weak total order on `routeLess`), causing the proxy ConfigMap to change on some reconciles, which triggered proxy Deployment rollouts and cascaded to the gateway's `wait-for-proxy` init container.

---

## Design

### Single provider registry

A single `knownProviders` map of `providerDefaults` structs in `claw_providers.go` replaces all four maps. Each entry captures everything the operator needs to know about a provider:

| Field | Purpose | Example (anthropic) |
|-------|---------|---------------------|
| `Domain` | Default upstream domain for apiKey credentials | `api.anthropic.com` |
| `Header` | Default auth header name | `x-api-key` |
| `API` | OpenClaw wire format identifier | `anthropic-messages` |
| `VertexAPI` | Wire format for Vertex AI SDK path | `anthropic-messages` |
| `BasePath` | URL path appended to upstream host | (empty) |
| `Companions` | Internal provider names auto-injected alongside | (empty) |
| `VertexPlugin` | ClawHub package required for Vertex SDK path | `@openclaw/anthropic-vertex-provider` |
| `Models` | Model catalog entries (name + alias) | `[{claude-sonnet-4-6, Claude Sonnet 4.6}, ...]` |

Providers not in the registry (e.g., `openrouter`, custom self-hosted endpoints) still work — they just get no defaults, no API override (OpenClaw defaults to `openai-completions`), and no model catalog.

### Builder function

`buildProviderEntry()` constructs `models.providers` entries with the correct `api` field baked in from `knownProviders`. This replaces the "build map then mutate" pattern that caused the wire format bug — the entry is correct at construction time.

### Implicit plugins

Providers that require an external OpenClaw plugin for the Vertex AI SDK path (e.g., `@openclaw/anthropic-vertex-provider` for Anthropic via Vertex) declare the package in the `VertexPlugin` field. `effectivePlugins()` merges these implicit plugins with explicit `spec.plugins` entries, deduplicating where both are declared.

### Deployment apply strategy: CreateOrUpdate

Deployments are applied via `controllerutil.CreateOrUpdate` instead of SSA. All other resource types (ConfigMaps, Services, NetworkPolicies, Routes) continue to use SSA, which works correctly for resources without field-ownership conflicts.

| Resource type | Apply method | Rationale |
|---|---|---|
| Deployments | `controllerutil.CreateOrUpdate` | Avoids SSA field-ownership generation bumps |
| Everything else | SSA (`client.Apply` + `Force: true`) | Simple resources don't have the ownership fight problem |

How `CreateOrUpdate` prevents the loop:
1. `Get` the existing Deployment
2. Run the mutate function on the live object
3. Compare before vs after with `equality.Semantic.DeepEqual`
4. **Skip Update if equal** — no generation bump, no watch event, no loop

The Kustomize-rendered unstructured Deployment is converted to a typed `*appsv1.Deployment` via `runtime.DefaultUnstructuredConverter.FromUnstructured()` at the apply boundary.

### NormalizeDeployment

A `NormalizeDeployment()` function pre-applies the same defaults the Kubernetes admission controller would apply. Without this, `CreateOrUpdate` would see spurious diffs between the operator's desired spec (missing defaulted fields) and the API server's stored spec (with defaults populated).

Fields normalized:

| Field | Default |
|---|---|
| `spec.strategy.type` | `RollingUpdate` |
| `spec.strategy.rollingUpdate` | `{maxUnavailable: "25%", maxSurge: "25%"}` |
| `spec.revisionHistoryLimit` | `10` |
| `spec.progressDeadlineSeconds` | `600` |
| `spec.template.spec.restartPolicy` | `Always` |
| `spec.template.spec.dnsPolicy` | `ClusterFirst` |
| `spec.template.spec.schedulerName` | `default-scheduler` |
| `spec.template.spec.terminationGracePeriodSeconds` | `30` |
| `spec.template.spec.serviceAccountName` | `default` |
| `spec.template.spec.deprecatedServiceAccount` | copies from `serviceAccountName` |
| `spec.template.spec.securityContext` | `{}` |
| `spec.template.spec.enableServiceLinks` | `true` |
| Container `terminationMessagePath` | `/dev/termination-log` |
| Container `terminationMessagePolicy` | `File` |
| Container `imagePullPolicy` | `Always` for `:latest`/no tag, else `IfNotPresent` |
| Container port `protocol` | `TCP` |
| Env `valueFrom.fieldRef.apiVersion` | `v1` |
| Probe `timeoutSeconds`/`periodSeconds`/`successThreshold`/`failureThreshold` | 1/10/1/3 |
| Probe `httpGet.scheme` | `HTTP` |
| Volume `configMap.defaultMode` / `secret.defaultMode` | `0644` |

### Proxy config sort fix

Changed `sort.Slice` to `sort.SliceStable` and added a deterministic tie-breaker (`a.Injector < b.Injector`) to the `routeLess` comparator. The previous comparator had a weak total order — when two routes shared the same domain and both had empty `AllowedPaths`, the sort was unstable, producing non-deterministic proxy ConfigMap JSON.

---

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | How to organize provider knowledge | Single `knownProviders` map of `providerDefaults` structs | One place to add a new provider; impossible to forget a field since the struct is self-documenting |
| 2 | How to set the wire format API | `buildProviderEntry()` reads from registry at construction time | Eliminates the "build then mutate" pattern; entry is correct from the start |
| 3 | Where to define model catalogs | `Models` field inside `providerDefaults` | Models are provider-specific knowledge; keeping them in the registry avoids a separate map that can drift |
| 4 | How to handle Vertex SDK plugins | `VertexPlugin` field + `effectivePlugins()` auto-merge | Users don't need to know about internal plugin requirements; deduplication prevents conflicts with explicit declarations |
| 5 | How to apply Deployments | `controllerutil.CreateOrUpdate` with `NormalizeDeployment` | Eliminates SSA field-ownership generation loop; matches upstream's proven pattern |

---

## Consequences

**Positive:**
- Single source of truth for provider configuration — adding a provider is a one-struct change
- Wire format bug eliminated at the type level — `buildProviderEntry()` makes incorrect API fields impossible
- All three Deployments (gateway, proxy, device-pairing) are stable — generations do not increment unless the spec actually changes
- `CreateOrUpdate` naturally handles create vs update — no separate first-apply logic needed

**Negative:**
- `NormalizeDeployment` is a maintenance surface — new Kubernetes versions may add admission defaults that need to be mirrored
- Two apply paths (SSA for non-Deployments, CreateOrUpdate for Deployments) adds conceptual complexity
- The unstructured→typed conversion at the boundary is a potential source of round-trip issues if the Kustomize YAML uses non-standard field names

**Risks:**
- If `NormalizeDeployment` misses a defaulted field, the operator will issue an Update on every reconcile (detectable via `claw_normalize_test.go` which validates against real API server responses via envtest)

---

## Backward Compatibility

- Provider behavior is unchanged — same defaults, same routing, same model catalogs.
- The wire format fix changes the `api` field in generated `models.providers` entries for `google`, `anthropic`, and `openai-codex`. This is a bug fix (they were using the wrong wire format before).
- Plugin install script now uses manifest-tracked cleanup, preserving user-installed plugins across pod restarts.

---

## Key files

- `internal/controller/claw_providers.go` — `knownProviders` registry, `buildProviderEntry()`
- `internal/controller/claw_models.go` — `GetModelCatalog()` delegates to registry
- `internal/controller/claw_plugins.go` — `effectivePlugins()` auto-merges Vertex plugins
- `internal/controller/claw_resource_controller.go` — `applyResources` (strategy split), `applyDeployment` (CreateOrUpdate)
- `internal/controller/claw_normalize.go` — `NormalizeDeployment` and helpers
- `internal/controller/claw_normalize_test.go` — normalization tests
- `internal/controller/claw_proxy.go` — `routeLess` sort fix

---

## References

- Investigation and options analysis: `docs/debug-recreate-rollout-loop.md`
- Upstream pattern: `controllerutil.CreateOrUpdate` in `tmp/openclaw-operator/internal/controller/`
- Upstream normalization: `tmp/openclaw-operator/internal/resources/statefulset.go`
