## 1. Rename API Type Files

- [x] 1.1 Use `git mv` to rename `api/v1alpha1/devicepairingrequest_types.go` to `api/v1alpha1/clawdevicepairingrequest_types.go`
- [x] 1.2 Update type definitions in the renamed file: `DevicePairingRequest` → `ClawDevicePairingRequest`
- [x] 1.3 Update type definitions: `DevicePairingRequestSpec` → `ClawDevicePairingRequestSpec`
- [x] 1.4 Update type definitions: `DevicePairingRequestStatus` → `ClawDevicePairingRequestStatus`  
- [x] 1.5 Update type definitions: `DevicePairingRequestList` → `ClawDevicePairingRequestList`
- [x] 1.6 Update all kubebuilder markers to reference new type names (printcolumn, resource markers)
- [x] 1.7 Update godoc comments to reference new type name

## 2. Rename Controller Files

- [x] 2.1 Use `git mv` to rename `internal/controller/devicepairingrequest_controller.go` to `internal/controller/clawdevicepairingrequest_controller.go`
- [x] 2.2 Use `git mv` to rename `internal/controller/devicepairingrequest_controller_test.go` to `internal/controller/clawdevicepairingrequest_controller_test.go`

## 3. Update Controller Implementation

- [x] 3.1 Update reconciler struct name: `DevicePairingRequestReconciler` → `ClawDevicePairingRequestReconciler`
- [x] 3.2 Update all method receivers from `DevicePairingRequest` types to `ClawDevicePairingRequest` types
- [x] 3.3 Update all variable declarations using old type names
- [x] 3.4 Update RBAC kubebuilder markers to reference new resource name `clawdevicepairingrequests`
- [x] 3.5 Update godoc comments in controller file
- [x] 3.6 Update log messages that reference the old type name

## 4. Update Controller Tests

- [x] 4.1 Update test function names referencing `DevicePairingRequest` to `ClawDevicePairingRequest`
- [x] 4.2 Update all variable declarations in tests to use new type names
- [x] 4.3 Update test helper function `deleteAndWaitDevicePairingRequest` to `deleteAndWaitClawDevicePairingRequest`
- [x] 4.4 Update all test assertions and error messages referencing old type name

## 5. Update Main Entry Point

- [x] 5.1 Update controller initialization in `cmd/main.go` to use `ClawDevicePairingRequestReconciler`
- [x] 5.2 Update reconciler setup call to reference new type

## 6. Regenerate Code and Manifests

- [x] 6.1 Run `make generate` to regenerate DeepCopy methods in `zz_generated.deepcopy.go`
- [x] 6.2 Run `make manifests` to regenerate CRD YAML in `config/crd/bases/`
- [x] 6.3 Verify new CRD file is named `claw.sandbox.redhat.com_clawdevicepairingrequests.yaml`
- [x] 6.4 Delete old CRD file `claw.sandbox.redhat.com_devicepairingrequests.yaml` if it still exists
- [x] 6.5 Verify RBAC role.yaml has been updated with new resource names

## 7. Update Documentation

- [x] 7.1 Update CLAUDE.md to replace all `DevicePairingRequest` references with `ClawDevicePairingRequest`
- [x] 7.2 Update CLAUDE.md CRD documentation section header
- [x] 7.3 Update CLAUDE.md controller documentation to reference new reconciler name
- [x] 7.4 Add migration notes to CLAUDE.md explaining the breaking change

## 8. Verification

- [x] 8.1 Run `make build` to verify compilation
- [x] 8.2 Run `make test` to verify all tests pass
- [x] 8.3 Run `make lint` to check for any linting issues
- [x] 8.4 Verify no references to old type names remain: `git grep -i "DevicePairingRequest" --exclude-dir=openspec`
- [x] 8.5 Verify CRD resource name in generated YAML is `clawdevicepairingrequests`
