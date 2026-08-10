## ADDED Requirements

### Requirement: Source-level read-only mode
The system SHALL support a `readOnly` boolean field on `InlineSource`, `ConfigMapSource`, and `GitSource`. When `readOnly: true`, all items in that source SHALL be mounted directly into the gateway container's workspace path via Kubernetes read-only volume mounts, bypassing PVC seeding entirely.

#### Scenario: Read-only inline source is mounted directly
- **GIVEN** a Claw CR with an inline source `{path: "POLICY.md", content: "Do not delete", readOnly: true}`
- **WHEN** the controller reconciles
- **THEN** the gateway container SHALL have a volumeMount from the `config` volume at `/home/node/.openclaw/workspace/POLICY.md` with `subPath: _ws_POLICY--md` (or equivalent encoded path) and `readOnly: true`
- **AND** the seed manifest SHALL NOT contain an entry for `POLICY.md`

#### Scenario: Read-only ConfigMap source is mounted directly
- **GIVEN** a ConfigMap `compliance-docs` with key `rules.md` containing "Compliance rules"
- **AND** a Claw CR with a ConfigMap source referencing `compliance-docs` with `readOnly: true` and item `{key: rules.md, path: docs/rules.md}`
- **WHEN** the controller reconciles
- **THEN** the gateway container SHALL have a volumeMount from the `ws-cm-compliance-docs` volume at `/home/node/.openclaw/workspace/docs/rules.md` with `subPath: rules.md` and `readOnly: true`
- **AND** the seed manifest SHALL NOT contain an entry for `docs/rules.md`

#### Scenario: Read-only Git source is mounted directly
- **GIVEN** a Claw CR with a git source at index 0 with `readOnly: true` and item `{repoPath: "policies/guide.md", path: "guide.md"}`
- **WHEN** the controller reconciles
- **THEN** the gateway container SHALL have a volumeMount from the `ws-git-0` volume at `/home/node/.openclaw/workspace/guide.md` with `subPath: policies/guide.md` and `readOnly: true`
- **AND** the seed manifest SHALL NOT contain an entry for `guide.md`

#### Scenario: Read-only files cannot be written by the OpenClaw process
- **GIVEN** a read-only workspace source is mounted
- **WHEN** the OpenClaw process attempts to write to the file
- **THEN** the write SHALL fail with a read-only filesystem error (EROFS)

#### Scenario: Mode field is ignored when readOnly is true
- **GIVEN** a ConfigMap source with `readOnly: true` and `mode: seedIfMissing`
- **WHEN** the controller reconciles
- **THEN** the file SHALL be mounted read-only (mode has no effect since PVC seeding is bypassed)

#### Scenario: Mixed read-only and writable sources
- **GIVEN** a Claw CR with a read-only ConfigMap source targeting `POLICY.md` and a writable inline source targeting `SOUL.md`
- **WHEN** the controller reconciles
- **THEN** `POLICY.md` SHALL be a read-only volume mount on the gateway container
- **AND** `SOUL.md` SHALL be seeded onto the PVC via the seed manifest (writable)

#### Scenario: Read-only source with multiple items
- **GIVEN** a ConfigMap source with `readOnly: true` and items `[{key: a.md, path: a.md}, {key: b.md, path: b.md}]`
- **WHEN** the controller reconciles
- **THEN** the gateway container SHALL have read-only volumeMounts for both `a.md` and `b.md`

#### Scenario: Path conflict between read-only and writable sources
- **GIVEN** a read-only inline source with `path: "SOUL.md"` and a writable ConfigMap source item with `path: "SOUL.md"`
- **WHEN** the controller reconciles
- **THEN** the Ready condition SHALL be False with a message indicating the path conflict
