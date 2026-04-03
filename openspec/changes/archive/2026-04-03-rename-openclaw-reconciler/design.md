## Context

The OpenClaw operator uses a unified Kustomize-based controller that manages multiple Kubernetes resources (ConfigMap, PVC, Deployment) atomically using server-side apply. The current naming `OpenClawReconciler` doesn't clearly indicate that this is a resource-oriented reconciler managing a set of resources.

The codebase follows kubebuilder/operator-sdk conventions where reconcilers are typically named after what they reconcile. Since this reconciler manages multiple Kubernetes resources as a coordinated set, `OpenClawResourceReconciler` better reflects its purpose.

## Goals / Non-Goals

**Goals:**
- Rename the reconciler struct and file to improve code clarity
- Maintain all existing functionality without behavioral changes
- Update all references in test files and main.go

**Non-Goals:**
- Change reconciliation logic or resource management behavior
- Modify CRD definitions, RBAC, or external APIs
- Refactor controller implementation beyond the rename

## Decisions

### Decision: Rename struct to OpenClawResourceReconciler
**Rationale**: The "Resource" qualifier makes explicit that this reconciler manages Kubernetes resources (not just a single OpenClaw CR), aligning with the controller's actual behavior of managing ConfigMap, PVC, and Deployment as a unified set.

**Alternative considered**: `OpenClawUnifiedReconciler` - Rejected because "unified" describes the implementation approach (Kustomize-based) rather than what it reconciles.

### Decision: Rename file to openclaw_resource_controller.go
**Rationale**: File naming should match the primary struct name for consistency. Kubebuilder convention is `<resource>_controller.go`.

**Alternative considered**: Keep original filename - Rejected to maintain consistency between struct and file names, making the codebase easier to navigate.

## Risks / Trade-offs

**[Risk]** In-flight PRs or branches may have merge conflicts in affected files
→ **Mitigation**: This is a straightforward rename with clear before/after state. Conflicts will be easy to resolve. Coordinate with team if active PRs exist.

**[Trade-off]** Git history tracking may be disrupted if file rename not detected
→ **Mitigation**: Ensure file rename is done atomically (delete old + create new in same commit) so git can track it. Use `git log --follow` to trace history across rename.
