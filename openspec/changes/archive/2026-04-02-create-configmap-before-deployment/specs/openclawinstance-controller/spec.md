## MODIFIED Requirements

### Requirement: Controller reconciles all OpenClawInstance resources
The system SHALL configure the controller to watch all OpenClawInstance resources but only reconcile resources named "instance", skipping all other named resources.

#### Scenario: Resource named 'instance' triggers reconciliation
- **WHEN** an OpenClawInstance named 'instance' is created
- **THEN** the controller's Reconcile function SHALL process the resource and create a ConfigMap

#### Scenario: Resource with different name is skipped
- **WHEN** an OpenClawInstance with a name other than 'instance' is created
- **THEN** the controller's Reconcile function SHALL skip processing and return successfully without creating any resources

#### Scenario: Multiple resources with different names
- **WHEN** multiple OpenClawInstance resources exist with different names
- **THEN** the controller SHALL only reconcile the resource named 'instance' and skip all others

#### Scenario: Skipped resource is logged
- **WHEN** an OpenClawInstance with a name other than 'instance' is reconciled
- **THEN** the controller SHALL log that the resource is being skipped due to name mismatch

### Requirement: Reconcile function is a no-op skeleton
The system SHALL implement the Reconcile function to fetch the OpenClawInstance resource and create resources in the following order: ConfigMap first using the manifest from `internal/manifests/configmap.yaml`, then Deployment using the manifest from `internal/manifests/deployment.yaml` only after the ConfigMap exists.

#### Scenario: Reconciliation fetches the OpenClawInstance resource
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL fetch the OpenClawInstance resource from the API server using the namespace and name from the reconcile request

#### Scenario: Reconciliation handles resource not found
- **WHEN** the OpenClawInstance resource does not exist (e.g., deleted)
- **THEN** the Reconcile function SHALL return successfully without error and log the event

#### Scenario: Reconciliation creates ConfigMap when it doesn't exist
- **WHEN** the OpenClawInstance exists and no ConfigMap exists for it
- **THEN** the Reconcile function SHALL create a ConfigMap using the embedded manifest from `internal/manifests/configmap.yaml`

#### Scenario: Reconciliation skips ConfigMap creation when ConfigMap exists
- **WHEN** the OpenClawInstance exists and a ConfigMap already exists
- **THEN** the Reconcile function SHALL skip ConfigMap creation and proceed to check for Deployment

#### Scenario: Reconciliation creates Deployment when ConfigMap exists
- **WHEN** the OpenClawInstance exists and ConfigMap 'openclaw-config' exists but no Deployment exists
- **THEN** the Reconcile function SHALL create a Deployment using the embedded manifest from `internal/manifests/deployment.yaml`

#### Scenario: Reconciliation skips Deployment creation when ConfigMap doesn't exist
- **WHEN** the OpenClawInstance exists but ConfigMap 'openclaw-config' does not exist yet
- **THEN** the Reconcile function SHALL skip Deployment creation and return successfully

#### Scenario: Reconciliation skips Deployment creation when Deployment exists
- **WHEN** the OpenClawInstance exists and a Deployment already exists
- **THEN** the Reconcile function SHALL skip Deployment creation and return successfully without error

#### Scenario: Reconciliation establishes ownership on ConfigMap
- **WHEN** creating the ConfigMap
- **THEN** the controller SHALL set the OpenClawInstance as the controller owner reference on the ConfigMap

#### Scenario: Reconciliation establishes ownership on Deployment
- **WHEN** creating the Deployment
- **THEN** the controller SHALL set the OpenClawInstance as the controller owner reference on the Deployment

### Requirement: Controller creates Deployment after ConfigMap exists
The system SHALL create a Deployment using the embedded manifest after detecting that the 'openclaw-config' ConfigMap exists.

#### Scenario: Deployment is created when ConfigMap exists
- **WHEN** the Reconcile function detects that the 'openclaw-config' ConfigMap exists
- **THEN** it SHALL create a Deployment using the embedded manifest from `internal/manifests/deployment.yaml`

#### Scenario: Deployment creation is skipped when ConfigMap does not exist
- **WHEN** the Reconcile function runs but the 'openclaw-config' ConfigMap does not exist yet
- **THEN** it SHALL skip Deployment creation and complete successfully

#### Scenario: Deployment creation is idempotent
- **WHEN** the Deployment already exists
- **THEN** the controller SHALL skip creation and return successfully without error

## ADDED Requirements

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
