## Why

The device-pairing service account only needs `create` and `get` permissions on `ClawDevicePairingRequest` CRs within its own namespace. Using a `ClusterRole` grants a cluster-wide definition that conflicts with multi-tenancy: when multiple namespaces each deploy their own Claw instance, the identically-named `ClusterRole` resources collide, and cleanup of one instance can remove permissions needed by another. A namespace-scoped `Role` eliminates these conflicts.

## What Changes

- Replace the `ClusterRole` manifest (`internal/assets/manifests/claw-device-pairing/clusterrole.yaml`) with a namespace-scoped `Role` (rename file to `role.yaml`)
- Update the `RoleBinding` manifest to reference `kind: Role` instead of `kind: ClusterRole` in its `roleRef`
- Update the Kustomize `kustomization.yaml` to reference `role.yaml` instead of `clusterrole.yaml`
- Update the controller code that builds unstructured objects for cleanup (`newDevicePairingClusterRoleUnstructured` → namespace-scoped Role)
- Remove the `isClusterScoped` function and its usage in the owner-reference loop (no more cluster-scoped resources in device-pairing)
- Update the RBAC kubebuilder marker to remove `clusterroles` from the operator's own permissions (if no other code path needs it)
- Update `ClusterRoleKind` constant usage — remove if no longer needed

## Capabilities

### New Capabilities
- `device-pairing-role`: Namespace-scoped RBAC for the device-pairing service account, replacing the cluster-scoped ClusterRole

### Modified Capabilities

## Impact

- **Manifests**: `internal/assets/manifests/claw-device-pairing/` — `clusterrole.yaml` replaced by `role.yaml`, `rolebinding.yaml` updated, `kustomization.yaml` updated
- **Controller**: `internal/controller/claw_resource_controller.go` — cleanup function, unstructured builder, `isClusterScoped` logic, RBAC markers
- **RBAC**: The operator itself may no longer need `clusterroles` verb permissions (verify no other code paths use it)
- **Multi-tenancy**: Enables safe deployment of multiple Claw instances across namespaces without RBAC resource name collisions
