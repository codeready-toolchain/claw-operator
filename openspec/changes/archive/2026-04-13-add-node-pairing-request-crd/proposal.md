## Why

The Claw Operator needs a mechanism to manage node pairing requests. A dedicated CRD will enable declarative management of pairing operations between nodes, allowing the system to track and reconcile pairing state through standard Kubernetes patterns.

## What Changes

- Add new namespaced CRD `NodePairingRequest` in API group `openclaw.sandbox.redhat.com/v1alpha1`
- Add `RequestID` field to the CRD Spec
- Implement controller to watch and reconcile NodePairingRequest resources
- Generate CRD manifests, DeepCopy methods, and RBAC markers
- Add controller tests following project patterns (envtest-based, testify assertions)

## Capabilities

### New Capabilities
- `node-pairing-request`: NodePairingRequest CRD and controller implementation for managing node pairing operations

### Modified Capabilities
<!-- No existing capabilities are being modified -->

## Impact

- New API type in `api/v1alpha1/nodepairingrequest_types.go`
- New controller in `internal/controller/nodepairingrequest_controller.go`
- New test file `internal/controller/nodepairingrequest_controller_test.go`
- Generated CRD manifest in `config/crd/bases/`
- RBAC updates in `config/rbac/` for NodePairingRequest resource access
- Controller registration in `cmd/main.go`
