## 1. Create OpenClawConfigMapController

- [x] 1.1 Create internal/controller/openclaw_configmap_controller.go file
- [x] 1.2 Define OpenClawConfigMapReconciler struct with Client and Scheme fields
- [x] 1.3 Implement Reconcile function to fetch OpenClawInstance resource
- [x] 1.4 Add name filtering logic to skip resources not named "instance"
- [x] 1.5 Add logic to parse ConfigMap manifest from internal/assets/manifests/configmap.yaml
- [x] 1.6 Add logic to set ConfigMap namespace and owner reference
- [x] 1.7 Add logic to create ConfigMap with idempotency (skip if already exists)
- [x] 1.8 Implement SetupWithManager to watch OpenClawInstance and ConfigMap resources
- [x] 1.9 Add predicate to filter ConfigMap watch by name "openclaw-config"
- [x] 1.10 Add RBAC markers for OpenClawInstance and ConfigMap permissions

## 2. Create OpenClawDeploymentController

- [x] 2.1 Create internal/controller/openclaw_deployment_controller.go file
- [x] 2.2 Define OpenClawDeploymentReconciler struct with Client and Scheme fields
- [x] 2.3 Implement Reconcile function to fetch OpenClawInstance resource
- [x] 2.4 Add name filtering logic to skip resources not named "instance"
- [x] 2.5 Add logic to check if ConfigMap "openclaw-config" exists before proceeding
- [x] 2.6 Skip Deployment creation if ConfigMap doesn't exist (return success)
- [x] 2.7 Add logic to parse Deployment manifest from internal/assets/manifests/deployment.yaml
- [x] 2.8 Add logic to set Deployment namespace and owner reference
- [x] 2.9 Add logic to create Deployment with idempotency (skip if already exists)
- [x] 2.10 Implement SetupWithManager to watch OpenClawInstance and Deployment resources
- [x] 2.11 Add predicate to filter Deployment watch by name "openclaw"
- [x] 2.12 Add RBAC markers for OpenClawInstance, ConfigMap (get only), and Deployment permissions

## 3. Update Main Entry Point

- [x] 3.1 Update cmd/main.go to register OpenClawConfigMapReconciler
- [x] 3.2 Update cmd/main.go to register OpenClawDeploymentReconciler
- [x] 3.3 Remove registration of old OpenClawInstanceReconciler

## 4. Create Test Files

- [x] 4.1 Create internal/controller/openclaw_configmap_controller_test.go
- [x] 4.2 Add test suite setup (BeforeSuite, AfterSuite) for ConfigMap controller tests
- [x] 4.3 Add test: ConfigMap created for OpenClawInstance named 'instance'
- [x] 4.4 Add test: ConfigMap has correct owner reference
- [x] 4.5 Add test: ConfigMap creation skipped for non-matching names
- [x] 4.6 Add test: ConfigMap watch triggers reconciliation
- [x] 4.7 Create internal/controller/openclaw_deployment_controller_test.go
- [x] 4.8 Add test suite setup (BeforeSuite, AfterSuite) for Deployment controller tests
- [x] 4.9 Add test: Deployment NOT created when ConfigMap doesn't exist
- [x] 4.10 Add test: Deployment created when ConfigMap exists
- [x] 4.11 Add test: Deployment has correct owner reference
- [x] 4.12 Add test: Deployment creation skipped for non-matching names
- [x] 4.13 Add test: Deployment watch triggers reconciliation

## 5. Remove Old Controller

- [x] 5.1 Delete internal/controller/openclawinstance_controller.go
- [x] 5.2 Delete internal/controller/openclawinstance_controller_suite_test.go

## 6. Verify and Validate

- [x] 6.1 Run test suite to verify all tests pass
- [x] 6.2 Verify RBAC markers generate correct permissions for both controllers
- [x] 6.3 Verify ConfigMap controller creates ConfigMap successfully
- [x] 6.4 Verify Deployment controller waits for ConfigMap before creating Deployment
- [x] 6.5 Verify garbage collection works correctly for both resources
