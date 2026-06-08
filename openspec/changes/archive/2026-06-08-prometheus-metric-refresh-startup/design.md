## Context

Prometheus gauge metrics (`claw_instance_status`, `claw_instance_info`) are stored in-memory and lost on operator restart. Currently, they are only repopulated when individual Claw resources are reconciled due to change events. This leaves a gap where dashboards and alerts have no data for existing instances.

The operator already has `recordClawMetrics()` which correctly sets gauges from a Claw instance's status and spec. The missing piece is a startup routine that lists all existing Claw resources and calls this function for each.

## Goals / Non-Goals

**Goals:**
- Populate all Claw gauge metrics immediately on operator startup
- Reuse existing `recordClawMetrics()` — no metric logic duplication
- Follow controller-runtime lifecycle patterns

**Non-Goals:**
- Changing metric definitions or label sets
- Adding new metrics
- Handling metric persistence across restarts (gauges are inherently in-memory)

## Decisions

### Use `mgr.Add(Runnable)` for startup refresh

Add a `Runnable` to the controller manager that lists all Claw resources and records their metrics. Controller-runtime runs runnables after cache sync and leader election, so the client is ready and the list call uses the informer cache (no extra API server load).

**Alternative considered:** Trigger a full reconcile for all Claw resources at startup. Rejected because it would run the entire three-phase reconciliation (including server-side apply of all resources) when we only need to read status and record metrics. Heavy and unnecessary.

**Alternative considered:** Add a one-time check inside `Reconcile()`. Rejected because reconcile is event-driven — if no events fire, metrics stay empty. Also adds branching complexity to the hot path.

### Implement as a method on `ClawResourceReconciler`

The reconciler already holds the `Client` needed to list Claw resources. Add a `RefreshMetrics(ctx)` method and a `Start(ctx)` method implementing `Runnable`. Register it with `mgr.Add(reconciler)` in `cmd/main.go`.

This keeps the metrics refresh logic co-located with the reconciler and avoids creating a separate struct.

### Log but don't fail on list errors

If the list call fails (e.g., CRD not installed), log a warning and return without error. The operator should still start — metrics will be populated on the first reconcile of each instance. This is a best-effort refresh, not a hard dependency.

## Risks / Trade-offs

- **[Small startup delay]** Listing all Claw resources adds a small delay to startup. Mitigated by using the informer cache (already synced). For typical deployments (< 100 instances), this is negligible.
- **[Race with first reconciles]** A reconcile event could fire concurrently with the startup refresh. `recordClawMetrics()` is idempotent (sets absolute gauge values, not increments), so concurrent calls produce correct results.
