## 1. Core Implementation

- [x] 1.1 Add `RefreshMetrics(ctx context.Context) error` method to `ClawResourceReconciler` that lists all Claw resources across all namespaces and calls `recordClawMetrics()` for each instance
- [x] 1.2 Add `Start(ctx context.Context) error` method to `ClawResourceReconciler` implementing the `Runnable` interface, which calls `RefreshMetrics()` and logs a warning on error instead of returning it

## 2. Wiring

- [x] 2.1 Register the reconciler as a `Runnable` via `mgr.Add(reconciler)` in `cmd/main.go` after `SetupWithManager`

## 3. Tests

- [x] 3.1 Add unit test for `RefreshMetrics()` with multiple Claw instances in different states (ready, provisioning, failed, idled) across different namespaces — verify all metrics are correctly populated
- [x] 3.2 Add unit test for `RefreshMetrics()` with no Claw instances — verify no metrics are set and no error is returned
