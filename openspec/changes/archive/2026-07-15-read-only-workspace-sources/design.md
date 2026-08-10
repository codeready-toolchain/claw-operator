## Context

Workspace sources (introduced in commit 9e9fb364) seed files from inline content, ConfigMaps, and Git repos into the OpenClaw workspace on the PVC. The seeding flow is: source volumes are mounted on init containers, the `init-seed` container reads a manifest and copies files to the PVC, and the gateway container reads/writes the PVC at runtime.

All three source types already have pod-level volumes defined:
- **Inline**: content baked into the gateway ConfigMap (`config` volume) as `_ws_<encoded-path>` keys
- **ConfigMap**: dedicated `ws-cm-<name>` volumes
- **Git**: `ws-git-<index>` emptyDir volumes populated by `init-git-sync`

The `mode` field (overwrite/seedIfMissing) controls re-seeding behavior on restart, but nothing prevents the OpenClaw process from modifying seeded files during runtime.

## Goals / Non-Goals

**Goals:**
- Prevent the OpenClaw process from modifying operator-managed workspace files at runtime
- Use OS-level enforcement (not just file permissions) so protection cannot be circumvented
- Support read-only mode for all three source types (inline, ConfigMap, Git)
- Keep the API simple: one `readOnly` bool per source, all items inherit it

**Non-Goals:**
- Per-item read-only control (can be added later if needed)
- Read-only directories beyond what individual file mounts provide
- Protecting files that live on the PVC (read-only requires bypassing the PVC)

## Decisions

### 1. Read-only via direct volume mounts, not PVC seeding

When `readOnly: true`, skip the PVC seed manifest entry entirely and instead mount the source volume directly into the gateway container at the workspace target path with `readOnly: true`.

**Rationale:** Kubernetes read-only volume mounts return `EROFS` on any write attempt. This is OS-level enforcement that cannot be circumvented by the process regardless of UID or capabilities. File permission approaches (`chmod 444`) would be bypassable since the same UID owns the files.

**Trade-off:** Files are volume mounts, not PVC copies. They cannot be modified by the user or agent under any circumstances. This is the desired behavior.

### 2. Reuse existing volumes

Each source type already has a pod-level volume. Read-only mounts add volumeMounts to the gateway container from these existing volumes using `subPath` to target individual files.

| Source Type    | Volume           | Gateway volumeMount subPath  |
|----------------|------------------|------------------------------|
| InlineSource   | `config`         | `_ws_<encoded-path>`         |
| ConfigMapSource| `ws-cm-<name>`   | `<item.Key>`                 |
| GitSource      | `ws-git-<index>` | `<item.RepoPath>`            |

All mounts target `mountPath: /home/node/.openclaw/workspace/<target-path>`.

**Rationale:** No new volumes, volume types, or pod-level changes needed. The volumes exist because the init containers already use them for seeding.

### 3. `readOnly` field at source level only

A single `ReadOnly bool` on each source type. All items in that source inherit it. No per-item override.

```go
type InlineSource struct {
    Path     string   `json:"path"`
    Content  string   `json:"content"`
    Mode     SeedMode `json:"mode,omitempty"`
    ReadOnly bool     `json:"readOnly,omitempty"`
}

type ConfigMapSource struct {
    ConfigMapRef ConfigMapRef    `json:"configMapRef"`
    Items        []ConfigMapItem `json:"items"`
    Mode         SeedMode        `json:"mode,omitempty"`
    ReadOnly     bool            `json:"readOnly,omitempty"`
}

type GitSource struct {
    URL       string         `json:"url"`
    Ref       string         `json:"ref,omitempty"`
    SecretRef *SecretRefEntry `json:"secretRef,omitempty"`
    Items     []GitItem      `json:"items"`
    Mode      SeedMode        `json:"mode,omitempty"`
    ReadOnly  bool            `json:"readOnly,omitempty"`
}
```

**Rationale:** Simpler API, covers the primary use case (a whole source is managed reference content). If per-item control is ever needed, the same ConfigMap can be split into two `ConfigMapSource` entries — one read-only, one writable.

### 4. `mode` is silently ignored when `readOnly: true`

The `mode` field (overwrite/seedIfMissing) controls PVC seeding behavior. When `readOnly: true`, there is no PVC seeding — the file is always the current source version via a direct mount. Rather than rejecting the combination with a validation error, `mode` is simply ignored.

**Rationale:** Reduces friction. Users setting source-level defaults for `mode` shouldn't have to remove them when enabling `readOnly`. The behavior is unambiguous regardless.

### 5. `subPath` mounts (no live ConfigMap update propagation)

Kubernetes only propagates ConfigMap updates to full-volume mounts, not `subPath` mounts. Read-only files from inline and ConfigMap sources will require a pod restart to pick up changes. This is consistent with the existing config-hash rollout mechanism that triggers restarts when the gateway ConfigMap changes.

**Rationale:** Using `subPath` is necessary to mount individual files into specific workspace paths without shadowing the entire directory. The restart-on-change behavior is already handled by `stampGatewayConfigHash`.

## Risks / Trade-offs

**[Risk] Volume mount count grows with read-only files** — Each read-only file adds one volumeMount to the gateway container. Kubernetes handles many mounts well, but very large numbers (100+) could affect pod startup time. Mitigation: this is unlikely in practice; typical use cases involve a handful of managed files.

**[Risk] Parent directory creation for deeply nested paths** — A read-only file at `docs/policies/guide.md` requires intermediate directories. The kubelet creates mount-point parent directories automatically, so this should work. Worth validating in e2e tests.

**[Risk] Path conflicts between read-only mounts and PVC** — A read-only mount overlays the PVC at that specific path. If a previous (non-read-only) run seeded the file to the PVC, the old PVC copy becomes invisible (shadowed by the mount) but still consumes disk. Not harmful, but the stale copy remains on the PVC.
