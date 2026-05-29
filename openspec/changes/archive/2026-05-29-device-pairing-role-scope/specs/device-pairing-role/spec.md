## ADDED Requirements

### Requirement: Device-pairing RBAC uses namespace-scoped Role
The device-pairing component SHALL use a `Role` (not `ClusterRole`) to grant its service account `create` and `get` permissions on `clawdevicepairingrequests` resources in the `claw.sandbox.redhat.com` API group. The Role SHALL be created in the same namespace as the Claw instance.

#### Scenario: Role manifest is namespace-scoped
- **WHEN** the operator reconciles a Claw instance with device pairing enabled
- **THEN** a `Role` named `{instance}-device-pairing` SHALL be created in the Claw instance's namespace with rules granting `create` and `get` on `clawdevicepairingrequests`

#### Scenario: RoleBinding references a Role
- **WHEN** the operator reconciles a Claw instance with device pairing enabled
- **THEN** the `RoleBinding` named `{instance}-device-pairing` SHALL have `roleRef.kind` set to `Role` (not `ClusterRole`)

#### Scenario: Role has owner reference
- **WHEN** the operator creates the device-pairing Role
- **THEN** the Role SHALL have an owner reference to the Claw CR, enabling garbage collection when the Claw CR is deleted

### Requirement: Multi-tenancy isolation for device-pairing RBAC
Multiple Claw instances in different namespaces SHALL have independent device-pairing RBAC resources with no naming collisions or permission leakage.

#### Scenario: Two instances in different namespaces
- **WHEN** two Claw instances with the same name exist in different namespaces
- **THEN** each SHALL have its own independent `Role` and `RoleBinding` in its respective namespace, with no resource conflicts

#### Scenario: Deleting one instance does not affect another
- **WHEN** a Claw instance is deleted from one namespace
- **THEN** the device-pairing Role and RoleBinding in that namespace SHALL be removed without affecting device-pairing RBAC in other namespaces

### Requirement: Cleanup removes namespace-scoped Role
When device pairing is disabled or the Claw instance is deleted, the cleanup function SHALL delete the namespace-scoped `Role` (not a `ClusterRole`).

#### Scenario: Device pairing toggled off
- **WHEN** `spec.auth.disableDevicePairing` is set to `true` on an existing Claw instance
- **THEN** the `Role` named `{instance}-device-pairing` in the instance's namespace SHALL be deleted

#### Scenario: Cleanup handles missing Role gracefully
- **WHEN** the cleanup function runs and the Role does not exist
- **THEN** the cleanup SHALL succeed without error (NotFound errors are ignored)

### Requirement: Legacy ClusterRole is cleaned up on upgrade
On every reconcile, the controller SHALL attempt to delete the legacy ClusterRole named `{instance}-device-pairing` that was created by previous versions of the operator. This runs unconditionally regardless of whether device pairing is enabled or disabled.

#### Scenario: Legacy ClusterRole exists from a previous operator version
- **WHEN** the operator reconciles a Claw instance and a ClusterRole named `{instance}-device-pairing` exists
- **THEN** the ClusterRole SHALL be deleted

#### Scenario: Legacy ClusterRole does not exist
- **WHEN** the operator reconciles a Claw instance and no ClusterRole named `{instance}-device-pairing` exists
- **THEN** the reconcile SHALL proceed without error (NotFound errors are ignored)
