## ADDED Requirements

### Requirement: OpenClawInstance CRD exists
The system SHALL define a CustomResourceDefinition named `OpenClaw` in the API group `openclaw.sandbox.redhat.com/v1alpha1`.

#### Scenario: CRD is installed
- **WHEN** the operator is deployed
- **THEN** the OpenClaw CRD SHALL be registered in the Kubernetes cluster

#### Scenario: CRD is discoverable via kubectl
- **WHEN** user runs `kubectl get crd openclaws.openclaw.sandbox.redhat.com`
- **THEN** the CRD SHALL be present and show version v1alpha1

### Requirement: OpenClawInstance has Go API types
The system SHALL define Go structs for `OpenClaw`, `OpenClawSpec`, and `OpenClawStatus` with appropriate Kubernetes API machinery tags.

#### Scenario: API types are available for controller use
- **WHEN** controller code imports the API package
- **THEN** it SHALL have access to typed OpenClaw structs

#### Scenario: Types support JSON serialization
- **WHEN** OpenClaw is marshaled to JSON
- **THEN** it SHALL include standard Kubernetes fields (apiVersion, kind, metadata, spec, status)

### Requirement: OpenClawInstanceSpec is an empty struct
The system SHALL define `OpenClawSpec` as an empty struct with no fields.

#### Scenario: CRD accepts empty spec
- **WHEN** user creates an OpenClaw with `spec: {}`
- **THEN** the resource SHALL be accepted and stored

#### Scenario: CRD accepts omitted spec
- **WHEN** user creates an OpenClaw with no spec field
- **THEN** the resource SHALL be accepted and stored

### Requirement: OpenClawInstanceStatus is an empty struct
The system SHALL define `OpenClawStatus` as an empty struct with no fields.

#### Scenario: Status subresource exists
- **WHEN** controller attempts to update the status subresource
- **THEN** the update SHALL succeed without validation errors

#### Scenario: Empty status is valid
- **WHEN** an OpenClaw is created
- **THEN** it SHALL have an empty status object

### Requirement: CRD manifest is generated
The system SHALL generate a CRD manifest YAML file at `config/crd/bases/` containing the OpenClaw schema.

#### Scenario: CRD manifest is available for deployment
- **WHEN** operator installation manifests are generated
- **THEN** the OpenClaw CRD YAML SHALL be present in `config/crd/bases/`

#### Scenario: CRD includes status subresource
- **WHEN** examining the generated CRD manifest
- **THEN** it SHALL include `status: {}` in the subresources section

### Requirement: CRD defines printcolumns for status visibility
The OpenClaw CRD SHALL define printcolumns to display the Available condition status and reason in kubectl table output.

#### Scenario: Ready column shows Available condition status
- **WHEN** user runs `kubectl get openclaw`
- **THEN** the output SHALL include a "Ready" column showing the status value from the Available condition (True, False, or Unknown)

#### Scenario: Reason column shows Available condition reason
- **WHEN** user runs `kubectl get openclaw`
- **THEN** the output SHALL include a "Reason" column showing the reason value from the Available condition (e.g., Provisioning, Ready)

#### Scenario: Age column is not displayed
- **WHEN** user runs `kubectl get openclaw`
- **THEN** the Age column SHALL NOT appear in the default table output

#### Scenario: Printcolumns use JSONPath to extract condition fields
- **WHEN** examining the CRD manifest
- **THEN** printcolumn definitions SHALL use JSONPath expressions to extract status and reason from the Available condition (e.g., `.status.conditions[?(@.type=="Available")].status`)

#### Scenario: Empty status cells when condition not yet set
- **WHEN** an OpenClaw instance is newly created and the controller has not yet set status conditions
- **THEN** the Ready and Reason columns MAY be empty until the first reconciliation completes
