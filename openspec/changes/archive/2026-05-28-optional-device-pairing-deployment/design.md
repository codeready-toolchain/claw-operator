## Context

The claw-operator currently always deploys the device-pairing application (Deployment, Service, ServiceAccount, ClusterRole, RoleBinding, Route) as part of the Kustomize build in `buildKustomizedObjects()`. The `spec.auth.disableDevicePairing` field already exists and controls whether the gateway's `dangerouslyDisableDeviceAuth` config flag is set, but it does not affect whether the device-pairing app itself is deployed. When device pairing is disabled, the app runs uselessly.

The controller's three-phase reconciliation builds all Kustomize components (claw, claw-proxy, claw-device-pairing) into a single `[]*unstructured.Unstructured` slice. Device-pairing resources are woven into several post-build steps: route host injection, status condition checking, idle scaling, and cleanup.

## Goals / Non-Goals

**Goals:**

- Skip building and applying device-pairing Kustomize component when `shouldDisableDevicePairing()` returns `true`
- Clean up previously-deployed device-pairing resources when a user toggles `disableDevicePairing` from `false` to `true`
- Adapt status conditions so the `Ready` condition does not block on a deployment that was intentionally not created
- Adapt idle handling to skip the device-pairing deployment when it doesn't exist
- Add e2e test coverage for the disabled-device-pairing scenario

**Non-Goals:**

- Changing the behavior of the `ClawDevicePairingRequest` controller — it already handles missing pods gracefully
- Adding a finalizer or additional CRD fields
- Changing the default behavior (device pairing enabled by default)

## Decisions

### 1. Conditionally build Kustomize component

**Decision**: In `buildKustomizedObjects()`, skip adding device-pairing manifests to the in-memory filesystem and skip building the `claw-device-pairing` Kustomize component when device pairing is disabled.

**Rationale**: This is the earliest point where resources can be excluded. By not including them in the rendered objects slice, all downstream code (route injection, apply, status) naturally has fewer objects to process. The alternative — building them and filtering later — would require more changes and is harder to reason about.

**Approach**: Pass the `*Claw` instance (or a `bool`) to `buildKustomizedObjects()` so it can conditionally skip the device-pairing component. The existing `shouldDisableDevicePairing()` function determines the flag.

### 2. Skip device-pairing Route injection when disabled

**Decision**: Guard the `injectRouteHostIntoDevicePairingRoute()` call and the subsequent `applyRouteByName()` call with the same `shouldDisableDevicePairing()` check.

**Rationale**: If device-pairing objects are not in the slice, `injectRouteHostIntoDevicePairingRoute()` will return an error ("Route not found in rendered manifests"). Rather than making that function tolerate missing routes (which would hide real bugs when pairing is enabled), we skip the call entirely.

### 3. Conditional status condition and deployment readiness

**Decision**: In `checkDeploymentsReady()` and `updateStatus()`, conditionally exclude the device-pairing deployment from the readiness check and skip setting the `DevicePairingConfigured` condition.

**Rationale**: If the deployment is intentionally not created, checking its readiness would permanently block the `Ready` condition. The `DevicePairingConfigured` condition is meaningless when pairing is disabled — setting it would be confusing. We'll pass the Claw instance (or relevant auth info) to these functions so they can adjust.

When device pairing transitions from enabled to disabled, we should also remove the `DevicePairingConfigured` condition from the status to keep conditions clean.

### 4. Cleanup of previously-deployed resources

**Decision**: When `shouldDisableDevicePairing()` is `true`, delete any existing device-pairing resources (Deployment, Service, ServiceAccount, RoleBinding, Route) that were created by a previous reconcile where pairing was enabled.

**Rationale**: Leaving orphaned resources wastes cluster resources and could cause confusion. Since all device-pairing resources have owner references pointing to the Claw CR, we could rely on garbage collection if we deleted the owner ref — but explicitly deleting is cleaner and gives immediate feedback. We'll delete by name using the existing naming helpers (`getDevicePairingDeploymentName`, etc.).

**Approach**: Add a `cleanupDevicePairingResources()` function that attempts to delete each device-pairing resource. NotFound errors are ignored (idempotent). This runs early in the reconcile loop when `shouldDisableDevicePairing()` is `true`.

### 5. Conditional idle scaling

**Decision**: In `handleIdle()`, skip the device-pairing deployment when `shouldDisableDevicePairing()` is `true`.

**Rationale**: Attempting to scale a non-existent deployment would cause an error. The idle handler lists deployment names to scale; we conditionally exclude the device-pairing deployment name.

### 6. E2E test approach

**Decision**: Add e2e test cases that create a Claw CR with `spec.auth.disableDevicePairing: true` and verify that device-pairing resources (Deployment, Service, ServiceAccount) are not created, while verifying the Claw instance reaches Ready state.

**Rationale**: E2e tests validate the full reconciliation loop including Kustomize rendering, server-side apply, and status updates — all of which are affected by this change.

## Risks / Trade-offs

- **[Risk] ClusterRole/ClusterRoleBinding not namespaced** → These are cluster-scoped resources with owner references. Deletion requires special handling (no namespace). The existing cleanup in `suite_test.go` already handles this pattern — follow the same approach.
- **[Risk] Race between disable toggle and in-flight pairing requests** → If a user disables pairing while a `ClawDevicePairingRequest` is being processed, the pairing request controller may try to exec into a pod that no longer exists. Mitigation: the pairing request controller already handles pod-not-found gracefully with condition updates. This is acceptable.
- **[Trade-off] Explicit deletion vs. relying on GC** → We choose explicit deletion for immediate cleanup rather than waiting for garbage collection. This adds code but provides predictable behavior.
