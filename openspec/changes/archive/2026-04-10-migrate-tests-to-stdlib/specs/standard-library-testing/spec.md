## ADDED Requirements

### Requirement: Test functions use standard naming
All test functions MUST follow Go standard library conventions with `Test*` naming and accept `*testing.T` parameter.

#### Scenario: Top-level test function
- **WHEN** a test file contains tests
- **THEN** each test MUST be a function named `Test<Name>(t *testing.T)`

#### Scenario: Nested subtests
- **WHEN** a test has multiple related scenarios
- **THEN** subtests MUST be created using `t.Run(name, func(t *testing.T) {...})`

### Requirement: Assertions use standard testing methods
Test assertions MUST use standard `testing` package methods and helper functions instead of external assertion libraries.

#### Scenario: Basic equality check
- **WHEN** comparing expected and actual values
- **THEN** test MUST use `if got != want { t.Errorf(...) }` pattern

#### Scenario: Fatal errors
- **WHEN** a condition prevents further test execution
- **THEN** test MUST use `t.Fatal()` or `t.Fatalf()` to stop immediately

#### Scenario: Error reporting
- **WHEN** reporting test failures
- **THEN** error messages MUST include both expected and actual values using `t.Errorf("got %v, want %v", got, want)`

### Requirement: Async assertions use polling helpers
Tests requiring eventual consistency MUST use polling helper functions instead of external assertion libraries.

#### Scenario: Wait for condition
- **WHEN** a test needs to wait for an async condition (e.g., Kubernetes resource ready)
- **THEN** test MUST use a polling helper that checks condition repeatedly with timeout

#### Scenario: Timeout exceeded
- **WHEN** polling exceeds timeout without success
- **THEN** helper MUST call `t.Fatalf()` with clear timeout message

#### Scenario: Polling configuration
- **WHEN** polling for conditions
- **THEN** timeout MUST be 10 seconds and poll interval MUST be 250 milliseconds (matching Gomega Eventually defaults)

### Requirement: Test suite setup uses TestMain
Test suites requiring shared setup (e.g., envtest) MUST use `TestMain` function for initialization and cleanup.

#### Scenario: Envtest initialization
- **WHEN** tests require Kubernetes API server (envtest)
- **THEN** `TestMain(m *testing.M)` MUST start envtest before `m.Run()` and stop after

#### Scenario: Cleanup on exit
- **WHEN** TestMain completes
- **THEN** cleanup MUST run via `defer` to ensure resources release even on test failure

#### Scenario: Exit code propagation
- **WHEN** TestMain exits
- **THEN** it MUST call `os.Exit(m.Run())` to propagate test result code

### Requirement: Table-driven tests for multiple scenarios
Tests with multiple input/output combinations MUST use table-driven test pattern.

#### Scenario: Test table structure
- **WHEN** a test has multiple scenarios
- **THEN** test MUST define a slice of structs containing `name`, `input`, and `want` fields

#### Scenario: Subtest per table entry
- **WHEN** iterating table entries
- **THEN** each entry MUST run as subtest using `t.Run(tt.name, func(t *testing.T) {...})`

#### Scenario: Parallel execution
- **WHEN** table-driven tests are independent
- **THEN** subtests MAY call `t.Parallel()` for concurrent execution

### Requirement: Test cleanup uses t.Cleanup
Tests requiring cleanup MUST use `t.Cleanup()` instead of `defer` for resource cleanup.

#### Scenario: Resource cleanup
- **WHEN** a test creates resources (e.g., Kubernetes objects)
- **THEN** test MUST register cleanup using `t.Cleanup(func() {...})` immediately after creation

#### Scenario: Cleanup order
- **WHEN** multiple cleanup handlers registered
- **THEN** handlers MUST execute in LIFO order (last registered runs first)

#### Scenario: Cleanup on failure
- **WHEN** test fails or panics
- **THEN** all registered cleanup handlers MUST still execute

### Requirement: No external test dependencies
Unit tests MUST NOT depend on Ginkgo or Gomega packages.

#### Scenario: Import statements
- **WHEN** reviewing test file imports
- **THEN** imports MUST NOT include `github.com/onsi/ginkgo` or `github.com/onsi/gomega`

#### Scenario: Module dependencies
- **WHEN** running `go mod tidy` after migration
- **THEN** `go.mod` MUST NOT contain Ginkgo or Gomega dependencies

### Requirement: Test coverage preserved
Migrated tests MUST maintain 100% coverage of existing test scenarios.

#### Scenario: Existing test scenarios
- **WHEN** comparing migrated tests to original Ginkgo tests
- **THEN** every Ginkgo `It` block MUST have corresponding standard library test or subtest

#### Scenario: Assertion count
- **WHEN** counting assertions in migrated tests
- **THEN** number of assertions MUST equal or exceed original Ginkgo tests
