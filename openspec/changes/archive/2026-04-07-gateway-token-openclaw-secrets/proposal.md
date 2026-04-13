## Why

The OpenClaw gateway currently lacks authentication, allowing unrestricted access to any client that can reach the service. A secure, randomly-generated token is needed to authenticate gateway requests and prevent unauthorized access.

## What Changes

- OpenClawResourceReconciler creates a new secret `openclaw-gateway-token` in the same namespace as the OpenClaw instance
- Secret contains a single data entry `token` with a cryptographically secure random token (64 hex characters, equivalent to 32 random bytes)
- Token generation uses Go's `crypto/rand` package (equivalent to `openssl rand -hex 32`)
- Secret is created/managed alongside other OpenClaw resources during reconciliation
- Secret has owner reference to the OpenClaw instance for automatic garbage collection

## Capabilities

### New Capabilities
- `gateway-token-secret`: Generation and management of a secure random token for OpenClaw gateway authentication

### Modified Capabilities
<!-- No existing capabilities are being modified -->

## Impact

- `internal/controller/openclaw_resource_controller.go`: Add secret creation logic to reconciler
- New Kubernetes Secret resource `openclaw-gateway-token` created in OpenClaw instance namespace
- Existing OpenClaw Deployment will need separate changes to mount and use this token (out of scope for this change)
