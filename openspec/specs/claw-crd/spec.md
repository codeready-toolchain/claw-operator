## ADDED Requirements

### Requirement: Claw CRD exists
The system SHALL define a CustomResourceDefinition named `Claw` in the API group `claw.sandbox.redhat.com/v1alpha1`.

#### Scenario: CRD is installed
- **WHEN** the operator is deployed
- **THEN** the Claw CRD SHALL be registered in the Kubernetes cluster

#### Scenario: CRD is discoverable via kubectl
- **WHEN** user runs `kubectl get crd claws.claw.sandbox.redhat.com`
- **THEN** the CRD SHALL be present and show version v1alpha1

### Requirement: Claw has Go API types
The system SHALL define Go structs for `Claw`, `ClawSpec`, and `ClawStatus` with appropriate Kubernetes API machinery tags.

#### Scenario: API types are available for controller use
- **WHEN** controller code imports the API package
- **THEN** it SHALL have access to typed Claw structs

#### Scenario: Types support JSON serialization
- **WHEN** Claw is marshaled to JSON
- **THEN** it SHALL include standard Kubernetes fields (apiVersion, kind, metadata, spec, status)

### Requirement: ClawSpec contains credentials field
The system SHALL define `ClawSpec` with an optional `credentials` field of type `[]CredentialSpec`.

#### Scenario: CRD accepts empty credentials array
- **WHEN** user creates a Claw with `spec: { credentials: [] }`
- **THEN** the resource SHALL be accepted and stored

#### Scenario: CRD accepts omitted credentials field
- **WHEN** user creates a Claw with `spec: {}`
- **THEN** the resource SHALL be accepted and stored

#### Scenario: CRD accepts credentials array with entries
- **WHEN** user creates a Claw with credentials containing valid CredentialSpec entries
- **THEN** the resource SHALL be accepted and stored

### Requirement: CredentialSpec type definition
The system SHALL define `CredentialSpec` with fields: name, type, secretRef, domain, defaultHeaders, apiKey, gcp, pathToken, oauth2.

#### Scenario: CredentialSpec validates required fields
- **WHEN** user creates a credential entry without required fields (name, type, domain)
- **THEN** the API server SHALL reject the request with validation error

#### Scenario: CredentialSpec validates type-specific config
- **WHEN** user creates a credential with type="apiKey" but missing apiKey config
- **THEN** the API server SHALL reject the request due to CEL validation

#### Scenario: CredentialSpec validates secretRef requirement
- **WHEN** user creates a credential with type other than "none" but missing secretRef
- **THEN** the API server SHALL reject the request due to CEL validation

### Requirement: ClawStatus contains observability fields
The system SHALL define `ClawStatus` with fields: gatewayTokenSecretRef, url, and conditions.

#### Scenario: Status subresource exists
- **WHEN** controller attempts to update the status subresource
- **THEN** the update SHALL succeed without validation errors

#### Scenario: Status fields are optional
- **WHEN** a Claw is created
- **THEN** it SHALL have an empty status object with all fields omitted

#### Scenario: Controller populates gatewayTokenSecretRef
- **WHEN** controller reconciles a Claw instance
- **THEN** status.gatewayTokenSecretRef SHALL be set to the gateway Secret name

#### Scenario: Controller populates URL field
- **WHEN** controller reconciles a Claw instance on OpenShift
- **THEN** status.url SHALL be set to the Route HTTPS URL

#### Scenario: Controller populates conditions array
- **WHEN** controller reconciles a Claw instance
- **THEN** status.conditions SHALL contain Ready, CredentialsResolved, and ProxyConfigured conditions

### Requirement: CRD manifest is generated
The system SHALL generate a CRD manifest YAML file at `config/crd/bases/` containing the Claw schema.

#### Scenario: CRD manifest is available for deployment
- **WHEN** operator installation manifests are generated
- **THEN** the Claw CRD YAML SHALL be present in `config/crd/bases/`

#### Scenario: CRD includes status subresource
- **WHEN** examining the generated CRD manifest
- **THEN** it SHALL include `status: {}` in the subresources section

### Requirement: CRD defines printcolumns for status visibility
The Claw CRD SHALL define printcolumns to display the Ready condition status and reason in kubectl table output.

#### Scenario: Ready column shows Ready condition status
- **WHEN** user runs `kubectl get claw`
- **THEN** the output SHALL include a "Ready" column showing the status value from the Ready condition (True, False, or Unknown)

#### Scenario: Reason column shows Ready condition reason
- **WHEN** user runs `kubectl get claw`
- **THEN** the output SHALL include a "Reason" column showing the reason value from the Ready condition (e.g., Provisioning, Ready)

#### Scenario: Age column is not displayed
- **WHEN** user runs `kubectl get claw`
- **THEN** the Age column SHALL NOT appear in the default table output

#### Scenario: Printcolumns use JSONPath to extract condition fields
- **WHEN** examining the CRD manifest
- **THEN** printcolumn definitions SHALL use JSONPath expressions to extract status and reason from the Ready condition (e.g., `.status.conditions[?(@.type=="Ready")].status`)

#### Scenario: Empty status cells when condition not yet set
- **WHEN** a Claw instance is newly created and the controller has not yet set status conditions
- **THEN** the Ready and Reason columns MAY be empty until the first reconciliation completes
