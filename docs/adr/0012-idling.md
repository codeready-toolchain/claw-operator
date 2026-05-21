# ADR-0012: Claw Instance Idling

**Status:** Implemented
**Date:** 2026-05-20

## Overview

Operator-managed idling allows external systems to request that a Claw instance
be scaled to zero (idled) without directly manipulating the Deployments. The
operator acts as the single source of truth for replica counts — external
controllers idle and unidle by setting a spec field on the Claw CR, and the
operator translates that intent into deployment scale changes.

This is important because the Claw operator already reconciles deployments to
`replicas: 1`. Without a dedicated idle mechanism, any external system that
scales deployments to zero would be immediately reverted on the next reconcile.

## Design Principles

1. **Operator owns deployment lifecycle** — No external system directly scales
   Claw deployments. The operator is the sole actor that sets replica counts.
2. **Simple boolean interface** — A single spec field toggles idle state; the
   operator handles the complexity of scaling multiple deployments.
3. **Data preservation** — PVCs and Secrets are never deleted during idling.
   User data and configuration persist across idle/unidle cycles.
4. **Status visibility** — The CR status clearly indicates whether the instance
   is idled, making it easy for UIs and monitoring tools to present the state.
5. **Fast unidle** — Unidling should be as fast as a normal cold start; no
   special warm-up procedures are needed beyond the standard init containers.

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Reconcile behavior when idled | Short-circuit early — only manage deployment scale and status | Fast and cheap reconcile when idled; won't fail on unrelated issues (missing secrets, Route not ready). Spec changes while idled are uncommon and the full reconcile runs naturally on unidle. Rejected: full pipeline (wasteful, can fail on unrelated issues) and selective reconcile (most complex, marginal benefit). |
| 2 | Spec field name | `idle` (bool) | Concise, intuitive for external idling systems, trivial interaction contract. Rejected: `suspended` (implies pause/resume semantics) and `replicas` (over-engineered for single-replica design, invites misuse). |
| 3 | Status representation | Both — `Idle` condition + `Ready=False, reason=Idle` | Maximum compatibility. Tools watching Ready see it's not running; tools that understand the Idle condition can distinguish "intentionally stopped" from "broken." Rejected: Idle condition only (tools watching Ready wouldn't know why) and Ready reason only (conflates intentional idle with failures). |

## Architecture

### Idle Flow

```
External System            Claw CR              Claw Operator           Deployments
      |                       |                       |                       |
      |-- PATCH spec.idle --> |                       |                       |
      |                       |-- reconcile trigger ->|                       |
      |                       |                       |-- scale to 0 -------> |
      |                       |                       |-- update status -----> |
      |                       |<-- status: Idle=True--|                       |
```

### Unidle Flow

```
External System            Claw CR              Claw Operator           Deployments
      |                       |                       |                       |
      |-- PATCH spec.idle --> |                       |                       |
      |                       |-- reconcile trigger ->|                       |
      |                       |                       |-- full reconcile ---> |
      |                       |                       |-- scale to 1 -------> |
      |                       |<-- status: Ready=True |                       |
```

### Reconcile Behavior When Idled

When `spec.idle` is `true`, the controller short-circuits the reconcile loop
early: it ensures all managed Deployments have `replicas: 0`, updates the status
to reflect the idled state, and returns without processing credentials, building
proxy config, or applying the full Kustomize stack. This minimizes unnecessary
work and avoids transient errors (e.g., trying to check Route readiness when pods
are down).

When `spec.idle` is `false` (the default — the field is omitted for normal
operation), the reconcile runs the full pipeline — credentials, proxy config,
Kustomize, server-side apply — which applies deployments with `replicas: 1`.

### What Gets Scaled Down

When idled, the operator sets `replicas: 0` on:
- The gateway Deployment
- The proxy Deployment
- The device-pairing Deployment

### What Is Preserved

- PVCs (user data)
- Secrets (gateway token, proxy CA, credential secrets)
- ConfigMaps (operator config, proxy config)
- Routes (so URLs remain stable for dashboards/bookmarks)
- Services (for Route→Service referential integrity)
- NetworkPolicies

## Configuration

External systems interact via a simple patch:

```bash
# Idle
kubectl patch claw my-instance --type=merge -p '{"spec":{"idle":true}}'

# Unidle
kubectl patch claw my-instance --type=merge -p '{"spec":{"idle":false}}'
```

### Status Representation

Two conditions work together to communicate idle state:

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: Idle
      message: "Instance is idled — set spec.idle to false to resume"
    - type: Idle
      status: "True"
      reason: IdledByRequest
      message: "Instance scaled to zero by spec.idle"
```

When unidled and running normally, the `Idle` condition is removed (absent
from the list) and Ready reports normally:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Ready
      message: "Claw instance is ready"
```

The `Idle` condition is only present when the instance is (or was recently)
idled. This follows the pattern used by `McpServersConfigured` in this
operator — conditions are added when relevant and removed when not applicable.

### Constants

| Constant | Value |
|----------|-------|
| `ConditionTypeIdle` | `"Idle"` |
| `ConditionReasonIdle` | `"Idle"` |
| `ConditionReasonIdledByRequest` | `"IdledByRequest"` |
