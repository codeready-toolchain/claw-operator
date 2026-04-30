## Context

The `NodePairingRequestApproval` CRD was originally named based on an early concept of "node pairing" but actually handles device pairing workflows. This naming mismatch creates confusion for users and developers. The codebase uses this name across:
- API type definitions in `api/v1alpha1/`
- Controller implementation in `internal/controller/`
- Generated CRD manifests
- RBAC configurations
- Documentation

Renaming requires careful coordination across code generation tools (Kubebuilder/controller-gen), Go package naming conventions, and Kubernetes CRD versioning.

## Goals / Non-Goals

**Goals:**
- Rename all occurrences of `NodePairingRequestApproval` to `DevicePairingRequest`
- Maintain consistent naming across Go types, file names, and CRD manifests
- Update generated code and manifests correctly
- Preserve existing functionality and behavior

**Non-Goals:**
- Provide backwards compatibility or migration path (this is a breaking change)
- Change the API group or version (`claw.sandbox.redhat.com/v1alpha1` remains unchanged)
- Modify the actual pairing logic or functionality
- Support both old and new names simultaneously

## Decisions

### 1. File Renaming Strategy
**Decision**: Rename source files to match the new type name following Go conventions.

**Rationale**: Go convention is to name files after their primary type using snake_case. This keeps file organization predictable.

**Changes**:
- `nodepairingrequestapproval_types.go` → `devicepairingrequest_types.go`
- `nodepairingrequestapproval_controller.go` → `devicepairingrequest_controller.go`
- Test files follow the same pattern

**Alternative Considered**: Keep file names unchanged. Rejected because it would create confusion between file names and type names.

### 2. Code Generation Approach
**Decision**: Use Kubebuilder's `make manifests` and `make generate` to regenerate all derived code.

**Rationale**: Kubebuilder code generation ensures consistency between Go types, CRD manifests, and DeepCopy methods. Manual updates are error-prone.

**Steps**:
1. Rename Go types and update kubebuilder markers
2. Run `make generate` to update `zz_generated.deepcopy.go`
3. Run `make manifests` to regenerate CRD YAML in `config/crd/bases/`
4. Verify generated CRD kind matches new name

**Alternative Considered**: Manually edit CRD YAML files. Rejected because it breaks the code generation workflow and creates maintenance burden.

### 3. String Reference Updates
**Decision**: Update all hardcoded string references to use the new name.

**Rationale**: Controller code references the kind name in various places (owner references, watches, etc.). These must match the CRD name.

**Areas to update**:
- Controller `For()` and `Owns()` declarations
- Kind constants in controller code
- Test fixtures and assertions
- Documentation in `CLAUDE.md`

### 4. Breaking Change Acknowledgment
**Decision**: Accept this as a breaking change requiring users to update their manifests.

**Rationale**: Kubernetes CRDs don't support renaming with backwards compatibility. Users must migrate by recreating resources with the new kind name.

**Impact**: Any deployed `NodePairingRequestApproval` resources will need to be recreated as `DevicePairingRequest` resources after operator upgrade.

## Risks / Trade-offs

**Risk**: Users' existing resources become orphaned after upgrade.  
**Mitigation**: Document the breaking change clearly in release notes. Provide migration guide if resources need to be preserved.

**Risk**: Incomplete renaming leaves inconsistencies in codebase.  
**Mitigation**: Search codebase for all variations (PascalCase, camelCase, kebab-case, snake_case) of the old name. Use grep and IDE search.

**Risk**: Generated code or manifests have errors after renaming.  
**Mitigation**: Run `make test` and `make lint` to verify. Check that CRD `kind` field in generated YAML matches new name exactly.

**Trade-off**: Breaking change vs backwards compatibility.  
**Accepted**: Clean break is better than maintaining dual naming or aliases. Project is early enough that breaking changes are acceptable.
