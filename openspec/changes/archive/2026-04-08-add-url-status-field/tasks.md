## 1. Update CRD Type Definition

- [x] 1.1 Add `URL string` field to `OpenClawStatus` struct in `api/v1alpha1/openclaw_types.go`
- [x] 1.2 Run `make manifests` to regenerate CRD YAML in `config/crd/bases/`
- [x] 1.3 Run `make generate` to regenerate DeepCopy methods

## 2. Update Controller Logic

- [x] 2.1 Add helper method `getRouteURL(ctx, instance)` to fetch Route and return `https://` + host
- [x] 2.2 Update `updateStatus` method to call `getRouteURL` after checking deployment readiness
- [x] 2.3 Populate `instance.Status.URL` only when both deployments are ready
- [x] 2.4 Handle Route not found error gracefully (leave URL empty on non-OpenShift)

## 3. Add Tests

- [x] 3.1 Add test for URL field populated when deployments ready and Route exists
- [x] 3.2 Add test for URL field empty when deployments not ready
- [x] 3.3 Add test for URL field empty when Route does not exist
- [x] 3.4 Add test for URL format includes `https://` scheme

## 4. Validation

- [x] 4.1 Run `make test` to verify unit tests pass
- [x] 4.2 Run `make lint` to verify code quality
- [x] 4.3 Verify URL is populated correctly when instance becomes ready
