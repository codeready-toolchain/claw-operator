## 1. Reconciler Setup

- [x] 1.1 Add `Config *rest.Config` field to `ClawDevicePairingRequestReconciler` struct
- [x] 1.2 Pass `mgr.GetConfig()` when constructing the reconciler in `cmd/main.go`
- [x] 1.3 Add `pods/exec` RBAC marker (`// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create`) and run `make manifests`

## 2. Core Exec Logic

- [x] 2.1 Add early-return guard: skip reconcile if CR already has `Ready=True, Reason=DevicePaired`
- [x] 2.2 In the single-pod-match branch, set `Ready=False, Reason=Processing` and persist via status update before exec
- [x] 2.3 Implement pod exec function using `client-go/tools/remotecommand` — exec `openclaw device approve <requestID> --json` in the `gateway` container with a 30-second context timeout
- [x] 2.4 On exec success (exit 0): log stdout, set `Ready=True, Reason=DevicePaired`
- [x] 2.5 On exec failure (non-zero exit or exec error): log stderr, set `Ready=False, Reason=PairingFailed` with error in message, return nil (no requeue)

## 3. Tests

- [x] 3.1 Test: already-paired CR is skipped (no exec, no status change)
- [x] 3.2 Test: Processing condition is set before exec attempt
- [x] 3.3 Test: successful exec sets DevicePaired condition
- [x] 3.4 Test: failed exec sets PairingFailed condition
- [x] 3.5 Verify existing tests still pass (`go test ./internal/controller/ -run TestClawDevicePairingRequest -v`)
