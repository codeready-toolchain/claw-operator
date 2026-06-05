## Context

The operator currently manages an optional device-pairing web application deployment (Deployment, Service, ServiceAccount, Role, RoleBinding, Route) controlled by `spec.auth.disableDevicePairing`. This application is outdated and no longer needed. The `ClawDevicePairingRequest` CRD and its controller remain — only the web application and its supporting resources are being removed.

The device-pairing deployment logic is spread across several files: manifest loading in the resource controller, route host injection, cleanup functions, status URL building, idle handling, auth helpers, and a dedicated test file.

## Goals / Non-Goals

**Goals:**
- Remove all device-pairing deployment manifests and controller logic
- Remove `DevicePairingURL` from `ClawStatus`
- Remove `ConditionTypeDevicePairingConfigured` condition constant
- Ensure existing device-pairing resources are cleaned up on upgrade (next reconcile)
- Keep `ClawDevicePairingRequest` CRD and its controller fully intact
- Keep `DisableDevicePairing` in `AuthSpec` and `shouldDisableDevicePairing()` — still used by the `ClawDevicePairingRequest` controller

**Non-Goals:**
- Removing the `ClawDevicePairingRequest` CRD, types, or controller
- Modifying the device-pairing request approval flow
- Adding a migration tool for existing clusters (the operator's reconcile loop handles cleanup naturally)

## Decisions

### 1. Cleanup strategy for existing clusters

**Decision**: Keep the `cleanupDevicePairingResources()` function (or inline equivalent) so the operator deletes any existing device-pairing resources on the first reconcile after upgrade. Remove it in a future release once all clusters have been reconciled.

**Rationale**: Clusters that had device pairing enabled will have orphaned Deployments, Services, etc. The operator should clean these up automatically rather than requiring manual intervention. Since the cleanup is idempotent (ignores NotFound), it's safe to run unconditionally.

**Alternative considered**: Relying on owner references for garbage collection. Rejected because owner references only trigger on CR deletion, not on operator upgrade. The resources would persist until the Claw CR is deleted.

### 2. Keep `DisableDevicePairing` in `AuthSpec`

**Decision**: Keep the field and `shouldDisableDevicePairing()` helper.

**Rationale**: The field is still used by the `ClawDevicePairingRequest` controller to gate whether device pairing requests are processed. Removing it would break that controller's logic.

### 3. Remove `ConditionTypeDevicePairingConfigured`

**Decision**: Remove the condition constant and stop setting it. The cleanup reconcile should also remove this condition from existing status if present.

**Rationale**: The condition has no meaning without the deployment. Leaving stale conditions in status would be confusing.

## Risks / Trade-offs

- **[Breaking API change]** Removing `DevicePairingURL` breaks any tooling that reads this field. → Mitigation: This field is operator-internal; external consumers should already handle optional fields. The `devicePairingURL` was only populated when device pairing was enabled (disabled by default).

- **[Orphaned resources on clusters that skip versions]** If a cluster skips from a pre-cleanup version to a post-cleanup version (where the cleanup code is also removed), resources may be orphaned. → Mitigation: Keep the cleanup code for at least one release cycle. Owner references will also eventually clean up if the Claw CR is deleted.
