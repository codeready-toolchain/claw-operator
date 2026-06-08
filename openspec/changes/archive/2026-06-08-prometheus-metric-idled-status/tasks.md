## 1. Metric mapping

- [x] 1.1 Add `metricStatusIdled = "idled"` constant in `internal/controller/claw_operator_metrics.go`
- [x] 1.2 Add `case clawv1alpha1.ConditionReasonIdle: return metricStatusIdled` to `conditionReasonToStatus()`
- [x] 1.3 Add the `metricStatusIdled` gauge set/reset in `recordClawMetrics()` alongside the existing three statuses

## 2. Unit tests

- [x] 2.1 Update `TestConditionReasonToStatus` table to expect `"idled"` for `ConditionReasonIdle` (instead of `"provisioning"`)
- [x] 2.2 Add a `TestRecordClawMetrics` subtest for idled instances: Ready condition with reason=Idle should produce `status="idled"` = 1 and all others = 0
- [x] 2.3 Run `make test` and verify all tests pass

## 3. E2E tests

- [x] 3.1 Add metric assertions in the idle e2e test: after idling, verify `claw_instance_status{status="idled"}` = 1 and `status="ready"` = 0
