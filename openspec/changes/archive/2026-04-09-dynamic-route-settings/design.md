## Context

The OpenClaw controller currently applies all resources atomically via Kustomize and server-side apply. The ConfigMap containing OpenClaw settings (`openclaw.json`) includes a `gateway.controlUI.allowedOrigins` array that needs the Route's external host for CORS. Currently, the ConfigMap has a static placeholder `OPENCLAW_ROUTE_HOST` that cannot be resolved because the Route's ingress host is dynamically assigned by OpenShift after the Route is created.

The controller already has a `getRouteURL()` helper (line 485-518 in `openclaw_resource_controller.go`) that fetches the Route and extracts `.spec.host`, currently used only for populating `instance.Status.URL`. The ConfigMap manifest already contains the placeholder at line 19: `"allowedOrigins": ["https://OPENCLAW_ROUTE_HOST"]`.

## Goals / Non-Goals

**Goals:**
- Enable dynamic CORS configuration for OpenClaw control UI using the actual Route host
- Maintain atomic resource creation where possible (apply Route separately, then remaining resources together)
- Reuse existing `getRouteURL()` logic to minimize code duplication
- Gracefully handle vanilla Kubernetes (no Route CRD) by skipping Route-based injection
- Ensure ConfigMap always has valid `allowedOrigins` value before Deployments start

**Non-Goals:**
- Support multiple Route hosts or dynamic Route updates after initial creation (CORS origins are set once at deployment time)
- Hot-reload ConfigMap changes without pod restarts (existing restart mechanism via annotations is sufficient)
- Apply resources fully atomically in a single pass (Route must be applied first and status must populate before ConfigMap can be finalized)

## Decisions

### Decision 1: Three-phase reconciliation instead of single atomic apply

**Rationale:** The Route must exist and have `.status.ingress[0].host` populated before the ConfigMap can be finalized. OpenShift's Route controller populates this field asynchronously after Route creation. We cannot apply all resources atomically because the ConfigMap depends on Route status.

**Approach:**
1. **Phase 1**: Apply gateway Secret (unchanged, already happens first)
2. **Phase 2**: Apply Route manifest only, wait for `.status.ingress[0].host` to populate
3. **Phase 3**: Inject Route host into ConfigMap, apply all remaining resources (ConfigMap, PVC, Deployments, Services, NetworkPolicies)

**Alternatives considered:**
- _Single-pass with post-apply patch_: Apply all resources including ConfigMap with placeholder, then patch ConfigMap after Route is ready. Rejected because Deployments would start with invalid CORS config, potentially causing startup failures or security issues.
- _Webhook-based injection_: Use mutating webhook to inject Route host at admission time. Rejected due to operational complexity (webhook cert management, additional deployment).

### Decision 2: Requeue with 5-second backoff when Route status not ready

**Rationale:** Route status population is asynchronous and typically completes within seconds. Polling with a short requeue interval balances responsiveness with API server load.

**Approach:**
- If `getRouteURL()` returns empty string (Route exists but `.status.ingress[0].host` not yet populated), return `ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}`
- If Route CRD not registered (`meta.IsNoMatchError`), skip Route phase entirely and proceed to Phase 3 with placeholder unchanged

**Alternatives considered:**
- _Longer backoff (30s)_: Rejected because it delays initial deployment unnecessarily when Route status typically populates in 5-10 seconds.
- _Exponential backoff_: Rejected because Route status population is not a transient error condition; it either succeeds quickly or indicates a cluster problem that won't resolve with retries.

### Decision 3: String replacement in parsed YAML instead of ConfigMap template mutation

**Rationale:** The ConfigMap data is nested JSON embedded in YAML. Modifying it in-place within the parsed `unstructured.Unstructured` object is cleaner than maintaining a separate templating system.

**Approach:**
- After parsing Kustomize output into `[]*unstructured.Unstructured`, locate the ConfigMap object
- Extract `.data["openclaw.json"]` as string
- Use `strings.ReplaceAll()` to replace `OPENCLAW_ROUTE_HOST` with the Route host (including `https://` scheme)
- Set the modified string back into the ConfigMap object
- Continue with existing server-side apply logic

**Alternatives considered:**
- _Go text/template in manifest file_: Rejected because it requires pre-processing manifests before Kustomize, complicating the build pipeline.
- _JSON unmarshal/marshal for surgical edit_: Rejected as unnecessarily complex; string replacement is simpler and less error-prone for a single-value substitution.

### Decision 4: Preserve existing server-side apply for remaining resources

**Rationale:** The existing Kustomize-based atomic apply works well for resources that don't have external dependencies. Only the Route and ConfigMap require special handling.

**Approach:**
- Refactor `applyKustomizedResources()` to accept a filter predicate for which resources to apply
- Phase 2 calls it with `kind == "Route"` filter
- Phase 3 calls it with `kind != "Route"` filter (after ConfigMap injection)
- Both phases use existing server-side apply logic with field manager `"openclaw-operator"`

**Alternatives considered:**
- _Separate Route application outside Kustomize_: Rejected because Route manifest still needs labels from kustomization.yaml and should remain in the same manifest directory.
- _Apply Route and ConfigMap together, then rest_: Rejected because ConfigMap depends on Route status, so they cannot be applied atomically.

### Decision 5: Reuse getRouteURL() for Route status checking

**Rationale:** The `getRouteURL()` method already implements Route fetching and `.spec.host` extraction with proper error handling (including `meta.IsNoMatchError` for non-OpenShift clusters). Reusing it avoids code duplication.

**Approach:**
- Extend `getRouteURL()` to check `.status.ingress[0].host` instead of `.spec.host` (current implementation reads spec, but status is the authoritative source after OpenShift populates it)
- Return empty string if Route exists but status not yet populated (instead of empty spec.host)
- Phase 2 calls `getRouteURL()` and requeues if result is empty string

**Note:** Current implementation reads `.spec.host` (line 509), but the spec asks for `.status.ingress[0].host`. Investigation needed: OpenShift Routes may have both fields or only status field.

## Risks / Trade-offs

**[Risk]** Route status may never populate if OpenShift Route controller is down or misconfigured
→ **Mitigation:** Controller will requeue indefinitely with backoff. Users can diagnose via `kubectl describe route openclaw` to see Route status. Add event recording on prolonged waiting (e.g., log warning after 5 retries).

**[Risk]** ConfigMap injection may fail if `openclaw.json` format changes (e.g., escaping, whitespace variations)
→ **Mitigation:** Use exact string match `OPENCLAW_ROUTE_HOST` (not regex). Add unit test verifying placeholder replacement with actual Route host. ConfigMap manifest must preserve exact placeholder format.

**[Risk]** On vanilla Kubernetes, placeholder remains in ConfigMap causing CORS failures
→ **Mitigation:** Add fallback logic: if Route CRD not registered, replace `OPENCLAW_ROUTE_HOST` with empty array `[]` or localhost fallback. Update spec requirement for Kubernetes handling (currently says "placeholder value unchanged or default value" - needs clarification).

**[Trade-off]** Reconciliation now takes longer (5-10 seconds for Route status + ConfigMap apply) instead of single atomic apply
→ **Accepted:** The delay is inherent to waiting for Route status and is acceptable for initial deployment. Subsequent reconciliations skip Route phase if host already known.

**[Trade-off]** ConfigMap is no longer applied atomically with Deployments
→ **Accepted:** Deployments depend on ConfigMap via volume mount, so Kubernetes will wait for ConfigMap to exist before starting pods. The brief timing gap is safe.

## Migration Plan

**Deployment steps:**
1. Update ConfigMap manifest to include placeholder (already present: `"allowedOrigins": ["https://OPENCLAW_ROUTE_HOST"]`)
2. Deploy refactored controller with three-phase reconciliation
3. Existing OpenClaw instances will reconcile: Route applies first, then ConfigMap updates with actual host, Deployments restart with correct CORS config
4. No manual intervention required (owner references ensure garbage collection works)

**Rollback strategy:**
- Revert controller to previous version
- ConfigMap will retain injected Route host value (no placeholder restoration needed)
- Existing deployments continue working with correct CORS config
- New deployments will use static placeholder until controller is upgraded again

## Open Questions

1. **Route spec.host vs status.ingress[0].host:** Current `getRouteURL()` reads `.spec.host`. Spec requirement says `.status.ingress[0].host`. Which is correct for OpenShift Routes?
   - Investigation: Check OpenShift Route API docs and test on live cluster
   - Decision needed before implementation

2. **Vanilla Kubernetes fallback value:** Should we use empty array, localhost, or preserve placeholder?
   - Option A: `"allowedOrigins": []` (disables CORS entirely, may break control UI)
   - Option B: `"allowedOrigins": ["http://localhost:18789"]` (works for local port-forward access)
   - Option C: Leave placeholder (explicitly invalid, forces user to configure manually)
   - Recommendation: Option B (localhost fallback) for better UX on vanilla Kubernetes

3. **Requeue limit:** Should we stop requeuing after N failures or continue indefinitely?
   - Current design: Infinite requeue with 5s backoff
   - Alternative: Stop after 20 retries (100 seconds), set Available=False with error message
   - Recommendation: Keep infinite requeue; cluster-level Route issues should be visible via Route status, not OpenClaw status
