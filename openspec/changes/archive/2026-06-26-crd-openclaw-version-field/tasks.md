## 1. API Types

- [x] 1.1 Add `Image` field to `ClawSpec` in `api/v1alpha1/claw_types.go` with `+optional`, `+kubebuilder:default="ghcr.io/openclaw/openclaw:slim"`, and JSON tag `image`
- [x] 1.2 Add `Image` field to `ClawStatus` in `api/v1alpha1/claw_types.go` with `+optional` and JSON tag `image`
- [x] 1.3 Add `+kubebuilder:printcolumn` marker for `Image` on the `Claw` type showing `spec.image`
- [x] 1.4 Run `make manifests generate` to regenerate CRD YAML and DeepCopy methods

## 2. Deployment Image Injection

- [x] 2.1 Replace `ghcr.io/openclaw/openclaw:slim` with placeholder `OPENCLAW_IMAGE` in `internal/assets/manifests/claw/deployment.yaml` (init-volume, init-config, gateway containers)
- [x] 2.2 Remove the `images` section from `internal/assets/manifests/claw/kustomization.yaml`
- [x] 2.3 Add `OPENCLAW_IMAGE` replacement in `buildKustomizedObjects` in `internal/controller/claw_resource_controller.go`, resolving from `instance.Spec.Image`

## 3. Plugin Version Extraction

- [x] 3.1 Add a helper function to extract the tag from a container image reference (split on `:`, handle digest `@sha256:` references)
- [x] 3.2 Change `VertexPlugin` field in `claw_providers.go` from hardcoded version to format string (`@openclaw/anthropic-vertex-provider@%s`)
- [x] 3.3 Update plugin injection code to resolve the format string using the tag extracted from `instance.Spec.Image`

## 4. Status Update

- [x] 4.1 Set `status.image` from `spec.image` in the status reconciliation path in `claw_status.go`

## 5. Tests

- [x] 5.1 Update `testGatewayImage` constant in `claw_plugins_test.go` to reflect the new default or parameterize it
- [x] 5.2 Add test case: image field defaults to `ghcr.io/openclaw/openclaw:slim` when omitted
- [x] 5.3 Add test case: custom image propagates to all container images in the deployment
- [x] 5.4 Add test case: tag extracted from image propagates to Vertex plugin reference
- [x] 5.5 Add test case: status.image reflects spec.image
- [x] 5.6 Run `make test` and `make lint` to verify all tests pass
