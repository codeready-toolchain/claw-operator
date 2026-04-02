## 1. Implement Name Filtering Logic

- [x] 1.1 Add name check in Reconcile function after successful Get of OpenClawInstance
- [x] 1.2 Compare resource name to literal string "instance"
- [x] 1.3 Return `ctrl.Result{}, nil` if name does not match
- [x] 1.4 Add info-level log message when skipping due to name mismatch

## 2. Update Tests

- [x] 2.1 Add test case: verify resource named "instance" is reconciled and creates Deployment
- [x] 2.2 Add test case: verify resource with different name (e.g., "other") is skipped
- [x] 2.3 Add test case: verify skipped resource does not create Deployment
- [x] 2.4 Add test case: verify multiple resources, only "instance" is reconciled
- [x] 2.5 Run `make test` to verify all tests pass
