## 1. Test Infrastructure Setup

- [x] 1.1 Create `waitFor` polling helper function in `internal/controller/suite_test.go`
- [x] 1.2 Convert `suite_test.go` to use `TestMain(m *testing.M)` for envtest setup
- [x] 1.3 Remove Ginkgo `RunSpecs()` and replace with standard `TestMain` pattern
- [x] 1.4 Update package-level variables for envtest client and config

## 2. Migrate Controller Test Files

- [x] 2.1 Migrate `internal/controller/openclaw_configmap_controller_test.go` to standard library
- [x] 2.2 Migrate `internal/controller/openclaw_deployment_controller_test.go` to standard library
- [x] 2.3 Migrate `internal/controller/openclaw_gatewaysecret_controller_test.go` to standard library
- [x] 2.4 Migrate `internal/controller/openclaw_pvc_controller_test.go` to standard library
- [x] 2.5 Migrate `internal/controller/openclaw_route_config_controller_test.go` to standard library
- [x] 2.6 Migrate `internal/controller/openclaw_secretref_controller_test.go` to standard library
- [x] 2.7 Migrate `internal/controller/openclaw_status_controller_test.go` to standard library
- [x] 2.8 Migrate `internal/controller/openclaw_url_status_controller_test.go` to standard library

## 3. Test Pattern Conversions

- [x] 3.1 Replace all `Describe`/`Context`/`It` blocks with `Test*` functions and `t.Run` subtests
- [x] 3.2 Replace all `Expect().To()` assertions with `if/t.Errorf` or `t.Fatalf` patterns
- [x] 3.3 Replace all `Eventually()` calls with `waitFor()` helper function
- [x] 3.4 Replace all `AfterEach` cleanup with `t.Cleanup()` calls
- [x] 3.5 Verify all table-driven tests use proper subtest structure with `t.Run`

## 4. Dependency Cleanup

- [x] 4.1 Remove all Ginkgo imports (`github.com/onsi/ginkgo/v2`) from test files
- [x] 4.2 Remove all Gomega imports (`github.com/onsi/gomega`) from test files
- [x] 4.3 Run `go mod tidy` to remove Ginkgo/Gomega dependencies from `go.mod` (NOTE: dependencies remain for e2e tests)
- [x] 4.4 Verify `go mod graph | grep ginkgo` returns empty (NOTE: ginkgo remains as transitive dep for e2e tests)

## 5. Validation and Documentation

- [x] 5.1 Run `make test` and verify all tests pass
- [x] 5.2 Run `go test -cover ./internal/controller/` and verify coverage matches original
- [x] 5.3 Run `go test -v ./internal/controller/` and verify test count matches original
- [x] 5.4 Update CLAUDE.md to document standard library testing patterns (polling helper, TestMain, table-driven tests)
- [x] 5.5 Verify `make lint` passes with no Ginkgo-related linter warnings

## 6. Testify Integration

- [x] 6.1 Add testify/require and testify/assert imports to all test files
- [x] 6.2 Replace manual error checks with `require.NoError(t, err)` and `require.Error(t, err)`
- [x] 6.3 Replace value assertions with `assert.Equal(t, expected, actual)` and related assert methods
- [x] 6.4 Update CLAUDE.md with testify usage examples
- [x] 6.5 Verify all tests pass with testify integration
