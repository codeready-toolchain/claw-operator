## MODIFIED Requirements

### Requirement: OpenClawDeploymentController exists
The system SHALL provide a controller that reconciles OpenClaw custom resources to manage Deployment lifecycle.

#### Scenario: Controller is registered with manager
- **WHEN** the operator starts
- **THEN** the OpenClawDeploymentController SHALL be registered with the controller-runtime manager

#### Scenario: Controller implements Reconciler interface
- **WHEN** examining the controller code
- **THEN** it SHALL implement the `Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)` method

### Requirement: Controller watches OpenClawInstance resources
The system SHALL configure the controller to watch for create, update, and delete events on OpenClaw resources.

#### Scenario: Controller triggers on create
- **WHEN** an OpenClaw resource is created
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on update
- **WHEN** an OpenClaw resource is updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on delete
- **WHEN** an OpenClaw resource is deleted
- **THEN** the controller's Reconcile function SHALL be invoked

### Requirement: Controller filters by resource name
The system SHALL configure the controller to watch all OpenClaw resources but only reconcile resources named "instance", skipping all other named resources.

#### Scenario: Resource named 'instance' triggers reconciliation
- **WHEN** an OpenClaw named 'instance' is created
- **THEN** the controller's Reconcile function SHALL process the resource

#### Scenario: Resource with different name is skipped
- **WHEN** an OpenClaw with a name other than 'instance' is created
- **THEN** the controller's Reconcile function SHALL skip processing and return successfully without creating a Deployment

#### Scenario: Multiple resources with different names
- **WHEN** multiple OpenClaw resources exist with different names
- **THEN** the controller SHALL only reconcile the resource named 'instance' and skip all others

#### Scenario: Skipped resource is logged
- **WHEN** an OpenClaw with a name other than 'instance' is reconciled
- **THEN** the controller SHALL log that the resource is being skipped due to name mismatch

### Requirement: Controller creates Deployment only after ConfigMap exists
The system SHALL implement the Reconcile function to create a Deployment using the manifest from `internal/manifests/deployment.yaml` only after detecting that the ConfigMap named 'openclaw-config' exists.

#### Scenario: Reconciliation fetches the OpenClaw resource
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL fetch the OpenClaw resource from the API server using the namespace and name from the reconcile request

#### Scenario: Reconciliation handles resource not found
- **WHEN** the OpenClaw resource does not exist (e.g., deleted)
- **THEN** the Reconcile function SHALL return successfully without error and log the event

#### Scenario: Reconciliation checks for ConfigMap existence
- **WHEN** the OpenClaw exists
- **THEN** the controller SHALL check if the ConfigMap named 'openclaw-config' exists in the same namespace

#### Scenario: Reconciliation skips Deployment creation when ConfigMap doesn't exist
- **WHEN** the OpenClaw exists but the ConfigMap 'openclaw-config' does not exist
- **THEN** the Reconcile function SHALL skip Deployment creation and return successfully

#### Scenario: Reconciliation creates Deployment when ConfigMap exists
- **WHEN** the OpenClaw exists and ConfigMap 'openclaw-config' exists but no Deployment named 'openclaw' exists
- **THEN** the Reconcile function SHALL create a Deployment using the embedded manifest from `internal/manifests/deployment.yaml`

#### Scenario: Reconciliation skips creation when Deployment exists
- **WHEN** the OpenClaw exists and a Deployment named 'openclaw' already exists
- **THEN** the Reconcile function SHALL skip creation and return successfully without error

#### Scenario: Reconciliation establishes ownership
- **WHEN** creating the Deployment
- **THEN** the controller SHALL set the OpenClaw as the controller owner reference on the Deployment

### Requirement: Controller embeds Deployment manifest
The system SHALL embed the Deployment manifest file `internal/manifests/deployment.yaml` into the controller binary at compile time using Go's embed directive.

#### Scenario: Manifest is accessible at runtime
- **WHEN** the controller starts
- **THEN** the embedded manifest content SHALL be available without filesystem access

#### Scenario: Manifest is parsed into Deployment object
- **WHEN** the controller needs to create a Deployment
- **THEN** it SHALL parse the embedded YAML manifest into an `appsv1.Deployment` struct using the controller-runtime serializer

### Requirement: Controller has RBAC permissions
The system SHALL generate RBAC markers and permissions for the controller to manage OpenClaw, ConfigMap (read-only), and Deployment resources.

#### Scenario: Controller can list OpenClaw resources
- **WHEN** the controller starts
- **THEN** it SHALL have permissions to list OpenClaw resources

#### Scenario: Controller can watch OpenClaw resources
- **WHEN** the controller is running
- **THEN** it SHALL have permissions to watch OpenClaw resources

#### Scenario: Controller can get ConfigMaps
- **WHEN** the controller checks if a ConfigMap exists
- **THEN** it SHALL have permissions to get ConfigMap resources

#### Scenario: Controller can create Deployments
- **WHEN** the controller attempts to create a Deployment
- **THEN** it SHALL have permissions to create Deployment resources in any namespace

#### Scenario: Controller can get Deployments
- **WHEN** the controller checks if a Deployment exists
- **THEN** it SHALL have permissions to get Deployment resources

#### Scenario: Controller can list Deployments
- **WHEN** the controller needs to query existing Deployments
- **THEN** it SHALL have permissions to list Deployment resources

#### Scenario: Controller can watch Deployments
- **WHEN** the controller sets up watches for owned resources
- **THEN** it SHALL have permissions to watch Deployment resources

### Requirement: Deployment is created in same namespace as OpenClawInstance
The system SHALL create the Deployment resource in the same namespace as the owning OpenClaw resource.

#### Scenario: Namespace matches owner
- **WHEN** an OpenClaw is created in namespace "test-ns"
- **THEN** the created Deployment SHALL also be in namespace "test-ns"

### Requirement: Deployment is deleted when OpenClawInstance is deleted
The system SHALL configure owner references such that Kubernetes garbage collection automatically deletes the Deployment when the owning OpenClaw is deleted.

#### Scenario: Garbage collection deletes Deployment
- **WHEN** an OpenClaw is deleted
- **THEN** the Kubernetes API server SHALL automatically delete the associated Deployment due to the controller owner reference

### Requirement: Controller watches Deployment resources
The system SHALL configure the controller to watch Deployment resources that are owned by OpenClaw resources.

#### Scenario: Controller triggers on Deployment create
- **WHEN** a Deployment with an owner reference to an OpenClaw is created
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on Deployment update
- **WHEN** a Deployment with an owner reference to an OpenClaw is updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on Deployment delete
- **WHEN** a Deployment with an owner reference to an OpenClaw is deleted
- **THEN** the controller's Reconcile function SHALL be invoked

### Requirement: Controller filters Deployment watch by name
The system SHALL configure the Deployment watch to only trigger reconciliation for Deployments named "openclaw", ignoring all other Deployments.

#### Scenario: Deployment named 'openclaw' triggers reconciliation
- **WHEN** a Deployment named 'openclaw' is created or updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Deployment with different name is ignored
- **WHEN** a Deployment with a name other than 'openclaw' is created or updated
- **THEN** the controller's Reconcile function SHALL NOT be invoked
