## Context

The claw-operator manages Claw instances on OpenShift/Kubernetes. Each instance goes through a lifecycle reflected by the `Ready` condition: `Provisioning` → `Ready` (or `ValidationFailed` on error). Platform teams need visibility into fleet health via Prometheus, which is the standard monitoring stack on OpenShift.

The operator already has a `/metrics` endpoint served by controller-runtime's built-in metrics server (port 8443). The `prometheus/client_golang` library is available as a transitive dependency. The existing `claw_metrics.go` file handles the OTel Collector sidecar for *gateway-level* metrics — this change adds *operator-level* metrics about Claw resources themselves.

## Goals / Non-Goals

**Goals:**
- Expose `claw_instance_status` gauge reflecting each instance's current state (`ready`, `provisioning`, `failed`)
- Expose `claw_instance_info` gauge with metadata labels for PromQL joins
- Update metrics in the existing `updateStatus()` path so they are always consistent with the `Ready` condition
- Clean up stale metric series when a Claw resource is deleted
- Unit tests for metric registration, update, and cleanup logic
- E2e test verifying metric scrapeability from the operator's `/metrics` endpoint

**Non-Goals:**
- Custom metrics endpoint or port — we reuse controller-runtime's built-in server
- Gateway-level application metrics (already handled by the OTel Collector sidecar)
- Alerting rules or Grafana dashboards (downstream concern)
- Histogram/counter metrics for reconcile duration or error rates (future work)

## Decisions

### 1. Metric type: GaugeVec with status label enum

Use `prometheus.GaugeVec` with labels `{name, namespace, status}`. For each instance, the current status label is set to `1` and the others to `0`. This follows the kube-state-metrics `kube_pod_status_phase` pattern and supports both per-instance queries (`claw_instance_status{name="my-claw"}`) and aggregation (`sum by (status) (claw_instance_status)`).

**Alternative considered:** A single gauge `claw_instances_total{status}` counting instances per state. Rejected because it loses per-instance granularity and requires a reconcile-all sweep to keep counts accurate.

### 2. Status label values derived from Ready condition reason

Map the `Ready` condition's reason to metric status:
- `ConditionReasonReady` → `"ready"`
- `ConditionReasonProvisioning` → `"provisioning"`
- `ConditionReasonValidationFailed` → `"failed"`
- Any other reason or missing condition → `"provisioning"` (safe default during startup)

This keeps the metric consistent with the CRD status without introducing a separate state machine.

### 3. Info metric as a separate GaugeVec

`claw_instance_info{name, namespace, auth_mode, idle}` is always `1` for each reconciled instance. This is the standard Prometheus info-metric pattern for attaching metadata to time series via `* on (name, namespace) group_left(auth_mode, ...) claw_instance_info`.

Label values:
- `auth_mode`: `"token"` or `"password"` (from `spec.auth.mode`, default `"token"`)
- `idle`: `"true"` or `"false"` (from `spec.idle`)

### 4. Registration via controller-runtime metrics registry

Use `metrics.Registry` from `sigs.k8s.io/controller-runtime/pkg/metrics` to register the gauge vectors. This ensures the metrics appear on the operator's existing `/metrics` endpoint with no additional configuration. Registration happens at package init time via `prometheus.MustRegister`.

### 5. Metric update location: inside updateStatus()

Call a new `recordClawMetrics(instance)` function from `updateStatus()` after the `Ready` condition is set but before the status subresource write. This guarantees the metric always reflects the condition that was just computed.

### 6. Stale series cleanup on deletion

When a Claw resource is deleted, the metric series for that instance must be removed to avoid phantom "provisioning" entries. Two options:

**Chosen: DeletePartialMatch in reconcile delete path.** When the reconciler detects `instance.DeletionTimestamp != nil`, call `DeletePartialMatch(prometheus.Labels{"name": ..., "namespace": ...})` on both gauge vectors. This removes all series for that instance. No finalizer needed — the reconciler already receives a delete event and can clean up metrics before returning. If the operator restarts, stale series are naturally absent (metrics are in-memory).

**Alternative considered:** Finalizer-based cleanup. Would add a finalizer to every Claw resource and remove metrics in the finalization step. Rejected because it adds complexity (finalizer add/remove lifecycle, blocking deletion on metric cleanup) for a problem that doesn't require it — in-memory Prometheus metrics are ephemeral and don't survive operator restarts anyway.

### 7. New file: `internal/controller/claw_operator_metrics.go`

The existing `claw_metrics.go` handles the OTel Collector sidecar. To avoid confusion, operator-level Prometheus metrics go in a separate file named `claw_operator_metrics.go` with corresponding `claw_operator_metrics_test.go`.

## Risks / Trade-offs

- **[Cardinality]** One series per instance × 3 status values + 1 info series = 4 series per Claw. At typical scale (tens of instances), this is negligible. → Mitigation: no action needed at current scale.
- **[Stale series on operator crash]** If the operator crashes while a Claw is being deleted, the metric series may reappear on restart (the Claw still exists until GC). → Mitigation: acceptable — the next reconcile will set the correct value, and if the Claw is gone, no metric is emitted.
- **[Label value changes]** If `auth_mode` or `idle` changes, the old info series persists with value 0 until the operator restarts (Prometheus GaugeVec doesn't auto-expire). → Mitigation: Use `DeletePartialMatch` before setting new info labels on each reconcile to ensure only the current label combination exists.
