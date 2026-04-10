## Why

The project currently uses Ginkgo/Gomega for unit tests, but this is not the team's default testing library. Migrating to Go's standard library `testing` package reduces external dependencies, simplifies onboarding for developers familiar with idiomatic Go testing, and aligns with Go ecosystem conventions.

## What Changes

- Convert all unit test files from Ginkgo/Gomega syntax to standard library `testing` package
- Replace `Describe`/`Context`/`It` blocks with standard `Test*` functions and `t.Run` subtests
- Replace Gomega assertions (`Expect`, `Eventually`) with standard `testing` methods and helper functions
- Update test suite setup (envtest initialization) to use standard `TestMain` pattern
- Maintain 100% test coverage and all existing test scenarios
- Remove Ginkgo/Gomega dependencies from `go.mod`

## Capabilities

### New Capabilities

- `standard-library-testing`: Defines the testing framework, patterns, and conventions for unit tests using Go standard library (table-driven tests, subtests, test helpers, envtest setup)

### Modified Capabilities

<!-- No existing capability requirements are changing - this is purely a test implementation change -->

## Impact

- **Test Files**: All `*_test.go` files in `internal/controller/` and `api/v1alpha1/`
- **Dependencies**: `go.mod` and `go.sum` (removal of Ginkgo/Gomega packages)
- **CI**: Existing `make test` command continues to work (internally runs `go test` which works with both frameworks)
- **Developers**: One-time learning curve for contributors familiar with Ginkgo patterns
