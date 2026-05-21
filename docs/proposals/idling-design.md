# Claw Instance Idling

**Status:** Final

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

## Architecture / How It Works

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

### Integration with External Idlers

The Claw operator creates resources with an ownership chain:
`Claw CR → Deployment → ReplicaSet → Pod`. External workload idlers (such as
the DevSandbox member-operator idler) walk this chain from pod to top-level
owner. For them to idle Claw instances via `spec.idle` rather than directly
scaling the Deployment, they must add a handler for the `Claw` kind — analogous
to how `AnsibleAutomationPlatform` is handled today:

```go
case "Claw":
    err = i.idleClaw(ctx, ownerWithGVR) // patches spec.idle: true
```

Without this idler-side change, the idler would match `Deployment` in the chain
and scale it directly — which the Claw operator would immediately revert on the
next reconcile. The idler also needs RBAC for
`claw.sandbox.redhat.com/claws` (get, list, patch).

This change is outside the scope of the claw-operator itself but is a required
companion change for environments that use workload idling.

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
- The gateway Deployment (`<instance>`)
- The proxy Deployment (`<instance>-proxy`)
- The device-pairing Deployment (`<instance>-device-pairing`)

### What Is Preserved

- PVCs (user data on `/home/node/.openclaw`)
- Secrets (gateway token, proxy CA, credential secrets)
- ConfigMaps (operator config, proxy config)
- Routes (so URLs remain stable for dashboards/bookmarks)
- Services (for Route→Service referential integrity)
- NetworkPolicies

## Core Concepts

### The `idle` Spec Field

A boolean field in `ClawSpec` that external systems set to `true` to request
idling and `false` to request unidling. The operator watches for changes to this
field and acts accordingly.

```go
type ClawSpec struct {
    // ...existing fields...

    // Idle, when set to true, instructs the operator to scale all managed
    // Deployments to zero replicas. Set to false (or omit) to run normally.
    // +optional
    // +kubebuilder:default=false
    Idle bool `json:"idle,omitempty"`
}
```

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

This approach lets:
- Simple tools check `Ready` to know if the instance is serving traffic.
- Aware tools check for the presence of `Idle=True` to distinguish
  "intentionally stopped" from "broken."

### Constants

```go
const (
    ConditionTypeIdle = "Idle"

    ConditionReasonIdle           = "Idle"
    ConditionReasonIdledByRequest = "IdledByRequest"
)
```

## Implementation Plan

Single PR covering API, controller, and tests:

1. Add `Idle` field to `ClawSpec` in `api/v1alpha1/claw_types.go`
2. Add `ConditionTypeIdle` and related reason constants
3. Run `make manifests generate` to regenerate CRD YAML and DeepCopy
4. Add early-return logic at the top of `Reconcile()` that checks `spec.idle`:
   - When `true`: fetch managed Deployments by name (gateway, proxy,
     device-pairing); for each that exists with replicas > 0, patch to
     `replicas: 0`; skip NotFound (handles CR created idle before resources
     exist); set `Idle` condition to True, `Ready` to False (reason: Idle);
     clear `status.url`; update status and return
   - When `false` (default): remove `Idle` condition if present, proceed
     with the existing full reconcile
5. Unit tests (envtest):
   - idle → deployments scaled to 0, status reflects idle
   - unidle → full reconcile runs, deployments at replicas 1
   - status transitions (Ready → Idle → Ready)
   - no-op when already idled (patch idempotency)
   - idling when deployments don't exist yet (no error)
6. E2E test (Kind cluster):
   - Create Claw, wait for Ready
   - Patch `spec.idle: true`, verify pods terminate and status is Idle
   - Patch `spec.idle: false`, verify pods come back and status is Ready
