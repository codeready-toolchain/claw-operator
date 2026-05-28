## MODIFIED Requirements

### Requirement: Ready condition indicates overall readiness
The controller SHALL maintain a Ready condition type to indicate whether the Claw instance is ready for use. When device pairing is disabled, the Ready condition SHALL NOT depend on the device-pairing Deployment.

#### Scenario: Ready condition set to False during provisioning
- **WHEN** a Claw instance named 'instance' is created
- **THEN** the controller SHALL set Ready condition with status=False, reason=Provisioning, message describing deployment progress

#### Scenario: Ready condition set to True when ready (device pairing enabled)
- **WHEN** the claw, proxy, and device-pairing Deployments all have Available=True status
- **THEN** the controller SHALL set Ready condition with status=True, reason=Ready

#### Scenario: Ready condition set to True when ready (device pairing disabled)
- **WHEN** `shouldDisableDevicePairing()` returns `true` and the claw and proxy Deployments have Available=True status
- **THEN** the controller SHALL set Ready condition with status=True, reason=Ready
- **THEN** the controller SHALL NOT check the device-pairing Deployment status

#### Scenario: DevicePairingConfigured condition omitted when disabled
- **WHEN** `shouldDisableDevicePairing()` returns `true`
- **THEN** the controller SHALL NOT set the `DevicePairingConfigured` condition
- **THEN** if a `DevicePairingConfigured` condition previously existed, the controller SHALL remove it from the status conditions

### Requirement: Controller checks Deployment status conditions
The controller SHALL read the Available condition from managed Deployments to determine readiness. The set of managed Deployments SHALL vary based on whether device pairing is enabled.

#### Scenario: Deployments checked when device pairing enabled
- **WHEN** `shouldDisableDevicePairing()` returns `false`
- **THEN** the controller SHALL check readiness of claw, proxy, and device-pairing Deployments

#### Scenario: Deployments checked when device pairing disabled
- **WHEN** `shouldDisableDevicePairing()` returns `true`
- **THEN** the controller SHALL check readiness of only the claw and proxy Deployments
- **THEN** the controller SHALL NOT attempt to fetch the device-pairing Deployment status
