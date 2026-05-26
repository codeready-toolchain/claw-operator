## Why

The claw-device-pairing Deployment manifest has liveness and readiness probes but is missing explicit `timeoutSeconds` and `failureThreshold` settings. Since this is a tiny backend that starts quickly, the probes should have small, explicit timeouts to ensure fast failure detection and rapid pod cycling on health issues.

## What Changes

- Add explicit `timeoutSeconds` (small value, e.g., 2s) to both liveness and readiness probes
- Add explicit `failureThreshold` to both probes for fast failure detection
- Add a `startupProbe` to separate startup detection from ongoing liveness checks, keeping all timeouts tight

## Capabilities

### New Capabilities

### Modified Capabilities
- `device-pairing-deployment`: Adding explicit probe timeout and failure threshold configuration, plus a startup probe

## Impact

- `internal/assets/manifests/claw-device-pairing/deployment.yaml` — probe configuration changes
- No API, CRD, or controller code changes required — this is a manifest-only change
