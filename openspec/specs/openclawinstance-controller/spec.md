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
The system SHALL configure the controller to watch all OpenClawInstance resources in all namespaces, not just a specific named resource.

#### Scenario: Multiple resources trigger reconciliation
- **WHEN** multiple OpenClawInstance resources exist with different names
- **THEN** the controller SHALL reconcile each resource independently

#### Scenario: Resource named 'instance' triggers reconciliation
- **WHEN** an OpenClawInstance named 'instance' is created
- **THEN** the controller's Reconcile function SHALL be invoked

### Requirement: Reconcile function is a no-op skeleton
The system SHALL implement the Reconcile function as a no-op that logs the event and returns successfully without performing any actions.

#### Scenario: Reconciliation succeeds without errors
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL return `(ctrl.Result{}, nil)` without errors

#### Scenario: Reconciliation logs the event
- **WHEN** the Reconcile function is invoked
- **THEN** it SHALL log the reconciliation event with the resource name and namespace

#### Scenario: No resources are fetched
- **WHEN** the Reconcile function executes
- **THEN** it SHALL NOT attempt to fetch the OpenClawInstance resource from the API server (minimal skeleton implementation)

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
