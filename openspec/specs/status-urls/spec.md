## ADDED Requirements

### Requirement: GatewayURL status field
The `ClawStatus` struct SHALL include a `GatewayURL` field (JSON: `gatewayURL`) that contains the same value as the existing `URL` field. The field SHALL be populated when all managed deployments are ready and cleared when they are not.

#### Scenario: GatewayURL populated when ready with token auth
- **WHEN** all managed deployments are available AND auth mode is token
- **THEN** `status.gatewayURL` SHALL equal `https://<route-host>#token=<encoded-token>`

#### Scenario: GatewayURL populated when ready with password auth
- **WHEN** all managed deployments are available AND auth mode is password
- **THEN** `status.gatewayURL` SHALL equal `https://<route-host>` (no token fragment)

#### Scenario: GatewayURL cleared when not ready
- **WHEN** any managed deployment is not available
- **THEN** `status.gatewayURL` SHALL be empty

### Requirement: DevicePairingURL status field
The `ClawStatus` struct SHALL include a `DevicePairingURL` field (JSON: `devicePairingURL`) that contains the device pairing application URL. The URL SHALL be the same as the gateway URL but with `/integration/device-pairing/` inserted as a path before the token fragment.

#### Scenario: DevicePairingURL populated when ready with token auth and device pairing enabled
- **WHEN** all managed deployments are available AND auth mode is token AND device pairing is not disabled
- **THEN** `status.devicePairingURL` SHALL equal `https://<route-host>/integration/device-pairing/#token=<encoded-token>`

#### Scenario: DevicePairingURL populated when ready with password auth and device pairing enabled
- **WHEN** all managed deployments are available AND auth mode is password AND device pairing is not disabled
- **THEN** `status.devicePairingURL` SHALL equal `https://<route-host>/integration/device-pairing/`

#### Scenario: DevicePairingURL empty when device pairing is disabled
- **WHEN** `spec.auth.disableDevicePairing` is true or unset (disabled by default)
- **THEN** `status.devicePairingURL` SHALL be empty regardless of deployment readiness

#### Scenario: DevicePairingURL cleared when not ready
- **WHEN** any managed deployment is not available AND device pairing is enabled
- **THEN** `status.devicePairingURL` SHALL be empty

### Requirement: Deprecated URL field preserved
The existing `URL` field in `ClawStatus` SHALL continue to be populated with the same value as `GatewayURL`. The Go struct field SHALL carry a deprecation comment indicating it will be removed in a future version.

#### Scenario: URL and GatewayURL are identical
- **WHEN** `status.gatewayURL` is populated
- **THEN** `status.url` SHALL have the same value as `status.gatewayURL`

#### Scenario: URL cleared when GatewayURL is cleared
- **WHEN** `status.gatewayURL` is empty
- **THEN** `status.url` SHALL also be empty
