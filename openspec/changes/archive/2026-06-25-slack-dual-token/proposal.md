## Why

The Slack integration requires two distinct tokens — an **app-level token** (`xapp-*`) for the Socket Mode handshake (`/api/apps.connections.open`) and a **bot token** (`xoxb-*`) for all other Slack API calls. Today the operator generates a single `CRED_SLACK` env var and a single proxy route for the credential's domain, so only one token reaches the proxy. This means the proxy cannot route the correct token to the correct Slack API path.

## What Changes

- **Multi-route generation for multi-secret channels**: When a channel credential declares multiple `SecretRoles` (e.g., Slack's `botToken` + `appToken`), the operator will emit one proxy route **per role** on the same domain, each with its own env var and optional path restrictions.
- **Per-role env vars**: Instead of one `CRED_SLACK`, two env vars are injected into the proxy Deployment: `CRED_SLACK_APP` (appToken) and `CRED_SLACK_BOT` (botToken), each sourced from the matching SecretRef entry by role.
- **Path-scoped route for app token**: The `appToken` route is restricted to `AllowedPaths: ["/api/apps.connections.open"]`; the `botToken` route is the catch-all for all other Slack API paths.
- **`channelSecretRole` gains `AllowedPaths`**: Path restrictions move from the `channelDefault` level to the per-role level, so each role can declare which paths it handles.

## Capabilities

### New Capabilities
- `channel-multi-secret-routing`: Operator-side expansion of multi-secret channel credentials into per-role proxy routes and env vars.

### Modified Capabilities


## Impact

- **`internal/controller/claw_channels.go`**: Add `AllowedPaths` to `channelSecretRole`; update Slack entry.
- **`internal/controller/claw_proxy.go`**: `credentialRoutes()` and `configureProxyForCredentials()` must expand multi-secret channels into per-role routes/env vars.
- **`internal/controller/claw_credentials.go`**: `proxySecretForCredential()` may be simplified or removed once per-role expansion replaces single-secret selection.
- **Proxy side** (`internal/proxy/`): No changes — the proxy already supports multiple routes per domain with AllowedPaths discrimination.
- **API types** (`api/v1alpha1/`): No changes — `SecretRefEntry.Role` and `CredentialSpec.AllowedPaths` already exist.
