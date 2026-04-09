## REMOVED Requirements

### Requirement: APIKey field is mandatory in OpenClawSpec
**Reason**: Replaced with Secret reference pattern for secure credential management  
**Migration**: Create a Secret with the API key and reference it using the new `geminiAPIKey` field

### Requirement: CRD schema includes APIKey validation
**Reason**: Field removed in favor of Secret reference structure  
**Migration**: Update CRD validation to require `geminiAPIKey` field with Secret reference structure

## ADDED Requirements

### Requirement: GeminiAPIKey field is mandatory in OpenClawSpec
The OpenClawSpec MUST include a mandatory `GeminiAPIKey` field of type `SecretRef` (custom type with `name` and `key` string fields) that references a user-managed Secret containing the Gemini API key.

#### Scenario: User creates OpenClaw CR without GeminiAPIKey field
- **WHEN** user attempts to create an OpenClaw CR without specifying the GeminiAPIKey field
- **THEN** the API server MUST reject the request with a validation error indicating GeminiAPIKey is required

#### Scenario: User creates OpenClaw CR with valid Secret reference
- **WHEN** user creates an OpenClaw CR with a valid GeminiAPIKey reference (name and key specified)
- **THEN** the CR MUST be accepted and created successfully

#### Scenario: User creates OpenClaw CR with missing Secret name
- **WHEN** user creates an OpenClaw CR with GeminiAPIKey.key specified but GeminiAPIKey.name empty or missing
- **THEN** the API server MUST reject the request with a validation error indicating Secret name is required (enforced by minLength=1)

#### Scenario: User creates OpenClaw CR with missing Secret key
- **WHEN** user creates an OpenClaw CR with GeminiAPIKey.name specified but GeminiAPIKey.key empty or missing
- **THEN** the API server MUST reject the request with a validation error indicating Secret key is required (enforced by minLength=1)

### Requirement: CRD schema includes GeminiAPIKey validation
The generated CRD YAML MUST include OpenAPI validation schema that marks GeminiAPIKey as a required field with nested validation for name and key subfields.

#### Scenario: CRD is installed in cluster
- **WHEN** the CRD is installed using `kubectl apply`
- **THEN** the CRD schema MUST show GeminiAPIKey as a required field with type object containing name and key string fields

#### Scenario: User runs kubectl explain on OpenClaw
- **WHEN** user runs `kubectl explain openclaw.spec.geminiAPIKey`
- **THEN** the output MUST show the field as required with type object containing name and key fields

## MODIFIED Requirements

### Requirement: Controller configures proxy deployment to reference user's Secret
The controller MUST configure the `openclaw-proxy` Deployment to directly reference the user-managed Secret specified in OpenClawSpec.GeminiAPIKey by modifying the deployment manifest BEFORE applying it, ensuring the `GEMINI_API_KEY` environment variable uses `valueFrom.secretKeyRef` pointing to the user's Secret.

#### Scenario: Controller reconciles OpenClaw CR with valid Secret reference
- **WHEN** the controller reconciles an OpenClaw CR with a valid GeminiAPIKey reference (name and key specified)
- **THEN** the controller MUST modify the `openclaw-proxy` deployment manifest before applying it to set the `GEMINI_API_KEY` env var's `valueFrom.secretKeyRef.name` to the user's Secret name and `valueFrom.secretKeyRef.key` to the user's Secret key

#### Scenario: User changes GeminiAPIKey reference to different Secret
- **WHEN** the user updates the GeminiAPIKey field to reference a different Secret (e.g., changing name from `secret-a` to `secret-b`)
- **THEN** the controller MUST update the `openclaw-proxy` deployment manifest to reference the new Secret, triggering Kubernetes to automatically restart pods with the new Secret reference

#### Scenario: User changes GeminiAPIKey key within same Secret
- **WHEN** the user updates the GeminiAPIKey.key field to reference a different key in the same Secret (e.g., changing from `api-key` to `gemini-key`)
- **THEN** the controller MUST update the `openclaw-proxy` deployment manifest to reference the new key, triggering Kubernetes to automatically restart pods

#### Scenario: User updates value in referenced Secret
- **WHEN** the user updates the API key value in the referenced Secret (without changing the Secret name or key)
- **THEN** the deployment manifest remains unchanged (same secretKeyRef), and Kubernetes automatically propagates the new value to running pods without restart

#### Scenario: Referenced Secret does not exist when pods start
- **WHEN** the controller applies the deployment with a Secret reference and the referenced Secret does not exist in the namespace
- **THEN** the deployment MUST be created successfully, but pods MUST fail to start with a clear error indicating the Secret is missing (Kubernetes validates Secret existence at pod creation time)

#### Scenario: Referenced Secret key does not exist when pods start
- **WHEN** the controller applies the deployment with a Secret reference and the Secret exists but does not contain the specified key
- **THEN** the deployment MUST be created successfully, but pods MUST fail to start with a clear error indicating the key is missing from the Secret (Kubernetes validates key existence at pod creation time)

### Requirement: Proxy deployment mounts user's Secret directly
The proxy Deployment MUST reference the user-managed Secret directly via `valueFrom.secretKeyRef` for the `GEMINI_API_KEY` environment variable, with `optional: false` to ensure the Secret and key exist before pods start.

#### Scenario: Proxy container starts with user's Secret
- **WHEN** the proxy Deployment is created or updated by the controller
- **THEN** the proxy container MUST have an environment variable `GEMINI_API_KEY` that references the user's Secret via `valueFrom.secretKeyRef` with `optional: false`

#### Scenario: Proxy uses API key from user's Secret
- **WHEN** the proxy forwards a request to an LLM provider
- **THEN** the proxy MUST include the API key from the `GEMINI_API_KEY` environment variable (populated by Kubernetes from the user's Secret) in the authentication header

#### Scenario: Pod template spec changes trigger restart
- **WHEN** the controller updates the deployment's pod template spec (e.g., changing the Secret reference)
- **THEN** Kubernetes MUST automatically trigger a rolling restart of proxy pods to apply the new configuration
