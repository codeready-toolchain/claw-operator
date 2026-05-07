## Why

The device pairing flow requires a dedicated web application (`claw-device-pairing`) to serve the pairing UI. Currently, the operator deploys the gateway and proxy components but has no mechanism to deploy the device pairing service alongside them. Adding this as an operator-managed component ensures it shares the same lifecycle, Route host, and namespace as the Claw instance.

## What Changes

- Add a new `claw-device-pairing` Kustomize manifest folder under `internal/assets/manifests/` containing a ServiceAccount, Deployment, Service, and Route
- Extend `buildKustomizedObjects()` to load and build the device-pairing manifests alongside the existing claw and claw-proxy components
- The device-pairing Route shares the same `.spec.host` as the Claw Route but serves traffic on the `/integration/device-pairing` path prefix
- All resource names follow the `CLAW_INSTANCE_NAME-device-pairing` naming convention (template-replaced at build time, same as existing components)
- The device-pairing resources are applied after the Claw and Claw Proxy resources (Phase 3, alongside remaining resources)

## Capabilities

### New Capabilities
- `device-pairing-deployment`: Operator-managed deployment of the claw-device-pairing web application (ServiceAccount, Deployment, Service, Route) using embedded Kustomize manifests, sharing the Claw Route host with path-based routing

### Modified Capabilities
- `claw-controller`: The `buildKustomizedObjects()` function is extended to load the new device-pairing manifests, and `applyRouteOnly()` now applies the device-pairing Route alongside the Claw Route

## Impact

- **Code**: `internal/controller/claw_resource_controller.go` — `buildKustomizedObjects()` gains a third manifest group; Route filtering in `applyRouteOnly()` already handles all Route-kind objects so the device-pairing Route is automatically included
- **Embedded assets**: New `internal/assets/manifests/claw-device-pairing/` directory with 5 files (kustomization.yaml + 4 resource YAMLs)
- **RBAC**: ServiceAccount resource creation requires existing RBAC permissions (the operator already has broad resource management permissions via server-side apply)
- **Networking**: The device-pairing Route uses path-based routing on the existing Claw Route host, so no additional DNS or TLS configuration is needed
