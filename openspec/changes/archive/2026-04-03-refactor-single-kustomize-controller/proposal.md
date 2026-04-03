## Why

The current architecture uses three separate controllers (OpenClawConfigMapReconciler, OpenClawPersistentVolumeClaimReconciler, OpenClawDeploymentReconciler) each managing a single resource type. This creates code duplication, requires complex ordering dependencies between controllers, makes it difficult to ensure consistent labeling across resources, and prevents atomic management of all related resources as a unit. Consolidating to a single controller using Kustomize for manifest generation will simplify the architecture, ensure consistent resource labeling, and enable server-side apply for more reliable resource management.

## What Changes

- **BREAKING**: Consolidate three resource-specific controllers into one unified `OpenClawReconciler` in `internal/controller/openclaw_controller.go`
- Use Kustomize to build all manifests in-memory from `internal/assets/manifests/` directory
- Create `kustomization.yaml` in `internal/assets/manifests/` that references all resource manifests (configmap.yaml, pvc.yaml, deployment.yaml, etc.)
- Add `commonLabels` to kustomization.yaml with `app.kubernetes.io/name: openclaw` for all resources
- Switch from individual client-side resource creation to server-side apply for the entire resource set
- Remove separate controller files: `openclaw_configmap_controller.go`, `openclaw_pvc_controller.go`, `openclaw_deployment_controller.go`
- Keep existing unit test files for readability but update them to test the unified controller
- Update `cmd/main.go` to register only the single OpenClawReconciler
- Simplify RBAC by consolidating permissions into one controller's annotations

## Capabilities

### New Capabilities
- `unified-kustomize-controller`: Single controller that uses Kustomize to build manifests in-memory, apply them server-side, and manage the complete lifecycle of ConfigMap, PVC, Deployment, and other resources for OpenClaw instances

### Modified Capabilities
<!-- The three existing controller capabilities (openclawconfigmap-controller, openclawpvc-controller, openclawdeployment-controller) are being replaced by the new unified controller. No spec-level requirement changes - same resources are created with same owner references, just different implementation approach. -->

## Impact

**Code Changes:**
- Remove: `internal/controller/openclaw_configmap_controller.go` (~120 lines)
- Remove: `internal/controller/openclaw_pvc_controller.go` (~120 lines)  
- Remove: `internal/controller/openclaw_deployment_controller.go` (~150 lines)
- Create: `internal/controller/openclaw_controller.go` (new unified controller)
- Create: `internal/assets/manifests/kustomization.yaml`
- Modify: `cmd/main.go` (remove 3 controller registrations, add 1)
- Modify: `internal/assets/manifests.go` (change from individual embeds to kustomization embed)
- Update: All three test files to work with unified controller
- Update: `CLAUDE.md` documentation

**Dependencies:**
- Add dependency on `sigs.k8s.io/kustomize/api` for in-memory Kustomize builds
- Add dependency on server-side apply APIs from `k8s.io/apimachinery`

**Behavior:**
- Resources now created as a unit via server-side apply instead of individually
- All managed resources will have `app.kubernetes.io/name: openclaw` label
- Ordering dependencies handled by Kubernetes instead of explicit controller sequencing
- Server-side apply provides better conflict resolution and field ownership

**Migration:**
- Existing resources created by old controllers will be adopted by the new controller through owner references
- No user action required - seamless upgrade path
