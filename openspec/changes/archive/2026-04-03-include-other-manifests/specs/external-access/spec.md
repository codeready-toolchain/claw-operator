## ADDED Requirements

### Requirement: Operator creates Service for OpenClaw gateway
The operator SHALL create a ClusterIP Service named `openclaw` exposing the OpenClaw gateway on port 18789.

#### Scenario: OpenClaw Service is created
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance
- **THEN** a Service `openclaw` is created with type ClusterIP
- **THEN** the Service selects pods with label `app: openclaw`
- **THEN** the Service exposes port 18789 targeting container port 18789
- **THEN** the Service has label `app.kubernetes.io/name: openclaw`

#### Scenario: Service is embedded and applied via Kustomize
- **WHEN** the controller builds Kustomize resources
- **THEN** service.yaml is read from the embedded filesystem
- **THEN** the Service is applied atomically with other manifests

### Requirement: Operator creates OpenShift Route for external access
The operator SHALL create an OpenShift Route named `openclaw` for external HTTPS access to the gateway.

#### Scenario: Route is created on OpenShift clusters
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance on OpenShift
- **THEN** a Route `openclaw` is created pointing to Service `openclaw`
- **THEN** the Route uses edge TLS termination
- **THEN** the Route redirects insecure traffic to HTTPS
- **THEN** the Route has label `app.kubernetes.io/name: openclaw`

#### Scenario: Route creation gracefully fails on non-OpenShift clusters
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance on vanilla Kubernetes
- **THEN** server-side apply skips the Route resource (CRD not registered)
- **THEN** reconciliation continues successfully for other resources

#### Scenario: Route has extended timeout
- **WHEN** the Route is created
- **THEN** it has annotation `haproxy.router.openshift.io/timeout: 3600s`
