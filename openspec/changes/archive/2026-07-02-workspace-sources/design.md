## Context

The operator currently seeds workspace files via `spec.workspace.files` (`map[string]string`). The controller bakes file content into the gateway ConfigMap as `_ws_`-prefixed keys (path encoded with `--` separator). At pod start, `merge.js` in the `init-config` container reads `_ws_*` keys and copies them to the PVC workspace using `seedIfMissing` semantics (user edits preserved across restarts).

This mechanism has three limitations: (1) all content must be inline in the CR, (2) it shares the 1 MiB ConfigMap size limit with `operator.json`, `merge.js`, and skill files, and (3) there's no way to share configuration across Claw instances without duplicating content.

The operator already has a pattern for dynamically injecting init containers — `claw_plugins.go` generates a shell script and appends an init container to the Deployment's `initContainers` list via unstructured manipulation.

## Goals / Non-Goals

**Goals:**
- Support three workspace file source types: inline content, ConfigMap keys, and Git repositories (HTTPS with token auth)
- Per-source and per-item seeding mode control (`overwrite` or `seedIfMissing`), defaulting to `overwrite`
- Backward compatibility with existing `spec.workspace.files` field
- Path conflict detection across all source types
- Git clone routed through the MITM proxy (reusing existing egress rules and CA certs)

**Non-Goals:**
- SSH key authentication for Git (HTTPS token only for now)
- Directory-level copy from Git repos (file-only items for now)
- "Mount all keys" from ConfigMaps without explicit item mapping
- Continuous sync / sidecar-based Git watching (one-shot clone at pod start only)
- Cross-namespace ConfigMap references

## Decisions

### 1. API shape: Option C — separate fields per source type

`WorkspaceSpec` gets three list fields (`inlineSources`, `configMapSources`, `gitSources`) alongside the deprecated `files` map.

**Rationale:** Backward compatible — `files` keeps working. Each source type has a distinct struct shape (inline has `content`, ConfigMap has `configMapRef` + `items`, Git has `url` + `ref` + `secretRef` + `items`), so separate fields are more natural than a discriminated union. Consistent naming (`*Sources`) makes the API predictable.

**Alternative considered:** Discriminated union list (`sources: [{type: inline, ...}, {type: git, ...}]`). Rejected because it requires CEL validation rules to enforce type-specific required fields, and Kubernetes CRD schemas don't ergonomically support tagged unions.

### 2. Deprecate `files`, normalize to `InlineSource` at reconcile time

The controller converts `files` entries to `InlineSource` with `mode: seedIfMissing` at the top of the reconcile loop. All downstream code only handles `InlineSource`.

**Rationale:** Preserves backward compatibility with zero downtime. Internal code has a single code path. The `seedIfMissing` default for migrated entries preserves the original behavior. A status condition or event nudges users to migrate.

**Alternative considered:** Clean rename (drop `files`, only support `inlineSources`). Rejected because it's a breaking change even in v1alpha1, and the normalization approach is trivial to implement.

### 3. Seeding mode with three-tier cascade

```
item.Mode  →  source.Mode  →  "overwrite" (global default)
```

Exception: migrated `files` entries get `seedIfMissing` as source-level default.

**Rationale:** Enterprise configs (SOUL.md, TOOLS.md from a shared repo) should overwrite on every restart — the Git repo is the source of truth. But per-item override allows mixing modes within a single source (e.g., overwrite SOUL.md but seed-if-missing for a user-customizable template).

### 4. Error on path conflicts

If two sources (of any type) target the same workspace path, the controller rejects the CR with a validation error in status conditions.

**Rationale:** Explicit is better than implicit. Last-writer-wins would create ordering-dependent behavior that's hard to debug. Priority fields add complexity without clear use cases.

### 5. Content flow: inline through ConfigMap keys, external through volumes

- **Inline sources**: Content baked into the gateway ConfigMap as `_ws_` keys (existing mechanism).
- **ConfigMap sources**: Referenced ConfigMaps mounted as additional volumes (`/configmap-sources/<cm-name>/`).
- **Git sources**: Cloned into emptyDir volumes (`/git-sources/<index>/`) by `init-git-sync`.
- **Seeding manifest**: A JSON array baked into the gateway ConfigMap that describes all files (source path, target path, mode). Consumed by `init-seed`.

**Rationale:** Inline content is small and fits the existing ConfigMap flow. External sources should not be copied into the gateway ConfigMap — it would hit size limits and create stale-data coupling (ConfigMap source updates wouldn't trigger reconcile). Mounting directly preserves freshness and scales independently.

### 6. Init container ordering: git clone after proxy

```
init-volume → init-config → wait-for-proxy → init-git-sync → init-seed
```

`init-git-sync` runs after `wait-for-proxy` so it can route through the MITM proxy. This means no additional NetworkPolicy rules are needed for git egress — the proxy's existing HTTPS egress rule covers it. The proxy CA cert is mounted for TLS interception.

`init-config` is slimmed to only handle the `operator.json` ↔ `openclaw.json` merge. All file seeding (inline, ConfigMap, git) moves to `init-seed`, which runs last and reads the seeding manifest.

**Rationale:** The proxy is in a separate Deployment and may not be ready when the gateway pod's init containers start. The existing `wait-for-proxy` container blocks until the proxy is available. Placing git clone after this guarantees network connectivity. Consolidating all file seeding into a single `init-seed` step simplifies the logic — one place handles mode resolution, path validation, and the copy/seed operation.

### 7. Git image: `alpine/git`

The `init-git-sync` container uses `alpine/git` (~8 MB). The controller generates a shell script (same pattern as the plugin init container in `claw_plugins.go`). A new `GIT_SYNC_IMAGE` env var configures the image.

**Rationale:** We only need one-shot `git clone --depth 1`, not a polling sidecar. `alpine/git` fits the existing init container script generation pattern. `git-sync` (the Kubernetes sidecar) is designed for continuous polling and has an opinionated symlink directory layout that would require extra steps to extract individual files.

### 8. Git auth: HTTPS token only

The secret value is injected into the clone URL as `https://oauth2:<token>@host/path.git`. SSH key auth is deferred to a future change.

**Rationale:** HTTPS token auth covers GitHub, GitLab, Bitbucket, and most enterprise Git servers. SSH adds complexity (known_hosts management, key file permissions, SSH agent). It can be added later without API changes — `SecretRefEntry` already has the fields needed.

### 9. Git items: file-only, no directory copy

Each `GitItem` maps exactly one file (`repoPath`) to one workspace path (`path`). Directory-level copy (trailing `/` convention) is deferred.

**Rationale:** The stated use case is seeding a handful of named `.md` files. File-only keeps path conflict detection simple and makes the workspace layout fully visible in the CR. Directory copy can be added later by relaxing validation on `repoPath`.

### 10. Reuse `SecretRefEntry` for Git credentials

`GitSource.SecretRef` uses the existing `SecretRefEntry` type (with `name` and `key` fields). The `role` field is unused but harmless.

**Rationale:** Consistent with the credential system's existing pattern. No need for a new type when the existing one fits.

## API Types

### SeedMode

```go
// +kubebuilder:validation:Enum=overwrite;seedIfMissing
type SeedMode string

const (
    SeedModeOverwrite     SeedMode = "overwrite"
    SeedModeSeedIfMissing SeedMode = "seedIfMissing"
)
```

### WorkspaceSpec

```go
type WorkspaceSpec struct {
    SkipBootstrap    bool              `json:"skipBootstrap,omitempty"`

    // Deprecated: use InlineSources instead.
    Files            map[string]string   `json:"files,omitempty"`

    InlineSources    []InlineSource      `json:"inlineSources,omitempty"`
    ConfigMapSources []ConfigMapSource   `json:"configMapSources,omitempty"`
    GitSources       []GitSource         `json:"gitSources,omitempty"`
}
```

### InlineSource

```go
type InlineSource struct {
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Path    string   `json:"path"`

    // +kubebuilder:validation:Required
    Content string   `json:"content"`

    // Default: overwrite.
    // +optional
    Mode    SeedMode `json:"mode,omitempty"`
}
```

### ConfigMapSource

```go
type ConfigMapSource struct {
    // +kubebuilder:validation:Required
    ConfigMapRef ConfigMapRef    `json:"configMapRef"`

    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinItems=1
    Items        []ConfigMapItem `json:"items"`

    // Default: overwrite. Items can override.
    // +optional
    Mode         SeedMode        `json:"mode,omitempty"`
}

type ConfigMapRef struct {
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`
}

type ConfigMapItem struct {
    // Key in the ConfigMap's data map.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Key  string   `json:"key"`

    // Workspace-relative target path.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Path string   `json:"path"`

    // Overrides source-level mode.
    // +optional
    Mode SeedMode `json:"mode,omitempty"`
}
```

### GitSource

```go
type GitSource struct {
    // HTTPS URL of the Git repository.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    URL       string           `json:"url"`

    // Branch, tag, or commit SHA. Default: repo's default branch.
    // +optional
    Ref       string           `json:"ref,omitempty"`

    // Secret holding an HTTPS personal access token for private repos.
    // +optional
    SecretRef *SecretRefEntry  `json:"secretRef,omitempty"`

    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinItems=1
    Items     []GitItem        `json:"items"`

    // Default: overwrite. Items can override.
    // +optional
    Mode      SeedMode         `json:"mode,omitempty"`
}

type GitItem struct {
    // Path to a single file in the repository.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    RepoPath string   `json:"repoPath"`

    // Workspace-relative target path.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Path     string   `json:"path"`

    // Overrides source-level mode.
    // +optional
    Mode     SeedMode `json:"mode,omitempty"`
}
```

## Architecture

### Content flow

```
┌────────────────┐   baked into gateway ConfigMap as _ws_ keys
│ InlineSources  │──────────────────────────────────────────────┐
└────────────────┘                                              │
                                                                │
┌────────────────┐   mounted as additional volumes              │
│ ConfigMap      │   on the gateway Deployment                  │
│ Sources        │──▶ /configmap-sources/<cm-name>/             │
└────────────────┘                                              │
                                                                ▼
┌────────────────┐   cloned into emptyDir volumes       ┌──────────────┐
│ Git Sources    │──▶ /git-sources/<index>/         ──▶ │ init-seed    │──▶ PVC workspace/
└────────────────┘   by init-git-sync container         │ (reads       │
                                                        │  manifest,   │
                                                        │  copies to   │
                                                        │  workspace)  │
                                                        └──────────────┘
```

### Seeding manifest

A JSON array baked into the gateway ConfigMap (key: `_seed_manifest.json`), generated by the controller:

```json
[
  {"source": "/config/_ws_notes--README.md", "target": "notes/README.md", "mode": "seedIfMissing"},
  {"source": "/configmap-sources/team-agent-config/soul.md", "target": "SOUL.md", "mode": "overwrite"},
  {"source": "/git-sources/0/agents/AGENTS.md", "target": "AGENTS.md", "mode": "overwrite"}
]
```

### Init container sequence

```
init-volume ──▶ init-config ──▶ wait-for-proxy ──▶ init-git-sync ──▶ init-seed
(mkdir PVC)     (merge.js:       (proxy ready)     (clone repos      (read manifest,
                 config merge                       through proxy,    copy from CM
                 only — no                          into emptyDir     volumes + git
                 file seeding)                      volumes)          emptyDirs → PVC
                                                                     workspace)
```

- **init-git-sync**: Only injected when `gitSources` is non-empty. Uses `alpine/git` image (from `GIT_SYNC_IMAGE` env var). Controller generates a shell script per the plugin init container pattern. Mounts proxy CA cert, sets `HTTP_PROXY`/`HTTPS_PROXY`. Runs `git clone --depth 1 --branch <ref>` per git source, with token injected into URL for private repos.
- **init-seed**: Always injected (replaces the file seeding logic previously in `merge.js`). Mounts the gateway ConfigMap, all ConfigMap source volumes, all git emptyDirs, and the PVC. Reads `_seed_manifest.json` and copies each file using the specified mode.

### Volumes added to gateway Deployment

| Source type | Volume type | Volume name | Mount path |
|---|---|---|---|
| Inline | Existing gateway ConfigMap | `config` (existing) | `/config/` |
| ConfigMap | ConfigMap volume (one per source) | `ws-cm-<cm-name>` | `/configmap-sources/<cm-name>/` |
| Git | emptyDir (one per git source) | `ws-git-<index>` | `/git-sources/<index>/` |

ConfigMap volumes and git emptyDirs are mounted on the relevant init containers (`init-git-sync` gets git emptyDirs; `init-seed` gets everything).

## Risks / Trade-offs

- **ConfigMap source freshness**: If a referenced ConfigMap is updated, no reconcile fires (the Claw CR hasn't changed). The workspace gets the new content only on next pod restart. **Mitigation**: This is acceptable for the `overwrite` mode — pod restarts pick up the latest. A future enhancement could add a watch on referenced ConfigMaps to trigger reconcile.
- **Git clone latency**: Adds pod startup time proportional to repo size and network speed. **Mitigation**: `--depth 1` minimizes clone size. Sparse checkout could be added later for very large repos.
- **ConfigMap 1 MiB limit for seeding manifest**: The manifest itself is small (metadata only, not file contents), so this is not a practical concern.
- **`init-config` refactoring**: Removing file seeding from `merge.js` changes a well-tested init script. **Mitigation**: The seeding logic moves to `init-seed` with equivalent behavior. Existing tests cover the merge logic independently.
- **Git token exposure**: The token is injected into the clone URL, which may appear in process listings or error logs. **Mitigation**: The init container runs briefly and the URL is constructed in a shell variable, not a command-line argument. A future enhancement could use `git credential.helper` for more secure injection.
