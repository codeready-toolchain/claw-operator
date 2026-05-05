## Context

The ClawDevicePairingRequest CRD was introduced to handle device pairing approvals for Claw instances. Currently, it only contains a `requestID` field, which uniquely identifies a pairing request but doesn't provide a way for the controller to locate the specific Claw pod that should handle the approval.

In a multi-instance environment, there may be multiple Claw pods running in the same namespace, each with different label sets. The controller needs a way to target the correct pod when processing a device pairing request.

## Goals / Non-Goals

**Goals:**
- Add a label selector field to ClawDevicePairingRequest.Spec to enable pod targeting
- Make the selector field required to ensure every pairing request can identify its target
- Provide clear validation errors when selectors are invalid or missing
- Enable the controller to query pods using standard Kubernetes label matching

**Non-Goals:**
- Automatic selector population (users must specify the selector when creating the CR)
- Fallback mechanisms if no pod matches the selector (fail fast with clear error)
- Support for field selectors (only label selectors are needed)

## Decisions

### Decision 1: Use metav1.LabelSelector type
Use the standard Kubernetes `metav1.LabelSelector` type for the selector field.

**Rationale**:
- Standard Kubernetes type that's well-understood and documented
- Supports both `matchLabels` (simple equality) and `matchExpressions` (complex set-based matching)
- Integrates seamlessly with client-go's label selector conversion utilities
- Provides built-in JSON/YAML serialization

**Alternative considered**: Custom selector struct
- Rejected because it would duplicate Kubernetes' existing functionality and reduce compatibility with standard tooling

### Decision 2: Make selector field required
Mark the selector field as required using kubebuilder validation tags.

**Rationale**:
- Every pairing request must target a specific Claw instance
- No reasonable default selector exists (can't assume label values)
- Explicit is better than implicit - forces users to think about which instance they're targeting
- Prevents orphaned pairing requests that can't be processed

**Alternative considered**: Optional with fallback to namespace-wide search
- Rejected because it could cause ambiguity in multi-instance scenarios and make the behavior unpredictable

### Decision 3: Controller uses client-go's label selector matching
Convert the selector to labels.Selector using standard client-go utilities and use it in List calls.

**Rationale**:
- Leverages Kubernetes' battle-tested selector matching logic
- Enables efficient server-side filtering via ListOptions
- Consistent with how other Kubernetes controllers work (Deployments, Services, etc.)

**Implementation approach**:
```go
selector, err := metav1.LabelSelectorAsSelector(&pairingRequest.Spec.Selector)
if err != nil {
    return reconcile.Result{}, fmt.Errorf("invalid selector: %w", err)
}

podList := &corev1.PodList{}
err = r.List(ctx, podList, &client.ListOptions{
    Namespace:     pairingRequest.Namespace,
    LabelSelector: selector,
})
```

### Decision 4: Breaking change with migration documentation
Accept that this is a breaking change requiring migration of existing CRs.

**Rationale**:
- The CRD is newly introduced and not yet widely used (based on recent rename from NodePairingRequestApproval)
- Adding a required field is cleaner than making it optional and handling missing selectors
- Clear migration path: users add selector field before upgrading CRD

**Alternative considered**: Make selector optional initially, deprecate later
- Rejected because it adds complexity and delays the inevitable

## Risks / Trade-offs

**Risk**: Existing ClawDevicePairingRequest resources will fail validation after CRD upgrade
→ **Mitigation**: Document clear migration steps in CLAUDE.md. The CRD was recently renamed, so adoption is likely minimal.

**Risk**: Users may specify selectors that match zero pods
→ **Mitigation**: Controller should detect this case and set a condition with a clear error message (e.g., "No pods match selector")

**Risk**: Users may specify selectors that match multiple pods
→ **Mitigation**: Controller should fail with an error condition if more than one pod matches (pairing request must target exactly one pod)

**Trade-off**: Requiring a selector adds verbosity to CR creation
→ **Accepted**: Explicitness is valuable for operational clarity and debugging

## Migration Plan

1. Update CRD with the new selector field
2. Run `make manifests` to regenerate CRD YAML
3. Document in CLAUDE.md that users must update existing ClawDevicePairingRequest CRs to add a selector before upgrading
4. Apply updated CRD to cluster
5. Existing CRs without selector will fail validation until updated

**Rollback**: Revert CRD to previous version if needed. Resources created with selector will continue to work (extra fields are ignored by older CRDs).

## Open Questions

None - the design is straightforward and builds on standard Kubernetes patterns.
