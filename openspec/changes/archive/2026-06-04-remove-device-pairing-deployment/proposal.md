## Why

The device-pairing deployment application is outdated and no longer needed. Removing it reduces operational complexity and maintenance burden. The `ClawDevicePairingRequest` CRD and its controller remain in place — only the web application deployment and its associated resources are being removed.

## What Changes

- **BREAKING**: Remove `devicePairingURL` field from `ClawStatus`
- Remove all embedded manifests from `internal/assets/manifests/claw-device-pairing/`
- Remove all device-pairing deployment logic from the resource controller (conditional manifest loading, route host injection, cleanup functions, legacy ClusterRole cleanup)
- Remove `buildDevicePairingURL()` and device-pairing status update logic from `claw_status.go`
- Remove `DevicePairingURL` clearing from idle handling in `claw_idle.go`
- Remove `ConditionTypeDevicePairingConfigured` condition constant
- Remove unit tests for device-pairing deployment (`claw_device_pairing_deployment_test.go`)
- Remove device-pairing E2E tests
- Keep: `ClawDevicePairingRequest` CRD, its controller, and associated types
- Keep: `DisableDevicePairing` field in `AuthSpec` and `shouldDisableDevicePairing()` helper (still used by the `ClawDevicePairingRequest` controller)

## Capabilities

### New Capabilities

_None — this is a removal change._

### Modified Capabilities

- `optional-device-pairing-app-deployment`: Removing this capability entirely — the device-pairing deployment is no longer managed by the operator
- `status-urls`: Removing the `devicePairingURL` field from Claw status

## Impact

- **API types** (`api/v1alpha1/claw_types.go`): `ClawStatus.DevicePairingURL` field removed — breaking change for consumers reading status
- **CRD YAML**: Must be regenerated after type changes (`make manifests`)
- **Controller**: Significant code removal from `claw_resource_controller.go`, `claw_auth.go`, `claw_status.go`, `claw_idle.go`
- **Manifests**: Entire `internal/assets/manifests/claw-device-pairing/` directory deleted
- **Tests**: `claw_device_pairing_deployment_test.go` deleted; status and E2E tests updated
- **Existing clusters**: Operator should clean up device-pairing resources on next reconcile for instances that had it enabled
