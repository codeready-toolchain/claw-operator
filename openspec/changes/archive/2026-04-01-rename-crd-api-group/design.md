## Context

The OpenClawInstance CRD currently uses `openclaw.codeready-toolchain.com` as its API group. This was scaffolded using the initial project setup but doesn't align with Red Hat's Developer Sandbox naming conventions. The operator should use `openclaw.sandbox.redhat.com` to properly reflect its domain within the sandbox ecosystem.

The current state includes:
- API types defined with `openclaw.codeready-toolchain.com` group in `api/v1alpha1/`
- Generated CRD manifests referencing the old group
- Sample resources using the old API group
- Controller RBAC annotations with the old group
- Documentation referencing the old group

This is a fresh project in early development on a feature branch with no production deployments.

## Goals / Non-Goals

**Goals:**
- Update the API group from `openclaw.codeready-toolchain.com` to `openclaw.sandbox.redhat.com`
- Maintain the `v1alpha1` version unchanged
- Regenerate all manifests with the new API group
- Update all code, configuration, and documentation references
- Ensure the operator works correctly with the new API group

**Non-Goals:**
- Maintaining backward compatibility (this is a breaking change accepted early in development)
- Supporting dual API groups or migration paths (no production deployments exist)
- Changing the version or CRD structure beyond the API group rename

## Decisions

### Decision 1: Complete rename without backward compatibility

**Rationale:** Since the project is in early development on a feature branch with no production deployments, we can make a clean break without migration complexity. This simplifies the change and avoids carrying migration code forward.

**Alternatives considered:**
- Support both API groups temporarily: Rejected due to added complexity for zero current users
- Create a new CRD alongside the old one: Rejected as it would cause confusion and require cleanup later

### Decision 2: Update Go module imports but keep package structure

**Rationale:** The Go package path `github.com/codeready-toolchain/claw-operator/api/v1alpha1` doesn't need to change—only the Kubernetes API group constant changes. This minimizes the scope of changes while achieving the goal.

**Alternatives considered:**
- Restructure package paths to match new domain: Rejected as unnecessary—Go packages and K8s API groups are independent concepts

### Decision 3: Regenerate all manifests using kubebuilder tooling

**Rationale:** Use `make manifests` to regenerate CRDs rather than manual edits. This ensures consistency and correctness.

**Alternatives considered:**
- Manual YAML edits: Rejected due to error-prone nature and drift from code

## Risks / Trade-offs

**[Risk] Existing OpenClawInstance resources become orphaned**
→ **Mitigation:** This is a feature branch with no production deployments. Any test resources should be deleted and recreated.

**[Risk] References missed during rename**
→ **Mitigation:** Use grep to find all references to the old API group. Verify with `make test` and `make lint` that all changes are complete.

**[Risk] Generated files not updated correctly**
→ **Mitigation:** Run `make manifests generate` to ensure all generated code and manifests reflect the new API group. Verify CRD YAML contains new group name.

## Migration Plan

### Implementation Steps
1. Update group name constant in `api/v1alpha1/groupversion_info.go`
2. Update RBAC annotations in controller files
3. Run `make manifests generate` to regenerate CRDs and code
4. Update sample resources in `config/samples/`
5. Update README.md documentation
6. Verify changes with `make test` and `make lint`
7. Manual verification: `kubectl get crd` after `make install`

### Rollback Strategy
Since this is a feature branch, rollback is a simple `git reset` to before the change. No production data is at risk.

### Validation
- CRD manifest in `config/crd/bases/` contains `openclaw.sandbox.redhat.com`
- `kubectl get crd` shows `openclawinstances.openclaw.sandbox.redhat.com`
- Sample resources apply successfully with new API group
- Controller RBAC permissions reference correct group
- All tests pass

## Open Questions

None—the path forward is clear given the early development stage.
