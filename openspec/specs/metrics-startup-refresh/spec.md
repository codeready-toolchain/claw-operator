## ADDED Requirements

### Requirement: Metrics refreshed on operator startup
The operator SHALL list all existing Claw resources on startup and populate gauge metrics (`claw_instance_status`, `claw_instance_info`) for each instance before the controller begins processing reconcile events.

#### Scenario: Metrics restored after restart with existing instances
- **WHEN** the operator starts and there are existing Claw resources in the cluster
- **THEN** the `/metrics` endpoint SHALL return correct `claw_instance_status` and `claw_instance_info` series for all existing instances

#### Scenario: Metrics reflect current status after restart
- **WHEN** the operator restarts and a Claw instance has Ready condition with reason=Ready
- **THEN** the metric `claw_instance_status{status="ready"}` SHALL be `1` for that instance
- **THEN** the metric `claw_instance_status{status="provisioning"}` SHALL be `0` for that instance

#### Scenario: Multiple instances across namespaces
- **WHEN** the operator starts with Claw instances in different namespaces
- **THEN** metrics SHALL be populated for all instances across all namespaces

#### Scenario: No Claw resources exist
- **WHEN** the operator starts and there are no Claw resources in the cluster
- **THEN** no metric series SHALL be created
- **THEN** the operator SHALL start normally

### Requirement: Startup refresh is best-effort
The operator SHALL NOT fail to start if the metric refresh encounters an error (e.g., CRD not installed). The error SHALL be logged as a warning.

#### Scenario: CRD not installed
- **WHEN** the operator starts and the Claw CRD is not installed
- **THEN** the operator SHALL log a warning
- **THEN** the operator SHALL continue starting normally

### Requirement: Startup refresh uses controller-runtime Runnable
The operator SHALL implement the startup metric refresh as a `Runnable` registered with the controller manager, ensuring it runs after cache sync and leader election.

#### Scenario: Runs after cache sync
- **WHEN** the operator starts and the manager cache is synced
- **THEN** the metric refresh SHALL execute using the cached client (no direct API server calls)

### Requirement: Reconciliation blocked until metrics refresh completes
The controller SHALL NOT process any reconcile events until the startup metric refresh has completed. This ensures no mismatch or double-counting between the refresh and normal reconciliation.

#### Scenario: Reconcile waits for refresh
- **WHEN** a reconcile event is triggered before the metric refresh has completed
- **THEN** the reconciler SHALL block until the refresh finishes before proceeding

#### Scenario: Reconcile proceeds after refresh
- **WHEN** the metric refresh has completed
- **THEN** subsequent reconcile events SHALL proceed without delay

#### Scenario: Context cancelled while waiting
- **WHEN** a reconcile event is waiting for the metric refresh and the context is cancelled
- **THEN** the reconciler SHALL return the context error immediately

### Requirement: Unit tests cover startup refresh
The operator SHALL have unit tests verifying that the startup refresh correctly populates metrics for existing Claw resources.

#### Scenario: Test refresh with multiple instances
- **WHEN** `RefreshMetrics()` is called with existing Claw resources in different states
- **THEN** each instance's metrics SHALL be correctly populated

#### Scenario: Test refresh with no instances
- **WHEN** `RefreshMetrics()` is called with no existing Claw resources
- **THEN** no metrics SHALL be set
- **THEN** no error SHALL be returned
