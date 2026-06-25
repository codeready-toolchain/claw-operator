## ADDED Requirements

### Requirement: Per-role proxy routes for multi-secret channels
When a channel credential declares multiple `SecretRoles` in `knownChannels`, the operator SHALL generate one proxy route per role on the credential's domain. Each route SHALL use a distinct env var derived from the credential name and the role's `EnvVarSuffix`. Roles with `AllowedPaths` SHALL have those paths set on the route; roles without `AllowedPaths` SHALL produce a catch-all route.

#### Scenario: Slack credential generates two routes
- **WHEN** a credential has `channel: slack` with two SecretRef entries (roles `appToken` and `botToken`)
- **THEN** the proxy config SHALL contain two routes on `slack.com`:
  - one with `envVar: CRED_<NAME>_APP` and `allowedPaths: ["/api/apps.connections.open"]`
  - one with `envVar: CRED_<NAME>_BOT` and no `allowedPaths`

#### Scenario: AllowedPaths route sorts before catch-all
- **WHEN** the proxy config contains both an AllowedPaths route and a catch-all route for `slack.com`
- **THEN** the AllowedPaths route SHALL appear before the catch-all route in the config output

#### Scenario: Single-role channels are unaffected
- **WHEN** a credential has `channel: telegram` (single SecretRole, no EnvVarSuffix)
- **THEN** the proxy config SHALL contain one route with `envVar: CRED_<NAME>` (no suffix), matching current behavior

### Requirement: Per-role env vars in proxy Deployment
When a channel credential has multiple `SecretRoles`, the operator SHALL inject one env var per role into the proxy Deployment container. Each env var SHALL reference the `SecretRefEntry` whose `Role` matches the channel's `SecretRole.Role`.

#### Scenario: Slack credential injects two env vars
- **WHEN** a credential has `channel: slack` with `secretRef` entries for roles `appToken` and `botToken`
- **THEN** the proxy Deployment SHALL have env vars `CRED_<NAME>_APP` and `CRED_<NAME>_BOT`, each referencing the correct secret key

#### Scenario: Missing role in SecretRef causes validation error
- **WHEN** a credential has `channel: slack` but only provides one SecretRef entry (missing a role)
- **THEN** credential resolution SHALL return an error indicating the missing role

### Requirement: channelSecretRole declares per-role AllowedPaths and EnvVarSuffix
The `channelSecretRole` struct SHALL support `AllowedPaths []string` and `EnvVarSuffix string` fields. These fields drive per-role route generation and env var naming.

#### Scenario: Slack appToken role declares path restriction
- **WHEN** the `knownChannels` entry for `slack` is read
- **THEN** the `appToken` SecretRole SHALL have `AllowedPaths: ["/api/apps.connections.open"]` and `EnvVarSuffix: "APP"`

#### Scenario: Slack botToken role is the catch-all
- **WHEN** the `knownChannels` entry for `slack` is read
- **THEN** the `botToken` SecretRole SHALL have no `AllowedPaths` and `EnvVarSuffix: "BOT"`

### Requirement: Companion routes remain unchanged
Companion routes (e.g., `.slack.com` for WebSocket) SHALL continue to be generated as `injector: none` passthrough routes, independent of the multi-secret role expansion.

#### Scenario: Slack companion route preserved
- **WHEN** a Slack credential generates multi-role routes
- **THEN** the proxy config SHALL also contain a route for `.slack.com` with `injector: none`
