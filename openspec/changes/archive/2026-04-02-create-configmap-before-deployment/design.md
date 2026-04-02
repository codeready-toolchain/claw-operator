## Context

Currently, the OpenClawInstance controller creates resources in this order:
1. Deployment (from `internal/manifests/deployment.yaml`)
2. ConfigMap (from `internal/manifests/configmap.yaml`) - only after Deployment exists

This means the Deployment may start before its configuration is available. We need to reverse this order to ensure configuration exists before the application starts.

The controller already:
- Filters reconciliation to only process OpenClawInstance resources named "instance"
- Embeds both manifest files at compile time
- Uses owner references for garbage collection
- Watches Deployments with name-based predicate filtering

## Goals / Non-Goals

**Goals:**
- Create ConfigMap immediately when reconciling an OpenClawInstance named 'instance'
- Only create Deployment after confirming ConfigMap 'openclaw-config' exists
- Maintain existing owner reference and garbage collection behavior
- Preserve existing watches and filtering logic

**Non-Goals:**
- Changing which resources are created (still just ConfigMap and Deployment)
- Modifying manifest contents
- Changing name-based filtering for OpenClawInstance or Deployment resources
- Adding new resource types

## Decisions

### Decision 1: Reverse creation order in single Reconcile function

**Choice:** Modify the existing Reconcile function to:
1. Check if ConfigMap exists; if not, create it and return
2. On next reconcile (triggered by ConfigMap creation), check if Deployment exists; if not, create it

**Rationale:** Maintains the existing two-step reconciliation pattern but reverses the order. Controller watches already trigger reconciliation when owned resources are created.

**Alternative considered:** Create both in order within a single reconcile pass.
- **Rejected because:** Less robust - if ConfigMap creation succeeds but Deployment creation fails, we'd need complex retry logic. The current watch-driven pattern is simpler.

### Decision 2: Add ConfigMap watch

**Choice:** Add a watch on ConfigMap resources (similar to the existing Deployment watch) filtered by name 'openclaw-config'.

**Rationale:** Ensures reconciliation is triggered when the ConfigMap is created, allowing the controller to proceed to Deployment creation.

**Alternative considered:** Requeue with a delay instead of watching.
- **Rejected because:** Watch-based reconciliation is more responsive and aligned with the existing Deployment watch pattern.

### Decision 3: Keep existing owner references

**Choice:** Both ConfigMap and Deployment continue to have OpenClawInstance as their controller owner.

**Rationale:** No change needed - garbage collection should work the same way regardless of creation order.

## Risks / Trade-offs

**Risk:** Deployment watch triggers reconciliation before ConfigMap exists
→ **Mitigation:** The Deployment watch only triggers for Deployments named 'openclaw', and we only create the Deployment after ConfigMap exists, so this race is impossible.

**Risk:** Test suite needs significant updates
→ **Mitigation:** Existing tests verify creation order explicitly. Update them to expect ConfigMap first, then Deployment.

**Trade-off:** Reconciliation now requires at least two passes to fully create all resources
→ **Accepted:** This is the existing pattern; we're just reversing the order. Performance impact is negligible.
