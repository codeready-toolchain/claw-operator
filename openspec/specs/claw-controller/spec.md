## ADDED Requirements

### Requirement: ClawInstance controller exists
The system SHALL provide a controller that reconciles ClawInstance custom resources.

#### Scenario: Controller is registered with manager
- **WHEN** the operator starts
- **THEN** the ClawInstance controller SHALL be registered with the controller-runtime manager

#### Scenario: Controller implements Reconciler interface
- **WHEN** examining the controller code
- **THEN** it SHALL implement the `Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)` method

### Requirement: Controller watches ClawInstance resources
The system SHALL configure the controller to watch for create, update, and delete events on ClawInstance resources.

#### Scenario: Controller triggers on create
- **WHEN** an ClawInstance resource is created
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on update
- **WHEN** an ClawInstance resource is updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on delete
- **WHEN** an ClawInstance resource is deleted
- **THEN** the controller's Reconcile function SHALL be invoked

### Requirement: Controller reconciles all ClawInstance resources
The system SHALL configure the controller to watch all ClawInstance resources but only reconcile resources named "instance", skipping all other named resources.

#### Scenario: Resource named 'instance' triggers reconciliation
- **WHEN** an ClawInstance named 'instance' is created
- **THEN** the controller's Reconcile function SHALL process the resource and create a ConfigMap

#### Scenario: Resource with different name is skipped
- **WHEN** an ClawInstance with a name other than 'instance' is created
- **THEN** the controller's Reconcile function SHALL skip processing and return successfully without creating any resources

#### Scenario: Multiple resources with different names
- **WHEN** multiple ClawInstance resources exist with different names
- **THEN** the controller SHALL only reconcile the resource named 'instance' and skip all others

#### Scenario: Skipped resource is logged
- **WHEN** an ClawInstance with a name other than 'instance' is reconciled
- **THEN** the controller SHALL log that the resource is being skipped due to name mismatch

### Requirement: Reconcile function is a no-op skeleton
The system SHALL implement the Reconcile function to fetch the ClawInstance resource and create resources in the following order: ConfigMap first using the manifest from `internal/manifests/configmap.yaml`, then Deployment using the manifest from `internal/manifests/deployment.yaml` only after the ConfigMap exists.

#### Scenario: Reconciliation fetches the ClawInstance resource
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL fetch the ClawInstance resource from the API server using the namespace and name from the reconcile request

#### Scenario: Reconciliation handles resource not found
- **WHEN** the ClawInstance resource does not exist (e.g., deleted)
- **THEN** the Reconcile function SHALL return successfully without error and log the event

#### Scenario: Reconciliation creates ConfigMap when it doesn't exist
- **WHEN** the ClawInstance exists and no ConfigMap exists for it
- **THEN** the Reconcile function SHALL create a ConfigMap using the embedded manifest from `internal/manifests/configmap.yaml`

#### Scenario: Reconciliation skips ConfigMap creation when ConfigMap exists
- **WHEN** the ClawInstance exists and a ConfigMap already exists
- **THEN** the Reconcile function SHALL skip ConfigMap creation and proceed to check for Deployment

#### Scenario: Reconciliation creates Deployment when ConfigMap exists
- **WHEN** the ClawInstance exists and ConfigMap 'claw-config' exists but no Deployment exists
- **THEN** the Reconcile function SHALL create a Deployment using the embedded manifest from `internal/manifests/deployment.yaml`

#### Scenario: Reconciliation skips Deployment creation when ConfigMap doesn't exist
- **WHEN** the ClawInstance exists but ConfigMap 'claw-config' does not exist yet
- **THEN** the Reconcile function SHALL skip Deployment creation and return successfully

#### Scenario: Reconciliation skips Deployment creation when Deployment exists
- **WHEN** the ClawInstance exists and a Deployment already exists
- **THEN** the Reconcile function SHALL skip Deployment creation and return successfully without error

#### Scenario: Reconciliation establishes ownership on ConfigMap
- **WHEN** creating the ConfigMap
- **THEN** the controller SHALL set the ClawInstance as the controller owner reference on the ConfigMap

#### Scenario: Reconciliation establishes ownership on Deployment
- **WHEN** creating the Deployment
- **THEN** the controller SHALL set the ClawInstance as the controller owner reference on the Deployment

### Requirement: Controller has RBAC permissions
The system SHALL generate RBAC markers and permissions for the controller to watch and reconcile ClawInstance resources.

#### Scenario: Controller can list ClawInstance resources
- **WHEN** the controller starts
- **THEN** it SHALL have permissions to list ClawInstance resources

#### Scenario: Controller can watch ClawInstance resources
- **WHEN** the controller is running
- **THEN** it SHALL have permissions to watch ClawInstance resources

#### Scenario: Controller can update ClawInstance status
- **WHEN** the controller needs to update status (in future implementations)
- **THEN** it SHALL have permissions to update the status subresource

### Requirement: Controller embeds deployment manifest
The system SHALL embed the deployment manifest file `internal/manifests/deployment.yaml` into the controller binary at compile time using Go's embed directive.

#### Scenario: Manifest is accessible at runtime
- **WHEN** the controller starts
- **THEN** the embedded manifest content SHALL be available without filesystem access

#### Scenario: Manifest is parsed into Deployment object
- **WHEN** the controller needs to create a Deployment
- **THEN** it SHALL parse the embedded YAML manifest into an `appsv1.Deployment` struct using the controller-runtime serializer

### Requirement: Controller has RBAC for Deployments
The system SHALL define RBAC permissions for the controller to create, get, list, and watch Deployment resources.

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

### Requirement: Deployment is created in same namespace as ClawInstance
The system SHALL create the Deployment resource in the same namespace as the owning ClawInstance resource.

#### Scenario: Namespace matches owner
- **WHEN** an ClawInstance is created in namespace "test-ns"
- **THEN** the created Deployment SHALL also be in namespace "test-ns"

### Requirement: Deployment is deleted when ClawInstance is deleted
The system SHALL configure owner references such that Kubernetes garbage collection automatically deletes the Deployment when the owning ClawInstance is deleted.

#### Scenario: Garbage collection deletes Deployment
- **WHEN** an ClawInstance is deleted
- **THEN** the Kubernetes API server SHALL automatically delete the associated Deployment due to the controller owner reference

### Requirement: Controller filters by resource name
The system SHALL check the ClawInstance resource name early in the reconciliation loop and skip processing if the name is not "instance".

#### Scenario: Name check occurs after fetch
- **WHEN** the Reconcile function fetches the ClawInstance resource
- **THEN** it SHALL immediately check if the resource name equals "instance" before proceeding with any other logic

#### Scenario: Name filtering returns success
- **WHEN** a resource name does not match "instance"
- **THEN** the Reconcile function SHALL return `ctrl.Result{}, nil` (success) without error

#### Scenario: Name filtering does not affect NotFound handling
- **WHEN** a resource is not found during fetch
- **THEN** the controller SHALL still handle NotFound errors before checking the name

### Requirement: Controller creates Deployment after ConfigMap exists
The system SHALL create a Deployment using the embedded manifest after detecting that the 'claw-config' ConfigMap exists.

#### Scenario: Deployment is created when ConfigMap exists
- **WHEN** the Reconcile function detects that the 'claw-config' ConfigMap exists
- **THEN** it SHALL create a Deployment using the embedded manifest from `internal/manifests/deployment.yaml`

#### Scenario: Deployment creation is skipped when ConfigMap does not exist
- **WHEN** the Reconcile function runs but the 'claw-config' ConfigMap does not exist yet
- **THEN** it SHALL skip Deployment creation and complete successfully

#### Scenario: Deployment creation is idempotent
- **WHEN** the Deployment already exists
- **THEN** the controller SHALL skip creation and return successfully without error

### Requirement: Controller watches ConfigMap resources
The system SHALL configure the controller to watch ConfigMap resources that are owned by ClawInstance resources.

#### Scenario: Controller triggers on ConfigMap create
- **WHEN** a ConfigMap with an owner reference to an ClawInstance is created
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on ConfigMap update
- **WHEN** a ConfigMap with an owner reference to an ClawInstance is updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on ConfigMap delete
- **WHEN** a ConfigMap with an owner reference to an ClawInstance is deleted
- **THEN** the controller's Reconcile function SHALL be invoked

### Requirement: Controller filters ConfigMap watch by name
The system SHALL configure the ConfigMap watch to only trigger reconciliation for ConfigMaps named "claw-config", ignoring all other ConfigMaps.

#### Scenario: ConfigMap named 'claw-config' triggers reconciliation
- **WHEN** a ConfigMap named 'claw-config' is created or updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: ConfigMap with different name is ignored
- **WHEN** a ConfigMap with a name other than 'claw-config' is created or updated
- **THEN** the controller's Reconcile function SHALL NOT be invoked
