## Why

The OpenClaw operator needs a primary custom resource to represent instances of OpenClaw deployments. This establishes the foundation for managing OpenClaw lifecycle through Kubernetes-native resources and the operator reconciliation pattern.

## What Changes

- Add `OpenClawInstance` CustomResourceDefinition with empty `spec` and `status` structures
- Create controller skeleton with reconcile loop that watches for `OpenClawInstance` resources
- Configure controller to trigger reconciliation when an `OpenClawInstance` named `instance` is created, updated, or deleted

## Capabilities

### New Capabilities
- `openclawinstance-crd`: CRD definition for the OpenClawInstance resource type with API schema
- `openclawinstance-controller`: Controller implementation with reconcile loop for managing OpenClawInstance resources

### Modified Capabilities
<!-- No existing capabilities are being modified -->

## Impact

- New API Group: Introduces `OpenClawInstance` kind (likely under `openclaw.codeready-toolchain.com/v1alpha1`)
- New CRD manifest: `config/crd/bases/` will contain the generated CRD YAML
- New API types: `api/v1alpha1/openclawinstance_types.go`
- New controller: `internal/controller/openclawinstance_controller.go`
- Updated main.go: Register new controller with the manager
