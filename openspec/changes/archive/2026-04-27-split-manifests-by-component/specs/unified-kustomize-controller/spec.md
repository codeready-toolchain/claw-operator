## MODIFIED Requirements

### Requirement: Controller builds Kustomize manifests in-memory
The controller SHALL use the Kustomize API to build manifests in-memory from two embedded manifest subdirectories: one for claw resources and one for claw-proxy resources.

#### Scenario: Kustomize build from claw component directory
- **WHEN** reconciling a Claw named 'instance'
- **THEN** the controller loads the embedded manifests filesystem
- **THEN** the controller invokes kustomize.Run() on the `manifests/claw/` subdirectory to build claw resources

#### Scenario: Kustomize build from claw-proxy component directory
- **WHEN** reconciling a Claw named 'instance'
- **THEN** the controller loads the embedded manifests filesystem
- **THEN** the controller invokes kustomize.Run() on the `manifests/claw-proxy/` subdirectory to build proxy resources

#### Scenario: Merge both component builds
- **WHEN** both component builds succeed
- **THEN** the controller SHALL merge the resulting objects into a single list
- **THEN** all subsequent processing (namespace assignment, owner references, injection, filtering) operates on the merged list

#### Scenario: Kustomization files are component-specific
- **WHEN** the Kustomize build executes for claw component
- **THEN** it SHALL process the kustomization.yaml file in `internal/assets/manifests/claw/`
- **THEN** the kustomization SHALL reference only claw resources (deployment.yaml, service.yaml, route.yaml, configmap.yaml, pvc.yaml, networkpolicy.yaml, ingress-networkpolicy.yaml)

#### Scenario: Kustomization file for proxy component
- **WHEN** the Kustomize build executes for claw-proxy component
- **THEN** it SHALL process the kustomization.yaml file in `internal/assets/manifests/claw-proxy/`
- **THEN** the kustomization SHALL reference only proxy resources (proxy-deployment.yaml, proxy-service.yaml, proxy-configmap.yaml)

#### Scenario: Common labels applied via both Kustomizations
- **WHEN** either Kustomize build executes
- **THEN** all resources SHALL have the label `app.kubernetes.io/name: claw` applied via commonLabels in their respective kustomization.yaml

#### Scenario: Build failure in any component fails reconciliation
- **WHEN** either the claw or claw-proxy kustomize build fails
- **THEN** the controller SHALL log the error
- **THEN** the controller SHALL return an error without applying any resources
- **THEN** reconciliation will retry
