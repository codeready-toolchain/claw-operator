## 1. Rename Controller File and Struct

- [x] 1.1 Rename `internal/controller/openclaw_controller.go` to `internal/controller/openclaw_resource_controller.go`
- [x] 1.2 Update struct name from `OpenClawReconciler` to `OpenClawResourceReconciler` in the renamed file
- [x] 1.3 Update receiver variable references in all methods (if needed for consistency)

## 2. Update Test Files

- [x] 2.1 Update test files to reference `OpenClawResourceReconciler` instead of `OpenClawReconciler`
- [x] 2.2 Verify test imports and suite setup still reference correct controller

## 3. Update Main Entrypoint

- [x] 3.1 Update `cmd/main.go` to instantiate `OpenClawResourceReconciler` instead of `OpenClawReconciler`
- [x] 3.2 Update any controller setup or registration calls

## 4. Verification

- [x] 4.1 Run `make test` to verify all unit tests pass
- [x] 4.2 Run `make lint` to verify no linting issues
- [x] 4.3 Run `make build` to verify compilation succeeds
