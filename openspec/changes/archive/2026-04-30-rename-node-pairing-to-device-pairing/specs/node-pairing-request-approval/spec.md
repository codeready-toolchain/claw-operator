## Requirements

### Requirement: NodePairingRequestApproval CRD definition
**FROM**: NodePairingRequestApproval  
**TO**: DevicePairingRequest

The CRD name changes from `NodePairingRequestApproval` to `DevicePairingRequest`. Resource path changes from `nodepairingrequestapprovals` to `devicepairingrequests`. All other attributes remain unchanged.

### Requirement: Spec field is required
**FROM**: NodePairingRequestApproval  
**TO**: DevicePairingRequest

The requirement remains the same, only the resource type name changes.

### Requirement: RequestID field in Spec
**FROM**: NodePairingRequestApproval  
**TO**: DevicePairingRequest

The Spec.RequestID field requirement remains unchanged, only references to the resource type name change.

### Requirement: Controller watches NodePairingRequestApproval resources
**FROM**: NodePairingRequestApproval / NodePairingRequestApprovalReconciler  
**TO**: DevicePairingRequest / DevicePairingRequestReconciler

The controller name changes from `NodePairingRequestApprovalReconciler` to `DevicePairingRequestReconciler`. Reconciliation behavior remains identical.

### Requirement: Status subresource support
**FROM**: NodePairingRequestApproval  
**TO**: DevicePairingRequest

The Status subresource requirement remains unchanged, only the resource type name changes.

### Requirement: Conditions array in Status
**FROM**: NodePairingRequestApproval  
**TO**: DevicePairingRequest

The Status.Conditions field requirement remains unchanged, only references to the resource type name change.

### Requirement: RBAC permissions for controller
**FROM**: NodePairingRequestApproval  
**TO**: DevicePairingRequest

RBAC permissions remain the same (get, list, watch, create, update, patch for resources; update for status and finalizers; no delete). Only the resource name in RBAC manifests changes.

## MODIFIED Requirements

### Requirement: NodePairingRequestApproval CRD definition
The system SHALL provide a namespaced Custom Resource Definition named `DevicePairingRequest` in the API group `claw.sandbox.redhat.com/v1alpha1` with resource path `devicepairingrequests`.

#### Scenario: CRD is registered with correct metadata
- **WHEN** the operator is installed
- **THEN** the DevicePairingRequest CRD exists with API group `claw.sandbox.redhat.com/v1alpha1`, scope `Namespaced`, and plural `devicepairingrequests`

### Requirement: Spec field is required
The DevicePairingRequest resource SHALL require a Spec field at the root level, enforced through CRD validation.

#### Scenario: Resource creation without Spec is rejected
- **WHEN** a user attempts to create a DevicePairingRequest resource without a Spec field
- **THEN** the API server rejects the request with a validation error

#### Scenario: Resource creation with Spec is accepted
- **WHEN** a user creates a DevicePairingRequest resource with a Spec field containing requestID
- **THEN** the resource is created successfully

### Requirement: RequestID field in Spec
The DevicePairingRequest Spec SHALL contain a required field named `RequestID` of type string with minimum length validation.

#### Scenario: DevicePairingRequest created with RequestID
- **WHEN** a user creates a DevicePairingRequest resource with Spec.RequestID set to "test-request-123"
- **THEN** the resource is created successfully and Spec.RequestID field contains "test-request-123"

#### Scenario: RequestID field is accessible
- **WHEN** the DevicePairingRequest resource is retrieved
- **THEN** the Spec.RequestID field can be read from the resource

#### Scenario: Empty RequestID is rejected
- **WHEN** a user attempts to create a DevicePairingRequest resource with empty RequestID
- **THEN** the API server rejects the request due to MinLength validation

### Requirement: Controller watches DevicePairingRequest resources
The system SHALL implement a controller that watches DevicePairingRequest resources and reconciles them. The controller SHALL NOT implement deletion cleanup as resources serve as immutable audit records.

#### Scenario: Controller reconciles on resource creation
- **WHEN** a DevicePairingRequest resource is created
- **THEN** the controller's Reconcile method is invoked with the resource's namespaced name

#### Scenario: Controller reconciles on resource update
- **WHEN** a DevicePairingRequest resource is modified
- **THEN** the controller's Reconcile method is invoked with the updated resource

#### Scenario: Controller ignores deleted resources
- **WHEN** a DevicePairingRequest resource is deleted by an external actor
- **THEN** the controller's Reconcile method receives a NotFound error and returns without performing cleanup

### Requirement: Status subresource support
The DevicePairingRequest CRD SHALL define a Status subresource to track reconciliation state.

#### Scenario: Status can be updated independently of Spec
- **WHEN** the controller updates the Status field
- **THEN** the Spec fields remain unchanged

### Requirement: Conditions array in Status
The DevicePairingRequest Status SHALL contain a Conditions field of type `[]metav1.Condition` following Kubernetes standard condition patterns.

#### Scenario: Status has Conditions field
- **WHEN** the DevicePairingRequest resource is created
- **THEN** the Status.Conditions field is available as an array of metav1.Condition

#### Scenario: Conditions array is initially empty
- **WHEN** a new DevicePairingRequest resource is created
- **THEN** the Status.Conditions array is empty until the controller updates it

#### Scenario: Controller can append conditions
- **WHEN** the controller updates the Status.Conditions array
- **THEN** the new conditions are persisted in the resource status

### Requirement: RBAC permissions for controller
The controller SHALL have RBAC permissions to get, list, watch, create, update, and patch DevicePairingRequest resources, and update their status and finalizers. The controller SHALL NOT have delete permissions to preserve resources as immutable audit records.

#### Scenario: Controller can read DevicePairingRequest resources
- **WHEN** the controller attempts to fetch a DevicePairingRequest
- **THEN** the operation succeeds without permission errors

#### Scenario: Controller can update DevicePairingRequest status
- **WHEN** the controller updates a DevicePairingRequest's status
- **THEN** the status update succeeds without permission errors

#### Scenario: Controller cannot delete DevicePairingRequest resources
- **WHEN** the controller attempts to delete a DevicePairingRequest resource
- **THEN** the operation fails with a forbidden permission error

#### Scenario: External actors can delete resources with appropriate RBAC
- **WHEN** a user or service with delete permissions attempts to delete a DevicePairingRequest
- **THEN** the operation succeeds (resources are not protected by finalizers, only by RBAC)
