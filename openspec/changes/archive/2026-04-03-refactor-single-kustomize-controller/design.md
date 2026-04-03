## Context

The openclaw-operator currently uses three separate controllers:
- `OpenClawConfigMapReconciler` - creates ConfigMap from embedded YAML
- `OpenClawPersistentVolumeClaimReconciler` - creates PVC from embedded YAML
- `OpenClawDeploymentReconciler` - creates Deployment with explicit ConfigMap dependency check

Each controller:
- Embeds a single manifest file via `//go:embed`
- Decodes it with universal deserializer
- Sets namespace and owner reference
- Creates the resource client-side
- Handles AlreadyExists idempotently

**Current State:**
- 3 controller files (~400 LOC total)
- 3 separate test files
- 3 controller registrations in main.go
- Individual RBAC marker comments
- Resource creation happens sequentially through separate reconciliation loops
- No consistent labeling strategy

**Constraints:**
- Must maintain backward compatibility (same resources created, same owner references)
- Must keep unit tests in separate files for readability
- Must only reconcile OpenClaw resources named "instance"
- Existing OpenClaw instances must continue working without manual intervention

## Goals / Non-Goals

**Goals:**
- Consolidate to single `OpenClawReconciler` controller
- Use Kustomize API for in-memory manifest building
- Apply all resources atomically via server-side apply
- Ensure all managed resources have `app.kubernetes.io/name: openclaw` label
- Simplify controller registration (1 instead of 3)
- Reduce code duplication
- Enable easier addition of new resources in the future

**Non-Goals:**
- Changing what resources are created (same ConfigMap, PVC, Deployment, etc.)
- Modifying the OpenClaw CRD schema
- Adding new features beyond consolidation
- Changing the "instance" name filter behavior
- Supporting multiple OpenClaw instances per namespace (still only "instance")

## Decisions

### Decision 1: Kustomize In-Memory Build
**Choice:** Use `sigs.k8s.io/kustomize/api` to build manifests in memory

**Rationale:**
- Maintains embedded manifest pattern (no filesystem dependencies at runtime)
- Kustomize provides native support for common labels, namespace transformation
- Industry-standard tool that operators team already understands
- Enables future enhancements (patches, strategic merge, etc.) without code changes

**Alternatives Considered:**
- Continue with individual embeds: Rejected - doesn't solve duplication or labeling
- Custom YAML merging logic: Rejected - reinvents what Kustomize does well

**Implementation:**
```go
// Embed the entire manifests directory including kustomization.yaml
//go:embed manifests
var manifestsFS embed.FS

// Use kustomize.Run() to build in-memory
kust := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
resMap, err := kust.Run(filesys.MakeFsOnDisk(), "manifests")
```

### Decision 2: Server-Side Apply
**Choice:** Use server-side apply (SSA) instead of client-side create/update

**Rationale:**
- Atomic application of all resources as a unit
- Better conflict resolution with field ownership tracking
- Idempotent by design (no need for AlreadyExists handling)
- Allows future multi-actor scenarios (user edits + operator management)
- Industry best practice for modern operators

**Alternatives Considered:**
- Client-side apply with update fallback: Rejected - more complex error handling
- Individual Create calls (current): Rejected - not atomic, race conditions possible

**Implementation:**
Use `client.Patch()` with `client.Apply` patch type and field manager name.

### Decision 3: Resource Labeling via Kustomize
**Choice:** Add `app.kubernetes.io/name: openclaw` via `commonLabels` in kustomization.yaml

**Rationale:**
- Applied consistently at build time to all resources
- No runtime code needed for label injection
- Follows Kubernetes common labels convention
- Enables easy querying: `kubectl get all -l app.kubernetes.io/name=openclaw`

**Alternatives Considered:**
- Runtime label injection: Rejected - code duplication, error-prone
- Resource-specific labels in YAML: Rejected - easy to miss, inconsistent

**kustomization.yaml:**
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

commonLabels:
  app.kubernetes.io/name: openclaw

resources:
  - configmap.yaml
  - pvc.yaml
  - deployment.yaml
```

### Decision 4: Keep Separate Test Files
**Choice:** Maintain existing test file structure (`*_test.go` per resource type)

**Rationale:**
- Preserves test readability and organization
- Each file can focus on its resource type's specific scenarios
- Easier code review and debugging
- Test coverage remains at same granularity

**Implementation:**
Each test file creates the unified `OpenClawReconciler` but focuses on asserting one resource type's behavior.

### Decision 5: Namespace and Owner Reference Handling
**Choice:** Set namespace and owner reference dynamically after Kustomize build, before apply

**Rationale:**
- Kustomize builds with placeholder/empty namespace
- Controller knows the actual namespace from the OpenClaw CR
- Owner reference must point to the specific OpenClaw instance
- Cannot be static in manifests

**Implementation:**
After `resMap` is built, iterate resources and set:
```go
for _, res := range resources {
    res.SetNamespace(instance.Namespace)
    // Set owner reference via controllerutil
}
```

### Decision 6: Remove Explicit Dependency Checks
**Choice:** Remove the explicit ConfigMap existence check from Deployment controller logic

**Rationale:**
- Server-side apply sends all resources atomically
- Kubernetes scheduler handles pod startup ordering (Deployment won't schedule pods until volumes are available)
- Simpler controller code
- If ConfigMap fails to create, entire SSA fails (no partial state)

**Alternatives Considered:**
- Keep dependency checks: Rejected - unnecessary with SSA, adds complexity

## Risks / Trade-offs

### Risk: Kustomize API Stability
**Impact:** The kustomize API is not always stable across versions
**Mitigation:** Pin to specific known-good version in go.mod. Kustomize API is widely used in Kubebuilder ecosystem and relatively stable at v1beta1.

### Risk: Server-Side Apply Field Conflicts
**Impact:** If resources were manually edited, SSA might encounter field ownership conflicts
**Mitigation:** Use force-apply on first reconciliation to claim ownership. Document that manual edits will be reverted.

### Risk: Migration from Client-Side to Server-Side Apply
**Impact:** Existing resources created via Create() might not have SSA field ownership data
**Mitigation:** SSA gracefully adopts resources via owner references. On first reconcile after upgrade, SSA will claim fields.

### Trade-off: All-or-Nothing Apply
**Trade-off:** SSA applies all resources or none. If one resource fails, none are created.
**Rationale:** This is actually safer than partial failure states. Clear error reporting for users.

### Risk: Increased Memory Usage
**Impact:** Kustomize builds entire resource set in memory before apply
**Mitigation:** Resource set is small (< 10 manifests), negligible memory impact. Much smaller than typical controller cache.

### Risk: Test Complexity
**Impact:** Tests now need to assert against multiple resources from one controller
**Mitigation:** Keep separate test files, each focusing on one resource type. No significant complexity increase.

## Migration Plan

### Pre-Deployment
1. Ensure all integration tests pass with new controller
2. Test on development cluster with existing OpenClaw instances
3. Verify resources are properly adopted (check owner references remain)

### Deployment
1. Build new operator image with consolidated controller
2. Update operator deployment via standard rollout
3. Operator restarts with new binary
4. On first reconcile, unified controller:
   - Detects existing resources via owner reference
   - Claims field ownership via SSA
   - Applies current manifests (should be no-op for unchanged fields)

### Rollback Strategy
If issues are detected:
1. Redeploy previous operator version
2. Old controllers resume managing resources
3. No data loss (resources remain unchanged)
4. Owner references remain valid

### Verification
After deployment:
1. Check all OpenClaw instances named "instance" have expected resources
2. Verify all resources have `app.kubernetes.io/name: openclaw` label
3. Check operator logs for successful reconciliations
4. Confirm metrics show single controller instead of three

## Open Questions

1. **Should we version the kustomization.yaml?** 
   - Probably not needed since it's embedded and versioned with the code

2. **Do we need a feature flag for gradual rollout?**
   - Likely not - change is self-contained and low-risk. All-at-once deployment is simpler.

3. **Should we add more resources to kustomization (Service, Route, etc.)?**
   - Out of scope for this change. Can be added incrementally after consolidation.
