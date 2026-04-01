## Context

This is a Kubernetes operator built with Operator SDK. The operator currently has scaffolding but no custom resources or controllers. This change introduces the first custom resource (`OpenClawInstance`) and its controller, establishing the foundation for the operator's reconciliation pattern.

The operator follows standard Kubernetes controller-runtime conventions:
- API types defined in `api/v1alpha1/`
- Controllers in `internal/controller/`
- CRD manifests generated to `config/crd/bases/`
- Main manager setup in `cmd/main.go`

## Goals / Non-Goals

**Goals:**
- Define OpenClawInstance CRD with minimal schema (empty spec/status for now)
- Create controller skeleton with reconcile loop registered with the manager
- Enable controller to watch and respond to OpenClawInstance resources
- Follow Operator SDK patterns and conventions

**Non-Goals:**
- Implementing actual reconciliation logic (empty spec/status means no business logic yet)
- Advanced controller features (finalizers, status conditions, etc.)
- Multi-resource watching or cross-resource dependencies
- Deployment manifests or Helm charts

## Decisions

### 1. API Version: v1alpha1
Use `v1alpha1` as the initial API version since this is the first iteration with empty spec/status.

**Rationale:** Alpha versions signal evolving APIs. Once we add fields to spec/status and stabilize the schema, we can promote to v1beta1 or v1.

**Alternatives considered:**
- v1 directly: Too early for a stable API with no fields defined yet

### 2. API Group: openclaw.codeready-toolchain.com
Follow the existing codeready-toolchain domain for consistency.

**Rationale:** Maintains alignment with the broader toolchain project structure.

### 3. Empty Spec/Status Structs
Define `OpenClawInstanceSpec` and `OpenClawInstanceStatus` as empty structs with JSON tags.

**Rationale:** Establishes the Go types and CRD structure without committing to specific fields. Fields will be added in future changes as requirements emerge.

```go
type OpenClawInstanceSpec struct {
    // Empty for now
}

type OpenClawInstanceStatus struct {
    // Empty for now
}
```

### 4. Controller Watches All OpenClawInstance Resources
Controller will watch all OpenClawInstance resources, not just one named "instance".

**Rationale:** While the user mentioned watching for a resource named "instance", Kubernetes controllers typically watch all instances of their CRD. The controller can filter or prioritize specific names in reconciliation logic if needed, but the watch should be broad. This is the standard pattern and makes the operator more flexible.

### 5. Minimal Reconcile Loop
The Reconcile function returns immediately with no-op logic for now.

**Rationale:** Establishes the wiring without implementing business logic. Future changes will add actual reconciliation behavior.

```go
func (r *OpenClawInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    log.Info("Reconciling OpenClawInstance", "name", req.Name, "namespace", req.Namespace)
    // No-op for now - reconciliation logic will be added in future changes
    return ctrl.Result{}, nil
}
```

## Risks / Trade-offs

**[Risk] Empty CRD provides no value to users** → Mitigation: This is intentional scaffolding. Document clearly that spec/status will be populated in follow-up changes.

**[Risk] Controller no-op creates confusion** → Mitigation: Add clear logging and comments indicating this is a skeleton implementation.

**[Risk] API version promotion complexity** → Mitigation: v1alpha1 clearly signals this is evolving. Conversion webhooks can be added later if needed.
