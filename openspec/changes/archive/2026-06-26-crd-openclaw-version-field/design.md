## Context

The OpenClaw container image is currently hardcoded in two places:
1. `internal/assets/manifests/claw/kustomization.yaml` — `images` section patches all three gateway containers from `ghcr.io/openclaw/openclaw:slim` to `ghcr.io/openclaw/openclaw:2026.6.10`.
2. `internal/controller/claw_providers.go` — `VertexPlugin: "@openclaw/anthropic-vertex-provider@2026.6.10"` pins the Anthropic Vertex plugin to a specific release.

Every OpenClaw upgrade requires changing these values, rebuilding, and releasing the operator. Users cannot run different images or versions across Claw instances, nor use a custom registry mirror.

The reconciler already manipulates embedded manifest content in-memory (replacing `CLAW_INSTANCE_NAME` in `buildKustomizedObjects`), so the same mechanism can inject the image.

## Goals / Non-Goals

**Goals:**
- Allow users to specify a full OpenClaw container image per Claw instance via `spec.image`.
- Default to `ghcr.io/openclaw/openclaw:slim` (rolling latest) when the field is omitted, preserving current behavior for existing CRs.
- Propagate the image to all OpenClaw containers in the deployment.
- Derive the plugin version from the image tag and propagate to versioned plugin references.
- Report the resolved image in `status.image`.

**Non-Goals:**
- Image validation against a container registry — the operator trusts the user-provided reference.
- Automatic upgrade or rollout strategy — the operator applies the requested image immediately.
- Pinning the proxy image — the proxy is a separate image managed by the operator, not OpenClaw.

## Decisions

### 1. Field location and default

Add `Image string` to `ClawSpec` with `+kubebuilder:default="ghcr.io/openclaw/openclaw:slim"` and `+optional`. This means existing CRs without the field are defaulted to `ghcr.io/openclaw/openclaw:slim` by the API server.

**Alternative considered:** A `version` field (tag only). Rejected because a full image reference also supports custom registries, mirrors, and digest-based pinning — more flexible with the same implementation cost.

### 2. Image injection via deployment placeholder

Replace the hardcoded `ghcr.io/openclaw/openclaw:slim` image references in `deployment.yaml` with a placeholder `OPENCLAW_IMAGE`. Remove the `images` section from `kustomization.yaml` entirely — it is no longer needed since the image is injected directly. In `buildKustomizedObjects`, replace the placeholder with `instance.Spec.Image` alongside the existing `CLAW_INSTANCE_NAME` replacement.

**Alternative considered:** Keeping the Kustomize `images` section and parsing the user-provided image into `newName`/`newTag`/`digest` components. Rejected because parsing image references is non-trivial (registry/name:tag vs name@sha256:...) and the placeholder pattern is already established.

### 3. Plugin version extraction from image tag

The Anthropic Vertex plugin version must match the OpenClaw release. Extract the tag from `spec.image` by splitting on `:` (or `@` for digests). Replace the hardcoded version in `VertexPlugin` in `claw_providers.go` with a format string (`@openclaw/anthropic-vertex-provider@%s`) resolved at plugin-injection time using the extracted tag.

For digest-based images (e.g., `ghcr.io/openclaw/openclaw@sha256:abc123`), the plugin version falls back to `latest` since npm packages don't support digest pinning.

**Alternative considered:** Keeping the plugin version independent from the image. Rejected because the Anthropic Vertex plugin is released in lockstep with OpenClaw — version mismatch causes runtime errors.

### 4. Status field

Add `Image string` to `ClawStatus` so users can confirm the deployed image via `kubectl get claw <name> -o jsonpath='{.status.image}'`. The reconciler writes this during status updates.

## Risks / Trade-offs

- **[Risk] Invalid image reference** → The operator does not validate that the image exists in the container registry. If a user specifies a non-existent image, the pod will fail with `ImagePullBackOff`. Mitigation: this is standard Kubernetes behavior; the `Ready` condition will surface the error.
- **[Risk] Plugin/image tag coupling** → The Vertex plugin version is derived from the image tag. If a user uses a custom image with a non-standard tag (e.g., `my-registry/openclaw:custom`), the plugin install may fail. Mitigation: acceptable for now; users with custom images likely don't need the auto-injected Vertex plugin.
- **[Trade-off] Default `slim` vs pinned version** → `slim` is a rolling tag that changes without user action. Users who want reproducibility must pin explicitly. This matches the current behavior (base deployment.yaml uses `:slim`).
