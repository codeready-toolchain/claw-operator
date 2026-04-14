## Requirements

### Requirement: Credentials field is optional in ClawSpec
The ClawSpec SHALL include an optional `credentials` field of type `[]CredentialSpec` that configures proxy credential injection per domain.

#### Scenario: User creates Claw CR without credentials field
- **WHEN** user creates a Claw CR without specifying the credentials field
- **THEN** the CR MUST be accepted and created successfully with empty credentials

#### Scenario: User creates Claw CR with empty credentials array
- **WHEN** user creates a Claw CR with an empty credentials array
- **THEN** the CR MUST be accepted and created successfully

#### Scenario: User creates Claw CR with credentials array
- **WHEN** user creates a Claw CR with a non-empty credentials array containing valid CredentialSpec entries
- **THEN** the CR MUST be accepted and created successfully

### Requirement: CredentialSpec structure validation
Each CredentialSpec entry MUST include required fields (name, type, domain) and type-specific configuration fields validated by CEL expressions.

#### Scenario: CredentialSpec requires name field
- **WHEN** user creates a credential entry without a name field
- **THEN** the API server MUST reject the request with a validation error

#### Scenario: CredentialSpec requires type field
- **WHEN** user creates a credential entry without a type field
- **THEN** the API server MUST reject the request with a validation error

#### Scenario: CredentialSpec requires domain field
- **WHEN** user creates a credential entry without a domain field
- **THEN** the API server MUST reject the request with a validation error

#### Scenario: CredentialSpec validates type enum
- **WHEN** user creates a credential with an invalid type value
- **THEN** the API server MUST reject the request (valid: apiKey, bearer, gcp, pathToken, oauth2, none)

### Requirement: Type-specific configuration validation
The system MUST enforce that type-specific configuration fields are present when required for a given credential type.

#### Scenario: apiKey type requires apiKey config
- **WHEN** user creates a credential with type="apiKey" but missing apiKey configuration
- **THEN** the API server MUST reject the request due to CEL validation rule

#### Scenario: gcp type requires gcp config
- **WHEN** user creates a credential with type="gcp" but missing gcp configuration
- **THEN** the API server MUST reject the request due to CEL validation rule

#### Scenario: pathToken type requires pathToken config
- **WHEN** user creates a credential with type="pathToken" but missing pathToken configuration
- **THEN** the API server MUST reject the request due to CEL validation rule

#### Scenario: oauth2 type requires oauth2 config
- **WHEN** user creates a credential with type="oauth2" but missing oauth2 configuration
- **THEN** the API server MUST reject the request due to CEL validation rule

#### Scenario: none type does not require secretRef
- **WHEN** user creates a credential with type="none" and no secretRef
- **THEN** the CR MUST be accepted (allowlist mode, no authentication)

### Requirement: SecretRef validation for credential types
The system MUST require secretRef field for all credential types except "none".

#### Scenario: Credential with type other than none requires secretRef
- **WHEN** user creates a credential with type="apiKey" (or bearer, gcp, pathToken, oauth2) but missing secretRef
- **THEN** the API server MUST reject the request due to CEL validation rule

#### Scenario: SecretRef validates name field
- **WHEN** user creates a credential with secretRef containing empty name
- **THEN** the API server MUST reject the request due to minLength validation

#### Scenario: SecretRef validates key field
- **WHEN** user creates a credential with secretRef containing empty key
- **THEN** the API server MUST reject the request due to minLength validation

### Requirement: Controller validates Secret existence
The controller MUST validate that Secrets referenced in credentials exist in the same namespace as the Claw instance.

#### Scenario: Controller reconciles Claw CR when Secret does not exist
- **WHEN** the controller reconciles a Claw CR with credentials referencing a non-existent Secret
- **THEN** the controller MUST set CredentialsResolved condition to False with reason ValidationFailed

#### Scenario: Controller reconciles Claw CR when Secret exists
- **WHEN** the controller reconciles a Claw CR with credentials referencing an existing Secret
- **THEN** the controller MUST set CredentialsResolved condition to True with reason Resolved

#### Scenario: User creates Secret after Claw CR
- **WHEN** the user creates a Secret referenced by an existing Claw CR
- **THEN** the controller MUST detect the change and update CredentialsResolved condition to True

#### Scenario: User deletes referenced Secret
- **WHEN** the user deletes a Secret referenced by a Claw CR
- **THEN** the controller MUST detect the deletion and update CredentialsResolved condition to False

### Requirement: Controller configures proxy with credentials
The controller MUST configure the openclaw-proxy Deployment to inject credentials based on the credentials array in the Claw spec.

#### Scenario: Proxy container environment variables configured
- **WHEN** the controller reconciles a Claw with credentials
- **THEN** the proxy Deployment MUST be configured with environment variables for each credential's Secret

#### Scenario: Proxy restarts on credential Secret reference change
- **WHEN** the user updates a credential's secretRef in the Claw CR
- **THEN** the proxy pod MUST restart to pick up the new Secret reference

#### Scenario: Proxy restarts on Secret data change
- **WHEN** the user updates data in a Secret referenced by credentials
- **THEN** the proxy pod MUST restart automatically (via Secret ResourceVersion annotation)

### Requirement: CRD schema includes credentials validation
The generated CRD YAML MUST include OpenAPI validation schema with CEL expressions for credential type-specific validation.

#### Scenario: CRD is installed in cluster
- **WHEN** the CRD is installed using `kubectl apply`
- **THEN** the CRD schema MUST show credentials as an optional array field with CredentialSpec items

#### Scenario: User runs kubectl explain on Claw
- **WHEN** user runs `kubectl explain claw.spec.credentials`
- **THEN** the output MUST show the field as optional with type array

#### Scenario: CEL validation rules are enforced
- **WHEN** examining the generated CRD manifest
- **THEN** it MUST include x-kubernetes-validations with CEL expressions for type-specific config validation
