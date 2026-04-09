## 1. Route Status Checking

- [x] 1.1 Investigate Route API: verify whether `.spec.host` or `.status.ingress[0].host` is populated by OpenShift and should be used
- [x] 1.2 Update `getRouteURL()` method to read from correct field (status.ingress[0].host if investigation confirms)
- [x] 1.3 Ensure `getRouteURL()` returns empty string when Route exists but status not yet populated

## 2. Resource Application Filtering

- [x] 2.1 Refactor `applyKustomizedResources()` to accept optional filter function `func(*unstructured.Unstructured) bool`
- [x] 2.2 Extract server-side apply loop into reusable `applyResources()` helper method
- [x] 2.3 Create `applyRouteOnly()` method that calls `applyKustomizedResources` with `kind == "Route"` filter
- [x] 2.4 Create `applyRemainingResources()` method that calls `applyKustomizedResources` with `kind != "Route"` filter

## 3. ConfigMap Injection Logic

- [x] 3.1 Create `injectRouteHostIntoConfigMap()` method that finds ConfigMap in parsed objects array
- [x] 3.2 Extract `data["openclaw.json"]` string from ConfigMap object using `unstructured.NestedString()`
- [x] 3.3 Use `strings.ReplaceAll()` to replace `OPENCLAW_ROUTE_HOST` with Route host (including `https://` scheme)
- [x] 3.4 Set modified JSON string back into ConfigMap using `unstructured.SetNestedField()`
- [x] 3.5 Add fallback logic: if routeHost is empty (vanilla Kubernetes), replace with `http://localhost:18789`

## 4. Reconciliation Flow Refactor

- [x] 4.1 Update `Reconcile()` to apply gateway Secret first (unchanged)
- [x] 4.2 Add Phase 2: call `applyRouteOnly()` and attempt to fetch Route URL with `getRouteURL()`
- [x] 4.3 Add requeue logic: if Route URL empty, return `ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}`
- [x] 4.4 Add Phase 3: call `injectRouteHostIntoConfigMap()` with fetched Route host before applying remaining resources
- [x] 4.5 Update Phase 3: call `applyRemainingResources()` to apply ConfigMap, Deployments, Services, NetworkPolicies
- [x] 4.6 Preserve existing `configureProxyDeployment()` and `stampSecretVersionAnnotation()` calls in Phase 3
- [x] 4.7 Keep existing `updateStatus()` call at end of reconciliation

## 5. Error Handling

- [x] 5.1 Handle `meta.IsNoMatchError` when Route CRD not registered: skip Phase 2 entirely, proceed to Phase 3 with localhost fallback
- [x] 5.2 Add logging: log info when waiting for Route status with "Route exists but status not populated, requeuing"
- [x] 5.3 Add logging: log info when Route CRD not found with "Route CRD not registered, using localhost fallback for CORS"

## 6. Unit Tests

- [x] 6.1 Add test case: Route exists with populated `.status.ingress[0].host`, verify ConfigMap contains injected host
- [x] 6.2 Add test case: Route exists but status not populated, verify reconciliation requeues with 5s backoff
- [x] 6.3 Add test case: Route CRD not registered, verify ConfigMap contains localhost fallback
- [x] 6.4 Add test case: verify `injectRouteHostIntoConfigMap()` replaces all occurrences of placeholder
- [x] 6.5 Add test case: verify existing deployments still have `configureProxyDeployment()` and `stampSecretVersionAnnotation()` called

## 7. Documentation Updates

- [x] 7.1 Update CLAUDE.md reconciliation flow diagram to show three-phase reconciliation
- [x] 7.2 Add note about Route status polling and requeue behavior
- [x] 7.3 Document vanilla Kubernetes fallback behavior (localhost CORS origin)
