## Context

The `claw_instance_status` Prometheus gauge currently maps condition reasons to three status values: `ready`, `provisioning`, `failed`. When an instance is idled (`spec.idle=true`), the Ready condition reason is set to `Idle`, which falls through the default case in `conditionReasonToStatus()` and reports as `provisioning`. This misrepresents the instance state.

The idle code path already calls `recordClawMetrics()` after setting conditions, so the metric update pipeline is in place -- only the mapping function needs to change.

## Goals / Non-Goals

**Goals:**
- Accurately represent idled instances in the `claw_instance_status` metric with a distinct `status="idled"` value.
- Maintain backward compatibility for the three existing status values.

**Non-Goals:**
- Changing the `claw_instance_info` metric (it already tracks idle state via the `idle` label).
- Adding new metrics or new label dimensions.
- Changing the idle/unidle reconciliation logic.

## Decisions

**Use `"idled"` as the status value (not `"idle"`).**
The existing status values use adjective/past-participle forms describing a state the instance is *in*: `ready`, `failed`. `"idled"` follows this convention -- the instance *has been idled*. This also avoids collision with the `idle` label on `claw_instance_info`.

**Add an explicit case to `conditionReasonToStatus()` rather than changing the default.**
The default → `provisioning` fallback is a safe catch-all for genuinely unknown reasons. Adding an explicit `ConditionReasonIdle` case keeps the default as a safety net.

## Risks / Trade-offs

**Existing dashboards/alerts may not expect a fourth status value.** → This is a minor observability concern. The `"idled"` value only appears when instances are explicitly idled, which is an operator-initiated action. Document the new value in the PR description.
