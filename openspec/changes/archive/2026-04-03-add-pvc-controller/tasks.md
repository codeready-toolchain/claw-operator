## 1. Setup Assets

- [x] 1.1 Add PVC manifest embed to internal/assets/manifests.go
- [x] 1.2 Copy internal/manifests/pvc.yaml to internal/assets/manifests/pvc.yaml

## 2. Create Controller File

- [x] 2.1 Create internal/controller/openclaw_pvc_controller.go with OpenClawPersistentVolumeClaimReconciler struct
- [x] 2.2 Add package imports (context, corev1, apierrors, runtime, serializer, ctrl, builder, client, controllerutil, log, predicate, clawv1alpha1, assets)
- [x] 2.3 Add constant OpenClawPVCName = "openclaw-home-pvc"
- [x] 2.4 Define OpenClawPersistentVolumeClaimReconciler struct with Client and Scheme fields

## 3. Implement Reconcile Method

- [x] 3.1 Add RBAC annotations for OpenClaw (get, list, watch) and PersistentVolumeClaim (get, list, watch, create) resources
- [x] 3.2 Implement Reconcile method signature with context and request parameters
- [x] 3.3 Add logging for reconciliation start with name and namespace
- [x] 3.4 Fetch OpenClaw resource using r.Get() with error handling for NotFound case
- [x] 3.5 Add name filtering to skip OpenClaw resources not named "instance"
- [x] 3.6 Parse embedded PVC manifest using serializer.NewCodecFactory and decode into corev1.PersistentVolumeClaim
- [x] 3.7 Set PVC namespace to match OpenClaw instance namespace
- [x] 3.8 Set controller owner reference using controllerutil.SetControllerReference
- [x] 3.9 Create PVC using r.Create() with AlreadyExists error handling
- [x] 3.10 Add success logging after PVC creation

## 4. Implement SetupWithManager Method

- [x] 4.1 Create SetupWithManager method that accepts ctrl.Manager parameter
- [x] 4.2 Create PVC name predicate using predicate.NewPredicateFuncs filtering for OpenClawPVCName
- [x] 4.3 Build controller using ctrl.NewControllerManagedBy with For OpenClaw, Owns PVC with predicate, Named "openclawpersistentvolumeclaim"
- [x] 4.4 Return Complete(r) to register the controller

## 5. Register Controller with Manager

- [x] 5.1 Add OpenClawPersistentVolumeClaimReconciler setup in cmd/main.go after OpenClawDeploymentReconciler registration
- [x] 5.2 Include error handling and setup logging for the new controller

## 6. Verification

- [x] 6.1 Run make manifests to generate RBAC manifests from kubebuilder annotations
- [x] 6.2 Verify the controller builds successfully (make build or go build)
- [x] 6.3 Check that RBAC manifests in config/rbac/ include PVC permissions

## 7. Unit Tests

- [x] 7.1 Create internal/controller/openclaw_pvc_controller_test.go with test suite
- [x] 7.2 Add test for creating PVC for OpenClaw named 'instance'
- [x] 7.3 Add test for verifying correct owner reference on PVC
- [x] 7.4 Add test for skipping PVC creation for non-matching names
- [x] 7.5 Add BeforeEach cleanup to handle test isolation
- [x] 7.6 Verify all controller tests pass (10/10 passing)
