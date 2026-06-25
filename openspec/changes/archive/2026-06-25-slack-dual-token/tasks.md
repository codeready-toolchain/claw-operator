## 1. Channel Registry Updates

- [x] 1.1 Add `AllowedPaths` and `EnvVarSuffix` fields to `channelSecretRole` struct in `claw_channels.go`
- [x] 1.2 Update Slack entry in `knownChannels`: set `EnvVarSuffix: "APP"` and `AllowedPaths: ["/api/apps.connections.open"]` on the `appToken` role, `EnvVarSuffix: "BOT"` on the `botToken` role
- [x] 1.3 Remove the now-unused `AllowedPaths` field from the `channelDefault` struct (path restrictions are per-role now)

## 2. Proxy Route Generation

- [x] 2.1 Update `credentialRoutes()` in `claw_proxy.go` to expand multi-secret channels into per-role routes: when a channel has multiple `SecretRoles` with `EnvVarSuffix`, emit one route per role with the role's `AllowedPaths` and role-specific env var (`CRED_<NAME>_<SUFFIX>`)
- [x] 2.2 Update `configureProxyForCredentials()` in `claw_proxy.go` to inject per-role env vars for multi-secret channels, each referencing the `SecretRefEntry` matching the role

## 3. Tests

- [x] 3.1 Update or add test in `claw_channels_test.go` verifying Slack's `channelSecretRole` entries have the correct `AllowedPaths` and `EnvVarSuffix`
- [x] 3.2 Add test in `claw_proxy_test.go` verifying that a Slack credential generates two routes (`CRED_<NAME>_APP` with AllowedPaths and `CRED_<NAME>_BOT` catch-all) plus the `.slack.com` companion route
- [x] 3.3 Add test in `claw_proxy_test.go` verifying `configureProxyForCredentials` injects two env vars for a Slack credential
- [x] 3.4 Add test verifying single-role channels (telegram, discord) continue to produce one route with `CRED_<NAME>` (no suffix)
- [x] 3.5 Run full test suite (`make test`) and lint (`make lint`) to verify no regressions
