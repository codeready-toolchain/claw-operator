## ADDED Requirements

### Requirement: APIKey field is mandatory in OpenClawSpec
The OpenClawSpec MUST include a mandatory `APIKey` field of type `string` that contains the LLM provider API key.

#### Scenario: User creates OpenClaw CR without APIKey field
- **WHEN** user attempts to create an OpenClaw CR without specifying the APIKey field
- **THEN** the API server MUST reject the request with a validation error indicating APIKey is required

#### Scenario: User creates OpenClaw CR with empty APIKey
- **WHEN** user creates an OpenClaw CR with an empty APIKey field
- **THEN** the API server MUST reject the request with a validation error indicating APIKey cannot be empty

#### Scenario: User creates OpenClaw CR with valid APIKey
- **WHEN** user creates an OpenClaw CR with a non-empty APIKey field
- **THEN** the CR MUST be accepted and created successfully

### Requirement: Controller injects API key into proxy Secret
The controller MUST read the API key from the OpenClawSpec and inject it into an operator-managed Secret named `openclaw-proxy-secrets` under the data key `GEMINI_API_KEY`.

#### Scenario: Controller reconciles OpenClaw CR when Secret does not exist
- **WHEN** the controller reconciles an OpenClaw CR with a valid APIKey and the `openclaw-proxy-secrets` Secret does not exist
- **THEN** the controller MUST create the Secret with the API key value under the `GEMINI_API_KEY` data entry

#### Scenario: Controller reconciles OpenClaw CR when Secret already exists
- **WHEN** the controller reconciles an OpenClaw CR with a valid APIKey and the `openclaw-proxy-secrets` Secret already exists
- **THEN** the controller MUST update the Secret's `GEMINI_API_KEY` data entry with the current API key value from the CR

#### Scenario: User updates APIKey in OpenClaw CR
- **WHEN** the user updates the APIKey field in the OpenClaw CR
- **THEN** the controller MUST detect the change and update the `GEMINI_API_KEY` entry in the `openclaw-proxy-secrets` Secret with the new value

#### Scenario: Secret is deleted manually
- **WHEN** the `openclaw-proxy-secrets` Secret is deleted manually
- **THEN** the controller MUST recreate the Secret with the API key from the OpenClaw CR on the next reconciliation

### Requirement: API key is securely mounted in proxy
The proxy Deployment MUST mount the `openclaw-proxy-secrets` Secret and use the `GEMINI_API_KEY` data entry for upstream LLM provider authentication.

#### Scenario: Proxy container starts with credentials
- **WHEN** the proxy Deployment is created or updated by the controller
- **THEN** the proxy Pod MUST mount the `openclaw-proxy-secrets` Secret as a volume

#### Scenario: Proxy uses API key for upstream requests
- **WHEN** the proxy forwards a request to an LLM provider
- **THEN** the proxy MUST include the API key from the `GEMINI_API_KEY` entry in the mounted Secret in the authentication header

### Requirement: CRD schema includes APIKey validation
The generated CRD YAML MUST include OpenAPI validation schema that marks APIKey as a required string field with minimum length validation.

#### Scenario: CRD is installed in cluster
- **WHEN** the CRD is installed using `kubectl apply`
- **THEN** the CRD schema MUST show APIKey as a required field with type string

#### Scenario: User runs kubectl explain on OpenClaw
- **WHEN** user runs `kubectl explain openclaw.spec.apiKey`
- **THEN** the output MUST show the field as required with type string
