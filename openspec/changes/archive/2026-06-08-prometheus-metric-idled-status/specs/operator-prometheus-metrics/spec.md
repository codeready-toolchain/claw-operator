## MODIFIED Requirements

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
