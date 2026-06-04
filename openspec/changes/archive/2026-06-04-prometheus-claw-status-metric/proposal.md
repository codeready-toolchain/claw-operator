## Why

The operator currently has no visibility into how many Claw instances exist or what state they are in. Adding Prometheus metrics lets platform teams build dashboards and alerts for provisioning failures, fleet health, and capacity planning — using the same monitoring stack already in place for OpenShift clusters.

## What Changes

- Expose a `claw_instance_status` gauge vector with labels `name`, `namespace`, and `status` (`ready`, `provisioning`, `failed`). For each instance, the current status is set to `1` and the others to `0`.
- Expose a `claw_instance_info` gauge vector with labels `name`, `namespace`, `auth_mode`, `metrics_enabled`, `idle` — set to `1` for every reconciled instance. Useful for PromQL join queries in dashboards.
- Register both metrics with the controller-runtime metrics registry so they are served on the operator's existing `/metrics` endpoint.
- Update the metrics inside `updateStatus()` on every reconcile, deriving `status` from the `Ready` condition.
- Add a finalizer to clean up stale metric series when a Claw resource is deleted.
- Add unit tests for the metric-update logic and e2e tests that verify the `/metrics` endpoint exposes the expected series.

## Capabilities

### New Capabilities
- `operator-prometheus-metrics`: Operator-level Prometheus metrics tracking Claw instance status and metadata, updated on every reconcile loop.

### Modified Capabilities
- `status-conditions`: The Ready condition's reason values (`Ready`, `Provisioning`, `ValidationFailed`) are now also used to derive the `status` label for the Prometheus metric. No new reasons are added, but the mapping must stay consistent.

## Impact

- **Code**: New file in `internal/controller/` for metric registration and update logic. Small additions to `claw_status.go` (`updateStatus`) and `claw_resource_controller.go` (finalizer handling, reconciler struct fields).
- **Dependencies**: `prometheus/client_golang` (already a transitive dependency via controller-runtime) — no new external dependencies.
- **API**: No CRD changes. Metrics are exposed on the operator pod's `/metrics` endpoint (port 8443 by default via controller-runtime).
- **Testing**: New unit tests for metric update/cleanup logic. New e2e test verifying metric scrapeability.
