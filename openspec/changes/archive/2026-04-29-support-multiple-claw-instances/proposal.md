## Why

The current controller artificially limits reconciliation to only Claw instances named "instance", preventing users from deploying multiple independent OpenClaw instances in the same namespace or cluster. This restriction serves no technical purpose and reduces the operator's flexibility and usability.

## What Changes

- Remove the hardcoded name filter that skips Claw instances not named "instance"
- Use the Claw CR name as the base name for all created resources instead of hardcoded names
- Update resource naming convention: `{claw-name}` for gateway resources, `{claw-name}-proxy` for proxy resources
- Support multiple Claw instances per namespace with unique resource names
- Update status conditions and owner references to work with dynamic names
- Update tests to verify multi-instance support

## Capabilities

### New Capabilities
<!-- None - this extends existing controller behavior -->

### Modified Capabilities
- `unified-kustomize-controller`: Remove instance name filtering, use dynamic resource names based on Claw CR name
- `gateway-token-secret`: Use `{claw-name}-gateway-token` naming pattern instead of fixed `claw-gateway-token`
- `dynamic-route-config`: Use `{claw-name}` for Route name instead of fixed `claw`
- `status-url-field`: Construct URL from dynamic Route name

## Impact

- **Controller code**: `ClawResourceReconciler.Reconcile()` method removes name filter, all resource creation uses dynamic naming
- **Resource names**: All created resources (Deployment, Service, Route, PVC, ConfigMap, Secret, NetworkPolicies) use Claw CR name as base
- **Constants**: Remove or update `ClawInstanceName` constant, update other resource name constants to be functions
- **Tests**: Update tests to use arbitrary instance names, add multi-instance tests
- **Backward compatibility**: Existing deployments with instance named "instance" continue to work with same resource names
- **RBAC**: No changes needed (already has cluster-wide permissions)
- **Documentation**: Update examples and docs to show multi-instance capabilities
