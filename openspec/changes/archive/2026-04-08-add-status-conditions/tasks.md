## 1. Update CRD Definition

- [x] 1.1 Add Conditions field to OpenClawStatus struct in api/v1alpha1/openclaw_types.go
- [x] 1.2 Add kubebuilder markers for listType=map and listMapKey=type on Conditions field
- [x] 1.3 Run `make manifests` to regenerate CRD YAML with status subresource
- [x] 1.4 Run `make generate` to update DeepCopy methods
- [x] 1.5 Verify generated CRD includes status subresource and conditions field in OpenAPI schema

## 2. Add Status Update Logic to Controller

- [x] 2.1 Add helper function to fetch Deployment and extract Available condition status
- [x] 2.2 Add helper function to check both openclaw and openclaw-proxy Deployment readiness
- [x] 2.3 Add helper function to set OpenClaw Available condition based on deployment states
- [x] 2.4 Add helper function to update status condition with proper LastTransitionTime handling
- [x] 2.5 Add status update call in Reconcile() after successful resource application
- [x] 2.6 Handle status update errors with logging and retry via error return

## 3. Update RBAC Permissions

- [x] 3.1 Add kubebuilder RBAC marker for OpenClaw status subresource (update, patch)
- [x] 3.2 Verify Deployment permissions include get for status checks
- [x] 3.3 Run `make manifests` to regenerate RBAC ClusterRole

## 4. Add Unit Tests

- [x] 4.1 Add test for Available condition set to False after initial resource creation
- [x] 4.2 Add test for Available condition remains False when only openclaw Deployment is ready
- [x] 4.3 Add test for Available condition remains False when only openclaw-proxy Deployment is ready
- [x] 4.4 Add test for Available condition set to True when both Deployments are ready
- [x] 4.5 Add test for LastTransitionTime updates only on status change
- [x] 4.6 Add test for LastTransitionTime preserved when status unchanged
- [x] 4.7 Add test for handling missing Deployments (not found errors)
- [x] 4.8 Add test for ObservedGeneration set correctly in conditions

## 5. Verification

- [x] 5.1 Run `make test` to verify all unit tests pass
- [x] 5.2 Run `make lint` to ensure code passes linting
- [x] 5.3 Create test OpenClaw instance and verify status conditions update correctly
- [x] 5.4 Verify status conditions visible via `kubectl get openclaw instance -o yaml`
- [x] 5.5 Verify Available=False during provisioning and Available=True when ready
