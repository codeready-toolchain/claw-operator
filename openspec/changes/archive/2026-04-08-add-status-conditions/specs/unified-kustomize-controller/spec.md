## ADDED Requirements

### Requirement: Controller updates OpenClaw status after applying resources
The controller SHALL update the OpenClaw instance status conditions after successfully applying all Kustomize resources.

#### Scenario: Status updated in same reconciliation loop
- **WHEN** all resources are successfully applied via server-side apply
- **THEN** the controller SHALL fetch Deployment statuses
- **THEN** the controller SHALL update OpenClaw status conditions based on deployment readiness
- **THEN** status update SHALL happen before returning from reconciliation

#### Scenario: Status update failure does not block resource creation
- **WHEN** resource application succeeds but status update fails
- **THEN** the controller SHALL log the status update error
- **THEN** the controller SHALL return an error to trigger retry
- **THEN** the next reconciliation SHALL re-attempt status update

### Requirement: Controller reads Deployment status conditions
The controller SHALL read the Available condition from both openclaw and openclaw-proxy Deployment resources to determine overall readiness.

#### Scenario: Get openclaw Deployment
- **WHEN** updating status conditions
- **THEN** the controller SHALL fetch the Deployment resource named 'openclaw' in instance.Namespace
- **THEN** if Deployment is not found, controller SHALL treat it as not ready

#### Scenario: Get openclaw-proxy Deployment
- **WHEN** updating status conditions
- **THEN** the controller SHALL fetch the Deployment resource named 'openclaw-proxy' in instance.Namespace
- **THEN** if Deployment is not found, controller SHALL treat it as not ready

#### Scenario: Parse Available condition from Deployment
- **WHEN** Deployment is fetched successfully
- **THEN** the controller SHALL search deployment.Status.Conditions for condition with Type="Available"
- **THEN** if condition is found and Status="True", deployment is considered ready
- **THEN** if condition is not found or Status!="True", deployment is considered not ready

### Requirement: Controller sets Available condition based on deployment readiness
The controller SHALL set the OpenClaw Available condition to True only when both deployments are ready, otherwise False.

#### Scenario: Both deployments ready sets Available True
- **WHEN** openclaw Deployment has Available=True
- **WHEN** openclaw-proxy Deployment has Available=True
- **THEN** the controller SHALL set OpenClaw Available condition with status=True, reason=Ready

#### Scenario: Any deployment not ready sets Available False
- **WHEN** either openclaw or openclaw-proxy Deployment does not have Available=True
- **THEN** the controller SHALL set OpenClaw Available condition with status=False, reason=Provisioning
- **THEN** the message SHALL indicate which deployments are pending

### Requirement: Controller uses status subresource for updates
The controller SHALL update OpenClaw status using the status subresource client.

#### Scenario: Update via status subresource
- **WHEN** updating status conditions
- **THEN** the controller SHALL use `r.Status().Update(ctx, instance)` where r is the controller client
- **THEN** status updates SHALL NOT update spec fields or trigger unnecessary reconciliations

## MODIFIED Requirements

### Requirement: Controller has RBAC permissions
The controller SHALL have necessary RBAC permissions defined via kubebuilder annotations, including permissions to read Deployment status and update OpenClaw status.

#### Scenario: OpenClaw resource permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, and watch permissions for OpenClaw resources

#### Scenario: OpenClaw status permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant update and patch permissions for OpenClaw/status subresource

#### Scenario: ConfigMap permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, create, update, and patch permissions for ConfigMap resources

#### Scenario: PersistentVolumeClaim permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, create, update, and patch permissions for PersistentVolumeClaim resources

#### Scenario: Deployment permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, create, update, and patch permissions for Deployment resources

### Requirement: Controller registers with manager
The controller SHALL register with the controller-runtime manager and configure appropriate watches, including watches for Deployments to trigger status updates.

#### Scenario: Controller registered with manager
- **WHEN** the controller's SetupWithManager is called
- **THEN** the controller registers to watch OpenClaw resources as the primary resource
- **THEN** the controller registers to own ConfigMap resources
- **THEN** the controller registers to own PersistentVolumeClaim resources
- **THEN** the controller registers to own Deployment resources
- **THEN** the controller is named "openclaw" for identification

#### Scenario: Deployment changes trigger reconciliation
- **WHEN** a Deployment owned by an OpenClaw instance changes status
- **THEN** the controller reconciliation loop SHALL be triggered
- **THEN** the controller SHALL update OpenClaw status conditions based on new Deployment status
