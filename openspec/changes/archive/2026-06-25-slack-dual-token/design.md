## Context

The proxy already supports multiple routes per domain with AllowedPaths-based path discrimination (`MatchRoute` in `internal/proxy/config.go`). The channel registry (`knownChannels` in `claw_channels.go`) already declares two `SecretRoles` for Slack (`botToken` and `appToken`). However, the operator-side route and env var generation treats each credential as a single unit — `credentialRoutes()` builds one route per credential using `credEnvVarName(cred.Name)`, and `configureProxyForCredentials()` injects one env var per credential via `proxySecretForCredential()`. This means Slack only gets `CRED_SLACK` with one token, and one route on `slack.com`.

## Goals / Non-Goals

**Goals:**
- Emit one proxy route and one env var **per SecretRole** for multi-secret channels, so each role's token is injected independently.
- Restrict the `appToken` route to `/api/apps.connections.open`; the `botToken` route handles everything else.
- Keep the change generic to the multi-secret channel mechanism (not Slack-specific logic in route generation).

**Non-Goals:**
- Changing the proxy-side route matching or injection logic (already works).
- Changing the CRD API types (SecretRefEntry.Role and AllowedPaths already exist).
- Supporting more than two SecretRoles per channel (two is sufficient; generalizes naturally if needed).

## Decisions

### 1. Add `AllowedPaths` to `channelSecretRole`

Move path restrictions from the `channelDefault` level to each `channelSecretRole`. This lets the Slack `appToken` role declare `AllowedPaths: ["/api/apps.connections.open"]` while `botToken` has none (catch-all).

**Alternative considered**: Keep `AllowedPaths` on `channelDefault` and assign it to the first role. Rejected because it couples ordering to semantics and doesn't generalize if a future channel needs different paths per role.

### 2. Add `EnvVarSuffix` to `channelSecretRole`

Each role declares its env var suffix (e.g., `"APP"`, `"BOT"`). The env var becomes `CRED_<CREDNAME>_<SUFFIX>`. When a channel has only one SecretRole (or no roles with suffixes), the existing `CRED_<CREDNAME>` convention is preserved.

**Alternative considered**: Derive the suffix from the Role field (e.g., `botToken` → `BOT_TOKEN`). Rejected because it couples internal role naming to env var naming and produces ugly names like `CRED_SLACK_BOT_TOKEN`.

### 3. Expand multi-secret routes in `credentialRoutes()`

When a credential's channel has multiple `SecretRoles`, `credentialRoutes()` emits one `proxyRoute` per role instead of one per credential. Each route gets its own `EnvVar` and `AllowedPaths` from the role definition. The existing single-role path (telegram, discord) is unaffected — it continues to produce one route with `credEnvVarName(cred.Name)`.

### 4. Expand multi-secret env vars in `configureProxyForCredentials()`

Instead of calling `proxySecretForCredential()` once, iterate over the channel's `SecretRoles` when multiple exist. Each role's env var is mapped to the matching `SecretRefEntry` (looked up by `Role`). The single-role path remains unchanged.

### 5. Retain `proxySecretForCredential()` as-is

This function is still useful for non-multi-secret channels. It doesn't need to change — the multi-role expansion happens at a higher level (`credentialRoutes` and `configureProxyForCredentials`).

## Risks / Trade-offs

- **Route ordering sensitivity**: The proxy's `MatchRoute` relies on AllowedPaths routes sorting before catch-all routes for the same domain. The existing `routeLess` comparator in `generateProxyConfig` already handles this correctly (line 166-168 in `claw_proxy.go`). **No additional risk.**
- **Backwards compatibility**: Existing Slack credentials with a single `secretRef` entry will fail validation because the channel now expects two roles. **Mitigation**: This is expected — Slack wasn't functional with a single token anyway. The error message from `resolveCredentials` will guide users to provide both roles.
