## Why

The Claw CR status currently exposes a single `.status.url` field containing the gateway URL with the auth token fragment. Consumers (e.g., the UI, CLI tooling, `make wait-ready`) also need the device pairing URL, which differs only by an extra path segment. Exposing both URLs in status avoids forcing consumers to construct them manually and keeps URL-building logic centralized in the operator. Additionally, renaming the gateway URL to a more explicit field (`gatewayURL`) improves clarity while deprecating the original `url` field for a clean future removal.

## What Changes

- Add a `GatewayURL` field to `ClawStatus` — identical value to the existing `URL` field
- Add a `DevicePairingURL` field to `ClawStatus` — same base URL as the gateway, but with `/integration/device-pairing/` inserted before the `#token=` fragment; only populated when device pairing is enabled (`disableDevicePairing` is not true)
- Keep the existing `URL` field populated (now deprecated) — will be removed in a subsequent change
- Update `updateStatus` to populate all three fields

## Capabilities

### New Capabilities
- `status-urls`: Expose `gatewayURL` and `devicePairingURL` fields in Claw status, deprecate the existing `url` field

### Modified Capabilities

## Impact

- **API types**: `ClawStatus` struct in `api/v1alpha1/claw_types.go` gains two new fields
- **CRD YAML**: Regenerated via `make manifests` / `make generate`
- **Controller**: `updateStatus` in `internal/controller/claw_status.go` populates the new fields
- **Tests**: Status tests updated to assert the new fields
- **No breaking changes**: Existing `URL` field remains populated
