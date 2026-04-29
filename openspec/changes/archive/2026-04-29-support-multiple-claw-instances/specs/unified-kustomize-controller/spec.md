## REMOVED Requirements

### Requirement: Controller filters by instance name
**Reason**: Artificial limitation removed to support multiple Claw instances per namespace  
**Migration**: No migration needed. All Claw instances are now reconciled regardless of name.

## ADDED Requirements

### Requirement: Controller uses dynamic resource names
The controller SHALL use the Claw instance name as the base name for all created resources, enabling multiple instances per namespace.

#### Scenario: Gateway deployment name derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a Deployment named 'my-openclaw'

#### Scenario: Proxy deployment name derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a Deployment named 'my-openclaw-proxy'

#### Scenario: Gateway token secret name derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a Secret named 'my-openclaw-gateway-token'

#### Scenario: ConfigMap name derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a ConfigMap named 'my-openclaw-config'

#### Scenario: PVC name derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a PersistentVolumeClaim named 'my-openclaw-home-pvc'

#### Scenario: Service names derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a Service named 'my-openclaw' for the gateway
- **THEN** the controller creates a Service named 'my-openclaw-proxy' for the proxy

#### Scenario: Route name derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates a Route named 'my-openclaw'

#### Scenario: NetworkPolicy names derived from CR name
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the controller creates NetworkPolicies named 'my-openclaw-ingress', 'my-openclaw-egress', 'my-openclaw-proxy-egress'

#### Scenario: Multiple instances in same namespace
- **WHEN** two Claw instances named 'claw-a' and 'claw-b' exist in the same namespace
- **THEN** the controller creates separate Deployments 'claw-a' and 'claw-b'
- **THEN** each instance has its own set of resources with unique names
- **THEN** resources do not conflict or interfere with each other

### Requirement: Controller applies instance label to all resources
The controller SHALL add the label `claw.sandbox.redhat.com/instance` with the Claw instance name to all created resources.

#### Scenario: Instance label on gateway deployment
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the gateway Deployment has label `claw.sandbox.redhat.com/instance: my-openclaw`

#### Scenario: Instance label on proxy deployment
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** the proxy Deployment has label `claw.sandbox.redhat.com/instance: my-openclaw`

#### Scenario: Instance label enables per-instance queries
- **WHEN** user runs `kubectl get all -l claw.sandbox.redhat.com/instance=my-openclaw`
- **THEN** only resources belonging to the 'my-openclaw' instance are returned

#### Scenario: NetworkPolicy selectors use instance label
- **WHEN** reconciling a Claw instance named 'my-openclaw'
- **THEN** NetworkPolicy pod selectors include both `app.kubernetes.io/name: claw` and `claw.sandbox.redhat.com/instance: my-openclaw`
- **THEN** network policies only apply to pods of the specific instance

### Requirement: Controller uses helper functions for resource names
The controller SHALL use centralized helper functions to generate resource names from the Claw instance name.

#### Scenario: Helper function for gateway deployment name
- **WHEN** controller needs the gateway deployment name for instance 'my-openclaw'
- **THEN** it calls `getClawDeploymentName("my-openclaw")` which returns `"my-openclaw"`

#### Scenario: Helper function for proxy deployment name
- **WHEN** controller needs the proxy deployment name for instance 'my-openclaw'
- **THEN** it calls `getProxyDeploymentName("my-openclaw")` which returns `"my-openclaw-proxy"`

#### Scenario: Helper function for gateway secret name
- **WHEN** controller needs the gateway secret name for instance 'my-openclaw'
- **THEN** it calls `getGatewaySecretName("my-openclaw")` which returns `"my-openclaw-gateway-token"`

#### Scenario: Helper functions centralize naming logic
- **WHEN** resource names need to be constructed
- **THEN** all resource name generation uses helper functions, not string concatenation
- **THEN** naming conventions are consistent and maintainable
