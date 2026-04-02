## ADDED Requirements

### Requirement: OpenClawInstance controller exists
The system SHALL provide a controller that reconciles OpenClawInstance custom resources.

#### Scenario: Controller is registered with manager
- **WHEN** the operator starts
- **THEN** the OpenClawInstance controller SHALL be registered with the controller-runtime manager

#### Scenario: Controller implements Reconciler interface
- **WHEN** examining the controller code
- **THEN** it SHALL implement the `Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)` method

### Requirement: Controller watches OpenClawInstance resources
The system SHALL configure the controller to watch for create, update, and delete events on OpenClawInstance resources.

#### Scenario: Controller triggers on create
- **WHEN** an OpenClawInstance resource is created
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on update
- **WHEN** an OpenClawInstance resource is updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on delete
- **WHEN** an OpenClawInstance resource is deleted
- **THEN** the controller's Reconcile function SHALL be invoked

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

### Requirement: Reconcile function is a no-op skeleton
The system SHALL implement the Reconcile function to fetch the OpenClawInstance resource and create a Deployment using the manifest from `internal/manifests/deployment.yaml`.

#### Scenario: Reconciliation fetches the OpenClawInstance resource
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL fetch the OpenClawInstance resource from the API server using the namespace and name from the reconcile request

#### Scenario: Reconciliation handles resource not found
- **WHEN** the OpenClawInstance resource does not exist (e.g., deleted)
- **THEN** the Reconcile function SHALL return successfully without error and log the event

#### Scenario: Reconciliation creates Deployment when it doesn't exist
- **WHEN** the OpenClawInstance exists and no Deployment exists for it
- **THEN** the Reconcile function SHALL create a Deployment using the embedded manifest from `internal/manifests/deployment.yaml`

#### Scenario: Reconciliation skips creation when Deployment exists
- **WHEN** the OpenClawInstance exists and a Deployment already exists
- **THEN** the Reconcile function SHALL skip creation and return successfully without error

#### Scenario: Reconciliation establishes ownership
- **WHEN** creating the Deployment
- **THEN** the controller SHALL set the OpenClawInstance as the controller owner reference on the Deployment

### Requirement: Controller has RBAC permissions
The system SHALL generate RBAC markers and permissions for the controller to watch and reconcile OpenClawInstance resources.

#### Scenario: Controller can list OpenClawInstance resources
- **WHEN** the controller starts
- **THEN** it SHALL have permissions to list OpenClawInstance resources

#### Scenario: Controller can watch OpenClawInstance resources
- **WHEN** the controller is running
- **THEN** it SHALL have permissions to watch OpenClawInstance resources

#### Scenario: Controller can update OpenClawInstance status
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

### Requirement: Deployment is created in same namespace as OpenClawInstance
The system SHALL create the Deployment resource in the same namespace as the owning OpenClawInstance resource.

#### Scenario: Namespace matches owner
- **WHEN** an OpenClawInstance is created in namespace "test-ns"
- **THEN** the created Deployment SHALL also be in namespace "test-ns"

### Requirement: Deployment is deleted when OpenClawInstance is deleted
The system SHALL configure owner references such that Kubernetes garbage collection automatically deletes the Deployment when the owning OpenClawInstance is deleted.

#### Scenario: Garbage collection deletes Deployment
- **WHEN** an OpenClawInstance is deleted
- **THEN** the Kubernetes API server SHALL automatically delete the associated Deployment due to the controller owner reference

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
