## ADDED Requirements

### Requirement: OpenClaw status has Conditions field
The OpenClawStatus struct SHALL include a Conditions field of type []metav1.Condition to track instance state.

#### Scenario: Conditions field in CRD status
- **WHEN** the OpenClaw CRD is defined
- **THEN** the OpenClawStatus struct SHALL contain a field `Conditions []metav1.Condition` with JSON tag `conditions`
- **THEN** the field SHALL have kubebuilder marker `+listType=map` and `+listMapKey=type`
- **THEN** the field SHALL be optional (conditions can be empty on creation)

#### Scenario: Generated CRD includes status subresource
- **WHEN** make manifests is run
- **THEN** the generated CRD YAML SHALL include status subresource configuration
- **THEN** the status.conditions field SHALL be present in the OpenAPI schema

### Requirement: Available condition indicates readiness
The controller SHALL maintain an Available condition type to indicate whether the OpenClaw instance is ready for use.

#### Scenario: Available condition set to False during provisioning
- **WHEN** an OpenClaw instance named 'instance' is created
- **THEN** the controller SHALL set Available condition with status=False, reason=Provisioning, message describing deployment progress

#### Scenario: Available condition set to True when ready
- **WHEN** both openclaw and openclaw-proxy Deployments have Available=True status
- **THEN** the controller SHALL set Available condition with status=True, reason=Ready, message confirming both deployments are ready

#### Scenario: Available condition remains False if any deployment not ready
- **WHEN** either openclaw or openclaw-proxy Deployment has Available condition not equal to True
- **THEN** the controller SHALL keep Available condition at status=False, reason=Provisioning

### Requirement: Controller checks Deployment status conditions
The controller SHALL read the Available condition from both openclaw and openclaw-proxy Deployment status to determine readiness.

#### Scenario: Fetch openclaw Deployment status
- **WHEN** updating OpenClaw status conditions
- **THEN** the controller SHALL fetch the Deployment named 'openclaw' in the same namespace
- **THEN** the controller SHALL read the Available condition from deployment.Status.Conditions

#### Scenario: Fetch openclaw-proxy Deployment status
- **WHEN** updating OpenClaw status conditions
- **THEN** the controller SHALL fetch the Deployment named 'openclaw-proxy' in the same namespace
- **THEN** the controller SHALL read the Available condition from deployment.Status.Conditions

#### Scenario: Handle missing Deployment
- **WHEN** a Deployment is not found during status check
- **THEN** the controller SHALL treat the deployment as not ready
- **THEN** the Available condition SHALL remain False with reason=Provisioning

### Requirement: Status updates use status subresource
The controller SHALL update OpenClaw status using the Kubernetes status subresource, not the main resource.

#### Scenario: Status updated via client.Status()
- **WHEN** the controller updates OpenClaw status conditions
- **THEN** the controller SHALL use `client.Status().Update(ctx, openclawInstance)` or `client.Status().Patch(ctx, openclawInstance, patch)`
- **THEN** status updates SHALL NOT trigger spec reconciliation

#### Scenario: Failed status update retried
- **WHEN** a status update fails due to conflict or API error
- **THEN** the controller SHALL return an error to trigger reconciliation retry
- **THEN** the next reconciliation SHALL attempt the status update again

### Requirement: Condition transitions update LastTransitionTime
The controller SHALL update the LastTransitionTime field only when the condition status changes.

#### Scenario: Status change updates LastTransitionTime
- **WHEN** the Available condition changes from False to True
- **THEN** the controller SHALL set LastTransitionTime to the current timestamp
- **THEN** the controller SHALL update the reason and message fields

#### Scenario: Same status preserves LastTransitionTime
- **WHEN** the Available condition status remains the same (e.g., False to False)
- **THEN** the controller SHALL preserve the existing LastTransitionTime value
- **THEN** the controller MAY update the reason or message fields

### Requirement: Condition uses standard meta fields
Each condition SHALL include all standard metav1.Condition fields: Type, Status, ObservedGeneration, LastTransitionTime, Reason, and Message.

#### Scenario: Condition fields populated
- **WHEN** the controller sets a condition
- **THEN** the condition SHALL have Type set to the condition type string (e.g., "Available")
- **THEN** the condition SHALL have Status set to "True", "False", or "Unknown"
- **THEN** the condition SHALL have Reason set to a CamelCase single-word or hyphenated reason
- **THEN** the condition SHALL have Message set to a human-readable description
- **THEN** the condition SHALL have ObservedGeneration set to the OpenClaw resource's metadata.generation
- **THEN** the condition SHALL have LastTransitionTime set to the time of the status change

### Requirement: Available condition reasons are well-defined
The Available condition SHALL use standardized reason values for common states.

#### Scenario: Provisioning reason when deployments not ready
- **WHEN** one or both Deployments are not yet available
- **THEN** the Available condition SHALL have reason=Provisioning
- **THEN** the message SHALL indicate which deployments are pending

#### Scenario: Ready reason when fully available
- **WHEN** both Deployments report Available=True
- **THEN** the Available condition SHALL have reason=Ready
- **THEN** the message SHALL confirm the instance is ready for use
