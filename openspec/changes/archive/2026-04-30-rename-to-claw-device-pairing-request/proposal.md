## Why

The current CRD name `DevicePairingRequest` lacks the operator-specific prefix, which creates potential naming conflicts and reduces clarity about which operator owns this resource. Renaming to `ClawDevicePairingRequest` aligns with Kubernetes naming best practices and makes the ownership explicit.

## What Changes

- **BREAKING**: Rename `DevicePairingRequest` CRD to `ClawDevicePairingRequest`
- Rename Go types: `DevicePairingRequest` → `ClawDevicePairingRequest`, `DevicePairingRequestSpec` → `ClawDevicePairingRequestSpec`, `DevicePairingRequestStatus` → `ClawDevicePairingRequestStatus`, `DevicePairingRequestList` → `ClawDevicePairingRequestList`
- Rename controller file: `devicepairingrequest_controller.go` → `clawdevicepairingrequest_controller.go`
- Rename controller test file: `devicepairingrequest_controller_test.go` → `clawdevicepairingrequest_controller_test.go`
- Rename type file: `devicepairingrequest_types.go` → `clawdevicepairingrequest_types.go`
- Update all code references to use new type names
- Regenerate CRD YAML manifests
- Update documentation (CLAUDE.md, godoc comments)

## Capabilities

### New Capabilities
<!-- No new capabilities - this is a renaming refactor -->

### Modified Capabilities
<!-- No requirement changes - this is a renaming refactor that preserves all existing behavior -->

## Impact

- `api/v1alpha1/devicepairingrequest_types.go` → renamed to `clawdevicepairingrequest_types.go`, type names updated
- `api/v1alpha1/zz_generated.deepcopy.go` — regenerated with new type names
- `internal/controller/devicepairingrequest_controller.go` → renamed to `clawdevicepairingrequest_controller.go`, reconciler renamed
- `internal/controller/devicepairingrequest_controller_test.go` → renamed to `clawdevicepairingrequest_controller_test.go`
- `internal/controller/suite_test.go` — no changes (scheme registration is automatic)
- `cmd/main.go` — reconciler initialization updated to use new type name
- `config/crd/bases/claw.sandbox.redhat.com_devicepairingrequests.yaml` → renamed to `claw.sandbox.redhat.com_clawdevicepairingrequests.yaml`
- `config/rbac/role.yaml` — regenerated with new resource names
- `CLAUDE.md` — updated CRD documentation
- **Users**: Must migrate existing `DevicePairingRequest` resources to `ClawDevicePairingRequest` (manual migration required)
