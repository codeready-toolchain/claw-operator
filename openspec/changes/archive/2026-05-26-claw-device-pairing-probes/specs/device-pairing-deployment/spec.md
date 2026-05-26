## MODIFIED Requirements

### Requirement: Device pairing Deployment uses correct image and security context
The Deployment SHALL use the `quay.io/xcoulon/claw-device-pairing:latest` image with security hardening matching the operator's conventions. All health probes SHALL use explicit timeout and threshold values tuned for a fast-starting backend.

#### Scenario: Container image
- **WHEN** examining the device-pairing Deployment
- **THEN** its container image SHALL be `quay.io/xcoulon/claw-device-pairing:latest`

#### Scenario: Security context
- **WHEN** examining the device-pairing Deployment
- **THEN** it SHALL run as non-root with `allowPrivilegeEscalation: false`, all capabilities dropped, `readOnlyRootFilesystem: true`, and `RuntimeDefault` seccomp profile

#### Scenario: ServiceAccount reference
- **WHEN** examining the device-pairing Deployment
- **THEN** its `spec.template.spec.serviceAccountName` SHALL reference `CLAW_INSTANCE_NAME-device-pairing`

#### Scenario: Liveness probe configuration
- **WHEN** examining the device-pairing Deployment's liveness probe
- **THEN** it SHALL use httpGet on path `/healthz` and named port `http` with `initialDelaySeconds: 3`, `periodSeconds: 15`, `timeoutSeconds: 2`, and `failureThreshold: 3`

#### Scenario: Readiness probe configuration
- **WHEN** examining the device-pairing Deployment's readiness probe
- **THEN** it SHALL use httpGet on path `/healthz` and named port `http` with `initialDelaySeconds: 2`, `periodSeconds: 10`, `timeoutSeconds: 2`, and `failureThreshold: 2`

#### Scenario: Startup probe configuration
- **WHEN** examining the device-pairing Deployment's startup probe
- **THEN** it SHALL use httpGet on path `/healthz` and named port `http` with `periodSeconds: 2`, `timeoutSeconds: 2`, and `failureThreshold: 3`
