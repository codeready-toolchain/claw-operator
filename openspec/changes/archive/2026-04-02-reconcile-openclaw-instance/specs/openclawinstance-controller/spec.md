## MODIFIED Requirements

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

## ADDED Requirements

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
