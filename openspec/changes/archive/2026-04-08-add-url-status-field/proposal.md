## Why

Users need to know the URL to access their OpenClaw instance after deployment. Currently, they must manually query the Route resource to discover the access URL, which adds friction to the user experience.

## What Changes

- Add a `URL` field to `OpenClaw.Status` that contains the full HTTPS URL of the OpenClaw Route
- Populate this field only when both `openclaw` and `openclaw-proxy` Deployments are ready
- The URL must include the `https://` scheme

## Capabilities

### New Capabilities

- `status-url-field`: Populate the URL status field from the Route resource when deployments are ready

### Modified Capabilities

<!-- No existing capabilities are being modified -->

## Impact

- `api/v1alpha1/openclaw_types.go`: Add `URL` field to `OpenClawStatus` struct
- `internal/controller/openclaw_resource_controller.go`: Update status reconciliation to fetch Route and populate URL field
- CRD manifests: Regenerate to include new status field
- Users can now discover the access URL directly from `kubectl get openclaw instance -o yaml` without querying the Route
