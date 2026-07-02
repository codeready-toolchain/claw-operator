## 1. API Types

- [x] 1.1 Add `SeedMode` type and constants (`SeedModeOverwrite`, `SeedModeSeedIfMissing`) to `api/v1alpha1/claw_types.go`
- [x] 1.2 Add `InlineSource` struct (`Path`, `Content`, `Mode`) with kubebuilder validation markers
- [x] 1.3 Add `ConfigMapRef` struct (`Name`)
- [x] 1.4 Add `ConfigMapItem` struct (`Key`, `Path`, `Mode`)
- [x] 1.5 Add `ConfigMapSource` struct (`ConfigMapRef`, `Items`, `Mode`)
- [x] 1.6 Add `GitItem` struct (`RepoPath`, `Path`, `Mode`)
- [x] 1.7 Add `GitSource` struct (`URL`, `Ref`, `SecretRef`, `Items`, `Mode`)
- [x] 1.8 Add `InlineSources`, `ConfigMapSources`, `GitSources` fields to `WorkspaceSpec`, mark `Files` as deprecated in its godoc
- [x] 1.9 Run `make generate` to regenerate `zz_generated.deepcopy.go`
- [x] 1.10 Run `make manifests` to regenerate CRD YAML in `config/crd/bases/`

## 2. Backward Compatibility — `files` Normalization

- [x] 2.1 Add `normalizeWorkspaceFiles()` function in `claw_workspace.go` that converts `files` map entries to `InlineSource` with `Mode: SeedModeSeedIfMissing`
- [x] 2.2 Call `normalizeWorkspaceFiles()` at the top of the reconcile loop (before any workspace processing)
- [x] 2.3 Emit a status condition or event when `files` is non-empty to warn about deprecation
- [x] 2.4 Add tests: normalization produces correct `InlineSource` entries, mode is `seedIfMissing`, `inlineSources` is not overwritten if already set

## 3. Path Conflict Validation

- [x] 3.1 Add `validateWorkspacePathConflicts()` that collects all target paths from `InlineSources`, `ConfigMapSources`, and `GitSources` and returns an error if any path appears more than once
- [x] 3.2 Apply existing path validation rules (no absolute, no `..`, no `--`, no operator skill conflicts) to all source types
- [x] 3.3 Call validation after normalization in the reconcile loop
- [x] 3.4 Add tests: duplicate path across source types errors, duplicate within same source type errors, no conflict passes

## 4. Seeding Manifest Generation

- [x] 4.1 Define seeding manifest entry struct (source path, target path, mode)
- [x] 4.2 Add `generateSeedManifest()` that builds the JSON manifest from all source types: inline sources → `/config/_ws_<encoded-path>`, ConfigMap sources → `/configmap-sources/<cm-name>/<key>`, Git sources → `/git-sources/<index>/<repoPath>`
- [x] 4.3 Inject manifest into gateway ConfigMap as `_seed_manifest.json` key
- [x] 4.4 Add tests: manifest generation for each source type, mixed sources, empty sources

## 5. Inline Sources — ConfigMap Key Injection

- [x] 5.1 Refactor `injectWorkspaceFiles()` to read from `InlineSources` instead of `Files` (the `_ws_` key mechanism stays the same)
- [x] 5.2 Update existing tests to use `InlineSources`
- [x] 5.3 Add tests: inline sources with different modes appear in manifest correctly

## 6. ConfigMap Sources — Volume Injection

- [x] 6.1 Add `injectConfigMapSourceVolumes()` that adds a ConfigMap volume (`ws-cm-<cm-name>`) and volumeMount (`/configmap-sources/<cm-name>/`) per ConfigMap source to the gateway Deployment
- [x] 6.2 Mount ConfigMap source volumes on the `init-seed` container
- [x] 6.3 Validate that referenced ConfigMaps exist and contain the specified keys (similar to credential secret validation)
- [x] 6.4 Add tests: volume and volumeMount injection, missing ConfigMap error, missing key error

## 7. Git Sources — Clone Init Container

- [x] 7.1 Add `GIT_SYNC_IMAGE` env var to `cmd/main.go` and the reconciler config struct
- [x] 7.2 Add `injectGitSyncInitContainer()` that generates a shell script performing `git clone --depth 1 --branch <ref>` per git source, with HTTPS token injection for private repos
- [x] 7.3 Add emptyDir volume (`ws-git-<index>`) and volumeMount (`/git-sources/<index>/`) per git source
- [x] 7.4 Mount proxy CA cert, set `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` env vars on the init container
- [x] 7.5 Insert `init-git-sync` container after `wait-for-proxy` in the init container list
- [x] 7.6 Only inject when `gitSources` is non-empty
- [x] 7.7 Add tests: script generation with public repo, script generation with private repo (token injection), emptyDir volume injection, container not injected when no git sources

## 8. Init-Seed Container

- [x] 8.1 Generate `init-seed` shell script that reads `_seed_manifest.json` and copies files to the PVC workspace with mode-aware logic (`overwrite` → always copy, `seedIfMissing` → copy only if target doesn't exist)
- [x] 8.2 Mount gateway ConfigMap, all ConfigMap source volumes, all git emptyDirs, and PVC on `init-seed`
- [x] 8.3 Insert `init-seed` after `init-git-sync` (or after `wait-for-proxy` if no git sources) in the init container list
- [x] 8.4 Add tests: overwrite mode replaces existing file, seedIfMissing mode preserves existing file, mixed modes in one manifest

## 9. Refactor `merge.js` — Remove File Seeding

- [x] 9.1 Remove `_ws_*` key iteration and `seedIfMissing` calls from `merge.js`
- [x] 9.2 Remove builtin file seeding (`AGENTS.md`, `SOUL.md`, `BOOTSTRAP.md`) from `merge.js` — these move to the seeding manifest as inline sources or are handled by `init-seed`
- [x] 9.3 Remove `_skill_*` key iteration from `merge.js` (skill files continue using `copyAlways` via existing mechanism or move to `init-seed`)
- [x] 9.4 Keep `merge.js` focused on `operator.json` ↔ `openclaw.json` merge and directory creation only
- [x] 9.5 Update existing `merge.js` tests if any

## 10. RBAC

- [x] 10.1 Add kubebuilder RBAC marker for `get`, `list`, `watch` on ConfigMaps (if not already present) to allow the controller to validate referenced ConfigMaps
- [x] 10.2 Run `make manifests` to regenerate RBAC

## 11. Integration Tests

- [x] 11.1 Add envtest integration test: Claw CR with `inlineSources` → ConfigMap has `_ws_` keys and seed manifest
- [x] 11.2 Add envtest integration test: Claw CR with `configMapSources` → Deployment has ConfigMap volumes, seed manifest has correct entries
- [x] 11.3 Add envtest integration test: Claw CR with `gitSources` → Deployment has emptyDir volumes and `init-git-sync` container, seed manifest has correct entries
- [x] 11.4 Add envtest integration test: Claw CR with deprecated `files` → normalized to inline sources, deprecation condition set
- [x] 11.5 Add envtest integration test: path conflict across source types → reconcile fails with condition
- [x] 11.6 Run full test suite (`make test`) and lint (`make lint`)

## 12. E2E Tests

- [x] 12.1 Add e2e test: ConfigMap sources — full e2e (create ConfigMap, apply Claw CR with `configMapSources`, verify volume, seed manifest, init-seed container, pod Running, init-seed logs)
- [x] 12.2 Add e2e test: Git sources — wiring only (apply Claw CR with `gitSources`, verify init-git-sync container image, proxy env vars, token secretKeyRef, emptyDir volume, seed manifest entries, clone script, init container ordering)
- [x] 12.3 Add `GIT_SYNC_IMAGE` env var to `config/manager/manager.yaml`
