## Context

The current CRD is named `OpenClawInstance` with kind `OpenClawInstance`. Since the operator only manages a single singleton resource named "instance", the "Instance" suffix is redundant. The Go types, CRD file names, and all references throughout the codebase reflect this verbose naming.

The codebase currently has:
- Go type: `OpenClawInstance` in `api/v1alpha1/openclawinstance_types.go`
- CRD file: `openclawinstances.openclaw.sandbox.redhat.com.yaml`
- Controllers watching `OpenClawInstance`
- Tests using `OpenClawInstance`
- Sample manifests with `kind: OpenClawInstance`

This is a breaking change that requires updating all references from `OpenClawInstance` to `OpenClaw`.

## Goals / Non-Goals

**Goals:**
- Rename Custom Resource Definition from `OpenClawInstance` to `OpenClaw`
- Update all Go types, imports, and references throughout the codebase
- Regenerate CRD manifests with the new name
- Update all tests to use the new type name
- Maintain all existing functionality and behavior

**Non-Goals:**
- Providing migration path from old CRD to new CRD (breaking change accepted)
- Backward compatibility with `OpenClawInstance` resources
- Changing the singleton pattern (still only reconcile resource named "instance")

## Decisions

### Decision 1: Direct rename without migration support
**Rationale:** Since this is an early-stage operator and the singleton pattern means users only have one resource named "instance", a clean break is simpler than maintaining dual CRD support. The breaking change is acceptable at this stage.

**Alternatives considered:**
- Support both CRDs temporarily with deprecation warning: Adds complexity for minimal benefit in early development
- Use conversion webhooks: Over-engineered for a simple name change

### Decision 2: Systematic file-by-file renaming
**Approach:** Rename Go types first, then regenerate manifests, then update tests

**Sequence:**
1. Rename `api/v1alpha1/openclawinstance_types.go` → `api/v1alpha1/openclaw_types.go`
2. Update all type definitions: `OpenClawInstance` → `OpenClaw`, `OpenClawInstanceList` → `OpenClawList`
3. Run `make manifests` to regenerate CRD files with new names
4. Update controller imports and type references
5. Update test files
6. Update sample manifests
7. Run `make generate` to update DeepCopy methods

**Rationale:** This order minimizes errors by regenerating code after type changes, ensuring consistency.

### Decision 3: Keep group and API version unchanged
The CRD group remains `openclaw.sandbox.redhat.com` and version remains `v1alpha1`. Only the Kind and resource name change.

**Rationale:** Changing the group would require more extensive updates to RBAC, webhooks, and deployment manifests. The current group already reflects "openclaw" semantically.

## Risks / Trade-offs

**[Risk]** Existing OpenClawInstance resources in clusters will become orphaned
- **Mitigation:** Document breaking change clearly. Since this is early development and uses singleton pattern, impact is minimal. Users must delete old CRD and recreate with new name.

**[Risk]** Generated code might reference old type names if generation is incomplete
- **Mitigation:** Run full `make generate && make manifests` after type changes. Verify with grep for remaining "OpenClawInstance" references.

**[Risk]** RBAC rules might still reference openclawinstances resource
- **Mitigation:** Kubebuilder markers will regenerate correct RBAC when running `make manifests`. Manually verify config/rbac/role.yaml uses "openclaws" resource name.

**[Trade-off]** Breaking change versus clean codebase
- **Accepted:** Clean naming now saves ongoing maintenance cost. Early development phase is ideal time for breaking changes.
