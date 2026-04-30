## 1. API Types Renaming

- [x] 1.1 Rename `api/v1alpha1/nodepairingrequestapproval_types.go` to `devicepairingrequest_types.go`
- [x] 1.2 Update type name from `NodePairingRequestApproval` to `DevicePairingRequest` in the file
- [x] 1.3 Update type name from `NodePairingRequestApprovalSpec` to `DevicePairingRequestSpec`
- [x] 1.4 Update type name from `NodePairingRequestApprovalStatus` to `DevicePairingRequestStatus`
- [x] 1.5 Update type name from `NodePairingRequestApprovalList` to `DevicePairingRequestList`
- [x] 1.6 Update all JSON/YAML struct tags to use lowercase `devicepairingrequest`
- [x] 1.7 Update all kubebuilder markers to reference `DevicePairingRequest`
- [x] 1.8 Update package-level comments and documentation
- [x] 1.9 Run `make generate` to regenerate DeepCopy methods

## 2. Controller Renaming

- [x] 2.1 Rename `internal/controller/nodepairingrequestapproval_controller.go` to `devicepairingrequest_controller.go`
- [x] 2.2 Update controller struct name from `NodePairingRequestApprovalReconciler` to `DevicePairingRequestReconciler`
- [x] 2.3 Update `For()` clause in `SetupWithManager` to use `&clawv1alpha1.DevicePairingRequest{}`
- [x] 2.4 Update all function receivers from `NodePairingRequestApprovalReconciler` to `DevicePairingRequestReconciler`
- [x] 2.5 Update all RBAC kubebuilder markers to reference `devicepairingrequests` resource
- [x] 2.6 Update all variable and type references within controller code

## 3. Controller Setup and Registration

- [x] 3.1 Update `cmd/main.go` to use `DevicePairingRequestReconciler` in controller setup
- [x] 3.2 Update `SetupWithManager` call to use renamed reconciler
- [x] 3.3 Verify manager registration uses correct controller name

## 4. Generated Manifests

- [x] 4.1 Run `make manifests` to regenerate CRD YAML
- [x] 4.2 Verify generated CRD in `config/crd/bases/` has `kind: DevicePairingRequest`
- [x] 4.3 Verify CRD plural is `devicepairingrequests`
- [x] 4.4 Verify RBAC manifests in `config/rbac/` reference `devicepairingrequests`
- [x] 4.5 Delete old CRD file if name changed (e.g., `claw.sandbox.redhat.com_nodepairingrequestapprovals.yaml`)
- [x] 4.6 Update `config/crd/kustomization.yaml` if CRD filename changed

## 5. Test Files

- [x] 5.1 Rename `internal/controller/nodepairingrequestapproval_controller_test.go` to `devicepairingrequest_controller_test.go`
- [x] 5.2 Update all test function names to reference `DevicePairingRequest`
- [x] 5.3 Update all type references in test code
- [x] 5.4 Update test fixtures and assertions to use new resource name
- [x] 5.5 Update test expectations for Kind, Plural, and resource names
- [x] 5.6 Run tests to verify they pass: `make test`

## 6. Documentation Updates

- [x] 6.1 Update `CLAUDE.md` to replace all `NodePairingRequestApproval` references with `DevicePairingRequest`
- [x] 6.2 Update CRD description in `CLAUDE.md` to reflect new name
- [x] 6.3 Update any example YAML snippets in documentation
- [x] 6.4 Update README if it references the CRD

## 7. Code Quality and Verification

- [x] 7.1 Run `make fmt` to format Go code
- [x] 7.2 Run `make vet` to check for issues
- [x] 7.3 Run `make lint` to verify linting passes
- [x] 7.4 Search codebase for any remaining `NodePairingRequestApproval` references using grep
- [x] 7.5 Search for lowercase variants: `nodepairingrequestapproval`, `node-pairing-request-approval`
- [x] 7.6 Run full test suite: `make test`
- [x] 7.7 Verify CRD installation works: `make install`

## 8. Final Validation

- [x] 8.1 Build operator binary successfully: `make build`
- [x] 8.2 Review all changed files to ensure consistency
- [x] 8.3 Verify no references to old name remain in tracked files
- [x] 8.4 Create commit with breaking change note in commit message
