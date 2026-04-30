## Purpose

Define how OpenShift Routes are configured for Claw instances, including naming conventions, hostname patterns, and service targeting.
## Requirements
### Requirement: Route name uses instance name
The OpenShift Route SHALL be named using the Claw instance name directly.

#### Scenario: Route name for instance 'my-openclaw'
- **WHEN** reconciling a Claw instance named 'my-openclaw' on OpenShift
- **THEN** the Route is named 'my-openclaw'

#### Scenario: Route name for instance 'production'
- **WHEN** reconciling a Claw instance named 'production' on OpenShift
- **THEN** the Route is named 'production'

#### Scenario: Multiple instances have separate routes
- **WHEN** two Claw instances 'claw-a' and 'claw-b' exist in the same namespace on OpenShift
- **THEN** Route 'claw-a' is created for instance 'claw-a'
- **THEN** Route 'claw-b' is created for instance 'claw-b'
- **THEN** routes have different hostnames and do not conflict

### Requirement: Route hostname reflects instance name
The Route hostname SHALL be derived from the Route name and cluster routing domain.

#### Scenario: Route hostname includes instance name
- **WHEN** a Claw instance named 'my-openclaw' is deployed on OpenShift
- **THEN** the Route hostname follows the pattern 'my-openclaw-{namespace}.{cluster-domain}'
- **THEN** the hostname uniquely identifies the instance within the cluster

### Requirement: Route targets instance-specific service
The Route SHALL route traffic to the service named after the Claw instance.

#### Scenario: Route targets correct service
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the Route's `spec.to.name` field is set to 'my-openclaw'
- **THEN** traffic to the Route is forwarded to the correct instance's gateway service

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

