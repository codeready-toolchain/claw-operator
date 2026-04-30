## ADDED Requirements

### Requirement: Controller fetches console route host
The controller SHALL fetch the `claw-console` Route resource and extract its hostname during reconciliation.

#### Scenario: Console route exists and has status
- **WHEN** reconciling a Claw instance on OpenShift
- **WHEN** a Route named `claw-console` exists in the same namespace
- **WHEN** the Route has `.status.ingress[0].host` populated
- **THEN** the controller extracts the hostname from the Route status

#### Scenario: Console route exists but status not ready
- **WHEN** reconciling a Claw instance on OpenShift
- **WHEN** a Route named `claw-console` exists but `.status.ingress[0].host` is not yet populated
- **THEN** the controller requeues reconciliation with a 5-second backoff
- **THEN** reconciliation retries until the Route status is populated

#### Scenario: Console route does not exist
- **WHEN** reconciling a Claw instance
- **WHEN** no Route named `claw-console` exists in the namespace
- **THEN** the controller logs a warning about the missing console route
- **THEN** the controller continues reconciliation without the console route host
- **THEN** only the gateway route host is included in allowedOrigins

#### Scenario: Route CRD not registered (vanilla Kubernetes)
- **WHEN** reconciling a Claw instance on vanilla Kubernetes (no Route CRD)
- **THEN** the controller skips console route fetching with NoMatchError
- **THEN** the controller uses localhost fallback for allowedOrigins

### Requirement: Controller injects console route host into ConfigMap
The controller SHALL include the console route host in the ConfigMap's `allowedOrigins` array.

#### Scenario: Both gateway and console routes available
- **WHEN** both `claw` and `claw-console` routes have populated status
- **WHEN** gateway route host is `claw-gateway.apps.cluster.example.com`
- **WHEN** console route host is `claw-console.apps.cluster.example.com`
- **THEN** ConfigMap `allowedOrigins` includes `["https://claw-gateway.apps.cluster.example.com", "https://claw-console.apps.cluster.example.com"]`

#### Scenario: Only gateway route available
- **WHEN** `claw` route exists and has status
- **WHEN** `claw-console` route does not exist
- **THEN** ConfigMap `allowedOrigins` includes only `["https://claw-gateway.apps.cluster.example.com"]`

#### Scenario: Vanilla Kubernetes with no routes
- **WHEN** reconciling on vanilla Kubernetes (no Route CRD)
- **THEN** ConfigMap `allowedOrigins` includes `["http://localhost:18789"]`

### Requirement: Console route host uses HTTPS scheme
The controller SHALL use the `https://` scheme when constructing the console route origin.

#### Scenario: Console route origin uses HTTPS
- **WHEN** console route host is `claw-console.apps.cluster.example.com`
- **THEN** the allowedOrigins entry is `https://claw-console.apps.cluster.example.com`
- **THEN** the scheme is lowercase `https://` (not HTTP)

### Requirement: Controller uses separate placeholders for route hosts
The ConfigMap template SHALL use distinct placeholders for gateway and console route hosts.

#### Scenario: ConfigMap template has two placeholders
- **WHEN** the ConfigMap template is defined in `internal/assets/manifests/claw/configmap.yaml`
- **THEN** the `allowedOrigins` array includes `"OPENCLAW_ROUTE_HOST"` placeholder
- **THEN** the `allowedOrigins` array includes `"OPENCLAW_CONSOLE_ROUTE_HOST"` placeholder

#### Scenario: Controller replaces both placeholders
- **WHEN** gateway route host is `gateway.example.com`
- **WHEN** console route host is `console.example.com`
- **THEN** `OPENCLAW_ROUTE_HOST` is replaced with `https://gateway.example.com`
- **THEN** `OPENCLAW_CONSOLE_ROUTE_HOST` is replaced with `https://console.example.com`

#### Scenario: Missing console route removes placeholder
- **WHEN** console route does not exist
- **THEN** `OPENCLAW_CONSOLE_ROUTE_HOST` placeholder is removed from the array
- **THEN** only valid origins remain in `allowedOrigins`
