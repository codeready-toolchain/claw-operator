## Why

The current CRD name `NodePairingRequestApproval` is misleading - it handles device pairing requests, not node pairing. Renaming to `DevicePairingRequest` improves API clarity and accurately reflects the resource's purpose.

## What Changes

- Rename CRD from `NodePairingRequestApproval` to `DevicePairingRequest`
- Rename API types in `api/v1alpha1/` package
- Rename controller from `NodePairingRequestApprovalReconciler` to `DevicePairingRequestReconciler`
- Update all references in code, tests, manifests, and documentation
- Update CRD file names and generated manifests
- Preserve API group (`claw.sandbox.redhat.com/v1alpha1`) and behavior

## Capabilities

### New Capabilities
<!-- None - this is a rename, not a new capability -->

### Modified Capabilities
- `node-pairing-request-approval`: Rename to `device-pairing-request` to reflect accurate resource purpose. Functionality remains unchanged.

## Impact

**Breaking Change**: API users must update their manifests and client code to use the new CRD name.

**Affected Areas**:
- `api/v1alpha1/nodepairingrequestapproval_types.go` - API type definitions
- `internal/controller/nodepairingrequestapproval_controller.go` - Controller implementation
- `config/crd/bases/` - Generated CRD manifests
- `config/rbac/` - RBAC manifests
- `CLAUDE.md` - Documentation
- All test files referencing the CRD
