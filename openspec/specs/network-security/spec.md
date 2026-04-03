# network-security Specification

## Purpose
TBD - created by archiving change include-other-manifests. Update Purpose after archive.
## Requirements
### Requirement: Operator creates NetworkPolicy for OpenClaw egress control
The operator SHALL create a NetworkPolicy named `openclaw-egress` that restricts OpenClaw pod egress traffic to only the proxy service and DNS.

#### Scenario: OpenClaw pod egress is restricted
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance
- **THEN** a NetworkPolicy `openclaw-egress` is created with podSelector matching `app: openclaw`
- **THEN** egress rules allow traffic to pods with label `app: openclaw-proxy` on port 8080
- **THEN** egress rules allow DNS traffic to all namespaces on ports 53 and 5353 (UDP/TCP)

#### Scenario: NetworkPolicy has correct labels
- **WHEN** the NetworkPolicy is created
- **THEN** it has label `app.kubernetes.io/name: openclaw`
- **THEN** it has owner reference set to the OpenClaw CR

### Requirement: Operator creates NetworkPolicy for proxy egress control
The operator SHALL create a NetworkPolicy named `openclaw-proxy-egress` that allows the proxy pod to access external HTTPS endpoints and DNS.

#### Scenario: Proxy pod can reach external APIs
- **WHEN** the OpenClawReconciler reconciles an OpenClaw instance
- **THEN** a NetworkPolicy `openclaw-proxy-egress` is created with podSelector matching `app: openclaw-proxy`
- **THEN** egress rules allow HTTPS traffic on port 443 to any destination
- **THEN** egress rules allow DNS traffic to all namespaces on ports 53 and 5353 (UDP/TCP)

#### Scenario: NetworkPolicy is embedded and applied via Kustomize
- **WHEN** the controller builds Kustomize resources
- **THEN** networkpolicy.yaml is read from the embedded filesystem
- **THEN** both NetworkPolicy resources are applied atomically with other manifests

