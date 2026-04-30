## Why

The OpenClaw Control UI needs to allow CORS requests from both the main gateway route and the separate console route. Currently, only the gateway route host is included in `allowedOrigins`. When the console is hosted on a different route (`claw-console`), the browser blocks requests due to CORS policy violations.

## What Changes

- The operator will fetch the `claw-console` route host during reconciliation (similar to the existing `claw` route fetch)
- The ConfigMap's `allowedOrigins` array will include both the gateway route host and the console route host
- Both origins will use the `https://` scheme
- On vanilla Kubernetes (no Route CRD), only the localhost fallback will be used

## Capabilities

### New Capabilities

- `console-route-cors`: Fetch the claw-console route and inject its host into the ConfigMap's allowedOrigins array

### Modified Capabilities

- `dynamic-route-config`: Extend the route host injection to support multiple routes (gateway + console)

## Impact

**Affected code:**
- `internal/controller/claw_resource_controller.go`: Add console route fetching logic and update ConfigMap injection
- `internal/assets/manifests/claw/configmap.yaml`: Change `allowedOrigins` to include both route placeholders

**Affected resources:**
- ConfigMap `claw-config`: Will now have two entries in `allowedOrigins` array
- Route `claw-console`: Will be queried for its host during reconciliation

**User impact:**
- Users deploying on OpenShift will see CORS automatically configured for both routes
- Users on vanilla Kubernetes (no console route) will see no change in behavior
