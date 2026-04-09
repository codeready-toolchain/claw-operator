## ADDED Requirements

### Requirement: URL includes gateway token fragment
The `URL` field MUST include a URL fragment containing the gateway authentication token in the format `#token=<gateway-token-value>`.

#### Scenario: Token fragment appended
- **WHEN** the `URL` field is populated with the Route URL
- **THEN** the URL MUST include a fragment in the format `#token=<value>` where `<value>` is the gateway token from the `openclaw-secrets` Secret

#### Scenario: Token retrieved from Secret
- **WHEN** constructing the URL with token fragment
- **THEN** the token value MUST be read from the `openclaw-secrets` Secret's `OPENCLAW_GATEWAY_TOKEN` data key

#### Scenario: Token Base64 decoded
- **WHEN** reading the token from the Secret data
- **THEN** the token value MUST be Base64-decoded before appending to the URL fragment

#### Scenario: Secret read failure
- **WHEN** the `openclaw-secrets` Secret cannot be read during status update
- **THEN** the `URL` field MUST be populated with the Route URL without the token fragment

## MODIFIED Requirements

### Requirement: URL includes HTTPS scheme
The `URL` field MUST include the `https://` scheme prefix and MAY include a URL fragment with the gateway token.

#### Scenario: URL format validation
- **WHEN** the `URL` field is populated
- **THEN** it MUST start with `https://`

#### Scenario: URL format with token
- **WHEN** the `URL` field is populated and token is available
- **THEN** it MUST follow the format `https://<route-host>#token=<gateway-token>`
