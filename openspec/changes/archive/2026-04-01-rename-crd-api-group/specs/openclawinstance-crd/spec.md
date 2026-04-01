## MODIFIED Requirements

### Requirement: OpenClawInstance CRD exists
The system SHALL define a CustomResourceDefinition named `OpenClawInstance` in the API group `openclaw.sandbox.redhat.com/v1alpha1`.

#### Scenario: CRD is installed
- **WHEN** the operator is deployed
- **THEN** the OpenClawInstance CRD SHALL be registered in the Kubernetes cluster

#### Scenario: CRD is discoverable via kubectl
- **WHEN** user runs `kubectl get crd openclawinstances.openclaw.sandbox.redhat.com`
- **THEN** the CRD SHALL be present and show version v1alpha1
