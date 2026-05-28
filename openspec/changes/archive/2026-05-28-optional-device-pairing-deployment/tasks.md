## 1. Conditional Kustomize Build

- [x] 1.1 Modify `buildKustomizedObjects()` to accept the Claw instance (or `disableDevicePairing` bool) and conditionally skip adding `claw-device-pairing` manifests to the in-memory filesystem and building the device-pairing Kustomize component
- [x] 1.2 Update the call site in `Reconcile()` to pass the required parameter to `buildKustomizedObjects()`

## 2. Skip Device-Pairing Route Injection

- [x] 2.1 Guard the `injectRouteHostIntoDevicePairingRoute()` and `applyRouteByName()` calls for the device-pairing Route with a `shouldDisableDevicePairing()` check in the reconcile loop

## 3. Cleanup Previously-Deployed Resources

- [x] 3.1 Add a `cleanupDevicePairingResources()` function that deletes Deployment, Service, ServiceAccount, RoleBinding, and Route for device-pairing by name, ignoring NotFound errors
- [x] 3.2 Call `cleanupDevicePairingResources()` early in the reconcile loop when `shouldDisableDevicePairing()` returns `true`

## 4. Status Condition Adaptation

- [x] 4.1 Modify `checkDeploymentsReady()` to accept a flag or auth spec and exclude the device-pairing Deployment from the readiness check when device pairing is disabled
- [x] 4.2 Modify `updateStatus()` to skip setting the `DevicePairingConfigured` condition and remove it from existing conditions when device pairing is disabled

## 5. Idle Scaling Adaptation

- [x] 5.1 Modify `handleIdle()` to conditionally exclude the device-pairing Deployment from the list of deployments to scale to zero when device pairing is disabled

## 6. Unit Tests

- [x] 6.1 Add unit tests in `claw_resource_controller_test.go` or `claw_device_pairing_deployment_test.go` verifying that `buildKustomizedObjects()` excludes device-pairing resources when `disableDevicePairing` is true
- [x] 6.2 Add unit tests verifying that `checkDeploymentsReady()` does not check the device-pairing Deployment when disabled
- [x] 6.3 Add unit tests verifying that status conditions omit `DevicePairingConfigured` when device pairing is disabled
- [x] 6.4 Update existing tests in `suite_test.go` helpers (`cleanupResources`, `setAllDeploymentsAvailable`) to handle the conditional presence of device-pairing resources
- [x] 6.5 Run `make test` and ensure all existing and new tests pass

## 7. E2E Tests

- [x] 7.1 Add an e2e test case that creates a Claw CR with `spec.auth.disableDevicePairing: true` and verifies device-pairing Deployment/Service/ServiceAccount are not created, while the Claw reaches Ready state
- [x] 7.2 Verify the existing e2e test (device pairing enabled by default) still passes as a regression check
