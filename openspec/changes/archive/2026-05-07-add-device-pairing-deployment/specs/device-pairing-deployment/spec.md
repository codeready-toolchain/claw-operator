## ADDED Requirements

### Requirement: Device pairing manifests exist as embedded Kustomize directory
The system SHALL include a `claw-device-pairing` directory under `internal/assets/manifests/` containing a `kustomization.yaml` and resource YAML files for ServiceAccount, Deployment, Service, and Route.

#### Scenario: Kustomize directory structure
- **WHEN** examining `internal/assets/manifests/claw-device-pairing/`
- **THEN** it SHALL contain `kustomization.yaml`, `serviceaccount.yaml`, `deployment.yaml`, `service.yaml`, and `route.yaml`

#### Scenario: Kustomization references all resources
- **WHEN** examining `kustomization.yaml`
- **THEN** it SHALL list all four resource files and apply the `app.kubernetes.io/name: claw-device-pairing` label via the Kustomize `labels` directive

### Requirement: Device pairing resources use CLAW_INSTANCE_NAME naming
All device-pairing resource names SHALL use the `CLAW_INSTANCE_NAME-device-pairing` template, which is replaced with the actual Claw CR name at build time by the existing `bytes.ReplaceAll` in `buildKustomizedObjects()`.

#### Scenario: ServiceAccount name
- **WHEN** the Claw CR is named "instance"
- **THEN** the ServiceAccount SHALL be named `instance-device-pairing`

#### Scenario: Deployment name
- **WHEN** the Claw CR is named "instance"
- **THEN** the Deployment SHALL be named `instance-device-pairing`

#### Scenario: Service name
- **WHEN** the Claw CR is named "instance"
- **THEN** the Service SHALL be named `instance-device-pairing`

#### Scenario: Route name
- **WHEN** the Claw CR is named "instance"
- **THEN** the Route SHALL be named `instance-device-pairing`

### Requirement: Device pairing Deployment uses correct image and security context
The Deployment SHALL use the `quay.io/xcoulon/claw-device-pairing:latest` image with security hardening matching the operator's conventions.

#### Scenario: Container image
- **WHEN** examining the device-pairing Deployment
- **THEN** its container image SHALL be `quay.io/xcoulon/claw-device-pairing:latest`

#### Scenario: Security context
- **WHEN** examining the device-pairing Deployment
- **THEN** it SHALL run as non-root with `allowPrivilegeEscalation: false`, all capabilities dropped, `readOnlyRootFilesystem: true`, and `RuntimeDefault` seccomp profile

#### Scenario: ServiceAccount reference
- **WHEN** examining the device-pairing Deployment
- **THEN** its `spec.template.spec.serviceAccountName` SHALL reference `CLAW_INSTANCE_NAME-device-pairing`

### Requirement: Device pairing Service exposes the application
The Service SHALL expose the device-pairing Deployment on a ClusterIP with an appropriate target port.

#### Scenario: Service type and selector
- **WHEN** examining the device-pairing Service
- **THEN** it SHALL be of type ClusterIP and select pods with the `app.kubernetes.io/name: claw-device-pairing` label

#### Scenario: Service port
- **WHEN** examining the device-pairing Service
- **THEN** it SHALL expose a named port targeting the device-pairing container port

### Requirement: Device pairing Route uses path-based routing on the Claw host
The Route SHALL share the same host as the Claw Route and serve traffic at the `/integration/device-pairing` path prefix.

#### Scenario: Route host matches Claw Route
- **WHEN** the Claw Route has host `claw.example.com`
- **THEN** the device-pairing Route's `.spec.host` SHALL be `claw.example.com`

#### Scenario: Route path prefix
- **WHEN** examining the device-pairing Route
- **THEN** its `.spec.path` SHALL be `/integration/device-pairing`

#### Scenario: Route TLS configuration
- **WHEN** examining the device-pairing Route
- **THEN** it SHALL use edge TLS termination with redirect for insecure traffic, matching the Claw Route pattern

#### Scenario: Route targets the device-pairing Service
- **WHEN** examining the device-pairing Route `.spec.to`
- **THEN** it SHALL reference the `CLAW_INSTANCE_NAME-device-pairing` Service

#### Scenario: Route skipped on vanilla Kubernetes
- **WHEN** the Route CRD is not registered in the cluster
- **THEN** the device-pairing Route SHALL be silently skipped (same as the Claw Route)

### Requirement: Device pairing Route host is injected from Claw Route status
The controller SHALL inject the resolved Claw Route host into the device-pairing Route's `.spec.host` field, replacing the `OPENCLAW_ROUTE_HOST` placeholder.

#### Scenario: Host placeholder replacement
- **WHEN** the Claw Route status provides host `claw.example.com`
- **THEN** the device-pairing Route's `.spec.host` placeholder SHALL be replaced with `claw.example.com`

#### Scenario: No Route CRD available
- **WHEN** the Route CRD is not registered (vanilla Kubernetes)
- **THEN** the device-pairing Route SHALL be skipped entirely by the existing `applyResources` NoMatch handling
