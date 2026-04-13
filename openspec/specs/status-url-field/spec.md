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
The `URL` field MUST include the `https://` scheme prefix and MAY include a URL fragment with the gateway token.

#### Scenario: URL format validation
- **WHEN** the `URL` field is populated
- **THEN** it MUST start with `https://`

#### Scenario: URL format with token
- **WHEN** the `URL` field is populated and token is available
- **THEN** it MUST follow the format `https://<route-host>#token=<gateway-token>`

### Requirement: URL derived from Route
The `URL` field SHALL be derived from the OpenClaw Route resource's host specification.

#### Scenario: Route exists
- **WHEN** the Route resource named `openclaw` exists in the same namespace
- **THEN** the `URL` field MUST be set to `https://` + Route.Spec.Host

#### Scenario: Route does not exist
- **WHEN** the Route resource does not exist (e.g., on non-OpenShift clusters)
- **THEN** the `URL` field MUST remain empty

### Requirement: URL includes gateway token fragment
The `URL` field MUST include a URL fragment containing the gateway authentication token in the format `#token=<gateway-token-value>`.

#### Scenario: Token fragment appended
- **WHEN** the `URL` field is populated with the Route URL
- **THEN** the URL MUST include a fragment in the format `#token=<value>` where `<value>` is the gateway token from the `openclaw-gateway-token` Secret

#### Scenario: Token retrieved from Secret
- **WHEN** constructing the URL with token fragment
- **THEN** the token value MUST be read from the `openclaw-gateway-token` Secret's `token` data key

#### Scenario: Token Base64 decoded
- **WHEN** reading the token from the Secret data
- **THEN** the token value MUST be Base64-decoded before appending to the URL fragment

#### Scenario: Secret read failure
- **WHEN** the `openclaw-gateway-token` Secret cannot be read during status update
- **THEN** the `URL` field MUST be populated with the Route URL without the token fragment
