## Context

The current CRD is named `DevicePairingRequest` without any operator-specific prefix. This naming pattern:
- Lacks clarity about which operator owns this resource
- Creates potential naming conflicts if multiple operators define similar resources
- Doesn't follow the established pattern where the main CRD is named `Claw`

The existing CRD has been recently implemented with automatic approval functionality. All behavior must be preserved during the rename.

## Goals / Non-Goals

**Goals:**
- Rename all Go types from `DevicePairingRequest*` to `ClawDevicePairingRequest*`
- Rename source files to match new type names
- Regenerate CRD manifests with new resource name
- Update all code references to use new names
- Preserve all existing functionality without behavioral changes

**Non-Goals:**
- Modify CRD spec or status fields
- Change controller logic or behavior
- Automatic migration of existing CRs (users must manually migrate)
- Backward compatibility with old CRD name

## Decisions

### Decision 1: Use ClawDevicePairingRequest prefix

**Choice**: Add `Claw` prefix to all type names

**Rationale**: Follows Kubernetes naming conventions where CRDs are typically prefixed with the operator name. The main CRD is already named `Claw`, so `ClawDevicePairingRequest` creates naming consistency.

**Alternatives considered**:
- Keep `DevicePairingRequest`: Rejected - doesn't address naming clarity issue
- Use `DevicePairing` only: Rejected - loses semantic meaning of "request"

### Decision 2: Rename all files to match type names

**Choice**: Rename source files from `devicepairingrequest_*` to `clawdevicepairingrequest_*`

**Rationale**: Kubernetes controller patterns typically name files after the primary type they contain. Consistency between file names and type names improves discoverability.

**Alternatives considered**:
- Keep old file names: Rejected - creates confusion when file names don't match type names
- Use shorter file name: Rejected - full name maintains traceability

### Decision 3: No automatic migration provided

**Choice**: Users must manually migrate existing CRs to the new CRD name

**Rationale**: This is a breaking change. Automatic migration would require:
- Keeping both old and new CRDs registered simultaneously
- Complex migration controller logic  
- Risk of data inconsistency

For a single-namespace operator in early development, manual migration is acceptable.

**Alternatives considered**:
- Provide migration script: Considered for future if user demand exists
- Support both names: Rejected - increases maintenance burden significantly

### Decision 4: Update CRD in place via make manifests

**Choice**: Regenerate CRD YAML manifests using controller-gen after renaming types

**Rationale**: Standard Kubebuilder workflow. The CRD generation is deterministic based on Go type markers.

**Alternatives considered**:
- Manual YAML edits: Rejected - error-prone and doesn't scale

## Risks / Trade-offs

**Risk**: Existing DevicePairingRequest CRs will become orphaned → **Mitigation**: Document migration steps in commit message and CLAUDE.md. Affected users must manually recreate CRs with new kind.

**Risk**: CI/CD pipelines may reference old CRD name → **Mitigation**: Update all manifests and documentation in same commit to ensure atomicity.

**Risk**: Git file renames may confuse history tracking → **Mitigation**: Use `git mv` for renames to preserve history. Document the rename in commit message.

**Trade-off**: Breaking change impacts existing deployments → Accepted - this is early development phase, minimal production usage expected.

## Migration Plan

### For Developers

1. Rename Go type files using `git mv`
2. Update all Go type definitions and receivers
3. Run `make generate` to regenerate DeepCopy methods
4. Run `make manifests` to regenerate CRD YAML and RBAC
5. Update main.go controller initialization
6. Update all tests
7. Update CLAUDE.md documentation
8. Verify build: `make build && make test`

### For Users

Users with existing DevicePairingRequest CRs must:

1. Export existing CRs: `kubectl get devicepairingrequests -o yaml > backup.yaml`
2. Update operator (CRD will be replaced)
3. Edit backup.yaml: change `kind: DevicePairingRequest` to `kind: ClawDevicePairingRequest`
4. Apply updated CRs: `kubectl apply -f backup.yaml`

**Rollback**: Revert to previous operator version. Old DevicePairingRequest CRD will be restored.

## Open Questions

- Should we provide a migration utility or script for users? (Defer until there's evidence of production usage)
- Should we version the CRD separately to track breaking changes? (Not needed - operator version tracks all changes)
