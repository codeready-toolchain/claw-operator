## Context

The Claw operator currently uses a three-phase reconciliation approach to dynamically inject the gateway route host into the ConfigMap's `allowedOrigins` field. The existing implementation:

1. Applies the Route resource
2. Polls Route status for `.status.ingress[0].host`
3. Injects the host into the ConfigMap by replacing the `OPENCLAW_ROUTE_HOST` placeholder

This works for the single gateway route but needs to be extended to support a second route (`claw-console`) for the console UI. The console may be hosted on a different domain than the gateway, requiring both origins to be allowed for CORS.

**Current flow:**
- `applyRouteOnly()` applies the Route
- `getRouteURL()` fetches and returns the route host
- `injectRouteHostIntoConfigMap()` replaces the placeholder with the actual host

**Constraints:**
- Must maintain backward compatibility with single-route deployments
- Must gracefully handle vanilla Kubernetes (no Route CRD)
- Must not break existing placeholder replacement logic
- Must preserve three-phase reconciliation pattern

## Goals / Non-Goals

**Goals:**
- Fetch both `claw` and `claw-console` route hosts during reconciliation
- Include both hosts in the ConfigMap's `allowedOrigins` array
- Maintain fallback behavior for vanilla Kubernetes clusters
- Use the same polling/requeue pattern for both routes

**Non-Goals:**
- Supporting more than two routes (not needed for current use case)
- Making route names configurable (fixed convention)
- Changing the Route CRD structure or OpenShift routing behavior
- Modifying the three-phase reconciliation pattern

## Decisions

### Decision 1: Fetch console route in the same phase as gateway route

**Rationale:**
Both routes need to be resolved before ConfigMap injection (Phase 3). Fetching them sequentially in Phase 2 keeps the reconciliation logic clean and avoids multiple ConfigMap updates.

**Implementation:**
- Extract route fetching logic into a reusable helper `getRouteHost(ctx, namespace, routeName)`
- Call it twice in Phase 2: once for `claw` route, once for `claw-console` route
- If either route status is unpopulated, requeue with 5s backoff (same as current behavior)

**Alternatives considered:**
- Fetch routes in parallel: Adds complexity with no meaningful performance benefit (both are cheap API calls)
- Fetch console route in a separate phase: Would require four phases instead of three, adding unnecessary complexity

### Decision 2: Use two placeholders in the ConfigMap template

**Rationale:**
Having distinct placeholders (`OPENCLAW_ROUTE_HOST` and `OPENCLAW_CONSOLE_ROUTE_HOST`) makes the template explicit and avoids array manipulation during injection.

**Implementation:**
```json
"allowedOrigins": ["OPENCLAW_ROUTE_HOST", "OPENCLAW_CONSOLE_ROUTE_HOST"]
```

During injection, replace both placeholders with actual hosts. On vanilla Kubernetes, replace both with `http://localhost:18789`.

**Alternatives considered:**
- Single placeholder with array string: Would require JSON parsing/manipulation, more error-prone
- Dynamically append to array: Would require parsing the JSON structure, more complex than string replacement
- Conditional inclusion: Would require template logic, violates the simple string replacement pattern

### Decision 3: Console route is optional (graceful degradation)

**Rationale:**
The console route may not exist in all deployments. The operator should not fail reconciliation if only the gateway route exists.

**Implementation:**
- If `claw-console` route fetch returns `NotFound`, log a warning and set placeholder to empty string
- During ConfigMap injection, filter out empty strings from the allowedOrigins array
- This allows single-route deployments to work without changes

**Alternatives considered:**
- Make console route mandatory: Would break existing deployments that only have the gateway route
- Use the gateway route as fallback: Misleading, better to be explicit about missing routes
- Skip placeholder for missing routes: Would require conditional template logic, breaks simple replacement pattern

### Decision 4: Reuse existing Route polling pattern

**Rationale:**
The existing `getRouteURL()` pattern (fetch, check status, requeue if unpopulated) is proven and handles OpenShift router timing correctly.

**Implementation:**
- Rename `getRouteURL()` to `getRouteHost()` and add a `routeName` parameter
- Call it for both `claw` and `claw-console` routes
- Requeue if either returns empty string (status not ready)

**Alternatives considered:**
- Poll both routes in a single function: Would hide which route is causing the requeue
- Use different requeue intervals: No benefit, both routes populate in the same timeframe
- Skip polling for console route: Would cause race conditions if console route status is slower than gateway

## Risks / Trade-offs

**[Risk] Console route status may lag behind gateway route**
→ **Mitigation:** Requeue on either route being unpopulated ensures both are ready before ConfigMap injection

**[Risk] Empty allowedOrigins if both routes missing**
→ **Mitigation:** Vanilla Kubernetes fallback (`http://localhost:18789`) provides a working origin for port-forwarding

**[Risk] Breaking existing deployments with only gateway route**
→ **Mitigation:** Console route is optional; missing route degrades gracefully by excluding it from allowedOrigins

**[Trade-off] Two placeholders vs. dynamic array construction**
→ **Chosen:** Two placeholders for simplicity. **Cost:** Less flexible if more routes are added in the future. **Benefit:** Keeps string replacement pattern, no JSON parsing required.

**[Trade-off] Both routes in same reconciliation phase vs. separate phases**
→ **Chosen:** Same phase. **Cost:** Cannot proceed if either route fails. **Benefit:** ConfigMap gets both origins atomically, no intermediate state with partial CORS config.

## Migration Plan

**Deployment steps:**
1. Update ConfigMap template with two placeholders
2. Update controller to fetch both routes
3. Update injection logic to handle both placeholders
4. Deploy updated operator
5. Operator will reconcile existing Claw instances, updating their ConfigMaps automatically

**Rollback strategy:**
- If deployment fails, revert to previous operator version
- Existing ConfigMaps will revert to single-origin behavior on next reconciliation
- No data loss or permanent state changes

**Compatibility:**
- New operator works with old ConfigMaps (single placeholder)
- New operator works with single-route deployments (console route optional)
- No CR schema changes required
