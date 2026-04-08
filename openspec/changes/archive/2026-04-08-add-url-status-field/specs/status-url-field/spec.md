## ADDED Requirements

### Requirement: URL field in status
The OpenClaw custom resource status MUST include a `URL` field that contains the full HTTPS URL for accessing the OpenClaw instance.

#### Scenario: URL field exists
- **WHEN** an OpenClaw resource is created
- **THEN** the status struct MUST have a `URL` field of type string

### Requirement: URL populated when deployments ready
The `URL` field SHALL be populated only when both the `openclaw` and `openclaw-proxy` Deployments report an Available condition with status True.

#### Scenario: Both deployments ready
- **WHEN** both `openclaw` and `openclaw-proxy` Deployments have Available condition status True
- **THEN** the `URL` field MUST be populated with the Route URL

#### Scenario: Deployments not ready
- **WHEN** either `openclaw` or `openclaw-proxy` Deployment has Available condition status not True
- **THEN** the `URL` field MUST be empty or unchanged from previous value

### Requirement: URL includes HTTPS scheme
The `URL` field MUST include the `https://` scheme prefix.

#### Scenario: URL format validation
- **WHEN** the `URL` field is populated
- **THEN** it MUST start with `https://`

### Requirement: URL derived from Route
The `URL` field SHALL be derived from the OpenClaw Route resource's host specification.

#### Scenario: Route exists
- **WHEN** the Route resource named `openclaw` exists in the same namespace
- **THEN** the `URL` field MUST be set to `https://` + Route.Spec.Host

#### Scenario: Route does not exist
- **WHEN** the Route resource does not exist (e.g., on non-OpenShift clusters)
- **THEN** the `URL` field MUST remain empty
