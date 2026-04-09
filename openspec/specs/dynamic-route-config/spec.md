## ADDED Requirements

### Requirement: Controller applies Route before other resources

The controller SHALL apply the Route manifest first in the reconciliation sequence, before applying ConfigMap, Deployments, Services, and NetworkPolicies.

#### Scenario: Route is applied first during reconciliation

- **WHEN** OpenClaw instance is reconciled
- **THEN** Route resource is created/updated before any other resources

### Requirement: Controller waits for Route ingress host

The controller SHALL wait for the Route status to populate `.status.ingress[0].host` before proceeding with ConfigMap creation.

#### Scenario: Route status contains ingress host

- **WHEN** Route is applied and OpenShift populates its status
- **THEN** controller detects `.status.ingress[0].host` is populated
- **THEN** controller proceeds to ConfigMap creation

#### Scenario: Route status not yet populated

- **WHEN** Route status does not have `.status.ingress[0].host`
- **THEN** controller requeues reconciliation with backoff
- **THEN** reconciliation retries until host is available

### Requirement: Controller injects Route host into ConfigMap

The controller SHALL replace the `OPENCLAW_ROUTE_HOST` placeholder in the ConfigMap's `openclaw.json` data with the actual Route host using HTTPS scheme.

#### Scenario: ConfigMap updated with Route host

- **WHEN** Route host is `example-openclaw.apps.cluster.com`
- **THEN** ConfigMap's `openclaw.json` has `"allowedOrigins": ["https://example-openclaw.apps.cluster.com"]`

#### Scenario: Placeholder is replaced exactly once

- **WHEN** ConfigMap template contains `OPENCLAW_ROUTE_HOST` placeholder
- **THEN** controller replaces all occurrences with `https://<route-host>`
- **THEN** no placeholder remains in applied ConfigMap

### Requirement: ConfigMap template includes placeholder

The ConfigMap manifest template SHALL include `OPENCLAW_ROUTE_HOST` as a placeholder in the `gateway.controlUI.allowedOrigins` array within `openclaw.json`.

#### Scenario: ConfigMap manifest has placeholder

- **WHEN** ConfigMap manifest is loaded from embedded filesystem
- **THEN** `openclaw.json` contains `"allowedOrigins": ["OPENCLAW_ROUTE_HOST"]`

### Requirement: Controller gracefully handles Route absence on non-OpenShift

The controller SHALL skip Route-based ConfigMap injection when running on vanilla Kubernetes (where Route CRD is not available).

#### Scenario: Running on vanilla Kubernetes

- **WHEN** Route CRD is not registered in the cluster
- **THEN** controller skips Route creation and host injection
- **THEN** ConfigMap is applied with placeholder value unchanged or default value

#### Scenario: Running on OpenShift

- **WHEN** Route CRD is registered in the cluster
- **THEN** controller applies Route and waits for ingress host
- **THEN** ConfigMap receives dynamically-injected Route host
