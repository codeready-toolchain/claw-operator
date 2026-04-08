## REMOVED Requirements

### Requirement: APIKey field is mandatory in OpenClawSpec
**Reason**: Replaced with Secret reference pattern for secure credential management  
**Migration**: Create a Secret with the API key and reference it using the new `geminiAPIKey` field

### Requirement: CRD schema includes APIKey validation
**Reason**: Field removed in favor of Secret reference structure  
**Migration**: Update CRD validation to require `geminiAPIKey` field with Secret reference structure

## ADDED Requirements

### Requirement: GeminiAPIKey field is mandatory in OpenClawSpec
The OpenClawSpec MUST include a mandatory `GeminiAPIKey` field of type `corev1.SecretKeySelector` that references a user-managed Secret containing the Gemini API key.

#### Scenario: User creates OpenClaw CR without GeminiAPIKey field
- **WHEN** user attempts to create an OpenClaw CR without specifying the GeminiAPIKey field
- **THEN** the API server MUST reject the request with a validation error indicating GeminiAPIKey is required

#### Scenario: User creates OpenClaw CR with valid Secret reference
- **WHEN** user creates an OpenClaw CR with a valid GeminiAPIKey reference (name and key specified)
- **THEN** the CR MUST be accepted and created successfully

#### Scenario: User creates OpenClaw CR with missing Secret name
- **WHEN** user creates an OpenClaw CR with GeminiAPIKey.Key specified but GeminiAPIKey.Name empty
- **THEN** the API server MUST reject the request with a validation error indicating Secret name is required

#### Scenario: User creates OpenClaw CR with missing Secret key
- **WHEN** user creates an OpenClaw CR with GeminiAPIKey.Name specified but GeminiAPIKey.Key empty
- **THEN** the API server MUST reject the request with a validation error indicating Secret key is required

### Requirement: CRD schema includes GeminiAPIKey validation
The generated CRD YAML MUST include OpenAPI validation schema that marks GeminiAPIKey as a required field with nested validation for name and key subfields.

#### Scenario: CRD is installed in cluster
- **WHEN** the CRD is installed using `kubectl apply`
- **THEN** the CRD schema MUST show GeminiAPIKey as a required field with type object containing name and key string fields

#### Scenario: User runs kubectl explain on OpenClaw
- **WHEN** user runs `kubectl explain openclaw.spec.geminiAPIKey`
- **THEN** the output MUST show the field as required with type object containing name and key fields

## MODIFIED Requirements

### Requirement: Controller injects API key into proxy Secret
The controller MUST read the API key from the Secret referenced in OpenClawSpec.GeminiAPIKey and inject it into an operator-managed Secret named `openclaw-proxy-secrets` under the data key `GEMINI_API_KEY`.

#### Scenario: Controller reconciles OpenClaw CR when referenced Secret exists
- **WHEN** the controller reconciles an OpenClaw CR with a valid GeminiAPIKey reference and the referenced Secret exists with the specified key
- **THEN** the controller MUST read the API key value from the referenced Secret and create/update the `openclaw-proxy-secrets` Secret with that value under the `GEMINI_API_KEY` data entry

#### Scenario: Controller reconciles OpenClaw CR when referenced Secret does not exist
- **WHEN** the controller reconciles an OpenClaw CR with a GeminiAPIKey reference and the referenced Secret does not exist
- **THEN** the controller MUST set the Available condition to False with Reason=SecretNotFound and Message indicating which Secret is missing

#### Scenario: Controller reconciles OpenClaw CR when referenced Secret key does not exist
- **WHEN** the controller reconciles an OpenClaw CR with a GeminiAPIKey reference and the Secret exists but does not contain the specified key
- **THEN** the controller MUST set the Available condition to False with Reason=SecretKeyNotFound and Message indicating which key is missing from which Secret

#### Scenario: User updates referenced Secret value
- **WHEN** the user updates the API key value in the referenced Secret
- **THEN** the controller MUST detect the change and update the `GEMINI_API_KEY` entry in the `openclaw-proxy-secrets` Secret with the new value

#### Scenario: Proxy Secret is deleted manually
- **WHEN** the `openclaw-proxy-secrets` Secret is deleted manually
- **THEN** the controller MUST recreate the Secret with the API key from the referenced Secret on the next reconciliation

#### Scenario: User changes GeminiAPIKey reference to different Secret
- **WHEN** the user updates the GeminiAPIKey field to reference a different Secret
- **THEN** the controller MUST detect the change, read the API key from the new Secret, and update the `openclaw-proxy-secrets` Secret accordingly

### Requirement: API key is securely mounted in proxy
The proxy Deployment MUST mount the `openclaw-proxy-secrets` Secret and use the `GEMINI_API_KEY` data entry for upstream LLM provider authentication.

#### Scenario: Proxy container starts with credentials
- **WHEN** the proxy Deployment is created or updated by the controller
- **THEN** the proxy Pod MUST mount the `openclaw-proxy-secrets` Secret and expose the `GEMINI_API_KEY` entry as an environment variable

#### Scenario: Proxy uses API key for upstream requests
- **WHEN** the proxy forwards a request to an LLM provider
- **THEN** the proxy MUST include the API key from the `GEMINI_API_KEY` environment variable in the authentication header

#### Scenario: Proxy Secret is updated with new key
- **WHEN** the `openclaw-proxy-secrets` Secret is updated with a new API key value
- **THEN** the proxy Pod MUST eventually use the new API key for subsequent requests (may require Pod restart depending on mount propagation)
