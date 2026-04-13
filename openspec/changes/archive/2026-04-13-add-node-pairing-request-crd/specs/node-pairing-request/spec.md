## ADDED Requirements

### Requirement: NodePairingRequest CRD definition
The system SHALL provide a namespaced Custom Resource Definition named `NodePairingRequest` in the API group `openclaw.sandbox.redhat.com/v1alpha1`.

#### Scenario: CRD is registered with correct metadata
- **WHEN** the operator is installed
- **THEN** the NodePairingRequest CRD exists with API group `openclaw.sandbox.redhat.com/v1alpha1` and scope `Namespaced`

### Requirement: RequestID field in Spec
The NodePairingRequest Spec SHALL contain a field named `RequestID` of type string.

#### Scenario: NodePairingRequest created with RequestID
- **WHEN** a user creates a NodePairingRequest resource with Spec.RequestID set to "test-request-123"
- **THEN** the resource is created successfully and Spec.RequestID field contains "test-request-123"

#### Scenario: RequestID field is accessible
- **WHEN** the NodePairingRequest resource is retrieved
- **THEN** the Spec.RequestID field can be read from the resource

### Requirement: Controller watches NodePairingRequest resources
The system SHALL implement a controller that watches NodePairingRequest resources and reconciles them.

#### Scenario: Controller reconciles on resource creation
- **WHEN** a NodePairingRequest resource is created
- **THEN** the controller's Reconcile method is invoked with the resource's namespaced name

#### Scenario: Controller reconciles on resource update
- **WHEN** a NodePairingRequest resource is modified
- **THEN** the controller's Reconcile method is invoked with the updated resource

#### Scenario: Controller reconciles on resource deletion
- **WHEN** a NodePairingRequest resource is deleted
- **THEN** the controller's Reconcile method is invoked for cleanup

### Requirement: Status subresource support
The NodePairingRequest CRD SHALL define a Status subresource to track reconciliation state.

#### Scenario: Status can be updated independently of Spec
- **WHEN** the controller updates the Status field
- **THEN** the Spec fields remain unchanged

### Requirement: Conditions array in Status
The NodePairingRequest Status SHALL contain a Conditions field of type `[]metav1.Condition` following Kubernetes standard condition patterns.

#### Scenario: Status has Conditions field
- **WHEN** the NodePairingRequest resource is created
- **THEN** the Status.Conditions field is available as an array of metav1.Condition

#### Scenario: Conditions array is initially empty
- **WHEN** a new NodePairingRequest resource is created
- **THEN** the Status.Conditions array is empty until the controller updates it

#### Scenario: Controller can append conditions
- **WHEN** the controller updates the Status.Conditions array
- **THEN** the new conditions are persisted in the resource status

### Requirement: RBAC permissions for controller
The controller SHALL have RBAC permissions to get, list, watch, create, update, patch, and delete NodePairingRequest resources, and update their status.

#### Scenario: Controller can read NodePairingRequest resources
- **WHEN** the controller attempts to fetch a NodePairingRequest
- **THEN** the operation succeeds without permission errors

#### Scenario: Controller can update NodePairingRequest status
- **WHEN** the controller updates a NodePairingRequest's status
- **THEN** the status update succeeds without permission errors
