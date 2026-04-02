## Why

The current reconciliation creates the Deployment before the ConfigMap, which means the Deployment may start without configuration. By creating the ConfigMap first, we ensure configuration is available before the application starts.

## What Changes

- Reverse the order of resource creation: ConfigMap first, then Deployment
- Controller creates ConfigMap from `internal/manifests/configmap.yaml` immediately when reconciling an OpenClawInstance named 'instance'
- Controller only creates Deployment after detecting ConfigMap 'openclaw-config' exists
- Deployment watch triggers reconciliation to create Deployment once ConfigMap is ready

## Capabilities

### New Capabilities

### Modified Capabilities

- `openclawinstance-controller`: Change reconciliation order to create ConfigMap before Deployment, with Deployment creation conditional on ConfigMap existence

## Impact

- Reconciliation logic in `internal/controller/openclawinstance_controller.go`
- Test suite in `internal/controller/openclawinstance_controller_suite_test.go` needs updates for new creation order
- Existing behavior changes: ConfigMap created first instead of last
