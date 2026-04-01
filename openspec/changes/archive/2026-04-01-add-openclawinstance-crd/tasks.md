## 1. Create API Types

- [x] 1.1 Create `api/v1alpha1/openclawinstance_types.go` file
- [x] 1.2 Define `OpenClawInstanceSpec` struct (empty with comment "// Empty for now")
- [x] 1.3 Define `OpenClawInstanceStatus` struct (empty with comment "// Empty for now")
- [x] 1.4 Define `OpenClawInstance` struct with TypeMeta, ObjectMeta, Spec, and Status fields
- [x] 1.5 Define `OpenClawInstanceList` struct for list operations
- [x] 1.6 Add kubebuilder markers for CRD generation (subresources:status, printcolumn for Name and Age)
- [x] 1.7 Register types in `api/v1alpha1/groupversion_info.go` if needed

## 2. Generate CRD Manifests

- [x] 2.1 Run `make manifests` to generate CRD YAML in `config/crd/bases/`
- [x] 2.2 Verify CRD file exists at `config/crd/bases/openclaw.codeready-toolchain.com_openclawinstances.yaml`
- [x] 2.3 Verify CRD includes status subresource configuration

## 3. Create Controller

- [x] 3.1 Create `internal/controller/openclawinstance_controller.go` file
- [x] 3.2 Define `OpenClawInstanceReconciler` struct with Client and Scheme fields
- [x] 3.3 Implement `Reconcile` method with no-op logic and logging
- [x] 3.4 Add RBAC markers for get, list, watch permissions on openclawinstances
- [x] 3.5 Add RBAC markers for update permission on openclawinstances/status
- [x] 3.6 Implement `SetupWithManager` method to configure controller and watch OpenClawInstance resources

## 4. Register Controller with Manager

- [x] 4.1 Update `cmd/main.go` (or `main.go`) to import the controller package
- [x] 4.2 Add controller setup call to register OpenClawInstanceReconciler with the manager
- [x] 4.3 Add error handling for controller registration

## 5. Generate RBAC Manifests

- [x] 5.1 Run `make manifests` to regenerate RBAC resources in `config/rbac/`
- [x] 5.2 Verify role YAML includes permissions for openclawinstances resources

## 6. Verification

- [x] 6.1 Run `make generate` to update generated code (DeepCopy methods)
- [x] 6.2 Run `go mod tidy` to ensure dependencies are correct
- [x] 6.3 Run `make build` to verify the code compiles
- [x] 6.4 Verify all generated files are present and correctly formatted
