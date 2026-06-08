## MODIFIED Requirements

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
