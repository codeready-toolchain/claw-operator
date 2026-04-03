## 1. Dependencies and Setup

- [x] 1.1 Add Kustomize dependency to go.mod (sigs.k8s.io/kustomize/api)
- [x] 1.2 Add kustomize/kyaml dependency for YAML handling
- [x] 1.3 Run go mod tidy to download dependencies

## 2. Create Kustomization File

- [x] 2.1 Create internal/assets/manifests/kustomization.yaml
- [x] 2.2 Add apiVersion and kind fields to kustomization.yaml
- [x] 2.3 Add commonLabels section with app.kubernetes.io/name: openclaw
- [x] 2.4 Add resources list referencing configmap.yaml, pvc.yaml, deployment.yaml

## 3. Update Assets Embedding

- [x] 3.1 Modify internal/assets/manifests.go to embed entire manifests directory using embed.FS
- [x] 3.2 Remove individual DeploymentManifest, ConfigMapManifest, PVCManifest embed directives
- [x] 3.3 Export the embedded FS for use by the controller

## 4. Create Unified Controller - Structure

- [x] 4.1 Create internal/controller/openclaw_controller.go file
- [x] 4.2 Add package declaration and imports (context, embed, corev1, appsv1, apierrors, runtime, ctrl, client, controllerutil, log, kustomize libs)
- [x] 4.3 Add constant OpenClawInstanceName = "instance"
- [x] 4.4 Define OpenClawReconciler struct with Client and Scheme fields

## 5. Create Unified Controller - RBAC

- [x] 5.1 Add RBAC annotation for OpenClaw resources (get, list, watch)
- [x] 5.2 Add RBAC annotation for ConfigMap resources (get, list, watch, create, update, patch)
- [x] 5.3 Add RBAC annotation for PersistentVolumeClaim resources (get, list, watch, create, update, patch)
- [x] 5.4 Add RBAC annotation for Deployment resources (get, list, watch, create, update, patch)

## 6. Create Unified Controller - Reconcile Method Core

- [x] 6.1 Implement Reconcile method signature (ctx context.Context, req ctrl.Request) (ctrl.Result, error)
- [x] 6.2 Add logging for reconciliation start with name and namespace
- [x] 6.3 Fetch OpenClaw resource using r.Get() with NotFound error handling
- [x] 6.4 Add name filtering to skip OpenClaw resources not named "instance"

## 7. Create Unified Controller - Kustomize Build

- [x] 7.1 Create kustomize filesystem from embedded assets
- [x] 7.2 Initialize kustomize options with MakeDefaultOptions()
- [x] 7.3 Create kustomizer with krusty.MakeKustomizer()
- [x] 7.4 Run kustomize build with kustomizer.Run() to generate resource map
- [x] 7.5 Convert resource map to unstructured objects list
- [x] 7.6 Add error handling and logging for kustomize build failures

## 8. Create Unified Controller - Resource Transformation

- [x] 8.1 Iterate through all resources from kustomize build
- [x] 8.2 Set namespace on each resource to match instance.Namespace
- [x] 8.3 Set controller owner reference on each resource using controllerutil.SetControllerReference
- [x] 8.4 Add error handling for owner reference failures

## 9. Create Unified Controller - Server-Side Apply

- [x] 9.1 Implement server-side apply loop for each resource
- [x] 9.2 Use client.Patch() with client.Apply patch type
- [x] 9.3 Set PatchOptions with FieldManager: "openclaw-operator"
- [x] 9.4 Set ForceOwnership: true for first-time adoption
- [x] 9.5 Add error handling and logging for apply failures
- [x] 9.6 Log successful application with resource count

## 10. Create Unified Controller - SetupWithManager

- [x] 10.1 Create SetupWithManager method accepting ctrl.Manager
- [x] 10.2 Configure controller to watch OpenClaw as primary resource using For()
- [x] 10.3 Configure controller to own ConfigMap using Owns()
- [x] 10.4 Configure controller to own PersistentVolumeClaim using Owns()
- [x] 10.5 Configure controller to own Deployment using Owns()
- [x] 10.6 Name the controller "openclaw" using Named()
- [x] 10.7 Return Complete(r) to register the controller

## 11. Update Main Controller Registration

- [x] 11.1 Open cmd/main.go for editing
- [x] 11.2 Remove OpenClawConfigMapReconciler registration block
- [x] 11.3 Remove OpenClawPersistentVolumeClaimReconciler registration block
- [x] 11.4 Remove OpenClawDeploymentReconciler registration block
- [x] 11.5 Add OpenClawReconciler registration with Client and Scheme
- [x] 11.6 Add error handling and setup logging for the new controller

## 12. Update ConfigMap Controller Tests

- [x] 12.1 Open internal/controller/openclaw_configmap_controller_test.go
- [x] 12.2 Change reconciler from OpenClawConfigMapReconciler to OpenClawReconciler
- [x] 12.3 Update imports if needed for new controller
- [x] 12.4 Verify test scenarios still make sense (resource creation, owner refs, name filtering)
- [x] 12.5 Update constant references (OpenClawConfigMapName might need verification)

## 13. Update PVC Controller Tests

- [x] 13.1 Open internal/controller/openclaw_pvc_controller_test.go
- [x] 13.2 Change reconciler from OpenClawPersistentVolumeClaimReconciler to OpenClawReconciler
- [x] 13.3 Update imports if needed for new controller
- [x] 13.4 Verify test scenarios still make sense
- [x] 13.5 Update constant references (OpenClawPVCName should still work)

## 14. Update Deployment Controller Tests

- [x] 14.1 Check if internal/controller/openclaw_deployment_controller_test.go exists
- [x] 14.2 If exists, change reconciler from OpenClawDeploymentReconciler to OpenClawReconciler
- [x] 14.3 If exists, update imports if needed
- [x] 14.4 If exists, verify test scenarios and remove explicit ConfigMap dependency checks
- [x] 14.5 If exists, update constant references

## 15. Delete Old Controller Files

- [x] 15.1 Delete internal/controller/openclaw_configmap_controller.go
- [x] 15.2 Delete internal/controller/openclaw_pvc_controller.go
- [x] 15.3 Check if internal/controller/openclaw_deployment_controller.go exists and delete if present

## 16. Generate Manifests and RBAC

- [x] 16.1 Run make manifests to generate RBAC from kubebuilder annotations
- [x] 16.2 Verify config/rbac/role.yaml has consolidated permissions (ConfigMap, PVC, Deployment all listed)
- [x] 16.3 Check that role includes update and patch verbs (for server-side apply)

## 17. Run Tests

- [x] 17.1 Run all controller tests with make test or go test ./internal/controller/...
- [x] 17.2 Verify all ConfigMap tests pass
- [x] 17.3 Verify all PVC tests pass
- [x] 17.4 Verify all Deployment tests pass (if they exist)
- [x] 17.5 Debug and fix any test failures

## 18. Verify Build

- [x] 18.1 Run go build ./cmd/main.go to verify compilation
- [x] 18.2 Check for any import or dependency errors
- [x] 18.3 Verify no unused imports remain

## 19. Update Documentation

- [x] 19.1 Update CLAUDE.md Architecture section
- [x] 19.2 Change from "Split reconcilers design" to describe unified Kustomize-based controller
- [x] 19.3 Document the Kustomize in-memory build approach
- [x] 19.4 Document server-side apply strategy
- [x] 19.5 Update key directories section to mention kustomization.yaml
- [x] 19.6 Update controller count from "three" to "one"
