## MODIFIED Requirements

### Requirement: Controller builds kustomized objects from embedded manifests
The system SHALL build kustomized objects from three embedded manifest directories: `claw`, `claw-proxy`, and `claw-device-pairing`. The `buildKustomizedObjects()` function SHALL load all three groups, perform `CLAW_INSTANCE_NAME` template replacement, build each via Kustomize, and merge the resulting objects.

#### Scenario: Device pairing manifests are loaded
- **WHEN** `buildKustomizedObjects()` is called
- **THEN** it SHALL load manifest files from `manifests/claw-device-pairing/` alongside `manifests/claw/` and `manifests/claw-proxy/`

#### Scenario: CLAW_INSTANCE_NAME replacement applies to device-pairing manifests
- **WHEN** the Claw CR is named "instance"
- **THEN** all `CLAW_INSTANCE_NAME` occurrences in device-pairing manifests SHALL be replaced with "instance"

#### Scenario: All three component groups are merged
- **WHEN** `buildKustomizedObjects()` completes
- **THEN** the returned objects SHALL include resources from all three directories: claw, claw-proxy, and claw-device-pairing

### Requirement: Route host is injected into device-pairing Route
The controller SHALL inject the resolved Claw Route host into the device-pairing Route's `.spec.host` field during reconciliation, using the same host obtained from `getRouteURL()`.

#### Scenario: Device-pairing Route host injection
- **WHEN** the Claw Route host is resolved to `claw.example.com`
- **THEN** the controller SHALL replace `OPENCLAW_ROUTE_HOST` in the device-pairing Route's `.spec.host` with `claw.example.com`

#### Scenario: Device-pairing Route applied in Phase 2
- **WHEN** `applyRouteOnly()` filters for Route-kind objects
- **THEN** both the Claw Route and device-pairing Route SHALL be included and applied

### Requirement: Device-pairing resources applied in Phase 3
The device-pairing non-Route resources (ServiceAccount, Deployment, Service) SHALL be applied in Phase 3 alongside other remaining resources, after the Route has been applied in Phase 2.

#### Scenario: ServiceAccount applied with remaining resources
- **WHEN** Phase 3 applies remaining resources
- **THEN** the device-pairing ServiceAccount SHALL be included in the apply batch

#### Scenario: Deployment applied with remaining resources
- **WHEN** Phase 3 applies remaining resources
- **THEN** the device-pairing Deployment SHALL be included in the apply batch

#### Scenario: Service applied with remaining resources
- **WHEN** Phase 3 applies remaining resources
- **THEN** the device-pairing Service SHALL be included in the apply batch
