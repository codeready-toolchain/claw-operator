## Context

The openclaw-operator currently has dedicated controllers for ConfigMap and Deployment resources, each watching OpenClaw custom resources and creating their respective Kubernetes resources. The PVC manifest (`internal/manifests/pvc.yaml`) already exists but there is no controller to manage its lifecycle. This design follows the established pattern used by `OpenClawConfigMapReconciler` and `OpenClawDeploymentReconciler`.

**Current State:**
- PVC manifest template exists at `internal/manifests/pvc.yaml`
- Existing controllers use embedded manifests from the `internal/assets` package
- Controllers filter for OpenClaw resources named 'instance'
- Controllers set owner references for garbage collection

**Constraints:**
- Must follow existing controller patterns for consistency
- Must use the same reconciliation filtering (name == 'instance')
- Must set controller owner references for automatic cleanup
- Must handle idempotency (PVC already exists scenario)

## Goals / Non-Goals

**Goals:**
- Create a dedicated PVC controller following the existing ConfigMap/Deployment controller pattern
- Automatically provision PVC when an OpenClaw named 'instance' is created
- Set proper owner references for garbage collection when OpenClaw is deleted
- Handle idempotent operations (skip if PVC already exists)
- Add necessary RBAC permissions for PVC operations

**Non-Goals:**
- Dynamic PVC sizing or configuration (uses static manifest template)
- PVC updates or modifications after creation
- Support for multiple PVCs per OpenClaw instance
- Volume expansion or resizing logic

## Decisions

### Decision 1: Dedicated Controller vs Combined Controller
**Choice:** Create a dedicated `OpenClawPersistentVolumeClaimReconciler` controller in a separate file

**Rationale:**
- Consistent with existing architecture (separate ConfigMap and Deployment controllers)
- Maintains single responsibility principle
- Easier to test and maintain independently
- Allows for future PVC-specific logic without affecting other controllers

**Alternatives Considered:**
- Combine PVC creation into existing ConfigMap controller: Rejected because it violates single responsibility and reduces clarity

### Decision 2: Controller Naming
**Choice:** Name the controller `OpenClawPersistentVolumeClaimReconciler`

**Rationale:**
- Explicit and clear about what resource type it manages
- Follows the pattern of being explicit about the Kubernetes resource type
- Consistent with other reconciler naming conventions that specify the full resource type
- Avoids ambiguity between volumes, persistent volumes, and persistent volume claims

### Decision 3: Manifest Loading Approach
**Choice:** Reuse the existing `internal/assets` package pattern with embedded manifests

**Rationale:**
- Consistent with ConfigMap and Deployment controllers
- Single source of truth for manifest templates
- Compile-time validation that manifests are included
- No external file dependencies at runtime

### Decision 4: Error Handling for AlreadyExists
**Choice:** Treat `AlreadyExists` errors as success (skip creation, return nil error)

**Rationale:**
- Consistent with ConfigMap controller behavior
- Idempotent reconciliation is required by controller-runtime pattern
- PVCs are immutable after creation (no updates needed)
- Owner reference ensures cleanup happens automatically

## Risks / Trade-offs

### Risk: PVC is not bound
**Impact:** PVC created but no PersistentVolume available to bind
**Mitigation:** This is expected Kubernetes behavior - PVC will remain Pending until a PV is available. Deployment controller will handle pod scheduling accordingly.

### Risk: Owner reference deletion delay
**Impact:** When OpenClaw is deleted, garbage collection may have a delay before removing PVC
**Mitigation:** This is standard Kubernetes behavior. The finalizer chain will clean up properly.

### Trade-off: Static PVC configuration
**Trade-off:** Using a static manifest template means PVC size and access modes cannot be configured per-instance
**Rationale:** Matches current architecture for ConfigMap and Deployment. Dynamic configuration can be added later if needed without breaking changes.

### Risk: Namespace mismatch
**Impact:** If PVC namespace isn't set correctly, creation could fail or create in wrong namespace
**Mitigation:** Explicitly set `pvc.Namespace = instance.Namespace` before creation, following the ConfigMap controller pattern.
