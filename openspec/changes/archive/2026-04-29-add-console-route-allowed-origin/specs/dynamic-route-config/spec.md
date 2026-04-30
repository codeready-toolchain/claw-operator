## ADDED Requirements

### Requirement: Console route is named claw-console
The OpenShift Route for the console SHALL be named `claw-console`.

#### Scenario: Console route name is fixed
- **WHEN** reconciling a Claw instance on OpenShift
- **THEN** the console Route is named `claw-console`
- **THEN** the console Route name is independent of the Claw instance name

#### Scenario: Console route coexists with gateway route
- **WHEN** reconciling a Claw instance named `instance` on OpenShift
- **THEN** two Routes are created: `claw` (gateway) and `claw-console`
- **THEN** both Routes have unique hostnames
- **THEN** both Routes are accessible independently

### Requirement: Console route hostname is derived from route name
The console Route hostname SHALL follow the same pattern as the gateway route, using the route name and cluster routing domain.

#### Scenario: Console route hostname
- **WHEN** the console Route named `claw-console` is deployed on OpenShift
- **THEN** the Route hostname follows the pattern `claw-console-{namespace}.{cluster-domain}`
- **THEN** the hostname is unique within the cluster

### Requirement: Console route targets console service
The console Route SHALL route traffic to the console service.

#### Scenario: Console route targets correct service
- **WHEN** reconciling the console Route
- **THEN** the Route's `spec.to.name` field is set to `claw-console`
- **THEN** traffic to the console Route is forwarded to the console service
