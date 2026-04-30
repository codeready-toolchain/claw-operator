---
name: test-and-fix
description: Run build, lint, and tests — fix all issues until green
---

# Check and Fix

Run `make build`, `make lint-fix`, and `make test` to find all issues, then fix them.

1. Run `make build` to catch compilation errors. Fix any that appear before proceeding.
2. Run `make lint-fix` to auto-fix lint issues and surface remaining ones. Fix anything that `--fix` couldn't resolve.
3. Run `make test` to run all envtest-based unit tests. For failures:
   - Read the failing test AND the code under test to understand the root cause
   - Fix the source code, not the tests — unless the test itself is wrong
   - Run the specific failing test to verify: `go test ./internal/controller -run TestName -v`
4. Repeat steps 1–3 until all three commands pass cleanly.
5. Run `make test-e2e` to run e2e tests against a Kind cluster. This builds the container image, sets up the cluster, and runs the full suite. For failures:
   - Read the failing e2e test in `test/e2e/` and the relevant controller code
   - Fix the source code, then re-run `make test-e2e`
6. Repeat from step 1 if source fixes from e2e break unit tests or lint.

Do not create documentation files or summaries. Focus exclusively on making the build green.
