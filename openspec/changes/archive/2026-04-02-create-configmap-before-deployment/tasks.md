## 1. Modify Reconcile Function

- [x] 1.1 Move ConfigMap creation logic to execute first (before Deployment check)
- [x] 1.2 Add check for ConfigMap 'openclaw-config' existence before creating Deployment
- [x] 1.3 Skip Deployment creation if ConfigMap does not exist and return successfully
- [x] 1.4 Ensure ConfigMap has OpenClawInstance owner reference set before creation
- [x] 1.5 Ensure Deployment has OpenClawInstance owner reference set before creation

## 2. Add ConfigMap Watch

- [x] 2.1 Create predicate function to filter ConfigMap events by name 'openclaw-config'
- [x] 2.2 Update SetupWithManager to include ConfigMap watch with Owns() and predicate filter
- [x] 2.3 Verify ConfigMap watch triggers reconciliation when ConfigMap is created

## 3. Update Test Suite

- [x] 3.1 Update test "should successfully create a Deployment" to verify ConfigMap is created first
- [x] 3.2 Update test to verify Deployment is only created after ConfigMap exists
- [x] 3.3 Update test "should skip reconciliation for resource with non-matching name" to verify no ConfigMap or Deployment created
- [x] 3.4 Update test for multiple OpenClawInstance resources to verify ConfigMap created for 'instance' only
- [x] 3.5 Update or add test to verify ConfigMap watch triggers reconciliation
- [x] 3.6 Update test names and assertions to reflect ConfigMap-first creation order

## 4. Verify and Validate

- [x] 4.1 Run test suite to verify all tests pass with new creation order
- [x] 4.2 Verify RBAC markers generate correct permissions for ConfigMap resources
- [x] 4.3 Verify garbage collection works correctly (both resources deleted when OpenClawInstance deleted)
