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

### Requirement: Deprecated URL field preserved
The existing `URL` field in `ClawStatus` SHALL continue to be populated with the same value as `GatewayURL`. The Go struct field SHALL carry a deprecation comment indicating it will be removed in a future version.

#### Scenario: URL and GatewayURL are identical
- **WHEN** `status.gatewayURL` is populated
- **THEN** `status.url` SHALL have the same value as `status.gatewayURL`

#### Scenario: URL cleared when GatewayURL is cleared
- **WHEN** `status.gatewayURL` is empty
- **THEN** `status.url` SHALL also be empty
