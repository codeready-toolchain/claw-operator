## Why

The current manifest structure stores all Kubernetes resources (claw and claw-proxy components) in a flat directory with a single top-level `kustomization.yaml`. This makes it harder to maintain and reason about which manifests belong to which component, especially as the operator manages two distinct deployments with different lifecycles and configurations.

## What Changes

- Create two subdirectories under `internal/assets/manifests/`:
  - `claw/` for OpenClaw gateway resources (deployment, service, route, configmap, pvc, network policies)
  - `claw-proxy/` for proxy resources (deployment, service, configmap, network policies)
- Each subdirectory gets its own `kustomization.yaml` that manages its component's resources
- Remove the top-level `kustomization.yaml` (no longer needed)
- Update controller code to build kustomize from both subdirectories independently
- Update `manifests.go` embed path if needed to include subdirectories

## Capabilities

### New Capabilities
<!-- None - this is a refactoring -->

### Modified Capabilities
- `manifest-organization`: Manifest loading changes from single kustomization to per-component kustomizations

## Impact

- **Controller code**: `ClawResourceReconciler` methods that build kustomize manifests (`buildKustomizedObjects`, etc.) will need to handle two separate kustomize builds
- **Embedded filesystem**: `internal/assets/manifests.go` embed directive may need adjustment
- **Testing**: Tests that reference manifest paths or kustomize builds may need updates
- **No runtime impact**: This is purely a code organization change with no behavioral differences
