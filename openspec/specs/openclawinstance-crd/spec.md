## ADDED Requirements

### Requirement: OpenClawInstance CRD exists
The system SHALL define a CustomResourceDefinition named `OpenClawInstance` in the API group `openclaw.sandbox.redhat.com/v1alpha1`.

#### Scenario: CRD is installed
- **WHEN** the operator is deployed
- **THEN** the OpenClawInstance CRD SHALL be registered in the Kubernetes cluster

#### Scenario: CRD is discoverable via kubectl
- **WHEN** user runs `kubectl get crd openclawinstances.openclaw.sandbox.redhat.com`
- **THEN** the CRD SHALL be present and show version v1alpha1

### Requirement: OpenClawInstance has Go API types
The system SHALL define Go structs for `OpenClawInstance`, `OpenClawInstanceSpec`, and `OpenClawInstanceStatus` with appropriate Kubernetes API machinery tags.

#### Scenario: API types are available for controller use
- **WHEN** controller code imports the API package
- **THEN** it SHALL have access to typed OpenClawInstance structs

#### Scenario: Types support JSON serialization
- **WHEN** OpenClawInstance is marshaled to JSON
- **THEN** it SHALL include standard Kubernetes fields (apiVersion, kind, metadata, spec, status)

### Requirement: OpenClawInstanceSpec is an empty struct
The system SHALL define `OpenClawInstanceSpec` as an empty struct with no fields.

#### Scenario: CRD accepts empty spec
- **WHEN** user creates an OpenClawInstance with `spec: {}`
- **THEN** the resource SHALL be accepted and stored

#### Scenario: CRD accepts omitted spec
- **WHEN** user creates an OpenClawInstance with no spec field
- **THEN** the resource SHALL be accepted and stored

### Requirement: OpenClawInstanceStatus is an empty struct
The system SHALL define `OpenClawInstanceStatus` as an empty struct with no fields.

#### Scenario: Status subresource exists
- **WHEN** controller attempts to update the status subresource
- **THEN** the update SHALL succeed without validation errors

#### Scenario: Empty status is valid
- **WHEN** an OpenClawInstance is created
- **THEN** it SHALL have an empty status object

### Requirement: CRD manifest is generated
The system SHALL generate a CRD manifest YAML file at `config/crd/bases/` containing the OpenClawInstance schema.

#### Scenario: CRD manifest is available for deployment
- **WHEN** operator installation manifests are generated
- **THEN** the OpenClawInstance CRD YAML SHALL be present in `config/crd/bases/`

#### Scenario: CRD includes status subresource
- **WHEN** examining the generated CRD manifest
- **THEN** it SHALL include `status: {}` in the subresources section
