## Why

The operator currently embeds only core manifests (ConfigMap, Deployment, PVC) in `internal/assets/manifests/`. Additional manifests exist in `internal/manifests/` that provide network security, API proxy capabilities, and external access, but these are not included in the unified Kustomize-based reconciliation. This leaves OpenClaw instances incomplete and missing critical security and networking features.

## What Changes

- Copy all remaining manifests from `internal/manifests/` to `internal/assets/manifests/`
- Add new resources to `internal/assets/manifests/kustomization.yaml` resource list
- Update OpenClawReconciler RBAC permissions to create NetworkPolicy, Service, Route, and additional Deployments/ConfigMaps

## Capabilities

### New Capabilities

- `network-security`: NetworkPolicy resources for egress control from OpenClaw and proxy pods
- `api-proxy`: Nginx-based proxy deployment with credential injection for LLM API access
- `external-access`: OpenShift Route and Services for exposing OpenClaw

### Modified Capabilities

## Impact

- `internal/assets/manifests/`: Add 6 new YAML files (networkpolicy.yaml, proxy-configmap.yaml, proxy-deployment.yaml, proxy-service.yaml, route.yaml, service.yaml)
- `internal/assets/manifests/kustomization.yaml`: Add new resources to the resource list
- `internal/controller/openclaw_controller.go`: Add RBAC markers for NetworkPolicy, Service, Route creation permissions
