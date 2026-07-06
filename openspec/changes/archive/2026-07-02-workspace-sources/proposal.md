## Why

Enterprise deployments need to seed agent configuration files (`SOUL.md`, `AGENTS.md`, `TOOLS.md`, etc.) from versioned Git repositories and shared ConfigMaps — not just inline strings in the Claw CR. The current `spec.workspace.files` (`map[string]string`) doesn't scale: it can't reference external sources, it bloats the CR with large content, and it offers no way to share configuration across multiple Claw instances. Teams managing dozens of agents need a single source of truth for agent personality and tooling configuration, with credentials support for private repositories.

## What Changes

- **New API types**: `InlineSource`, `ConfigMapSource`, `GitSource`, `SeedMode`, plus supporting types (`ConfigMapRef`, `ConfigMapItem`, `GitItem`). Added to `WorkspaceSpec` as `inlineSources`, `configMapSources`, `gitSources`.
- **Deprecate `spec.workspace.files`**: Retained for backward compatibility. The controller normalizes `files` entries to `InlineSource` with `mode: seedIfMissing` at reconcile time. A status condition warns users to migrate.
- **Per-source seeding mode**: Each source and item supports `mode: overwrite` (default) or `mode: seedIfMissing`. Three-tier cascade: item → source → global default (`overwrite`).
- **Path conflict detection**: Controller rejects the CR if two sources target the same workspace path.
- **New init containers**: `init-git-sync` (using `alpine/git`) clones Git repos through the proxy after `wait-for-proxy`. `init-seed` handles all file seeding from inline, ConfigMap volumes, and git emptyDirs using a controller-generated seeding manifest.
- **`init-config` slimmed down**: No longer handles file seeding — only `operator.json` ↔ `openclaw.json` merge.
- **New env var**: `GIT_SYNC_IMAGE` for the git clone init container image.

## Capabilities

### New Capabilities
- `workspace-configmap-sources`: Seed workspace files from ConfigMap keys, mounted as additional volumes on the gateway Deployment.
- `workspace-git-sources`: Seed workspace files from Git repositories (HTTPS + token auth for private repos), cloned by an `alpine/git` init container into emptyDir volumes.
- `workspace-seed-modes`: Per-source and per-item seeding mode control (`overwrite` or `seedIfMissing`).
- `workspace-seeding-manifest`: Controller-generated JSON manifest describing all file sources, target paths, and modes — consumed by the `init-seed` container.

### Modified Capabilities
- `claw-crd`: `WorkspaceSpec` gains `inlineSources`, `configMapSources`, `gitSources` fields. `files` deprecated.
- `claw-deployment-controller`: Deployment mutation adds ConfigMap volumes, emptyDir volumes, `init-git-sync` and `init-seed` init containers dynamically based on workspace sources.

## Impact

- **`api/v1alpha1/claw_types.go`**: New types (`SeedMode`, `InlineSource`, `ConfigMapSource`, `ConfigMapRef`, `ConfigMapItem`, `GitSource`, `GitItem`). New fields on `WorkspaceSpec`.
- **`api/v1alpha1/zz_generated.deepcopy.go`**: Regenerated via `make generate`.
- **`config/crd/bases/`**: Regenerated via `make manifests`.
- **`internal/controller/claw_workspace.go`**: `files` → `InlineSource` normalization, path conflict validation across all source types, seeding manifest generation, ConfigMap volume injection, emptyDir volume injection, `init-git-sync` script generation, `init-seed` script generation.
- **`internal/assets/manifests/claw/configmap.yaml`**: `merge.js` stripped of file seeding logic (moved to `init-seed`).
- **`internal/assets/manifests/claw/deployment.yaml`**: Init container ordering updated.
- **`cmd/main.go`**: Read `GIT_SYNC_IMAGE` env var.
- **`internal/controller/claw_resource_controller.go`**: Wire new env var, call new workspace injection functions.
