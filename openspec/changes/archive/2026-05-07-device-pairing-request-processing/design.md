## Context

The `ClawDevicePairingRequestReconciler` already handles pod selection via label selectors and sets status conditions for error cases (no match, multiple matches, invalid selector). The success path currently has a `TODO` placeholder — it finds the target pod but doesn't execute the actual pairing command. The controller needs to exec `openclaw devices approve <requestID> --json` inside the matched pod's `gateway` container and report results.

The existing codebase has no pod exec usage. The controller currently only uses `controller-runtime`'s `client.Client` (which doesn't support exec). Pod exec requires a `rest.Config` and the `client-go` `remotecommand` package.

## Goals / Non-Goals

**Goals:**
- Execute `openclaw devices approve <requestID> --json` inside the matched pod's `gateway` container
- Set intermediate `Processing` condition before exec, then `DevicePaired` or `PairingFailed` after
- Log the JSON output from the command
- Add `pods/exec` RBAC permission
- Terminal state: no requeue after success or failure (one-shot operation)

**Non-Goals:**
- Retry logic for failed exec (the CR is a one-shot request)
- Streaming or real-time output
- Timeout configuration (use a reasonable fixed timeout)
- Parsing or acting on the JSON output (just log it)

## Decisions

### 1. Pod exec mechanism: `client-go/tools/remotecommand`

Use the standard `k8s.io/client-go/tools/remotecommand` package with `SPDYExecutor`. This is the canonical way to exec into pods from a Go controller.

**Alternative considered:** Using a `Job` to run a kubectl exec command — rejected because it adds unnecessary complexity and resources for a simple one-shot command.

**Alternative considered:** Using the `PodExec` subresource directly via REST — `remotecommand` already wraps this cleanly.

### 2. Passing `rest.Config` to the reconciler

Add a `Config *rest.Config` field to `ClawDevicePairingRequestReconciler` and pass `mgr.GetConfig()` from `cmd/main.go`. This is the same pattern used by other controllers that need raw API access.

### 3. Target container: `gateway`

The `openclaw` CLI is available in the `gateway` container (defined as `ClawGatewayContainerName = "gateway"` in the codebase). The exec command targets this container explicitly.

### 4. Command: `openclaw devices approve <requestID> --json`

The command uses `--json` flag for structured output. The controller captures stdout and stderr, logs both, and determines success from the exit code.

### 5. Status condition flow

```
CR created → Reconcile triggers
  → Pod found → set Ready=False/Processing → exec command
    → exec succeeds (exit 0) → set Ready=True/DevicePaired
    → exec fails (non-zero exit or error) → set Ready=False/PairingFailed
```

The `Processing` condition is set and persisted before exec to provide visibility into in-progress operations. Two status updates happen: one before exec, one after.

### 6. No requeue on terminal states

All outcomes are terminal: `DevicePaired` and `PairingFailed` return `ctrl.Result{}` with no error (no requeue). The CR is a one-shot request — the user creates a new CR if they want to retry.

### 7. Guard against re-processing

If the CR already has `Ready=True/DevicePaired`, skip exec and return immediately. This prevents re-running the approval command on subsequent reconcile triggers (e.g., from status updates).

## Risks / Trade-offs

- **[Risk] Exec timeout** → Use a context with a 30-second timeout for the exec operation. If OpenClaw hangs, the controller won't block indefinitely.
- **[Risk] Pod not running** → The exec will fail naturally if the pod isn't in Running phase. The error message in the `PairingFailed` condition will indicate this.
- **[Risk] Double status update per reconcile** → Setting `Processing` then `DevicePaired` requires two API calls per reconcile. Acceptable for a one-shot operation that happens infrequently.
- **[Trade-off] No retry on exec failure** → Simpler design, but the user must create a new CR to retry. Acceptable because pairing failures typically need human investigation.
