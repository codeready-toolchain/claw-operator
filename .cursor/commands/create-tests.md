---
name: create-tests
description: Write tests for recent code changes following project conventions
---

# Writing Tests for Recent Changes

Analyze the recent code changes (staged, unstaged, or described by the user) and write tests that cover the new or modified behavior. Follow the guidelines below.

## Running Tests

### From Project Root

- `make test` — Run all unit tests (envtest-based, excludes e2e)
- `make lint` — Run golangci-lint
- `make test-e2e` — Run e2e tests (requires Kind)

### Direct Go Commands

- **All controller tests**: `go test ./internal/controller/... -v`
- **Specific test**: `go test ./internal/controller -run TestOpenClawConfigMap -v`
- **Specific subtest**: `go test ./internal/controller -run "TestOpenClawDeploymentController/When_reconciling_an_OpenClaw_named" -v`
- **With race detector**: `go test -race ./internal/controller/... -v`

## Critical Rules

### 1. Be Pragmatic
Write tests that validate meaningful reconciler behavior. If a test requires excessive setup or doesn't test real operator logic, skip it. Prefer integration-level envtest tests over pure unit tests with heavy mocking — envtest gives a real API server for free.

### 2. Test Behavior, Not Implementation
Focus on what resources the reconciler creates and what status it sets — not internal method call sequences. Assert on the Kubernetes objects that exist after reconciliation.

### 3. No Historical Comments in Code
No `// Added in PR-123` or `// Testing new feature`. Historical context belongs in git commits. Tests should be self-explanatory.

### 4. Extend Existing Tests Before Creating New Ones
Each test file covers a specific resource or concern. When adding test coverage, first check if an existing test file and test function already covers that area. Add subtests via `t.Run()` to existing functions rather than creating new top-level test functions that duplicate setup.

### 5. All Tests Must Pass
New tests are done ONLY when they ALL pass 100%. Don't leave failing tests. If a test is too complex to get right, don't write it.
- `make test` — runs envtest-based controller tests (fast, no cluster needed). Run this first and iterate until green.
- `make test-e2e` — runs e2e tests against a Kind cluster (slow, builds Docker image). Run after `make test` passes to catch integration issues.

### 6. No Documentation Files Unless Explicitly Requested
DO NOT create summary documents, README files, or any markdown documentation files unless the user explicitly asks for them. Focus exclusively on creating the actual test code.

## Test Organization

- **Shared setup**: `internal/controller/suite_test.go` — boots envtest via `TestMain`, provides `k8sClient`, `ctx`, cleanup helpers
- **Per-resource test files**: Separate `*_test.go` per concern for readability:
  - `openclaw_configmap_controller_test.go` — ConfigMap creation, ownership
  - `openclaw_deployment_controller_test.go` — Deployment creation, ownership, NetworkPolicy
  - `openclaw_pvc_controller_test.go` — PVC creation, ownership
  - `openclaw_secretref_controller_test.go` — Secret reference configuration, version stamping
  - `openclaw_gatewaysecret_controller_test.go` — Gateway Secret creation, token generation
  - `openclaw_status_controller_test.go` — Status conditions, URL field, transition times
  - `openclaw_route_config_controller_test.go` — Route host injection, CORS configuration
  - `openclaw_url_status_controller_test.go` — URL status field behavior
- **E2E tests**: `test/e2e/` — runs against a Kind cluster with the real operator image deployed

## Project Conventions

- **Assertions**: `testify/require` for fatal setup errors, `testify/assert` for value comparisons
- **Async polling**: `waitFor(t, timeout, interval, conditionFunc, message)` — custom helper (10s timeout, 250ms poll)
- **Table-driven tests**: Standard Go pattern with `t.Run(tt.name, ...)` for parameterized scenarios
- **Cleanup**: Always use `t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })`
- **Test helpers**: Mark with `t.Helper()`. Use `createTestAPIKeySecret()` and `createTestGatewaySecret()` from suite_test.go
- **License header**: All test files must include the Apache 2.0 license header (copy from any existing test file)

## Test Structure Pattern

Every controller test follows this structure:

```go
func TestOpenClawSomething(t *testing.T) {
    t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
        const resourceName = ClawInstanceName
        ctx := context.Background()

        t.Run("should do something specific", func(t *testing.T) {
            t.Cleanup(func() {
                deleteAndWaitAllResources(t, namespace)
            })
            // given
            createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
            
            // when
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

            // then
            resource := &SomeType{}
            waitFor(t, timeout, interval, func() bool {
                err := k8sClient.Get(ctx, client.ObjectKey{
                    Name:      ExpectedName,
                    Namespace: namespace,
                }, resource)
                return err == nil
            }, "resource should be created")

            // 5. Assert specific properties with testify
            assert.Equal(t, expected, actual, "description")
        })
    })
}
```

## Key Constants and Helpers (from suite_test.go and controller)

- `ClawInstanceName` — only `"instance"` is reconciled
- `ClawConfigMapName`, `ClawPVCName`, `ClawDeploymentName`, `ClawGatewaySecretName`
- `ClawProxyDeploymentName`, `ClawProxyServiceName`, `ClawServiceName`
- `ClawNetworkPolicyName`, `ClawIngressNetworkPolicyName`
- `ClawResourceKind` — for owner reference assertions
- `GatewayTokenKeyName` — data key in gateway Secret
- `k8sClient` — shared envtest client
- `namespace` — `"default"`
- `timeout` / `interval` — 10s / 250ms
- `deleteAndWaitAllResources(t, namespace)` — cleanup all operator-managed resources
- `createClawInstance(t, name, namespace)` - creates a Claw instance along with a secret for the API key
- `createTestAPIKeySecret(name, namespace, key, value)` — creates a test Secret for the API key
- `createTestGatewaySecret(t, name, namespace)` — creates a gateway Secret with generated token

## Pitfalls

- Every Claw instance MUST have `Spec.GeminiAPIKey` set — the reconciler requires it
- The API key Secret must exist before reconciliation or the reconciler returns an error
- envtest has no Route CRD — tests requiring Route behavior are skipped or use dynamic CRD installation
- PVCs have finalizers in envtest — `deleteAndWait` handles stripping them; always use the cleanup helpers
- Tests run in the `"default"` namespace — don't create a custom namespace
- Server-side apply means resources may exist from a previous subtest — always clean up via `t.Cleanup`
- Only Claw instances named `"instance"` trigger reconciliation — tests for non-matching names verify no resources are created
- Do NOT use `t.Parallel()` — all tests share a single envtest cluster, one namespace, and fixed resource names; parallel subtests will cause race conditions and flaky failures

---

**BE PRACTICAL. CREATE ONLY TESTS THAT BRING REAL VALUE. DO NOT DUPLICATE TESTS AT THE SAME LEVEL.**

**DO NOT CREATE DOCUMENTATION OR SUMMARY FILES UNLESS EXPLICITLY REQUESTED.**

**Now create tests for the new functionality following the above guidelines.**
