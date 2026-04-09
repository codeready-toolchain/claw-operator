## ADDED Requirements

### Requirement: URL includes gateway token fragment when available
The `URL` field MUST include a URL fragment containing the gateway authentication token in the exact format `#token=<gateway-token-value>` when the token is successfully retrieved. When the token is unavailable or Secret read fails, the `URL` field MUST NOT include the token fragment.

#### Scenario: Token fragment appended when token available
- **WHEN** the `URL` field is populated with the Route URL and the gateway token is successfully retrieved
- **THEN** the URL MUST include a fragment in the exact format `#token=<value>` where `<value>` is the gateway token from the `openclaw-secrets` Secret

#### Scenario: Token retrieved from Secret
- **WHEN** constructing the URL with token fragment
- **THEN** the token value MUST be read from the `openclaw-secrets` Secret's `OPENCLAW_GATEWAY_TOKEN` data key

#### Scenario: Token URL-encoded for fragment safety
- **WHEN** appending the token to the URL fragment
- **THEN** the token value MUST be percent-encoded (URL-encoded) to ensure special characters are safely represented in the fragment

#### Scenario: Token Base64 decoded
- **WHEN** reading the token from the Secret data
- **THEN** the token value MUST be Base64-decoded before URL-encoding and appending to the URL fragment

#### Scenario: Secret read failure - URL without fragment
- **WHEN** the `openclaw-secrets` Secret cannot be read during status update
- **THEN** the `URL` field MUST be populated with the Route URL without the token fragment (no `#token=` portion)

#### Scenario: Secret read failure - status condition
- **WHEN** the `openclaw-secrets` Secret cannot be read during status update
- **THEN** the controller SHOULD log the error but MUST NOT fail the reconciliation
- **AND** the `Available` condition SHOULD reflect the overall instance status based on deployment readiness, not Secret availability

## MODIFIED Requirements

### Requirement: URL includes HTTPS scheme
The `URL` field MUST include the `https://` scheme prefix. When the gateway token is successfully retrieved, the URL MUST include the token fragment; otherwise, the URL MUST NOT include the token fragment.

#### Scenario: URL format validation
- **WHEN** the `URL` field is populated
- **THEN** it MUST start with `https://`

#### Scenario: URL format with token (token available)
- **WHEN** the `URL` field is populated and the gateway token is successfully retrieved
- **THEN** it MUST follow the exact format `https://<route-host>#token=<gateway-token>`

#### Scenario: URL format without token (token unavailable)
- **WHEN** the `URL` field is populated but the gateway token cannot be retrieved
- **THEN** it MUST follow the format `https://<route-host>` without any fragment
