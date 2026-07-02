## ADDED Requirements

### Requirement: WorkspaceSpec supports inline sources
The system SHALL define an `InlineSources` field on `WorkspaceSpec` of type `[]InlineSource`, where each `InlineSource` has `Path` (required, workspace-relative), `Content` (required), and `Mode` (optional, default `overwrite`).

#### Scenario: Inline source seeds a file
- **GIVEN** a Claw CR with `spec.workspace.inlineSources[0].path: "SOUL.md"` and `content: "You are helpful"`
- **WHEN** the pod starts
- **THEN** `/home/node/.openclaw/workspace/SOUL.md` SHALL contain "You are helpful"

#### Scenario: Inline source with overwrite mode replaces existing file
- **GIVEN** an existing `SOUL.md` on the PVC with user edits
- **AND** an inline source with `path: "SOUL.md"` and `mode: overwrite`
- **WHEN** the pod restarts
- **THEN** the file SHALL be overwritten with the inline content

#### Scenario: Inline source with seedIfMissing mode preserves existing file
- **GIVEN** an existing `SOUL.md` on the PVC with user edits
- **AND** an inline source with `path: "SOUL.md"` and `mode: seedIfMissing`
- **WHEN** the pod restarts
- **THEN** the existing file SHALL be preserved

### Requirement: WorkspaceSpec supports ConfigMap sources
The system SHALL define a `ConfigMapSources` field on `WorkspaceSpec` of type `[]ConfigMapSource`. Each `ConfigMapSource` has `ConfigMapRef` (required, with `Name`), `Items` (required, at least one), and `Mode` (optional, default `overwrite`). Each `ConfigMapItem` has `Key` (required), `Path` (required, workspace-relative), and `Mode` (optional, overrides source-level).

#### Scenario: ConfigMap source seeds files from a ConfigMap
- **GIVEN** a ConfigMap `team-config` with key `soul.md` containing "Team soul"
- **AND** a Claw CR with a ConfigMap source referencing `team-config` with item `{key: soul.md, path: SOUL.md}`
- **WHEN** the pod starts
- **THEN** `/home/node/.openclaw/workspace/SOUL.md` SHALL contain "Team soul"

#### Scenario: ConfigMap source references are validated
- **GIVEN** a Claw CR referencing a ConfigMap that does not exist
- **WHEN** the controller reconciles
- **THEN** the Ready condition SHALL be False with reason `ConfigFailed` and a message indicating the missing ConfigMap

#### Scenario: ConfigMap key references are validated
- **GIVEN** a Claw CR referencing a key that does not exist in the ConfigMap
- **WHEN** the controller reconciles
- **THEN** the Ready condition SHALL be False with reason `ConfigFailed` and a message indicating the missing key

#### Scenario: ConfigMap source mounted as volume
- **GIVEN** a Claw CR with a ConfigMap source referencing `team-config`
- **WHEN** the controller reconciles
- **THEN** the gateway Deployment SHALL have a volume `ws-cm-team-config` of type ConfigMap and a corresponding volumeMount at `/configmap-sources/team-config/`

### Requirement: WorkspaceSpec supports Git sources
The system SHALL define a `GitSources` field on `WorkspaceSpec` of type `[]GitSource`. Each `GitSource` has `URL` (required, HTTPS), `Ref` (optional), `SecretRef` (optional, `*SecretRefEntry`), `Items` (required, at least one, file-only), and `Mode` (optional, default `overwrite`). Each `GitItem` has `RepoPath` (required, single file), `Path` (required, workspace-relative), and `Mode` (optional, overrides source-level).

#### Scenario: Git source seeds files from a public repository
- **GIVEN** a Claw CR with a git source referencing a public HTTPS repo with item `{repoPath: "configs/SOUL.md", path: "SOUL.md"}`
- **WHEN** the pod starts
- **THEN** the init-git-sync container SHALL clone the repo and `/home/node/.openclaw/workspace/SOUL.md` SHALL contain the file from the repo

#### Scenario: Git source with token auth for private repos
- **GIVEN** a Secret `git-creds` with key `token` containing a personal access token
- **AND** a Claw CR with a git source referencing a private HTTPS repo with `secretRef: {name: git-creds, key: token}`
- **WHEN** the pod starts
- **THEN** the init-git-sync container SHALL clone the repo using the token for authentication

#### Scenario: Git clone goes through the MITM proxy
- **GIVEN** a Claw CR with git sources
- **WHEN** the init-git-sync container runs
- **THEN** it SHALL have `HTTP_PROXY` and `HTTPS_PROXY` env vars pointing to the proxy service and the proxy CA cert mounted

#### Scenario: Git clone init container only injected when needed
- **GIVEN** a Claw CR with no git sources
- **WHEN** the controller reconciles
- **THEN** the gateway Deployment SHALL NOT have an `init-git-sync` init container

#### Scenario: Git source uses shallow clone
- **WHEN** the init-git-sync container clones a repository
- **THEN** it SHALL use `--depth 1` to minimize clone size

### Requirement: Seeding mode cascade
The system SHALL resolve the seeding mode for each file using a three-tier cascade: item-level mode → source-level mode → global default (`overwrite`). Migrated `files` entries SHALL use `seedIfMissing` as source-level default.

#### Scenario: Item mode overrides source mode
- **GIVEN** a ConfigMap source with `mode: overwrite` and an item with `mode: seedIfMissing`
- **WHEN** the seeding manifest is generated
- **THEN** the item's manifest entry SHALL have `mode: seedIfMissing`

#### Scenario: Source mode applies when item mode is omitted
- **GIVEN** a ConfigMap source with `mode: seedIfMissing` and an item with no mode
- **WHEN** the seeding manifest is generated
- **THEN** the item's manifest entry SHALL have `mode: seedIfMissing`

#### Scenario: Global default applies when both are omitted
- **GIVEN** a ConfigMap source with no mode and an item with no mode
- **WHEN** the seeding manifest is generated
- **THEN** the item's manifest entry SHALL have `mode: overwrite`

### Requirement: Path conflict detection
The system SHALL reject a Claw CR if two or more sources (of any type) target the same workspace-relative path.

#### Scenario: Duplicate path across source types
- **GIVEN** an inline source with `path: "SOUL.md"` and a ConfigMap source item with `path: "SOUL.md"`
- **WHEN** the controller reconciles
- **THEN** the Ready condition SHALL be False with a message indicating the path conflict

#### Scenario: Duplicate path within same source type
- **GIVEN** two inline sources both with `path: "SOUL.md"`
- **WHEN** the controller reconciles
- **THEN** the Ready condition SHALL be False with a message indicating the path conflict

### Requirement: Backward compatibility with `files` field
The system SHALL retain the deprecated `files` field on `WorkspaceSpec`. The controller SHALL normalize `files` entries to `InlineSource` entries with `mode: seedIfMissing` at reconcile time. When `files` is non-empty, the controller SHALL emit a deprecation warning via status condition or event.

#### Scenario: Existing CR with `files` continues to work
- **GIVEN** a Claw CR with `spec.workspace.files: {"SOUL.md": "content"}`
- **WHEN** the controller reconciles
- **THEN** the file SHALL be seeded with `seedIfMissing` semantics (existing behavior preserved)

#### Scenario: Deprecation warning is emitted
- **GIVEN** a Claw CR with `spec.workspace.files` set
- **WHEN** the controller reconciles
- **THEN** a status condition or event SHALL warn that `files` is deprecated in favor of `inlineSources`

#### Scenario: `files` is not normalized when `inlineSources` is already set
- **GIVEN** a Claw CR with both `files` and `inlineSources` set
- **WHEN** the controller reconciles
- **THEN** `inlineSources` SHALL take precedence and `files` SHALL be ignored

### Requirement: Init container ordering
The gateway Deployment init containers SHALL follow this order: `init-volume` → `init-config` → `wait-for-proxy` → `init-git-sync` (if git sources) → `init-seed`.

#### Scenario: Full init container chain with git sources
- **GIVEN** a Claw CR with git sources
- **WHEN** the controller reconciles
- **THEN** the Deployment SHALL have init containers in order: init-volume, init-config, wait-for-proxy, init-git-sync, init-seed

#### Scenario: Init container chain without git sources
- **GIVEN** a Claw CR with no git sources
- **WHEN** the controller reconciles
- **THEN** the Deployment SHALL have init containers in order: init-volume, init-config, wait-for-proxy, init-seed

### Requirement: Seeding manifest
The controller SHALL generate a seeding manifest (JSON array) baked into the gateway ConfigMap that describes all workspace file sources, target paths, and modes. The `init-seed` container SHALL consume this manifest to perform file seeding.

#### Scenario: Manifest contains entries for all source types
- **GIVEN** a Claw CR with inline, ConfigMap, and git sources
- **WHEN** the controller reconciles
- **THEN** the gateway ConfigMap SHALL contain a `_seed_manifest.json` key with entries for all sources

### Requirement: GIT_SYNC_IMAGE env var
The operator SHALL read a `GIT_SYNC_IMAGE` environment variable to configure the container image used for the `init-git-sync` init container.

#### Scenario: Custom git image is used
- **GIVEN** the operator is deployed with `GIT_SYNC_IMAGE=registry.corp.com/alpine/git:2.47`
- **WHEN** the controller creates an init-git-sync container
- **THEN** the container image SHALL be `registry.corp.com/alpine/git:2.47`
