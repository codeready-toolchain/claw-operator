## Why

The OpenClaw container image (`ghcr.io/openclaw/openclaw:2026.6.10`) is hardcoded in two places — the Kustomize image override and the Anthropic Vertex plugin reference — requiring operator code changes and a new release for every OpenClaw upgrade. Adding an `image` field to the Claw CRD lets users pin a specific image (including custom registries or digests) per-instance without touching the operator.

## What Changes

- Add an optional `image` field to `ClawSpec` in the Claw CRD. When omitted, defaults to `ghcr.io/openclaw/openclaw:slim` (latest).
- The reconciler uses the specified image for all OpenClaw containers (init-volume, init-config, gateway) instead of the hardcoded Kustomize image override.
- The reconciler extracts the tag from the image reference and uses it for versioned plugin references (e.g., Anthropic Vertex provider plugin).
- The reconciler reports the resolved image in the Claw status so users can confirm which image is deployed.

## Capabilities

### New Capabilities
- `image-field`: CRD field and reconciler logic for user-specified OpenClaw image selection, defaulting to `ghcr.io/openclaw/openclaw:slim` when unset.

### Modified Capabilities
- `claw-crd`: Add the `image` field to `ClawSpec` type definition and CRD schema.

## Impact

- **CRD schema**: New optional field in `ClawSpec`; non-breaking (existing CRs without the field get `ghcr.io/openclaw/openclaw:slim`).
- **API types**: `api/v1alpha1/claw_types.go` gains an `Image` field.
- **Controller**: `claw_resource_controller.go` must inject the image into the deployment manifests at reconciliation time. `claw_providers.go` must derive the plugin version from the image tag.
- **Kustomize manifests**: The `images` section in `kustomization.yaml` is removed; image injection moves to a placeholder replacement in `deployment.yaml`, consistent with the existing `CLAW_INSTANCE_NAME` pattern.
- **Tests**: New and updated tests for image propagation to deployment, plugins, and status.
- **Code generation**: `make manifests generate` required after type change.
