## Context

The openclaw-operator currently uses Ginkgo v2 and Gomega for all unit tests in `internal/controller/` and `api/v1alpha1/`. Tests use envtest (real Kubernetes API server) with shared setup in `suite_test.go`. The team's standard practice is to use Go's standard `testing` package for simplicity and reduced dependencies.

Current test structure:
- `suite_test.go`: Boots envtest once per package using `RunSpecs()`
- Individual test files: Use `Describe`/`Context`/`It` blocks with `Eventually()` for async assertions
- Cleanup: Uses `AfterEach` blocks

## Goals / Non-Goals

**Goals:**
- Migrate all unit tests to Go standard library `testing` package
- Remove Ginkgo/Gomega dependencies from `go.mod`
- Maintain 100% test coverage and all existing test scenarios
- Preserve envtest-based testing approach
- Maintain compatibility with `make test` command

**Non-Goals:**
- Changing test behavior or adding new test cases
- Modifying e2e tests (currently in `test/e2e/`, separate migration if needed)
- Changing envtest configuration or cluster setup
- Refactoring production code

## Decisions

### Decision 1: File-by-file migration strategy

**Choice**: Migrate all test files in a single commit rather than incremental file-by-file migration.

**Rationale**: 
- Ginkgo and standard library can coexist in the same package, but having mixed test styles creates confusion
- Suite setup (`suite_test.go`) is shared, making partial migration awkward
- Single commit makes review easier and enables atomic rollback if needed

**Alternative considered**: Incremental migration
- Would allow spreading work across multiple PRs
- Rejected because: shared suite setup creates coupling, and mixed test styles reduce readability

### Decision 2: Polling helper for async assertions

**Choice**: Create a `waitFor(t *testing.T, timeout, interval time.Duration, condition func() bool, message string)` helper function to replace Gomega's `Eventually()`.

**Rationale**:
- Preserves timeout semantics (10s timeout, 250ms poll interval)
- Centralizes async logic rather than duplicating in each test
- Accepts condition function for flexibility

**Alternative considered**: Inline polling loops in each test
- Rejected because: duplicates code and increases error surface area

**Implementation**:
```go
func waitFor(t *testing.T, timeout, interval time.Duration, condition func() bool, message string) {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if condition() {
            return
        }
        time.Sleep(interval)
    }
    t.Fatalf("timeout waiting for condition: %s", message)
}
```

### Decision 3: TestMain for envtest setup

**Choice**: Replace Ginkgo's `RunSpecs()` in `suite_test.go` with `TestMain(m *testing.M)` for envtest lifecycle.

**Rationale**:
- `TestMain` is the standard library mechanism for suite-level setup
- Provides equivalent functionality (setup before tests, cleanup after)
- Exit code propagation via `os.Exit(m.Run())` maintains CI compatibility

**Structure**:
```go
func TestMain(m *testing.M) {
    // Start envtest
    testEnv = &envtest.Environment{...}
    cfg, err := testEnv.Start()
    // Setup client, scheme, etc.
    
    code := m.Run()
    
    // Stop envtest
    testEnv.Stop()
    os.Exit(code)
}
```

### Decision 4: Table-driven tests for multiple scenarios

**Choice**: Use table-driven test pattern for tests with multiple input/output combinations.

**Rationale**:
- Idiomatic Go pattern for parameterized tests
- Clear separation of test data from test logic
- Easy to add new scenarios

**Pattern**:
```go
func TestReconciler(t *testing.T) {
    tests := []struct {
        name string
        input X
        want Y
    }{
        {name: "scenario1", input: ..., want: ...},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

### Decision 5: Preserve test file organization

**Choice**: Keep current test file organization (separate test files per resource type: `openclaw_configmap_controller_test.go`, etc.).

**Rationale**:
- Existing organization is clear and follows controller structure
- No benefit to restructuring during migration
- Reduces diff size and review burden

## Risks / Trade-offs

**[Risk] Test bugs introduced during manual conversion**
- **Mitigation**: 
  - Run `make test` after each file migration to catch failures early
  - Compare test count before/after (number of `It` blocks vs `t.Run` calls)
  - Leverage code review to catch missed assertions

**[Risk] Polling helper timeout/interval mismatch**
- **Mitigation**: 
  - Document timeout values in helper function
  - Use named constants (`defaultTimeout = 10*time.Second`)
  - Verify async tests still pass after migration

**[Risk] Developer learning curve**
- **Mitigation**:
  - Standard library testing is widely known (lower barrier than Ginkgo)
  - CLAUDE.md will document new patterns for future contributors
  - Table-driven tests and `t.Run` are idiomatic Go

**[Risk] Temporary CI instability**
- **Mitigation**:
  - Run full test suite locally before pushing
  - Merge during low-activity period if possible
  - Rollback plan: revert single commit

## Migration Plan

**Implementation steps:**
1. Create polling helper (`waitFor`) in `suite_test.go`
2. Convert `suite_test.go` to use `TestMain` for envtest setup
3. Migrate each test file:
   - Replace `Describe`/`It` with `Test*` functions and `t.Run` subtests
   - Replace `Expect().To()` with `if/t.Errorf` assertions
   - Replace `Eventually()` with `waitFor()` helper
   - Replace `AfterEach` with `t.Cleanup()`
4. Remove Ginkgo/Gomega imports from all test files
5. Run `go mod tidy` to remove dependencies
6. Verify `make test` passes
7. Update CLAUDE.md to document standard library testing patterns

**Rollback strategy:**
- Single commit migration enables clean revert via `git revert`
- If issues discovered post-merge: revert, fix, re-migrate

**Validation:**
- All tests pass: `make test`
- Coverage unchanged: `go test -cover ./...`
- No Ginkgo deps: `go mod graph | grep ginkgo` returns empty
