## 1. Metric Registration

- [x] 1.1 Create `internal/controller/claw_operator_metrics.go` with `claw_instance_status` GaugeVec (labels: `name`, `namespace`, `status`) and `claw_instance_info` GaugeVec (labels: `name`, `namespace`, `auth_mode`, `metrics_enabled`, `idle`). Register both on the controller-runtime metrics registry via `init()`.
- [x] 1.2 Define the status label constants (`ready`, `provisioning`, `failed`) and the mapping function from Ready condition reason to status label value.

## 2. Metric Update Logic

- [x] 2.1 Implement `recordClawMetrics(instance *Claw)` that reads the Ready condition reason, sets the matching status gauge to 1 and the others to 0, and sets the info gauge with current spec values. Use `DeletePartialMatch` before setting info labels to clear stale label combinations.
- [x] 2.2 Implement `clearClawMetrics(name, namespace string)` that calls `DeletePartialMatch` on both gauge vectors to remove all series for a deleted instance.

## 3. Integration into Reconciler

- [x] 3.1 Call `recordClawMetrics(instance)` from `updateStatus()` in `claw_status.go`, after the Ready condition is set but before the status subresource write.
- [x] 3.2 Call `clearClawMetrics(name, namespace)` from the reconciler's delete handling path in `claw_resource_controller.go` (when `instance.DeletionTimestamp != nil` or when the instance is not found).

## 4. Unit Tests

- [x] 4.1 Create `internal/controller/claw_operator_metrics_test.go` with tests for `recordClawMetrics`: verify correct gauge values for each Ready condition reason (Ready, Provisioning, ValidationFailed, missing condition).
- [x] 4.2 Add tests for `clearClawMetrics`: verify all series for the instance are removed from both gauge vectors after cleanup.
- [x] 4.3 Add tests for the info metric: verify label values match spec (auth_mode, metrics_enabled, idle) and that stale labels are cleaned on spec change.

## 5. E2e Tests

- [x] 5.1 Add an e2e test that creates a Claw instance, waits for reconciliation, then scrapes the operator's `/metrics` endpoint and asserts that `claw_instance_status` and `claw_instance_info` lines are present with the expected labels.
- [x] 5.2 Add an e2e test that deletes the Claw instance, then scrapes `/metrics` and asserts that the series for that instance are no longer present.
