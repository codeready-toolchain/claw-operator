## ADDED Requirements

### Requirement: Controller watches OpenClaw resources
The OpenClawPersistentVolumeClaimReconciler controller SHALL watch OpenClaw custom resources in all namespaces and trigger reconciliation when changes occur.

#### Scenario: OpenClaw resource created triggers reconciliation
- **WHEN** an OpenClaw custom resource is created
- **THEN** the controller reconciliation loop is triggered with the resource name and namespace

#### Scenario: OpenClaw resource updated triggers reconciliation
- **WHEN** an OpenClaw custom resource is updated
- **THEN** the controller reconciliation loop is triggered

#### Scenario: OpenClaw resource deleted triggers reconciliation
- **WHEN** an OpenClaw custom resource is deleted
- **THEN** the controller reconciliation loop is triggered to handle cleanup

### Requirement: Controller filters by instance name
The controller SHALL only reconcile OpenClaw resources named 'instance' and skip all other names.

#### Scenario: Reconcile OpenClaw named 'instance'
- **WHEN** an OpenClaw resource named 'instance' is created or updated
- **THEN** the controller proceeds with PVC creation logic

#### Scenario: Skip OpenClaw with different name
- **WHEN** an OpenClaw resource with a name other than 'instance' is created
- **THEN** the controller logs a skip message and returns without creating a PVC

### Requirement: Controller creates PVC from template
The controller SHALL create a PersistentVolumeClaim based on the manifest template in `internal/manifests/pvc.yaml`.

#### Scenario: PVC created from embedded manifest
- **WHEN** reconciling an OpenClaw named 'instance'
- **THEN** the controller decodes the embedded PVC manifest from the assets package
- **THEN** the controller creates a PVC with the specification from the manifest

#### Scenario: PVC namespace matches OpenClaw namespace
- **WHEN** creating a PVC for an OpenClaw resource
- **THEN** the PVC namespace SHALL be set to match the OpenClaw resource's namespace

### Requirement: Controller sets owner reference
The controller SHALL set the OpenClaw instance as the controller owner reference on the created PVC.

#### Scenario: Owner reference set on PVC creation
- **WHEN** creating a PVC for an OpenClaw resource
- **THEN** the controller sets a controller owner reference pointing to the OpenClaw instance
- **THEN** the PVC will be automatically garbage collected when the OpenClaw is deleted

### Requirement: Controller handles idempotent creation
The controller SHALL handle the case where the PVC already exists without returning an error.

#### Scenario: PVC already exists
- **WHEN** attempting to create a PVC that already exists
- **THEN** the controller treats this as success and returns without error
- **THEN** the controller logs that the PVC already exists

### Requirement: Controller handles resource not found
The controller SHALL handle the case where the OpenClaw resource is not found during reconciliation.

#### Scenario: OpenClaw deleted before reconciliation
- **WHEN** the OpenClaw resource is not found during reconciliation
- **THEN** the controller logs that the resource was deleted
- **THEN** the controller returns without error (no requeue)

### Requirement: Controller logs reconciliation events
The controller SHALL log key reconciliation events for observability.

#### Scenario: Log reconciliation start
- **WHEN** reconciliation begins
- **THEN** the controller logs the OpenClaw name and namespace being reconciled

#### Scenario: Log PVC creation success
- **WHEN** a PVC is successfully created
- **THEN** the controller logs a success message

#### Scenario: Log PVC creation failure
- **WHEN** PVC creation fails with an error other than AlreadyExists
- **THEN** the controller logs the error
- **THEN** the controller returns the error to trigger retry

### Requirement: Controller registers with manager
The controller SHALL register with the controller-runtime manager and configure appropriate predicates.

#### Scenario: Controller registered with manager
- **WHEN** the controller's SetupWithManager is called
- **THEN** the controller registers to watch OpenClaw resources as the primary resource
- **THEN** the controller registers to own PersistentVolumeClaim resources
- **THEN** the controller is named "openclawpersistentvolumeclaim" for identification

#### Scenario: PVC name predicate configured
- **WHEN** setting up watches for owned PVCs
- **THEN** the controller configures a predicate to filter PVC events by name "openclaw-home-pvc"

### Requirement: Controller has RBAC permissions
The controller SHALL have necessary RBAC permissions defined via kubebuilder annotations.

#### Scenario: OpenClaw resource permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, and watch permissions for OpenClaw resources

#### Scenario: PVC permissions
- **WHEN** the controller is deployed
- **THEN** RBAC rules grant get, list, watch, and create permissions for PersistentVolumeClaim resources
