## Why

The OpenClaw control UI requires CORS configuration with the Route's external URL in `gateway.controlUI.allowedOrigins`. Currently, the ConfigMap is static and cannot include the dynamically-assigned OpenShift Route host, preventing the control UI from functioning correctly when accessed via the Route.

## What Changes

- Refactor OpenClawResourceReconciler to apply Route manifest first, before other resources
- Add wait logic to poll Route status until `.status.ingress[0].host` is populated
- Dynamically inject Route host into ConfigMap's `openclaw.json` settings (replacing `OPENCLAW_ROUTE_HOST` placeholder with `https://<route-host>`)
- Apply remaining manifests (ConfigMap, Deployments, Services, etc.) after Route host is resolved
- Update ConfigMap manifest template to include `OPENCLAW_ROUTE_HOST` placeholder in `gateway.controlUI.allowedOrigins` array

## Capabilities

### New Capabilities

- `dynamic-route-config`: Controller logic to dynamically configure OpenClaw settings based on OpenShift Route status

### Modified Capabilities

<!-- No existing capabilities are being modified -->

## Impact

- **Controller**: `internal/controller/openclaw_resource_controller.go` - reconciliation flow reordered and new Route-polling logic added
- **ConfigMap manifest**: `internal/assets/manifests/configmap.yaml` - add placeholder for Route host in `openclaw.json`
- **Reconciliation ordering**: Route must be applied and ready before ConfigMap and dependent resources
