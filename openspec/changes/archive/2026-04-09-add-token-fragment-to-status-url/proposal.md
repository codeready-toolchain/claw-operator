## Why

The OpenClaw status URL currently provides the Route HTTPS endpoint but requires users to manually copy the gateway token from the `openclaw-gateway-token` Secret and paste it into the UI. This creates unnecessary friction in the user experience and requires additional kubectl commands to retrieve the token.

## What Changes

- Modify `OpenClaw.status.url` to include a URL fragment containing the gateway token: `https://openclaw-route.example.com#token=<gateway-token-value>`
- Update the `updateStatus()` method in the controller to read the gateway token from the `openclaw-gateway-token` Secret and append it as a URL fragment
- Ensure the token value is retrieved from the `token` key in the `openclaw-gateway-token` Secret

## Capabilities

### New Capabilities
<!-- No new capabilities being introduced -->

### Modified Capabilities
- `status-url`: Status URL field now includes gateway token as URL fragment for seamless authentication

## Impact

- Controller code: `internal/controller/openclaw_resource_controller.go` - `updateStatus()` method needs to fetch Secret and append token fragment
- Status update logic: Additional Secret read operation during status updates
- User experience: Users can now click the status URL and automatically authenticate without manual token entry
