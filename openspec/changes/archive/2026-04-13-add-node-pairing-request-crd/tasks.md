## 1. API Type Definition

- [x] 1.1 Create `api/v1alpha1/nodepairingrequest_types.go` with NodePairingRequest, NodePairingRequestSpec, and NodePairingRequestStatus structs
- [x] 1.2 Add RequestID field to NodePairingRequestSpec as required string with kubebuilder validation marker
- [x] 1.3 Add Conditions field to NodePairingRequestStatus as `[]metav1.Condition` with json tag and optional marker
- [x] 1.4 Add Status subresource marker (`+kubebuilder:subresource:status`) to NodePairingRequest type
- [x] 1.5 Add kubebuilder printcolumn markers for `kubectl get` output (at minimum: name, age, requestID)
- [x] 1.6 Add resource and kind markers (`+kubebuilder:resource:scope=Namespaced`)
- [x] 1.7 Register NodePairingRequest type in `api/v1alpha1/groupversion_info.go` init() function

## 2. Code Generation

- [x] 2.1 Run `make manifests` to generate CRD YAML in `config/crd/bases/`
- [x] 2.2 Run `make generate` to generate DeepCopy methods in `api/v1alpha1/zz_generated.deepcopy.go`
- [x] 2.3 Verify generated CRD manifest has correct API group, version, and scope

## 3. Controller Implementation

- [x] 3.1 Create `internal/controller/nodepairingrequest_controller.go` with NodePairingRequestReconciler struct
- [x] 3.2 Implement Reconcile() method with minimal logic (fetch resource, log, return success)
- [x] 3.3 Add RBAC markers for get, list, watch, create, update, patch, delete on NodePairingRequest resources
- [x] 3.4 Add RBAC marker for update on NodePairingRequest/status subresource
- [x] 3.5 Implement SetupWithManager() to configure controller watch on NodePairingRequest resources

## 4. Controller Registration

- [x] 4.1 Import NodePairingRequest types in `cmd/main.go`
- [x] 4.2 Add NodePairingRequest scheme registration in `cmd/main.go` init()
- [x] 4.3 Create and register NodePairingRequestReconciler with manager in main()
- [x] 4.4 Verify controller logs startup message when operator runs

## 5. Testing

- [x] 5.1 Create `internal/controller/nodepairingrequest_controller_test.go`
- [x] 5.2 Add NodePairingRequest scheme registration to `suite_test.go` TestMain
- [x] 5.3 Write test for NodePairingRequest creation with RequestID field
- [x] 5.4 Write test for controller reconciliation on resource creation
- [x] 5.5 Write test for controller reconciliation on resource update
- [x] 5.6 Write test for Status subresource update independence
- [x] 5.7 Write test for Status.Conditions field accessibility (verify empty array initially)
- [x] 5.8 Run `make test` and verify all tests pass

## 6. CRD Installation and Verification

- [x] 6.1 Run `make install` to install CRDs in development cluster
- [x] 6.2 Verify CRD registration with `kubectl get crd nodepairingrequests.openclaw.sandbox.redhat.com`
- [x] 6.3 Create sample NodePairingRequest CR and verify it is accepted
- [x] 6.4 Verify controller reconciles the sample CR (check logs)
- [x] 6.5 Test `kubectl get nodepairingrequest` shows printcolumns correctly

## 7. Documentation and Finalization

- [x] 7.1 Run `make lint` and fix any linting issues
- [x] 7.2 Run `make fmt` and `make vet` to ensure code quality
- [x] 7.3 Verify license headers in new files match `hack/boilerplate.go.txt`
- [x] 7.4 Update RBAC manifests with `make manifests` to capture new permissions
