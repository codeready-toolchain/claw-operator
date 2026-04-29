## ADDED Requirements

### Requirement: Status URL derived from dynamic route name
The Claw status field `url` SHALL be populated with the HTTPS URL constructed from the dynamically named Route.

#### Scenario: URL constructed from instance name
- **WHEN** reconciling a Claw instance named 'my-openclaw' on OpenShift
- **THEN** the controller fetches the Route named 'my-openclaw'
- **THEN** status field `url` contains the Route's HTTPS URL (e.g., 'https://my-openclaw-namespace.apps.cluster.example.com')

#### Scenario: URL reflects correct instance route
- **WHEN** two Claw instances 'claw-a' and 'claw-b' exist in the same namespace
- **THEN** instance 'claw-a' status URL points to Route 'claw-a'
- **THEN** instance 'claw-b' status URL points to Route 'claw-b'
- **THEN** each instance has a unique URL for its gateway

#### Scenario: URL empty on vanilla Kubernetes
- **WHEN** reconciling a Claw instance on vanilla Kubernetes (no Route CRD)
- **THEN** status field `url` remains empty or contains localhost fallback
- **THEN** users access the instance via port-forward

### Requirement: Controller fetches route status by instance name
The controller SHALL fetch the Route resource using the Claw instance name to populate the status URL.

#### Scenario: Route lookup uses instance name
- **WHEN** updating status for Claw instance 'my-openclaw'
- **THEN** the controller calls `getRouteURL(ctx, instance)` which fetches Route 'my-openclaw'
- **THEN** Route `.status.ingress[0].host` is extracted and used to construct the HTTPS URL
