## 1. Update API Types

- [x] 1.1 Add `ReadOnly bool` field with `json:"readOnly,omitempty"` tag to `InlineSource` in `api/v1alpha1/claw_types.go`
- [x] 1.2 Add `ReadOnly bool` field with `json:"readOnly,omitempty"` tag to `ConfigMapSource` in `api/v1alpha1/claw_types.go`
- [x] 1.3 Add `ReadOnly bool` field with `json:"readOnly,omitempty"` tag to `GitSource` in `api/v1alpha1/claw_types.go`
- [x] 1.4 Run `make generate` to regenerate DeepCopy methods
- [x] 1.5 Run `make manifests` to regenerate CRD YAML

## 2. Skip Read-Only Entries in Seed Manifest

- [x] 2.1 Update `generateSeedManifest()` in `claw_workspace.go` to skip inline sources where `ReadOnly == true`
- [x] 2.2 Update `generateSeedManifest()` to skip ConfigMap source items where the parent ConfigMapSource has `ReadOnly == true`
- [x] 2.3 Update `generateSeedManifest()` to skip Git source items where the parent GitSource has `ReadOnly == true`
- [x] 2.4 Add unit tests verifying read-only entries are excluded from the manifest while writable entries remain

## 3. Inject Read-Only Volume Mounts on Gateway Container

- [x] 3.1 Implement `injectReadOnlyWorkspaceMounts()` in `claw_workspace.go` that adds per-item volumeMounts to the gateway container for all read-only sources
- [x] 3.2 For read-only inline sources: mount from `config` volume with `subPath: _ws_<encoded-path>`, `readOnly: true`
- [x] 3.3 For read-only ConfigMap sources: mount from `ws-cm-<name>` volume with `subPath: <item.Key>`, `readOnly: true`
- [x] 3.4 For read-only Git sources: mount from `ws-git-<index>` volume with `subPath: <item.RepoPath>`, `readOnly: true`
- [x] 3.5 All mounts target `mountPath: /home/node/.openclaw/workspace/<target-path>`
- [x] 3.6 Add unit tests verifying correct volumeMounts are added for each source type
- [x] 3.7 Add unit test for mixed read-only and writable sources in the same spec

## 4. Wire Into Reconciler

- [x] 4.1 Call `injectReadOnlyWorkspaceMounts()` in phase 3 of `claw_resource_controller.go` (alongside existing `inject*` calls)

## 5. Verification

- [x] 5.1 Run `make build` to verify compilation
- [x] 5.2 Run `make lint` to verify linting passes
- [x] 5.3 Run `make test` to verify all tests pass (existing + new)
