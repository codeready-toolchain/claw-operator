## ADDED Requirements

### Requirement: Selector field in ClawDevicePairingRequest Spec
The ClawDevicePairingRequest Spec SHALL contain a required field named `selector` of type `metav1.LabelSelector` that specifies which pod should process the device pairing request.

#### Scenario: ClawDevicePairingRequest created with selector
- **WHEN** a user creates a ClawDevicePairingRequest with Spec.Selector containing matchLabels
- **THEN** the resource is created successfully and Spec.Selector field is accessible

#### Scenario: ClawDevicePairingRequest created without selector is rejected
- **WHEN** a user attempts to create a ClawDevicePairingRequest without a Spec.Selector field
- **THEN** the API server rejects the request with a validation error indicating selector is required

#### Scenario: Selector with matchLabels is valid
- **WHEN** a user creates a ClawDevicePairingRequest with Spec.Selector.MatchLabels containing `{"app": "claw", "instance": "my-claw"}`
- **THEN** the resource is created successfully

#### Scenario: Selector with matchExpressions is valid
- **WHEN** a user creates a ClawDevicePairingRequest with Spec.Selector.MatchExpressions containing a valid expression
- **THEN** the resource is created successfully

### Requirement: Controller uses selector to find target pod
The ClawDevicePairingRequestController SHALL use the selector field to query for pods in the same namespace as the ClawDevicePairingRequest resource.

#### Scenario: Controller queries pods using selector
- **WHEN** a ClawDevicePairingRequest is reconciled with selector `{"app": "claw"}`
- **THEN** the controller performs a pod List operation with LabelSelector matching the selector

#### Scenario: Controller finds exactly one matching pod
- **WHEN** the selector matches exactly one pod in the namespace
- **THEN** the controller processes the pairing request for that pod

#### Scenario: Controller handles no matching pods
- **WHEN** the selector matches zero pods in the namespace
- **THEN** the controller sets a condition with status False and reason "NoMatchingPod" and message indicating no pods matched the selector

#### Scenario: Controller handles multiple matching pods
- **WHEN** the selector matches more than one pod in the namespace
- **THEN** the controller sets a condition with status False and reason "MultipleMatchingPods" and message indicating how many pods matched

#### Scenario: Controller handles invalid selector
- **WHEN** the selector cannot be converted to a valid labels.Selector
- **THEN** the controller sets a condition with status False and reason "InvalidSelector" and message describing the validation error

### Requirement: Selector validation in CRD
The ClawDevicePairingRequest CRD SHALL enforce that the selector field is required through kubebuilder validation markers.

#### Scenario: CRD schema requires selector field
- **WHEN** the CRD is installed
- **THEN** the OpenAPI schema includes selector as a required field in the Spec

#### Scenario: Empty selector object is rejected
- **WHEN** a user creates a ClawDevicePairingRequest with an empty selector object (no matchLabels or matchExpressions)
- **THEN** the API server rejects the request with a validation error

### Requirement: Selector field type compatibility
The selector field SHALL use the standard Kubernetes `metav1.LabelSelector` type to ensure compatibility with Kubernetes tooling and client libraries.

#### Scenario: Selector serializes to standard Kubernetes format
- **WHEN** a ClawDevicePairingRequest with a selector is retrieved as JSON
- **THEN** the selector field matches the standard Kubernetes LabelSelector schema

#### Scenario: Selector can be used with client-go utilities
- **WHEN** the controller converts Spec.Selector to labels.Selector using metav1.LabelSelectorAsSelector
- **THEN** the conversion succeeds without error for valid selectors
