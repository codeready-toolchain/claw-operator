## Why

The current `OpenClawReconciler` name doesn't clearly communicate that this controller manages multiple Kubernetes resources (ConfigMap, PVC, Deployment) as a unified set using Kustomize and server-side apply. Renaming to `OpenClawResourceReconciler` improves clarity and follows the pattern of naming reconcilers after what they reconcile.

## What Changes

- Rename `OpenClawReconciler` struct to `OpenClawResourceReconciler` in `internal/controller/openclaw_controller.go`
- Rename file `internal/controller/openclaw_controller.go` to `internal/controller/openclaw_resource_controller.go`
- Update all test files to reference the new struct name
- Update `cmd/main.go` to instantiate `OpenClawResourceReconciler`

## Capabilities

### New Capabilities
<!-- No new capabilities introduced by this refactor -->

### Modified Capabilities
<!-- No requirement changes - this is a naming refactor only -->

## Impact

- **Code**: Controller implementation file and all test files in `internal/controller/`
- **Main entrypoint**: `cmd/main.go` controller setup
- **No API changes**: CRD, RBAC, or external APIs remain unchanged
- **No behavioral changes**: Reconciliation logic remains identical
