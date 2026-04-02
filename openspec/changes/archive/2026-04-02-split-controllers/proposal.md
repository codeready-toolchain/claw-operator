## Why

The current single controller manages both ConfigMap and Deployment creation, coupling unrelated concerns and making testing more complex. Splitting into dedicated controllers improves maintainability, testability, and follows the single responsibility principle.

## What Changes

- **BREAKING**: Remove existing OpenClawInstanceReconciler
- Create OpenClawConfigMapController to manage ConfigMap lifecycle
- Create OpenClawDeploymentController to manage Deployment lifecycle
- Both controllers watch OpenClawInstance resources named 'instance'
- Each controller has its own RBAC permissions and watches
- Split test suite into separate test files for each controller
- Update cmd/main.go to register both controllers

## Capabilities

### New Capabilities

- `openclaw-configmap-controller`: Controller that creates and manages the 'openclaw-config' ConfigMap when OpenClawInstance named 'instance' exists
- `openclaw-deployment-controller`: Controller that creates and manages the 'openclaw' Deployment when OpenClawInstance named 'instance' exists

### Modified Capabilities

## Impact

- Removes `internal/controller/openclawinstance_controller.go`
- Adds `internal/controller/openclaw_configmap_controller.go`
- Adds `internal/controller/openclaw_deployment_controller.go`
- Removes `internal/controller/openclawinstance_controller_suite_test.go`
- Adds `internal/controller/openclaw_configmap_controller_test.go`
- Adds `internal/controller/openclaw_deployment_controller_test.go`
- Updates `cmd/main.go` to register both controllers
- RBAC roles split between controllers
