## Context

The Claw CR status currently has a single `URL` field populated in `updateStatus` when all deployments are ready. It contains the route URL with an optional `#token=<value>` fragment. The device pairing app lives at the same host under `/integration/device-pairing/`, but consumers must construct this URL themselves.

## Goals / Non-Goals

**Goals:**
- Expose `gatewayURL` and `devicePairingURL` as explicit fields in `ClawStatus`
- Keep `url` populated with the same value as `gatewayURL` for backward compatibility
- Only populate `devicePairingURL` when device pairing is enabled

**Non-Goals:**
- Removing the deprecated `url` field (deferred to a follow-up change)
- Changing how the route URL or token are resolved
- Modifying the device pairing application itself

## Decisions

1. **New fields alongside existing field**: Add `GatewayURL` and `DevicePairingURL` as sibling fields to `URL` in `ClawStatus`. The `URL` field keeps its current behavior and gets a deprecation marker in the Go doc comment. This avoids breaking existing consumers.

2. **Device pairing URL construction**: Insert `/integration/device-pairing/` between the route host and the `#token=` fragment. For password mode (no token fragment), append the path without a fragment. The `buildClawURL` helper is extended with a new `buildDevicePairingURL` function that reuses the same route URL and token.

3. **Conditional population of `devicePairingURL`**: Only set when `shouldDisableDevicePairing(auth)` returns false. When device pairing is disabled, the field remains empty — matching the existing behavior where the device pairing deployment is skipped entirely.

## Risks / Trade-offs

- **Additional status fields increase CRD surface** → Minimal risk; two string fields with clear semantics. The deprecated `url` field will be cleaned up in a follow-up.
- **URL format coupling** → The `/integration/device-pairing/` path is an OpenClaw convention. If it changes upstream, the operator must be updated. This is acceptable since the operator already manages the device pairing deployment.
