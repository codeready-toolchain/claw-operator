## ADDED Requirements

### Requirement: OpenClawConfigMapController exists
The system SHALL provide a controller that reconciles OpenClawInstance custom resources to manage ConfigMap lifecycle.

#### Scenario: Controller is registered with manager
- **WHEN** the operator starts
- **THEN** the OpenClawConfigMapController SHALL be registered with the controller-runtime manager

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

### Requirement: Controller filters by resource name
The system SHALL configure the controller to watch all OpenClawInstance resources but only reconcile resources named "instance", skipping all other named resources.

#### Scenario: Resource named 'instance' triggers reconciliation
- **WHEN** an OpenClawInstance named 'instance' is created
- **THEN** the controller's Reconcile function SHALL process the resource and create a ConfigMap

#### Scenario: Resource with different name is skipped
- **WHEN** an OpenClawInstance with a name other than 'instance' is created
- **THEN** the controller's Reconcile function SHALL skip processing and return successfully without creating a ConfigMap

#### Scenario: Multiple resources with different names
- **WHEN** multiple OpenClawInstance resources exist with different names
- **THEN** the controller SHALL only reconcile the resource named 'instance' and skip all others

#### Scenario: Skipped resource is logged
- **WHEN** an OpenClawInstance with a name other than 'instance' is reconciled
- **THEN** the controller SHALL log that the resource is being skipped due to name mismatch

### Requirement: Controller creates ConfigMap
The system SHALL implement the Reconcile function to fetch the OpenClawInstance resource and create a ConfigMap using the manifest from `internal/manifests/configmap.yaml`.

#### Scenario: Reconciliation fetches the OpenClawInstance resource
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL fetch the OpenClawInstance resource from the API server using the namespace and name from the reconcile request

#### Scenario: Reconciliation handles resource not found
- **WHEN** the OpenClawInstance resource does not exist (e.g., deleted)
- **THEN** the Reconcile function SHALL return successfully without error and log the event

#### Scenario: Reconciliation creates ConfigMap when it doesn't exist
- **WHEN** the OpenClawInstance exists and no ConfigMap named 'openclaw-config' exists
- **THEN** the Reconcile function SHALL create a ConfigMap using the embedded manifest from `internal/manifests/configmap.yaml`

#### Scenario: Reconciliation skips creation when ConfigMap exists
- **WHEN** the OpenClawInstance exists and a ConfigMap named 'openclaw-config' already exists
- **THEN** the Reconcile function SHALL skip creation and return successfully without error

#### Scenario: Reconciliation establishes ownership
- **WHEN** creating the ConfigMap
- **THEN** the controller SHALL set the OpenClawInstance as the controller owner reference on the ConfigMap

### Requirement: Controller embeds ConfigMap manifest
The system SHALL embed the ConfigMap manifest file `internal/manifests/configmap.yaml` into the controller binary at compile time using Go's embed directive.

#### Scenario: Manifest is accessible at runtime
- **WHEN** the controller starts
- **THEN** the embedded manifest content SHALL be available without filesystem access

#### Scenario: Manifest is parsed into ConfigMap object
- **WHEN** the controller needs to create a ConfigMap
- **THEN** it SHALL parse the embedded YAML manifest into a `corev1.ConfigMap` struct using the controller-runtime serializer

### Requirement: Controller has RBAC permissions
The system SHALL generate RBAC markers and permissions for the controller to manage OpenClawInstance and ConfigMap resources.

#### Scenario: Controller can list OpenClawInstance resources
- **WHEN** the controller starts
- **THEN** it SHALL have permissions to list OpenClawInstance resources

#### Scenario: Controller can watch OpenClawInstance resources
- **WHEN** the controller is running
- **THEN** it SHALL have permissions to watch OpenClawInstance resources

#### Scenario: Controller can create ConfigMaps
- **WHEN** the controller attempts to create a ConfigMap
- **THEN** it SHALL have permissions to create ConfigMap resources in any namespace

#### Scenario: Controller can get ConfigMaps
- **WHEN** the controller checks if a ConfigMap exists
- **THEN** it SHALL have permissions to get ConfigMap resources

#### Scenario: Controller can list ConfigMaps
- **WHEN** the controller needs to query existing ConfigMaps
- **THEN** it SHALL have permissions to list ConfigMap resources

#### Scenario: Controller can watch ConfigMaps
- **WHEN** the controller sets up watches for owned resources
- **THEN** it SHALL have permissions to watch ConfigMap resources

### Requirement: ConfigMap is created in same namespace as OpenClawInstance
The system SHALL create the ConfigMap resource in the same namespace as the owning OpenClawInstance resource.

#### Scenario: Namespace matches owner
- **WHEN** an OpenClawInstance is created in namespace "test-ns"
- **THEN** the created ConfigMap SHALL also be in namespace "test-ns"

### Requirement: ConfigMap is deleted when OpenClawInstance is deleted
The system SHALL configure owner references such that Kubernetes garbage collection automatically deletes the ConfigMap when the owning OpenClawInstance is deleted.

#### Scenario: Garbage collection deletes ConfigMap
- **WHEN** an OpenClawInstance is deleted
- **THEN** the Kubernetes API server SHALL automatically delete the associated ConfigMap due to the controller owner reference

### Requirement: Controller watches ConfigMap resources
The system SHALL configure the controller to watch ConfigMap resources that are owned by OpenClawInstance resources.

#### Scenario: Controller triggers on ConfigMap create
- **WHEN** a ConfigMap with an owner reference to an OpenClawInstance is created
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on ConfigMap update
- **WHEN** a ConfigMap with an owner reference to an OpenClawInstance is updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: Controller triggers on ConfigMap delete
- **WHEN** a ConfigMap with an owner reference to an OpenClawInstance is deleted
- **THEN** the controller's Reconcile function SHALL be invoked

### Requirement: Controller filters ConfigMap watch by name
The system SHALL configure the ConfigMap watch to only trigger reconciliation for ConfigMaps named "openclaw-config", ignoring all other ConfigMaps.

#### Scenario: ConfigMap named 'openclaw-config' triggers reconciliation
- **WHEN** a ConfigMap named 'openclaw-config' is created or updated
- **THEN** the controller's Reconcile function SHALL be invoked

#### Scenario: ConfigMap with different name is ignored
- **WHEN** a ConfigMap with a name other than 'openclaw-config' is created or updated
- **THEN** the controller's Reconcile function SHALL NOT be invoked
