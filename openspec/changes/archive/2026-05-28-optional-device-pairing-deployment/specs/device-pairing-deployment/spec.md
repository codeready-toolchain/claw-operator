## MODIFIED Requirements

### Requirement: Device pairing manifests exist as embedded Kustomize directory
The system SHALL include a `claw-device-pairing` directory under `internal/assets/manifests/` containing a `kustomization.yaml` and resource YAML files for ServiceAccount, ClusterRole, RoleBinding, Deployment, Service, and Route. The controller SHALL only include these manifests in the Kustomize build when device pairing is enabled (`shouldDisableDevicePairing()` returns `false`).

#### Scenario: Kustomize directory structure
- **WHEN** examining `internal/assets/manifests/claw-device-pairing/`
- **THEN** it SHALL contain `kustomization.yaml`, `serviceaccount.yaml`, `clusterrole.yaml`, `rolebinding.yaml`, `deployment.yaml`, `service.yaml`, and `route.yaml`

#### Scenario: Kustomization references all resources
- **WHEN** examining `kustomization.yaml`
- **THEN** it SHALL list all six resource files and apply the `app.kubernetes.io/name: claw-device-pairing` label via the Kustomize `labels` directive

#### Scenario: Manifests excluded from build when device pairing disabled
- **WHEN** `shouldDisableDevicePairing()` returns `true`
- **THEN** the `buildKustomizedObjects()` function SHALL NOT include `claw-device-pairing` manifests in the in-memory filesystem
- **THEN** the returned objects slice SHALL NOT contain any device-pairing resources
