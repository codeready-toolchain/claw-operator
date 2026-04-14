## ADDED Requirements

### Requirement: Controller watches Claw resources
The ClawReconciler controller SHALL watch Claw custom resources in all namespaces and trigger reconciliation when changes occur.

#### Scenario: Claw resource created triggers reconciliation
- **WHEN** an Claw custom resource is created
- **THEN** the controller reconciliation loop is triggered with the resource name and namespace

#### Scenario: Claw resource updated triggers reconciliation
- **WHEN** an Claw custom resource is updated
- **THEN** the controller reconciliation loop is triggered

#### Scenario: Claw resource deleted triggers reconciliation
- **WHEN** an Claw custom resource is deleted
- **THEN** the controller reconciliation loop is triggered to handle cleanup

### Requirement: Controller filters by instance name
The controller SHALL only reconcile Claw resources named 'instance' and skip all other names.

#### Scenario: Reconcile Claw named 'instance'
- **WHEN** an Claw resource named 'instance' is created or updated
- **THEN** the controller proceeds with manifest generation and application

#### Scenario: Skip Claw with different name
- **WHEN** an Claw resource with a name other than 'instance' is created
- **THEN** the controller logs a skip message and returns without applying resources

### Requirement: Controller builds Kustomize manifests in-memory
The controller SHALL use the Kustomize API to build manifests in-memory from the embedded manifests directory.

#### Scenario: Kustomize build from embedded filesystem
- **WHEN** reconciling an Claw named 'instance'
- **THEN** the controller loads the embedded manifests filesystem
- **THEN** the controller invokes kustomize.Run() to build the resource map

#### Scenario: Kustomization file specifies resources
- **WHEN** the Kustomize build executes
- **THEN** it SHALL process the kustomization.yaml file in internal/assets/manifests/
- **THEN** the kustomization SHALL reference all resource YAML files (configmap.yaml, pvc.yaml, deployment.yaml)

#### Scenario: Common labels applied via Kustomize
- **WHEN** the Kustomize build executes
- **THEN** all resources SHALL have the label `app.kubernetes.io/name: claw` applied via commonLabels in kustomization.yaml

### Requirement: Controller sets namespace on all resources
The controller SHALL set the namespace on all built resources to match the Claw instance's namespace.

#### Scenario: Namespace matches Claw instance
- **WHEN** processing the built resource map
- **THEN** the controller iterates each resource and sets namespace to instance.Namespace
- **THEN** all resources are created in the same namespace as the Claw CR

### Requirement: Controller sets owner references on all resources
The controller SHALL set the Claw instance as the controller owner reference on all created resources.

#### Scenario: Owner reference set on all resources
- **WHEN** processing the built resource map
- **THEN** the controller sets a controller owner reference on each resource pointing to the Claw instance
- **THEN** all resources will be automatically garbage collected when the Claw is deleted

### Requirement: Controller applies resources using server-side apply
The controller SHALL apply all resources atomically using Kubernetes server-side apply.

#### Scenario: Server-side apply with field manager
- **WHEN** applying the built resources
- **THEN** the controller uses client.Patch() with Apply patch type
- **THEN** the controller specifies a field manager name (e.g., "claw-operator")
- **THEN** Kubernetes tracks field ownership for the controller

#### Scenario: Idempotent application
- **WHEN** reconciliation runs multiple times
- **THEN** server-side apply handles resource existence automatically
- **THEN** the controller does not need explicit AlreadyExists error handling
- **THEN** only changed fields are updated

#### Scenario: Atomic resource application
- **WHEN** applying multiple resources
- **THEN** all resources are applied in a single reconciliation pass
- **THEN** if any resource fails to apply, the reconciliation returns an error

### Requirement: Controller handles resource not found
The controller SHALL handle the case where the Claw resource is not found during reconciliation.

#### Scenario: Claw deleted before reconciliation
- **WHEN** the Claw resource is not found during reconciliation
- **THEN** the controller logs that the resource was deleted
- **THEN** the controller returns without error (no requeue)
- **THEN** Kubernetes garbage collection removes owned resources

### Requirement: Controller logs reconciliation events
The controller SHALL log key reconciliation events for observability.

#### Scenario: Log reconciliation start
- **WHEN** reconciliation begins
- **THEN** the controller logs the Claw name and namespace being reconciled

#### Scenario: Log Kustomize build success
- **WHEN** Kustomize manifests are built successfully
- **THEN** the controller logs the number of resources generated

#### Scenario: Log server-side apply success
- **WHEN** resources are successfully applied
- **THEN** the controller logs a success message

#### Scenario: Log reconciliation failures
- **WHEN** Kustomize build or server-side apply fails
- **THEN** the controller logs the error
- **THEN** the controller returns the error to trigger retry

### Requirement: Controller registers with manager
The controller SHALL register with the controller-runtime manager and configure appropriate watches, including watches for Deployments to trigger status updates.

#### Scenario: Controller registered with manager
- **WHEN** the controller's SetupWithManager is called
- **THEN** the controller registers to watch Claw resources as the primary resource
- **THEN** the controller registers to own ConfigMap resources
- **THEN** the controller registers to own PersistentVolumeClaim resources
- **THEN** the controller registers to own Deployment resources
- **THEN** the controller is named "claw" for identification

#### Scenario: Deployment changes trigger reconciliation
- **WHEN** a Deployment owned by an Claw instance changes status
- **THEN** the controller reconciliation loop SHALL be triggered
- **THEN** the controller SHALL update Claw status conditions based on new Deployment status

### Requirement: Controller has RBAC permissions
The controller SHALL have necessary RBAC permissions defined via kubebuilder annotations, including permissions to read Deployment status and update Claw status.

#### Scenario: Claw resource permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, and watch permissions for Claw resources

#### Scenario: Claw status permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant update and patch permissions for Claw/status subresource

#### Scenario: ConfigMap permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, create, update, and patch permissions for ConfigMap resources

#### Scenario: PersistentVolumeClaim permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, create, update, and patch permissions for PersistentVolumeClaim resources

#### Scenario: Deployment permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, create, update, and patch permissions for Deployment resources

### Requirement: All managed resources have common labels
All resources created by the controller SHALL have the label `app.kubernetes.io/name: claw`.

#### Scenario: ConfigMap has common label
- **WHEN** the ConfigMap resource is created
- **THEN** it SHALL have the label `app.kubernetes.io/name: claw`

#### Scenario: PVC has common label
- **WHEN** the PersistentVolumeClaim resource is created
- **THEN** it SHALL have the label `app.kubernetes.io/name: claw`

#### Scenario: Deployment has common label
- **WHEN** the Deployment resource is created
- **THEN** it SHALL have the label `app.kubernetes.io/name: claw`

#### Scenario: Resources queryable by label
- **WHEN** listing resources with label selector `app.kubernetes.io/name=claw`
- **THEN** all resources managed by the controller SHALL be returned

### Requirement: Controller updates Claw status after applying resources
The controller SHALL update the Claw instance status conditions after successfully applying all Kustomize resources.

#### Scenario: Status updated in same reconciliation loop
- **WHEN** all resources are successfully applied via server-side apply
- **THEN** the controller SHALL fetch Deployment statuses
- **THEN** the controller SHALL update Claw status conditions based on deployment readiness
- **THEN** status update SHALL happen before returning from reconciliation

#### Scenario: Status update failure does not block resource creation
- **WHEN** resource application succeeds but status update fails
- **THEN** the controller SHALL log the status update error
- **THEN** the controller SHALL return an error to trigger retry
- **THEN** the next reconciliation SHALL re-attempt status update

### Requirement: Controller reads Deployment status conditions
The controller SHALL read the Available condition from both claw and claw-proxy Deployment resources to determine overall readiness.

#### Scenario: Get claw Deployment
- **WHEN** updating status conditions
- **THEN** the controller SHALL fetch the Deployment resource named 'claw' in instance.Namespace
- **THEN** if Deployment is not found, controller SHALL treat it as not ready

#### Scenario: Get claw-proxy Deployment
- **WHEN** updating status conditions
- **THEN** the controller SHALL fetch the Deployment resource named 'claw-proxy' in instance.Namespace
- **THEN** if Deployment is not found, controller SHALL treat it as not ready

#### Scenario: Parse Available condition from Deployment
- **WHEN** Deployment is fetched successfully
- **THEN** the controller SHALL search deployment.Status.Conditions for condition with Type="Available"
- **THEN** if condition is found and Status="True", deployment is considered ready
- **THEN** if condition is not found or Status!="True", deployment is considered not ready

### Requirement: Controller sets Available condition based on deployment readiness
The controller SHALL set the Claw Available condition to True only when both deployments are ready, otherwise False.

#### Scenario: Both deployments ready sets Available True
- **WHEN** claw Deployment has Available=True
- **WHEN** claw-proxy Deployment has Available=True
- **THEN** the controller SHALL set Claw Available condition with status=True, reason=Ready

#### Scenario: Any deployment not ready sets Available False
- **WHEN** either claw or claw-proxy Deployment does not have Available=True
- **THEN** the controller SHALL set Claw Available condition with status=False, reason=Provisioning
- **THEN** the message SHALL indicate which deployments are pending

### Requirement: Controller uses status subresource for updates
The controller SHALL update Claw status using the status subresource client.

#### Scenario: Update via status subresource
- **WHEN** updating status conditions
- **THEN** the controller SHALL use `r.Status().Update(ctx, instance)` where r is the controller client
- **THEN** status updates SHALL NOT update spec fields or trigger unnecessary reconciliations
