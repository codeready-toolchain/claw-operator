## 1. Update ConfigMap Template

- [x] 1.1 Update `internal/assets/manifests/claw/configmap.yaml` to change `allowedOrigins` from single placeholder to array with two placeholders: `["OPENCLAW_ROUTE_HOST", "OPENCLAW_CONSOLE_ROUTE_HOST"]`

## 2. Add Route Fetching Helpers

- [x] 2.1 Extract existing route fetching logic into a reusable helper function `getRouteHost(ctx context.Context, namespace string, routeName string) (string, error)` that fetches a route by name and returns its host or empty string if status not ready
- [x] 2.2 Update existing `getRouteURL()` function to call the new `getRouteHost()` helper with route name "claw"

## 3. Fetch Console Route During Reconciliation

- [x] 3.1 Add call to `getRouteHost()` in Phase 2 to fetch the `claw-console` route host
- [x] 3.2 Handle NotFound error gracefully by logging a warning and continuing with empty console route host
- [x] 3.3 Handle unpopulated route status by requeuing with 5-second backoff (same as gateway route)
- [x] 3.4 Pass both gateway and console route hosts to the ConfigMap injection function

## 4. Update ConfigMap Injection Logic

- [x] 4.1 Rename `injectRouteHostIntoConfigMap()` to `injectRouteHostsIntoConfigMap()` and add a `consoleRouteHost` parameter
- [x] 4.2 Replace `OPENCLAW_ROUTE_HOST` placeholder with gateway route host (or localhost fallback)
- [x] 4.3 Replace `OPENCLAW_CONSOLE_ROUTE_HOST` placeholder with console route host (or empty string if missing)
- [x] 4.4 Filter out empty strings from the `allowedOrigins` array after placeholder replacement
- [x] 4.5 Ensure localhost fallback applies to both placeholders when on vanilla Kubernetes

## 5. Update Tests

- [x] 5.1 Add test case for ConfigMap with both gateway and console route hosts in `allowedOrigins`
- [x] 5.2 Add test case for missing console route (only gateway route in `allowedOrigins`)
- [x] 5.3 Add test case for vanilla Kubernetes (localhost fallback, no console route)
- [x] 5.4 Add test case for console route with unpopulated status (requeue behavior)
- [x] 5.5 Update existing route-related tests to use the new `getRouteHost()` helper

## 6. Update Documentation

- [x] 6.1 Update CLAUDE.md to document the console route CORS configuration
- [x] 6.2 Add comment in ConfigMap template explaining the two placeholders
