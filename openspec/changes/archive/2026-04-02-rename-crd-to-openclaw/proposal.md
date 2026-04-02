## Why

The current CRD name "OpenClawInstance" is verbose and redundant. Since we only manage a singleton resource named "instance", simplifying to "OpenClaw" improves clarity and reduces naming overhead throughout the codebase.

## What Changes

- **BREAKING**: Rename Custom Resource Definition from OpenClawInstance to OpenClaw
- Update Go type from `OpenClawInstance` to `OpenClaw` in `api/v1alpha1/`
- Rename CRD file from `openclawinstances.openclaw.sandbox.redhat.com.yaml` to `openclaws.openclaw.sandbox.redhat.com.yaml`
- Update all controller imports and type references to use `OpenClaw`
- Update all test files to reference `OpenClaw` instead of `OpenClawInstance`
- Update sample manifests in `config/samples/`
- Update scheme registration in `cmd/main.go`

## Capabilities

### New Capabilities

### Modified Capabilities

- `openclawinstance-crd`: Rename CRD from OpenClawInstance to OpenClaw, update all type definitions and generated manifests
- `openclawconfigmap-controller`: Update controller to watch and reconcile OpenClaw resources instead of OpenClawInstance
- `openclawdeployment-controller`: Update controller to watch and reconcile OpenClaw resources instead of OpenClawInstance

## Impact

- **BREAKING CHANGE**: Existing OpenClawInstance resources will no longer be recognized
- CRD definition file in `config/crd/bases/`
- Go types in `api/v1alpha1/openclawinstance_types.go` (will be renamed to `openclaw_types.go`)
- Controllers in `internal/controller/openclaw_configmap_controller.go` and `openclaw_deployment_controller.go`
- All test files in `internal/controller/*_test.go`
- Sample manifests in `config/samples/`
- Scheme registration in `cmd/main.go`
- Generated code (DeepCopy methods, etc.)
