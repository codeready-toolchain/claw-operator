## Why

Workspace sources (inline, ConfigMap, Git) currently seed files onto the PVC, where the OpenClaw process can freely modify or delete them at runtime. For managed reference files (company policies, compliance docs, skill definitions, curated configs), the operator needs a way to guarantee the files remain untouched — preventing both accidental and intentional edits by the agent.

The existing `mode: overwrite` re-seeds on restart but does not protect files during runtime.

## What Changes

- Add a `readOnly` boolean field to `InlineSource`, `ConfigMapSource`, and `GitSource`
- When `readOnly: true`, bypass PVC seeding entirely and mount source files directly into the gateway container's workspace path using Kubernetes read-only volume mounts (OS-level enforcement via `EROFS`)
- Reuse existing pod volumes — no new volume types needed

## Capabilities

### Modified Capabilities

- `workspace-sources`: Adds `readOnly` field at the source level for all three source types. When set, items are mounted directly into the workspace via read-only Kubernetes volume mounts instead of being seeded onto the PVC.

## Impact

- **API**: New `ReadOnly bool` field on `InlineSource`, `ConfigMapSource`, `GitSource` in `api/v1alpha1/claw_types.go`
- **CRD**: Regenerated CRD YAML (`make manifests`, `make generate`)
- **Controller**: New `injectReadOnlyWorkspaceMounts()` function adds per-item volumeMounts to the gateway container; `generateSeedManifest()` skips read-only entries
- **Reconciler**: Call new injection function in phase 3 (alongside existing `inject*` calls)
- **Spec**: Updated `workspace-sources` spec with new read-only requirements and scenarios
- **Tests**: New test cases in `claw_workspace_test.go` covering all three source types, mixed read-only/writable sources, and volume mount verification
