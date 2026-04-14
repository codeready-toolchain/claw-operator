## 1. Rename Go API Types

- [x] 1.1 Rename file `api/v1alpha1/openclawinstance_types.go` to `api/v1alpha1/openclaw_types.go`
- [x] 1.2 Update type name from `OpenClawInstance` to `OpenClaw` in openclaw_types.go
- [x] 1.3 Update type name from `OpenClawInstanceList` to `OpenClawList` in openclaw_types.go
- [x] 1.4 Update type name from `OpenClawInstanceSpec` to `OpenClawSpec` in openclaw_types.go
- [x] 1.5 Update type name from `OpenClawInstanceStatus` to `OpenClawStatus` in openclaw_types.go
- [x] 1.6 Update kubebuilder comments to reference `OpenClaw` instead of `OpenClawInstance`

## 2. Regenerate Manifests and Code

- [x] 2.1 Run `make generate` to regenerate DeepCopy methods for new type names
- [x] 2.2 Run `make manifests` to regenerate CRD file with new resource name
- [x] 2.3 Verify CRD file renamed from `openclawinstances.openclaw.sandbox.redhat.com.yaml` to `openclaws.openclaw.sandbox.redhat.com.yaml`
- [x] 2.4 Verify RBAC role.yaml uses `openclaws` resource name instead of `openclawinstances`

## 3. Update Controllers

- [x] 3.1 Update import statements in `internal/controller/openclaw_configmap_controller.go` to use `OpenClaw` type
- [x] 3.2 Update type references from `OpenClawInstance` to `OpenClaw` in openclaw_configmap_controller.go
- [x] 3.3 Update kubebuilder RBAC comments to reference `openclaws` resource in openclaw_configmap_controller.go
- [x] 3.4 Update log messages and comments from "OpenClawInstance" to "OpenClaw" in openclaw_configmap_controller.go
- [x] 3.5 Update SetupWithManager to use `&clawv1alpha1.OpenClaw{}` in openclaw_configmap_controller.go
- [x] 3.6 Update import statements in `internal/controller/openclaw_deployment_controller.go` to use `OpenClaw` type
- [x] 3.7 Update type references from `OpenClawInstance` to `OpenClaw` in openclaw_deployment_controller.go
- [x] 3.8 Update kubebuilder RBAC comments to reference `openclaws` resource in openclaw_deployment_controller.go
- [x] 3.9 Update log messages and comments from "OpenClawInstance" to "OpenClaw" in openclaw_deployment_controller.go
- [x] 3.10 Update SetupWithManager to use `&clawv1alpha1.OpenClaw{}` in openclaw_deployment_controller.go

## 4. Update Test Files

- [x] 4.1 Update type references from `OpenClawInstance` to `OpenClaw` in `internal/controller/openclaw_configmap_controller_test.go`
- [x] 4.2 Update comments and test descriptions from "OpenClawInstance" to "OpenClaw" in openclaw_configmap_controller_test.go
- [x] 4.3 Update type references from `OpenClawInstance` to `OpenClaw` in `internal/controller/openclaw_deployment_controller_test.go`
- [x] 4.4 Update comments and test descriptions from "OpenClawInstance" to "OpenClaw" in openclaw_deployment_controller_test.go
- [x] 4.5 Update owner reference assertion in tests to check for `Kind == "OpenClaw"` instead of `Kind == "OpenClawInstance"`

## 5. Update Sample Manifests

- [x] 5.1 Update `config/samples/` manifest files to use `kind: OpenClaw` instead of `kind: OpenClawInstance`
- [x] 5.2 Verify sample manifests still use correct apiVersion `openclaw.sandbox.redhat.com/v1alpha1`

## 6. Verification and Cleanup

- [x] 6.1 Run `grep -r "OpenClawInstance" .` to find any remaining references (exclude archive and vendor directories)
- [x] 6.2 Update any remaining stray references found in step 6.1
- [x] 6.3 Run `make test` to verify all unit tests pass with new type names
- [x] 6.4 Run `make manifests` again to ensure generated files are up to date
