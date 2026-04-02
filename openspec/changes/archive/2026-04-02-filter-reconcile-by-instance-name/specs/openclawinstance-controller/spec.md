## MODIFIED Requirements

### Requirement: Controller reconciles all OpenClawInstance resources
The system SHALL configure the controller to watch all OpenClawInstance resources but only reconcile resources named "instance", skipping all other named resources.

#### Scenario: Resource named 'instance' triggers reconciliation
- **WHEN** an OpenClawInstance named 'instance' is created
- **THEN** the controller's Reconcile function SHALL process the resource and create a Deployment

#### Scenario: Resource with different name is skipped
- **WHEN** an OpenClawInstance with a name other than 'instance' is created
- **THEN** the controller's Reconcile function SHALL skip processing and return successfully without creating a Deployment

#### Scenario: Multiple resources with different names
- **WHEN** multiple OpenClawInstance resources exist with different names
- **THEN** the controller SHALL only reconcile the resource named 'instance' and skip all others

#### Scenario: Skipped resource is logged
- **WHEN** an OpenClawInstance with a name other than 'instance' is reconciled
- **THEN** the controller SHALL log that the resource is being skipped due to name mismatch

## ADDED Requirements

### Requirement: Controller filters by resource name
The system SHALL check the OpenClawInstance resource name early in the reconciliation loop and skip processing if the name is not "instance".

#### Scenario: Name check occurs after fetch
- **WHEN** the Reconcile function fetches the OpenClawInstance resource
- **THEN** it SHALL immediately check if the resource name equals "instance" before proceeding with any other logic

#### Scenario: Name filtering returns success
- **WHEN** a resource name does not match "instance"
- **THEN** the Reconcile function SHALL return `ctrl.Result{}, nil` (success) without error

#### Scenario: Name filtering does not affect NotFound handling
- **WHEN** a resource is not found during fetch
- **THEN** the controller SHALL still handle NotFound errors before checking the name
