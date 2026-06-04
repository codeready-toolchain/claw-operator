## 1. Remove API type fields

- [x] 1.1 Remove `DevicePairingURL` field from `ClawStatus` in `api/v1alpha1/claw_types.go`
- [x] 1.2 Remove `ConditionTypeDevicePairingConfigured` constant from `api/v1alpha1/claw_types.go`
- [x] 1.3 Run `make manifests generate` to regenerate CRD YAML and DeepCopy methods

## 2. Remove device-pairing manifests

- [x] 2.1 Delete the entire `internal/assets/manifests/claw-device-pairing/` directory

## 3. Remove controller logic

- [x] 3.1 Remove device-pairing manifest loading and kustomize build logic from `claw_resource_controller.go` (conditional FS overlay, `buildDevicePairingObjects`, naming functions)
- [x] 3.2 Remove `injectRouteHostIntoDevicePairingRoute()` function and its call site from `claw_resource_controller.go`
- [x] 3.3 Remove `cleanupLegacyDevicePairingClusterRole()` function and its call site from `claw_resource_controller.go`
- [x] 3.4 Convert `cleanupDevicePairingResources()` to run unconditionally (for upgrade cleanup) instead of conditionally on disable toggle — remove the `shouldDisableDevicePairing` guard
- [x] 3.5 Remove `buildDevicePairingURL()` and device-pairing status update logic from `claw_status.go`
- [x] 3.7 Remove `ConditionTypeDevicePairingConfigured` condition removal/setting logic from `claw_status.go`
- [x] 3.8 Remove `DevicePairingURL` clearing from idle handling in `claw_idle.go`
- [x] 3.9 Remove device-pairing deployment scaling from idle handling in `claw_idle.go`

## 4. Update tests

- [x] 4.1 Delete `internal/controller/claw_device_pairing_deployment_test.go`
- [x] 4.2 Remove device-pairing deployment test cases from `internal/controller/claw_status_test.go`
- [x] 4.3 Remove device-pairing deployment test cases from `internal/controller/claw_idle_test.go`
- [x] 4.4 Update E2E tests in `test/e2e/e2e_test.go`: remove device-pairing deployment test cases, but keep a test that verifies `DisableDevicePairing: false` does NOT create device-pairing deployment resources (since they no longer exist)
- [x] 4.5 Add unit test verifying that a Claw CR with `spec.auth.disableDevicePairing: false` reconciles successfully without creating any device-pairing deployment resources

## 5. Verify

- [x] 5.1 Run `make build` to verify compilation
- [x] 5.2 Run `make lint` to verify linting passes
- [x] 5.3 Run `make test` to verify all unit tests pass
