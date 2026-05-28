## Why

When `spec.auth.disableDevicePairing` is `true`, the device pairing app serves no purpose — browser device identity checks are disabled, so no pairing requests will ever be issued. Deploying the device-pairing Deployment, Service, ServiceAccount, ClusterRole, RoleBinding, and Route wastes cluster resources and adds unnecessary attack surface.

## What Changes

- When `spec.auth.disableDevicePairing` is explicitly `true`, skip rendering and applying all `claw-device-pairing` Kustomize component resources (Deployment, Service, ServiceAccount, ClusterRole, RoleBinding, Route).
- When `spec.auth.disableDevicePairing` is `false` or unset, deploy the device-pairing app as today (default behavior preserved).
- When device pairing is disabled, clean up any previously-deployed device-pairing resources (handles the transition from enabled → disabled).
- Adapt status conditions: skip the `DevicePairingConfigured` condition check when device pairing is disabled. The `Ready` condition should not depend on a deployment that was intentionally skipped.
- Adapt idle handling: skip scaling the device-pairing Deployment to zero when device pairing is disabled.
- Add e2e tests covering both the enabled and disabled device-pairing scenarios.

## Capabilities

### New Capabilities

- `optional-device-pairing-app-deployment`: Conditional deployment of the device-pairing application based on `spec.auth.disableDevicePairing`. Covers resource lifecycle (skip deploy, cleanup on disable), status condition adaptation, and idle handling.

### Modified Capabilities

- `device-pairing-deployment`: The device-pairing Deployment is now conditionally deployed. Existing spec requirements still apply when pairing is enabled; they are skipped when disabled.
- `status-conditions`: The `DevicePairingConfigured` condition behavior changes — it should not be set (or should be removed) when device pairing is disabled, to avoid blocking the `Ready` condition.

## Impact

- **Controller code** (`internal/controller/`): `claw_resource_controller.go` (manifest rendering, route injection), `claw_status.go` (deployment readiness checks, condition setting), `claw_idle.go` (deployment scaling).
- **Unit tests**: `claw_device_pairing_deployment_test.go`, `claw_status_test.go`, `claw_idle_test.go`, `suite_test.go` (cleanup and helper functions need to be conditional).
- **E2E tests** (`test/e2e/`): New test cases for the disabled-device-pairing scenario.
- **No API changes**: The `DisableDevicePairing` field already exists in `api/v1alpha1/claw_types.go`. No CRD regeneration needed.
