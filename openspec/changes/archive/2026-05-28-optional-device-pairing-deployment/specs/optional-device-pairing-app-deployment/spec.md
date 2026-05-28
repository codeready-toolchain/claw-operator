## ADDED Requirements

### Requirement: Device pairing resources are skipped when disabled
The controller SHALL NOT build, render, or apply any `claw-device-pairing` Kustomize component resources (Deployment, Service, ServiceAccount, ClusterRole, RoleBinding, Route) when `shouldDisableDevicePairing()` returns `true`.

#### Scenario: Claw CR with disableDevicePairing true
- **WHEN** a Claw CR is created with `spec.auth.disableDevicePairing: true`
- **THEN** the controller SHALL NOT create a Deployment named `{instance}-device-pairing`
- **THEN** the controller SHALL NOT create a Service named `{instance}-device-pairing`
- **THEN** the controller SHALL NOT create a ServiceAccount named `{instance}-device-pairing`
- **THEN** the controller SHALL NOT create a Route named `{instance}-device-pairing`
- **THEN** the Claw instance SHALL reach `Ready=True` without the device-pairing deployment

#### Scenario: Claw CR with disableDevicePairing false
- **WHEN** a Claw CR is created with `spec.auth.disableDevicePairing: false`
- **THEN** the controller SHALL create all device-pairing resources as normal

#### Scenario: Claw CR with disableDevicePairing unset
- **WHEN** a Claw CR is created without the `spec.auth.disableDevicePairing` field
- **THEN** the controller SHALL create all device-pairing resources as normal (default behavior preserved)

### Requirement: Device pairing Route injection is skipped when disabled
The controller SHALL NOT call `injectRouteHostIntoDevicePairingRoute()` or attempt to apply the device-pairing Route when device pairing is disabled.

#### Scenario: No Route injection when disabled
- **WHEN** `shouldDisableDevicePairing()` returns `true` and the Claw Route host has been resolved
- **THEN** the controller SHALL skip the `injectRouteHostIntoDevicePairingRoute()` call
- **THEN** the controller SHALL skip the `applyRouteByName()` call for the device-pairing Route

### Requirement: Previously deployed device-pairing resources are cleaned up
When device pairing transitions from enabled to disabled, the controller SHALL delete any previously-deployed device-pairing resources to avoid leaving orphaned resources in the cluster.

#### Scenario: Cleanup on disable toggle
- **WHEN** a Claw CR previously had device pairing enabled (resources exist) and `spec.auth.disableDevicePairing` is changed to `true`
- **THEN** the controller SHALL delete the device-pairing Deployment, Service, ServiceAccount, RoleBinding, and Route
- **THEN** NotFound errors during deletion SHALL be silently ignored (idempotent)

#### Scenario: Cleanup is idempotent
- **WHEN** `shouldDisableDevicePairing()` returns `true` and no device-pairing resources exist
- **THEN** the cleanup SHALL complete without errors

### Requirement: Idle scaling skips device-pairing when disabled
The `handleIdle()` function SHALL NOT attempt to scale the device-pairing Deployment to zero when device pairing is disabled.

#### Scenario: Idle with device pairing disabled
- **WHEN** `spec.idle` is `true` and `shouldDisableDevicePairing()` returns `true`
- **THEN** the controller SHALL only scale the claw and proxy Deployments to zero
- **THEN** the controller SHALL NOT attempt to scale `{instance}-device-pairing`

### Requirement: E2E test covers disabled device pairing
The e2e test suite SHALL include a test case verifying that a Claw CR with `spec.auth.disableDevicePairing: true` does not create device-pairing resources and reaches the Ready state.

#### Scenario: E2E disabled device pairing
- **WHEN** a Claw CR is applied with `spec.auth.disableDevicePairing: true` and valid credentials
- **THEN** the claw and proxy Deployments SHALL be created
- **THEN** no device-pairing Deployment SHALL exist
- **THEN** no device-pairing Service SHALL exist
- **THEN** no device-pairing ServiceAccount SHALL exist
- **THEN** the Claw instance SHALL eventually reach `Ready=True`

#### Scenario: E2E enabled device pairing (existing behavior preserved)
- **WHEN** a Claw CR is applied without `spec.auth.disableDevicePairing` and valid credentials
- **THEN** the device-pairing Deployment SHALL be created alongside the claw and proxy Deployments
