## Why

When a Claw instance is idled (`spec.idle=true`), the `claw_instance_status` metric currently reports `status="provisioning"` because the `ConditionReasonIdle` falls through to the default case in `conditionReasonToStatus()`. This is misleading -- an idled instance is not provisioning. Adding a dedicated `"idled"` value to the `status` label lets operators distinguish idle instances from genuinely provisioning ones in dashboards and alerts.

## What Changes

- Add `"idled"` as a fourth possible value for the `status` label on the `claw_instance_status` gauge.
- Map `ConditionReasonIdle` to `status="idled"` in the `conditionReasonToStatus()` function instead of falling through to the default `"provisioning"`.
- Update unit tests and e2e tests to verify the new status value.

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `operator-prometheus-metrics`: The `status` label on `claw_instance_status` gains a fourth value `"idled"` when the instance is idle. The existing three values are unchanged.

## Impact

- **Code**: `internal/controller/claw_operator_metrics.go` (new constant, updated switch), `internal/controller/claw_operator_metrics_test.go` (updated tests), `test/e2e/e2e_test.go` (idle metric assertion).
- **Observability**: Existing Prometheus queries or alerts that assume exactly three status values may need updating (e.g., `sum by (status)` will now include `"idled"`).
- **APIs/Dependencies**: No API or dependency changes.
