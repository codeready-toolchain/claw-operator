## Why

The OpenClaw controller needs to surface deployment readiness information to users and tooling. Currently, there's no way to determine if an OpenClaw instance is fully provisioned and ready for use without manually checking both the main and proxy deployments.

## What Changes

- Add `Conditions` array to `OpenClawStatus` struct following Kubernetes standard condition pattern
- Controller watches both `openclaw` and `openclaw-proxy` Deployment status conditions
- Controller sets `Available` condition to `False` with `Provisioning` reason during initial deployment
- Controller sets `Available` condition to `True` once both Deployments report `Available=True`
- Status updates are atomic and idempotent using status subresource

## Capabilities

### New Capabilities
- `status-conditions`: Status condition tracking for OpenClaw CRD, including condition types, status transitions, and controller logic for monitoring deployment readiness

### Modified Capabilities
- `unified-kustomize-controller`: Extend reconciler to watch Deployment status and update OpenClaw status conditions based on readiness

## Impact

- `api/v1alpha1/openclaw_types.go`: Add `Conditions []metav1.Condition` field to `OpenClawStatus`
- `internal/controller/openclaw_resource_controller.go`: Add status update logic after resource application
- Generated CRD manifests: Re-run `make manifests` to update status subresource schema
- Tests: Add unit tests for status condition transitions and deployment watching
