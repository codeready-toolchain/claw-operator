## Context

The device-pairing component deploys a service account, a ClusterRole, and a RoleBinding per Claw instance. The ClusterRole grants `create` and `get` on `ClawDevicePairingRequest` CRs. Because ClusterRole is cluster-scoped, names must be globally unique. The current naming convention (`{instance}-device-pairing`) works for single-namespace deployments but collides when multiple namespaces deploy instances with the same name.

Additionally, the ClusterRole cannot carry an owner reference to the namespace-scoped Claw CR, so the controller skips setting owner references on it via the `isClusterScoped` helper. This means ClusterRoles are not garbage-collected when the Claw CR is deleted — they rely on explicit cleanup in `cleanupDevicePairingResources`.

## Goals / Non-Goals

**Goals:**
- Replace the ClusterRole with a namespace-scoped Role so each Claw instance's RBAC is fully contained in its namespace
- Enable owner references on all device-pairing RBAC resources for proper garbage collection
- Eliminate the `isClusterScoped` special-case logic from the controller
- Keep the same permissions: `create` and `get` on `clawdevicepairingrequests`

**Non-Goals:**
- Changing the device-pairing service account permissions themselves
- Modifying the operator's own RBAC beyond what's needed for managing Roles instead of ClusterRoles
- Addressing any other multi-tenancy concerns beyond RBAC scoping

## Decisions

### 1. Replace ClusterRole manifest with Role manifest

Rename `clusterrole.yaml` → `role.yaml` and change `kind: ClusterRole` → `kind: Role`. The rules stay identical. The RoleBinding already uses `kind: RoleBinding` (not ClusterRoleBinding), so it only needs `roleRef.kind` changed from `ClusterRole` to `Role`.

**Alternative**: Keep ClusterRole but add namespace-unique suffixes. Rejected — adds complexity without addressing the fundamental issue that cluster-scoped resources shouldn't be used for namespace-scoped permissions.

### 2. Remove `isClusterScoped` and `ClusterRoleKind` if unused elsewhere

With no ClusterRole in the device-pairing manifests, the `isClusterScoped` check in the owner-reference loop becomes unnecessary. The `ClusterRoleKind` constant and `newDevicePairingClusterRoleUnstructured` function should be replaced with Role equivalents.

**Note**: Verify that `ClusterRoleKind` and `isClusterScoped` are not used by any other code path before removing.

### 3. Update operator RBAC markers

The operator currently has a kubebuilder RBAC marker granting `clusterroles` permissions. If no other code path requires managing ClusterRoles, change this to `roles`. If both are needed, add `roles` alongside `clusterroles`.

### 4. Cleanup function uses namespace-scoped Role

`cleanupDevicePairingResources` currently builds an unstructured ClusterRole (without namespace) for deletion. This must become a Role with a namespace set, matching the pattern of all other resources in that function.

## Risks / Trade-offs

- **[Upgrade path]** Existing deployments have a ClusterRole that won't be cleaned up automatically when the operator deploys the new Role. → The controller calls `cleanupLegacyDevicePairingClusterRole` on every reconcile to delete the legacy ClusterRole (by its known name `{instance}-device-pairing`) if it still exists. This runs unconditionally — regardless of whether device pairing is enabled or disabled — so the migration happens automatically on the first reconcile after upgrade. The operator's RBAC includes `get` and `delete` on `clusterroles` to support this.
- **[RBAC marker change]** Removing `clusterroles` from the operator's RBAC markers means the operator loses permission to manage ClusterRoles. → Verify no other code path needs it. The operator's own ClusterRole (in `config/rbac/`) is separate from the device-pairing one.
