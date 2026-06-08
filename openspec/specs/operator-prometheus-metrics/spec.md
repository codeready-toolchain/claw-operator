## ADDED Requirements

### Requirement: Operator exposes claw_instance_status gauge
The operator SHALL expose a Prometheus gauge vector named `claw_instance_status` with label `status`. The `status` label SHALL have exactly four possible values: `ready`, `provisioning`, `failed`, `idled`.

#### Scenario: Instance is ready
- **WHEN** a Claw instance has Ready condition with reason=Ready
- **THEN** the metric `claw_instance_status{status="ready"}` SHALL be `1`
- **THEN** the metric `claw_instance_status{status="provisioning"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="failed"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="idled"}` SHALL be `0`

#### Scenario: Instance is provisioning
- **WHEN** a Claw instance has Ready condition with reason=Provisioning
- **THEN** the metric `claw_instance_status{status="provisioning"}` SHALL be `1`
- **THEN** the metric `claw_instance_status{status="ready"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="failed"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="idled"}` SHALL be `0`

#### Scenario: Instance has failed
- **WHEN** a Claw instance has Ready condition with reason=ValidationFailed
- **THEN** the metric `claw_instance_status{status="failed"}` SHALL be `1`
- **THEN** the metric `claw_instance_status{status="ready"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="provisioning"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="idled"}` SHALL be `0`

#### Scenario: Instance has no Ready condition yet
- **WHEN** a Claw instance has no Ready condition set (e.g. first reconcile)
- **THEN** the metric SHALL default to `status="provisioning"` with value `1`

#### Scenario: Instance is idled
- **WHEN** a Claw instance has Ready condition with reason=Idle
- **THEN** the metric `claw_instance_status{status="idled"}` SHALL be `1`
- **THEN** the metric `claw_instance_status{status="ready"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="provisioning"}` SHALL be `0`
- **THEN** the metric `claw_instance_status{status="failed"}` SHALL be `0`

### Requirement: Operator exposes claw_instance_info gauge
The operator SHALL expose a Prometheus gauge vector named `claw_instance_info` with labels `auth_mode` and `idle`. The value SHALL always be `1` for each reconciled instance.

#### Scenario: Info metric reflects instance spec
- **WHEN** a Claw instance is reconciled with auth mode "token" and idle=false
- **THEN** the metric `claw_instance_info{auth_mode="token", idle="false"}` SHALL be `1`

#### Scenario: Info metric reflects password auth mode
- **WHEN** a Claw instance has spec.auth.mode="password"
- **THEN** the metric `claw_instance_info{auth_mode="password", ...}` SHALL be `1`

#### Scenario: Info metric reflects idle state
- **WHEN** a Claw instance has spec.idle=true
- **THEN** the metric `claw_instance_info{..., idle="true"}` SHALL be `1`

#### Scenario: Stale info labels cleaned on spec change
- **WHEN** a Claw instance changes from idle=false to idle=true
- **THEN** the previous series with `idle="false"` SHALL be removed
- **THEN** a new series with `idle="true"` SHALL be set to `1`

### Requirement: Metrics updated in updateStatus path
The operator SHALL update both `claw_instance_status` and `claw_instance_info` metrics every time `updateStatus()` is called during reconciliation, after the Ready condition is computed. The operator SHALL also populate these metrics for all existing Claw resources on startup.

#### Scenario: Metrics consistent with condition
- **WHEN** `updateStatus()` sets the Ready condition to reason=Ready
- **THEN** the `claw_instance_status` metric SHALL reflect `status="ready"` before the status subresource write completes

#### Scenario: Metrics updated on every reconcile
- **WHEN** the reconciler runs and calls `updateStatus()`
- **THEN** the metrics SHALL be updated regardless of whether the Ready condition changed

#### Scenario: Metrics populated on startup
- **WHEN** the operator starts with existing Claw resources
- **THEN** all metrics SHALL be populated from the current status of each Claw resource before the first reconcile event is processed

### Requirement: Metrics cleaned up on Claw deletion
The operator SHALL reset all metric series when a Claw instance is deleted.

#### Scenario: Delete resets status metric series
- **WHEN** a Claw instance is deleted
- **THEN** all `claw_instance_status` series SHALL be reset

#### Scenario: Delete resets info metric series
- **WHEN** a Claw instance is deleted
- **THEN** all `claw_instance_info` series SHALL be reset

#### Scenario: Metrics absent after operator restart
- **WHEN** the operator restarts and a previously tracked Claw instance no longer exists
- **THEN** no metric series for that instance SHALL be present (metrics are in-memory only)

### Requirement: Metrics registered on controller-runtime metrics registry
The operator SHALL register both gauge vectors on the controller-runtime metrics registry so they are served on the operator's existing `/metrics` endpoint.

#### Scenario: Metrics visible on /metrics endpoint
- **WHEN** the operator is running and a Claw instance has been reconciled
- **THEN** an HTTP GET to the operator's `/metrics` endpoint SHALL return `claw_instance_status` and `claw_instance_info` lines in Prometheus exposition format

#### Scenario: Registration at init time
- **WHEN** the controller package is loaded
- **THEN** the gauge vectors SHALL be registered via `metrics.Registry.MustRegister()`
- **THEN** registration failure SHALL cause a panic (standard prometheus behavior)

### Requirement: Unit tests cover metric logic
The operator SHALL have unit tests for the metric update and cleanup functions.

#### Scenario: Test status metric for each reason
- **WHEN** `recordClawMetrics()` is called with a Claw instance whose Ready condition reason is Ready, Provisioning, ValidationFailed, or Idle
- **THEN** the corresponding status gauge SHALL be `1` and the others SHALL be `0`

#### Scenario: Test conditionReasonToStatus maps Idle to idled
- **WHEN** `conditionReasonToStatus()` is called with `ConditionReasonIdle`
- **THEN** it SHALL return `"idled"`

#### Scenario: Test cleanup removes series
- **WHEN** `clearClawMetrics()` is called
- **THEN** all series SHALL be removed from both gauge vectors

#### Scenario: Test info metric label values
- **WHEN** `recordClawMetrics()` is called with a Claw instance
- **THEN** the info gauge labels SHALL match the instance's spec values

### Requirement: E2e test verifies metric scrapeability
The e2e test suite SHALL verify that the operator's `/metrics` endpoint serves the expected Claw metrics after reconciliation.

#### Scenario: E2e metric verification
- **WHEN** a Claw instance is created and reconciled in the e2e cluster
- **THEN** scraping the operator's metrics endpoint SHALL return `claw_instance_status` lines
- **THEN** scraping the operator's metrics endpoint SHALL return `claw_instance_info` lines
