## Why

When the operator restarts, in-memory Prometheus gauge metrics (`claw_instance_status`, `claw_instance_info`) are lost. They only get repopulated when each Claw resource happens to be reconciled due to a change event. This creates an observability gap where Prometheus dashboards and alerts show no data for existing Claw instances until something triggers their reconciliation.

## What Changes

- Add a startup routine that lists all existing Claw resources and records their metrics before the controller starts processing events
- This ensures the `/metrics` endpoint immediately reflects the correct state of all Claw instances after an operator restart

## Capabilities

### New Capabilities

- `metrics-startup-refresh`: On operator startup, list all existing Claw resources and populate gauge metrics so they are available immediately on the `/metrics` endpoint

### Modified Capabilities

- `operator-prometheus-metrics`: Add requirement that metrics are restored on operator startup, not only during reconciliation

## Impact

- `internal/controller/claw_operator_metrics.go` — add startup refresh function that lists Claw resources and calls `recordClawMetrics()` for each
- `internal/controller/claw_resource_controller.go` or `cmd/main.go` — wire the startup routine into the manager lifecycle
- No API changes, no CRD changes, no breaking changes
