## Why

The Claw controller currently exists as a no-op skeleton that only logs events. To enable the operator to provision OpenClaw instances, the controller needs to actually create and manage the required Kubernetes resources when an Claw CR is created.

## What Changes

- Claw controller reconciliation logic will fetch the Claw resource and create a Deployment
- Controller will parse and apply the deployment manifest from `internal/manifests/deployment.yaml`
- Controller will establish ownership between the Claw and created Deployment for proper garbage collection
- Controller will add RBAC permissions for managing Deployments

## Capabilities

### New Capabilities

None - this change enhances existing controller functionality.

### Modified Capabilities

- `openclawinstance-controller`: Controller will transition from no-op skeleton to creating Deployments based on manifest file

## Impact

**Code:**
- `internal/controller/claw_resource_controller.go` - Reconcile function implementation
- `internal/controller/suite_test.go` or test files - New reconciliation tests
- RBAC markers in controller for Deployment permissions

**Dependencies:**
- Requires reading and parsing deployment manifest from `internal/manifests/deployment.yaml`
- May require embedding manifest file or reading from filesystem at runtime

**Systems:**
- Controller will create Deployment resources in the same namespace as the Claw
- Kubernetes garbage collection will delete Deployments when owning Claw is deleted
