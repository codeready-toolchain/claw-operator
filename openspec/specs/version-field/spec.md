## ADDED Requirements

### Requirement: ClawSpec contains image field
The system SHALL define `ClawSpec` with an optional `image` field of type `string` that specifies the full OpenClaw container image reference to deploy.

#### Scenario: CRD accepts image field with tagged image
- **WHEN** user creates a Claw with `spec: { image: "ghcr.io/openclaw/openclaw:2026.6.10" }`
- **THEN** the resource SHALL be accepted and stored with the specified image

#### Scenario: CRD accepts image field with custom registry
- **WHEN** user creates a Claw with `spec: { image: "my-registry.io/openclaw:latest" }`
- **THEN** the resource SHALL be accepted and stored with the specified image

#### Scenario: CRD accepts omitted image field
- **WHEN** user creates a Claw with `spec: {}` (no image field)
- **THEN** the resource SHALL be accepted and the image SHALL default to `ghcr.io/openclaw/openclaw:slim`

#### Scenario: Image field is optional
- **WHEN** user creates a Claw without specifying `spec.image`
- **THEN** the API server SHALL default the field to `ghcr.io/openclaw/openclaw:slim`

### Requirement: Image propagates to gateway container images
The reconciler SHALL use `spec.image` as the container image for all OpenClaw containers in the gateway deployment (init-volume, init-config, gateway).

#### Scenario: Custom image sets all container images
- **WHEN** a Claw is reconciled with `spec.image: "ghcr.io/openclaw/openclaw:2026.6.10"`
- **THEN** the gateway deployment's init-volume, init-config, and gateway containers SHALL use image `ghcr.io/openclaw/openclaw:2026.6.10`

#### Scenario: Default image sets container images
- **WHEN** a Claw is reconciled with `spec.image` defaulted to `ghcr.io/openclaw/openclaw:slim`
- **THEN** the gateway deployment's containers SHALL use image `ghcr.io/openclaw/openclaw:slim`

#### Scenario: Image change triggers deployment update
- **WHEN** user updates `spec.image` from `ghcr.io/openclaw/openclaw:2026.6.10` to `ghcr.io/openclaw/openclaw:2026.6.11`
- **THEN** the reconciler SHALL update the deployment with the new image

### Requirement: Image tag propagates to versioned plugin references
The reconciler SHALL extract the tag from `spec.image` and use it for plugin package references that are version-locked to the OpenClaw release.

#### Scenario: Anthropic Vertex plugin uses extracted tag
- **WHEN** a Claw with Anthropic credentials using GCP Vertex is reconciled with `spec.image: "ghcr.io/openclaw/openclaw:2026.6.10"`
- **THEN** the injected Vertex plugin SHALL be `@openclaw/anthropic-vertex-provider@2026.6.10`

#### Scenario: Anthropic Vertex plugin uses default tag
- **WHEN** a Claw with Anthropic credentials using GCP Vertex is reconciled with `spec.image` defaulted to `ghcr.io/openclaw/openclaw:slim`
- **THEN** the injected Vertex plugin SHALL be `@openclaw/anthropic-vertex-provider@slim`

### Requirement: Image reported in status
The reconciler SHALL report the resolved image in `status.image`.

#### Scenario: Status reflects specified image
- **WHEN** a Claw is reconciled with `spec.image: "ghcr.io/openclaw/openclaw:2026.6.10"`
- **THEN** `status.image` SHALL be `ghcr.io/openclaw/openclaw:2026.6.10`

#### Scenario: Status reflects default image
- **WHEN** a Claw is reconciled with `spec.image` defaulted to `ghcr.io/openclaw/openclaw:slim`
- **THEN** `status.image` SHALL be `ghcr.io/openclaw/openclaw:slim`

### Requirement: Image displayed in kubectl output
The Claw CRD SHALL include an `Image` print column showing the resolved image.

#### Scenario: kubectl get shows image
- **WHEN** user runs `kubectl get claw`
- **THEN** the output SHALL include an `Image` column showing the value of `spec.image`
