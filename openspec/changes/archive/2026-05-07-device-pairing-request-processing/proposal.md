## Why

The ClawDevicePairingRequest controller currently finds the target pod but doesn't actually execute the device pairing approval command. The `TODO` in the reconcile loop needs to be replaced with real logic that runs `openclaw devices approve <requestID> --json` inside the matched pod and reports the outcome via status conditions.

## What Changes

- Replace the `TODO` placeholder in `ClawDevicePairingRequestReconciler.Reconcile()` with pod exec logic that runs `openclaw devices approve <requestID> --json` inside the matched pod
- Add an intermediate `Ready=False, Reason=Processing` status condition before executing the command
- Update status to `Ready=True, Reason=DevicePaired` on success or `Ready=False, Reason=PairingFailed` on exec failure
- Add `pods/exec` RBAC permission to the controller
- Log the JSON output from the exec command

## Capabilities

### New Capabilities
- `device-pairing-exec`: Pod exec logic to run the `openclaw devices approve` command inside the target pod and report results via status conditions

### Modified Capabilities

## Impact

- `internal/controller/claw_device_pairing_request_controller.go` — main reconcile logic changes
- `internal/controller/claw_device_pairing_request_controller_test.go` — new tests for exec scenarios
- `cmd/main.go` — may need to pass rest.Config to the reconciler for pod exec
- RBAC markers — new `pods/exec` permission
- CRD status conditions — new reason values (`Processing`, `DevicePaired`, `PairingFailed`)
