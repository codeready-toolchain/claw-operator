## 1. API Types

- [x] 1.1 Add `GatewayURL` and `DevicePairingURL` fields to `ClawStatus` in `api/v1alpha1/claw_types.go`, add deprecation comment on existing `URL` field
- [x] 1.2 Run `make generate` and `make manifests` to regenerate deepcopy and CRD YAML

## 2. Controller Logic

- [x] 2.1 Add `buildDevicePairingURL` helper in `internal/controller/claw_status.go` that inserts `/integration/device-pairing/` path before the token fragment
- [x] 2.2 Update `updateStatus` to populate `GatewayURL` (same value as `URL`) and `DevicePairingURL` (only when device pairing is enabled)

## 3. Tests

- [x] 3.1 Add unit tests for `buildDevicePairingURL` covering token auth, password auth, and empty route URL cases
- [x] 3.2 Update status integration tests to assert `GatewayURL` matches `URL` and `DevicePairingURL` is set/empty based on device pairing config

## 4. E2E Tests

- [x] 4.1 Add e2e test verifying `status.gatewayURL` and `status.url` are populated and equal when Claw is ready (token auth, device pairing enabled)
- [x] 4.2 Add e2e assertion verifying `status.devicePairingURL` is populated with `/integration/device-pairing/` path when device pairing is enabled
- [x] 4.3 Add e2e assertion in the existing `disableDevicePairing=true` test verifying `status.devicePairingURL` is empty

## 5. Validation

- [x] 5.1 Run `make build`, `make lint`, and `make test` to verify everything passes
