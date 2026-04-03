## Why

The OpenClaw application requires persistent storage for user data and home directories. Currently, there is no controller managing the lifecycle of PersistentVolumeClaim (PVC) resources when an OpenClaw instance is created. This change adds a dedicated controller to automatically provision the required PVC when an OpenClaw named 'instance' is created, following the same pattern as the existing ConfigMap and Deployment controllers.

## What Changes

- Add a new `OpenClawPersistentVolumeClaimReconciler` controller in `internal/controller/openclaw_pvc_controller.go`
- Controller watches OpenClaw resources and creates PVCs based on the template in `internal/manifests/pvc.yaml`
- Only reconciles OpenClaw resources named 'instance' (consistent with existing controllers)
- PVC is created with the OpenClaw instance as the controller owner reference
- RBAC permissions added for PersistentVolumeClaim resources (get, list, watch, create)

## Capabilities

### New Capabilities
- `openclawpvc-controller`: Controller that manages PersistentVolumeClaim lifecycle for OpenClaw instances, creating a PVC from the embedded manifest template when an OpenClaw named 'instance' is created

### Modified Capabilities
<!-- No existing capabilities are being modified -->

## Impact

- New controller file: `internal/controller/openclaw_pvc_controller.go`
- Existing PVC manifest: `internal/manifests/pvc.yaml` (already present)
- Main controller setup will need to register the new controller
- RBAC manifests will be updated with PVC permissions via kubebuilder annotations
