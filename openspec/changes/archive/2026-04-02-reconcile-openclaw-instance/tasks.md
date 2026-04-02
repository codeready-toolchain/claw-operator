## 1. Setup Manifest Embedding

- [x] 1.1 Add `//go:embed internal/manifests/deployment.yaml` directive to controller file
- [x] 1.2 Add `_ "embed"` import to controller file
- [x] 1.3 Declare variable to hold embedded manifest content as `[]byte` or `string`

## 2. Add RBAC Permissions

- [x] 2.1 Add RBAC marker comments for Deployment resources (create, get, list, watch)
- [x] 2.2 Regenerate RBAC manifests with `make manifests`

## 3. Implement Reconcile Logic - Fetch OpenClawInstance

- [x] 3.1 Add code to fetch OpenClawInstance resource using `r.Get()` with request namespace/name
- [x] 3.2 Handle `NotFound` error case - return successfully if resource doesn't exist
- [x] 3.3 Handle other fetch errors - return error to requeue

## 4. Implement Reconcile Logic - Parse Manifest

- [x] 4.1 Create deserializer using `scheme.Codecs.UniversalDeserializer()`
- [x] 4.2 Call `Decode()` on embedded manifest bytes to parse into `appsv1.Deployment`
- [x] 4.3 Handle decode errors appropriately (log and return error)

## 5. Implement Reconcile Logic - Create Deployment

- [x] 5.1 Set Deployment namespace to match OpenClawInstance namespace
- [x] 5.2 Call `controllerutil.SetControllerReference()` to establish ownership
- [x] 5.3 Call `r.Create()` to create Deployment
- [x] 5.4 Handle `AlreadyExists` error - skip creation and return successfully
- [x] 5.5 Handle other creation errors - return error to requeue

## 6. Testing

- [x] 6.1 Write envtest test case: create OpenClawInstance and verify Deployment is created
- [x] 6.2 Write envtest test case: verify Deployment has correct owner reference
- [x] 6.3 Write envtest test case: verify Deployment is in same namespace as OpenClawInstance
- [x] 6.4 Write envtest test case: delete OpenClawInstance and verify Deployment is garbage collected
- [x] 6.5 Run `make test` to verify all tests pass
