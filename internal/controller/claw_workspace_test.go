/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func makeTestConfigMap() *unstructured.Unstructured {
	cm := &unstructured.Unstructured{}
	cm.SetKind(ConfigMapKind)
	cm.SetName(getConfigMapName(testInstanceName))
	cm.Object["data"] = map[string]any{
		"operator.json": "{}",
		"openclaw.json": "{}",
	}
	return cm
}

// --- encodeWorkspacePath tests ---

func TestEncodeWorkspacePath(t *testing.T) {
	t.Run("should return simple filename unchanged", func(t *testing.T) {
		assert.Equal(t, "IDENTITY.md", encodeWorkspacePath("IDENTITY.md"))
	})

	t.Run("should encode slashes as --", func(t *testing.T) {
		assert.Equal(t, "docs--README.md", encodeWorkspacePath("docs/README.md"))
	})

	t.Run("should encode multiple path segments", func(t *testing.T) {
		assert.Equal(t, "a--b--c.md", encodeWorkspacePath("a/b/c.md"))
	})
}

// --- validateWorkspaceFiles tests ---

func TestValidateWorkspaceFiles(t *testing.T) {
	t.Run("should accept valid simple path", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"IDENTITY.md": "content"})
		assert.NoError(t, err)
	})

	t.Run("should accept valid nested path", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"docs/README.md": "content"})
		assert.NoError(t, err)
	})

	t.Run("should accept AGENTS.md override", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"AGENTS.md": "custom agents"})
		assert.NoError(t, err)
	})

	t.Run("should reject empty path", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("should reject absolute path", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"/etc/passwd": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be absolute")
	})

	t.Run("should reject directory traversal", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"../../etc/passwd": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not contain ".."`)
	})

	t.Run("should reject directory traversal embedded in middle of path", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"foo/../bar": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not contain ".."`)
	})

	t.Run("should reject path with -- delimiter", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"file--name.md": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved as path encoding delimiter")
	})

	t.Run("should reject path conflicting with platform skill", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"skills/platform/SKILL.md": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with operator-managed platform skill")
	})

	t.Run("should reject path conflicting with kubernetes skill", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"skills/kubernetes/SKILL.md": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with operator-managed kubernetes skill")
	})

	t.Run("should accept non-conflicting skill path", func(t *testing.T) {
		err := validateWorkspaceFiles(map[string]string{"skills/custom/SKILL.md": "content"})
		assert.NoError(t, err)
	})

	t.Run("should accept nil map", func(t *testing.T) {
		err := validateWorkspaceFiles(nil)
		assert.NoError(t, err)
	})
}

// --- validateSkillNames tests ---

func TestValidateSkillNames(t *testing.T) {
	t.Run("should accept valid name", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"quote-builder": "content"})
		assert.NoError(t, err)
	})

	t.Run("should reject empty name", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("should reject dot name", func(t *testing.T) {
		err := validateSkillNames(map[string]string{".": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not be "."`)
	})

	t.Run("should reject dot-dot name", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"..": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not be ".."`)
	})

	t.Run("should reject name with slash", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"my/skill": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not contain "/"`)
	})

	t.Run("should reject name with -- delimiter", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"my--skill": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved as path encoding delimiter")
	})

	t.Run("should reject builtin platform name", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"platform": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with builtin operator skill")
	})

	t.Run("should reject builtin kubernetes name", func(t *testing.T) {
		err := validateSkillNames(map[string]string{"kubernetes": "content"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with builtin operator skill")
	})

	t.Run("should accept nil map", func(t *testing.T) {
		err := validateSkillNames(nil)
		assert.NoError(t, err)
	})
}

// --- normalizeWorkspaceFiles tests ---

func TestNormalizeWorkspaceFiles(t *testing.T) {
	t.Run("should be no-op when workspace is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{},
		}
		migrated := normalizeWorkspaceFiles(instance)
		assert.False(t, migrated)
	})

	t.Run("should be no-op when files is empty", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{},
			},
		}
		migrated := normalizeWorkspaceFiles(instance)
		assert.False(t, migrated)
	})

	t.Run("should convert files to inline sources with seedIfMissing", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					Files: map[string]string{ //nolint:staticcheck // testing deprecated field
						"SOUL.md":  "soul content",
						"TOOLS.md": "tools content",
					},
				},
			},
		}
		migrated := normalizeWorkspaceFiles(instance)
		assert.True(t, migrated)
		require.Len(t, instance.Spec.Workspace.InlineSources, 2)

		byPath := map[string]clawv1alpha1.InlineSource{}
		for _, src := range instance.Spec.Workspace.InlineSources {
			byPath[src.Path] = src
		}
		assert.Equal(t, "soul content", byPath["SOUL.md"].Content)
		assert.Equal(t, clawv1alpha1.SeedModeSeedIfMissing, byPath["SOUL.md"].Mode)
		assert.Equal(t, "tools content", byPath["TOOLS.md"].Content)
		assert.Equal(t, clawv1alpha1.SeedModeSeedIfMissing, byPath["TOOLS.md"].Mode)
	})

	t.Run("should not overwrite existing inline sources", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					Files: map[string]string{ //nolint:staticcheck // testing deprecated field
						"SOUL.md": "old content",
					},
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "new content", Mode: clawv1alpha1.SeedModeOverwrite},
					},
				},
			},
		}
		migrated := normalizeWorkspaceFiles(instance)
		assert.False(t, migrated)
		require.Len(t, instance.Spec.Workspace.InlineSources, 1)
		assert.Equal(t, "new content", instance.Spec.Workspace.InlineSources[0].Content)
		assert.Equal(t, clawv1alpha1.SeedModeOverwrite, instance.Spec.Workspace.InlineSources[0].Mode)
	})
}

// --- validateAllWorkspacePaths tests ---

func TestValidateAllWorkspacePaths(t *testing.T) {
	t.Run("should be no-op when workspace is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{},
		}
		assert.NoError(t, validateAllWorkspacePaths(instance))
	})

	t.Run("should pass with no conflicts", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "soul"},
					},
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "tools", Path: "TOOLS.md"},
							},
						},
					},
					GitSources: []clawv1alpha1.GitSource{
						{
							URL: "https://git.example.com/repo.git",
							Items: []clawv1alpha1.GitItem{
								{RepoPath: "agents/AGENTS.md", Path: "AGENTS.md"},
							},
						},
					},
				},
			},
		}
		assert.NoError(t, validateAllWorkspacePaths(instance))
	})

	t.Run("should reject duplicate path across inline and configMap", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "inline"},
					},
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "soul", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SOUL.md")
		assert.Contains(t, err.Error(), "inline source")
		assert.Contains(t, err.Error(), `configMapSource "team"`)
	})

	t.Run("should reject duplicate path across configMap and git", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "agents", Path: "AGENTS.md"},
							},
						},
					},
					GitSources: []clawv1alpha1.GitSource{
						{
							URL: "https://git.example.com/repo.git",
							Items: []clawv1alpha1.GitItem{
								{RepoPath: "AGENTS.md", Path: "AGENTS.md"},
							},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "AGENTS.md")
	})

	t.Run("should reject duplicate path within same source type", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "first"},
						{Path: "SOUL.md", Content: "second"},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SOUL.md")
	})

	t.Run("should reject invalid path in configMap source", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "bad", Path: "../../etc/passwd"},
							},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not contain ".."`)
	})

	t.Run("should reject invalid path in git source", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					GitSources: []clawv1alpha1.GitSource{
						{
							URL: "https://git.example.com/repo.git",
							Items: []clawv1alpha1.GitItem{
								{RepoPath: "ok.md", Path: "/etc/passwd"},
							},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be absolute")
	})

	t.Run("should reject duplicate configMapSource refs", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "shared"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "a", Path: "a.md"}},
						},
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "shared"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "b", Path: "b.md"}},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"shared" is referenced more than once`)
	})

	t.Run("should accept distinct configMapSource refs with different names", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team-config"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "soul.md", Path: "SOUL.md"}},
						},
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "shared-tools"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "tools.md", Path: "TOOLS.md"}},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		assert.NoError(t, err)
	})

	t.Run("should reject duplicate configMapSource refs even with other sources present", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "README.md", Content: "hello"},
					},
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "a", Path: "a.md"}},
						},
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "other"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "b", Path: "b.md"}},
						},
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "c", Path: "c.md"}},
						},
					},
				},
			},
		}
		err := validateAllWorkspacePaths(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"team" is referenced more than once`)
	})
}

// --- generateSeedManifest tests ---

func TestGenerateSeedManifest(t *testing.T) {
	t.Run("should include builtin files when workspace is nil", func(t *testing.T) {
		entries := generateSeedManifest(nil)
		require.Len(t, entries, 3)
		assert.Equal(t, "AGENTS.md", entries[0].Target)
		assert.Equal(t, "SOUL.md", entries[1].Target)
		assert.Equal(t, ".operator/BOOTSTRAP.md", entries[2].Target)
		for _, e := range entries {
			assert.Equal(t, "seedIfMissing", e.Mode)
		}
	})

	t.Run("should include builtin files when no user sources", func(t *testing.T) {
		entries := generateSeedManifest(&clawv1alpha1.WorkspaceSpec{})
		require.Len(t, entries, 3)
	})

	t.Run("should generate entries for inline sources plus builtins", func(t *testing.T) {
		entries := generateSeedManifest(&clawv1alpha1.WorkspaceSpec{
			InlineSources: []clawv1alpha1.InlineSource{
				{Path: "custom/SOUL.md", Content: "soul", Mode: clawv1alpha1.SeedModeSeedIfMissing},
				{Path: "docs/guide.md", Content: "guide"},
			},
		})
		require.Len(t, entries, 5) // 3 builtins + 2 inline

		byTarget := map[string]seedManifestEntry{}
		for _, e := range entries {
			byTarget[e.Target] = e
		}
		assert.Equal(t, "/config/_ws_custom--SOUL.md", byTarget["custom/SOUL.md"].Source)
		assert.Equal(t, "seedIfMissing", byTarget["custom/SOUL.md"].Mode)
		assert.Equal(t, "/config/_ws_docs--guide.md", byTarget["docs/guide.md"].Source)
		assert.Equal(t, "overwrite", byTarget["docs/guide.md"].Mode)
	})

	t.Run("should generate entries for configMap sources", func(t *testing.T) {
		entries := generateSeedManifest(&clawv1alpha1.WorkspaceSpec{
			ConfigMapSources: []clawv1alpha1.ConfigMapSource{
				{
					ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team-config"},
					Mode:         clawv1alpha1.SeedModeSeedIfMissing,
					Items: []clawv1alpha1.ConfigMapItem{
						{Key: "soul.md", Path: "custom-soul.md"},
						{Key: "tools.md", Path: "TOOLS.md", Mode: clawv1alpha1.SeedModeOverwrite},
					},
				},
			},
		})
		require.Len(t, entries, 5) // 3 builtins + 2 configMap items

		byTarget := map[string]seedManifestEntry{}
		for _, e := range entries {
			byTarget[e.Target] = e
		}
		assert.Equal(t, "/configmap-sources/ws-cm-team-config/soul.md", byTarget["custom-soul.md"].Source)
		assert.Equal(t, "seedIfMissing", byTarget["custom-soul.md"].Mode, "should inherit source-level mode")
		assert.Equal(t, "/configmap-sources/ws-cm-team-config/tools.md", byTarget["TOOLS.md"].Source)
		assert.Equal(t, "overwrite", byTarget["TOOLS.md"].Mode, "item mode should override source mode")
	})

	t.Run("should generate entries for git sources", func(t *testing.T) {
		entries := generateSeedManifest(&clawv1alpha1.WorkspaceSpec{
			GitSources: []clawv1alpha1.GitSource{
				{
					URL: "https://git.example.com/repo.git",
					Items: []clawv1alpha1.GitItem{
						{RepoPath: "agents/CUSTOM_AGENTS.md", Path: "CUSTOM_AGENTS.md"},
					},
				},
			},
		})
		require.Len(t, entries, 4) // 3 builtins + 1 git item

		gitEntry := entries[3]
		assert.Equal(t, "/git-sources/0/agents/CUSTOM_AGENTS.md", gitEntry.Source)
		assert.Equal(t, "CUSTOM_AGENTS.md", gitEntry.Target)
		assert.Equal(t, "overwrite", gitEntry.Mode)
	})

	t.Run("should generate entries for mixed sources", func(t *testing.T) {
		entries := generateSeedManifest(&clawv1alpha1.WorkspaceSpec{
			InlineSources: []clawv1alpha1.InlineSource{
				{Path: "notes.md", Content: "notes"},
			},
			ConfigMapSources: []clawv1alpha1.ConfigMapSource{
				{
					ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
					Items: []clawv1alpha1.ConfigMapItem{
						{Key: "soul", Path: "custom-soul.md"},
					},
				},
			},
			GitSources: []clawv1alpha1.GitSource{
				{
					URL: "https://example.com/repo.git",
					Items: []clawv1alpha1.GitItem{
						{RepoPath: "TOOLS.md", Path: "TOOLS.md"},
					},
				},
			},
		})
		require.Len(t, entries, 6) // 3 builtins + 3 user sources
		targets := map[string]bool{}
		for _, e := range entries {
			targets[e.Target] = true
		}
		assert.True(t, targets["notes.md"])
		assert.True(t, targets["custom-soul.md"])
		assert.True(t, targets["TOOLS.md"])
		assert.True(t, targets["AGENTS.md"], "builtin AGENTS.md should be present")
	})

	t.Run("should use correct git source index for multiple git sources", func(t *testing.T) {
		entries := generateSeedManifest(&clawv1alpha1.WorkspaceSpec{
			GitSources: []clawv1alpha1.GitSource{
				{
					URL: "https://example.com/repo1.git",
					Items: []clawv1alpha1.GitItem{
						{RepoPath: "a.md", Path: "a.md"},
					},
				},
				{
					URL: "https://example.com/repo2.git",
					Items: []clawv1alpha1.GitItem{
						{RepoPath: "b.md", Path: "b.md"},
					},
				},
			},
		})
		require.Len(t, entries, 5) // 3 builtins + 2 git items
		assert.Equal(t, "/git-sources/0/a.md", entries[3].Source)
		assert.Equal(t, "/git-sources/1/b.md", entries[4].Source)
	})
}

func TestResolveSeedMode(t *testing.T) {
	t.Run("item mode wins", func(t *testing.T) {
		assert.Equal(t, "seedIfMissing", resolveSeedMode(clawv1alpha1.SeedModeSeedIfMissing, clawv1alpha1.SeedModeOverwrite))
	})

	t.Run("source mode when item is empty", func(t *testing.T) {
		assert.Equal(t, "seedIfMissing", resolveSeedMode("", clawv1alpha1.SeedModeSeedIfMissing))
	})

	t.Run("global default when both empty", func(t *testing.T) {
		assert.Equal(t, "overwrite", resolveSeedMode("", ""))
	})
}

func TestInjectSeedManifest(t *testing.T) {
	t.Run("should inject builtin entries even with no user sources", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := injectSeedManifest([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		raw, found, _ := unstructured.NestedString(cm.Object, "data", seedManifestKey)
		require.True(t, found)

		var entries []seedManifestEntry
		require.NoError(t, json.Unmarshal([]byte(raw), &entries))
		require.Len(t, entries, 3, "should contain 3 builtin entries")
	})

	t.Run("should inject manifest JSON with inline and builtins into ConfigMap", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "custom.md", Content: "custom"},
					},
				},
			},
		}
		err := injectSeedManifest([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		raw, found, _ := unstructured.NestedString(cm.Object, "data", seedManifestKey)
		require.True(t, found)

		var entries []seedManifestEntry
		require.NoError(t, json.Unmarshal([]byte(raw), &entries))
		require.Len(t, entries, 4) // 3 builtins + 1 inline

		byTarget := map[string]seedManifestEntry{}
		for _, e := range entries {
			byTarget[e.Target] = e
		}
		assert.Equal(t, "/config/_ws_custom.md", byTarget["custom.md"].Source)
		assert.Equal(t, "overwrite", byTarget["custom.md"].Mode)
		assert.Equal(t, "seedIfMissing", byTarget["AGENTS.md"].Mode)
	})
}

// --- injectWorkspaceFiles tests ---

func TestInjectWorkspaceFiles(t *testing.T) {
	t.Run("should be no-op when workspace is nil", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := injectWorkspaceFiles([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		data, _, _ := unstructured.NestedStringMap(cm.Object, "data")
		assert.NotContains(t, data, "_ws_IDENTITY.md")
	})

	t.Run("should be no-op when workspace has skipBootstrap but no files", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					SkipBootstrap: true,
				},
			},
		}
		err := injectWorkspaceFiles([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		data, _, _ := unstructured.NestedStringMap(cm.Object, "data")
		for k := range data {
			assert.False(t, len(k) > 4 && k[:4] == "_ws_", "no _ws_ keys should be added when files map is empty")
		}
	})

	t.Run("should inject _ws_ prefixed keys for inline sources", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "IDENTITY.md", Content: "# Identity\nName: Test"},
						{Path: "AGENTS.md", Content: "# Custom Agents"},
					},
				},
			},
		}
		err := injectWorkspaceFiles([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		val, found, _ := unstructured.NestedString(cm.Object, "data", "_ws_IDENTITY.md")
		assert.True(t, found)
		assert.Equal(t, "# Identity\nName: Test", val)

		val, found, _ = unstructured.NestedString(cm.Object, "data", "_ws_AGENTS.md")
		assert.True(t, found)
		assert.Equal(t, "# Custom Agents", val)
	})

	t.Run("should encode slashes in path as --", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "docs/README.md", Content: "readme content"},
					},
				},
			},
		}
		err := injectWorkspaceFiles([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		val, found, _ := unstructured.NestedString(cm.Object, "data", "_ws_docs--README.md")
		assert.True(t, found)
		assert.Equal(t, "readme content", val)
	})

	t.Run("should return error for invalid path", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "../../etc/passwd", Content: "bad content"},
					},
				},
			},
		}
		err := injectWorkspaceFiles([]*unstructured.Unstructured{cm}, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not contain ".."`)
	})
}

// --- injectSkillFiles tests ---

func TestInjectSkillFiles(t *testing.T) {
	t.Run("should be no-op when skills is nil", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := injectSkillFiles([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)
	})

	t.Run("should inject _skill_ prefixed keys", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: map[string]string{
					"quote-builder": "# Quote Builder\nUse pricing API...",
					"compliance":    "# Compliance\nFollow policy...",
				},
			},
		}
		err := injectSkillFiles([]*unstructured.Unstructured{cm}, instance)
		require.NoError(t, err)

		val, found, _ := unstructured.NestedString(cm.Object, "data", "_skill_quote-builder")
		assert.True(t, found)
		assert.Equal(t, "# Quote Builder\nUse pricing API...", val)

		val, found, _ = unstructured.NestedString(cm.Object, "data", "_skill_compliance")
		assert.True(t, found)
		assert.Equal(t, "# Compliance\nFollow policy...", val)
	})

	t.Run("should return error for builtin skill name", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Skills: map[string]string{
					"platform": "should fail",
				},
			},
		}
		err := injectSkillFiles([]*unstructured.Unstructured{cm}, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with builtin operator skill")
	})
}

// --- injectSkipBootstrap tests ---

func TestInjectSkipBootstrap(t *testing.T) {
	t.Run("should set skipBootstrap when enabled", func(t *testing.T) {
		config := map[string]any{}
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					SkipBootstrap: true,
				},
			},
		}
		injectSkipBootstrap(config, instance)

		agents, ok := config["agents"].(map[string]any)
		require.True(t, ok)
		defaults, ok := agents["defaults"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, defaults["skipBootstrap"])
	})

	t.Run("should not set skipBootstrap when disabled", func(t *testing.T) {
		config := map[string]any{}
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					SkipBootstrap: false,
				},
			},
		}
		injectSkipBootstrap(config, instance)

		_, ok := config["agents"]
		assert.False(t, ok, "agents key should not be created when skipBootstrap is false")
	})

	t.Run("should not set skipBootstrap when workspace is nil", func(t *testing.T) {
		config := map[string]any{}
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{},
		}
		injectSkipBootstrap(config, instance)

		_, ok := config["agents"]
		assert.False(t, ok, "agents key should not be created when workspace is nil")
	})

	t.Run("should preserve existing config when setting skipBootstrap", func(t *testing.T) {
		config := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"workspace": "~/.openclaw/workspace",
				},
			},
		}
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					SkipBootstrap: true,
				},
			},
		}
		injectSkipBootstrap(config, instance)

		agents := config["agents"].(map[string]any)
		defaults := agents["defaults"].(map[string]any)
		assert.Equal(t, true, defaults["skipBootstrap"])
		assert.Equal(t, "~/.openclaw/workspace", defaults["workspace"])
	})
}

// --- injectConfigMapSourceVolumes tests ---

func makeTestDeployment() *unstructured.Unstructured {
	dep := &unstructured.Unstructured{}
	dep.SetKind(DeploymentKind)
	dep.SetName(getClawDeploymentName(testInstanceName))
	dep.Object["spec"] = map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":         ClawGatewayContainerName,
						"env":          []any{},
						"volumeMounts": []any{},
					},
				},
				"volumes": []any{
					map[string]any{
						"name": "config",
						"configMap": map[string]any{
							"name": getConfigMapName(testInstanceName),
						},
					},
				},
			},
		},
	}
	return dep
}

func TestInjectConfigMapSourceVolumes(t *testing.T) {
	t.Run("should be no-op when no ConfigMap sources", func(t *testing.T) {
		dep := makeTestDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := injectConfigMapSourceVolumes([]*unstructured.Unstructured{dep}, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "volumes")
		assert.Len(t, volumes, 1, "should only have the original config volume")
	})

	t.Run("should add ConfigMap volumes for each source", func(t *testing.T) {
		dep := makeTestDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team-config"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "soul.md", Path: "SOUL.md"},
							},
						},
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "shared-tools"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "tools.md", Path: "TOOLS.md"},
							},
						},
					},
				},
			},
		}
		err := injectConfigMapSourceVolumes([]*unstructured.Unstructured{dep}, instance)
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 3, "should have original + 2 ConfigMap source volumes")

		vol1 := volumes[1].(map[string]any)
		assert.Equal(t, "ws-cm-team-config", vol1["name"])
		cmRef1 := vol1["configMap"].(map[string]any)
		assert.Equal(t, "team-config", cmRef1["name"])

		vol2 := volumes[2].(map[string]any)
		assert.Equal(t, "ws-cm-shared-tools", vol2["name"])
		cmRef2 := vol2["configMap"].(map[string]any)
		assert.Equal(t, "shared-tools", cmRef2["name"])
	})
}

func TestConfigMapSourceVolumeName(t *testing.T) {
	t.Run("should prefix with ws-cm-", func(t *testing.T) {
		assert.Equal(t, "ws-cm-team-config", configMapSourceVolumeName("team-config"))
	})

	t.Run("should replace dots with dashes", func(t *testing.T) {
		assert.Equal(t, "ws-cm-my-app-config", configMapSourceVolumeName("my.app.config"))
	})

	t.Run("should truncate long names with hash suffix to stay within 63 chars", func(t *testing.T) {
		longName := strings.Repeat("a", 200)
		result := configMapSourceVolumeName(longName)
		assert.LessOrEqual(t, len(result), 63)
		assert.True(t, strings.HasPrefix(result, "ws-cm-"))
		assert.Regexp(t, `-[0-9a-f]{8}$`, result, "truncated name should end with hash suffix")
	})

	t.Run("should not end with a dash before hash suffix after truncation", func(t *testing.T) {
		// 48 chars of truncated body (maxLen 63 - prefix 6 - dash 1 - hash 8), place a dash right at the boundary
		name := strings.Repeat("a", 48) + "-" + strings.Repeat("b", 20)
		result := configMapSourceVolumeName(name)
		assert.LessOrEqual(t, len(result), 63)
		assert.Regexp(t, `[a-z0-9]-[0-9a-f]{8}$`, result, "no trailing dash before hash suffix")
	})

	t.Run("should produce different names for long names sharing a prefix", func(t *testing.T) {
		a := configMapSourceVolumeName(strings.Repeat("a", 100) + "-alpha")
		b := configMapSourceVolumeName(strings.Repeat("a", 100) + "-beta")
		assert.NotEqual(t, a, b, "different ConfigMap names must produce different volume names")
	})
}

func TestValidateConfigMapSources(t *testing.T) {
	t.Run("should be no-op when workspace is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{},
		}
		err := validateConfigMapSources(context.Background(), k8sClient, instance)
		assert.NoError(t, err)
	})

	t.Run("should fail when referenced ConfigMap does not exist", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "nonexistent-cm"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "soul.md", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		err := validateConfigMapSources(context.Background(), k8sClient, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `ConfigMap "nonexistent-cm" not found`)
	})

	t.Run("should fail when key is missing from ConfigMap", func(t *testing.T) {
		ctx := context.Background()
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm-missing-key", Namespace: namespace},
			Data:       map[string]string{"existing-key": "value"},
		}
		require.NoError(t, k8sClient.Create(ctx, cm))
		t.Cleanup(func() {
			_ = k8sClient.Delete(ctx, cm)
		})

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "test-cm-missing-key"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "nonexistent-key", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		err := validateConfigMapSources(ctx, k8sClient, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `key "nonexistent-key" not found`)
	})

	t.Run("should pass when ConfigMap and keys exist", func(t *testing.T) {
		ctx := context.Background()
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm-valid", Namespace: namespace},
			Data:       map[string]string{"soul.md": "soul content"},
		}
		require.NoError(t, k8sClient.Create(ctx, cm))
		t.Cleanup(func() {
			_ = k8sClient.Delete(ctx, cm)
		})

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "test-cm-valid"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "soul.md", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		err := validateConfigMapSources(ctx, k8sClient, instance)
		assert.NoError(t, err)
	})
}

// --- shellQuote tests ---

func TestShellQuote(t *testing.T) {
	t.Run("should wrap simple string in single quotes", func(t *testing.T) {
		assert.Equal(t, "'hello'", shellQuote("hello"))
	})

	t.Run("should escape embedded single quotes", func(t *testing.T) {
		assert.Equal(t, "'it'\\''s'", shellQuote("it's"))
	})

	t.Run("should handle multiple single quotes", func(t *testing.T) {
		assert.Equal(t, "'a'\\''b'\\''c'", shellQuote("a'b'c"))
	})

	t.Run("should handle empty string", func(t *testing.T) {
		assert.Equal(t, "''", shellQuote(""))
	})

	t.Run("should preserve double quotes and special chars", func(t *testing.T) {
		assert.Equal(t, "'hello \"world\" $HOME'", shellQuote(`hello "world" $HOME`))
	})
}

// --- isCommitSHA tests ---

func TestIsCommitSHA(t *testing.T) {
	assert.True(t, isCommitSHA("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"))
	assert.True(t, isCommitSHA("0000000000000000000000000000000000000000"))
	assert.False(t, isCommitSHA("main"))
	assert.False(t, isCommitSHA("v2.0"))
	assert.False(t, isCommitSHA("a1b2c3d"))                                          // too short
	assert.False(t, isCommitSHA("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2xx"))       // too long
	assert.False(t, isCommitSHA("g1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"))         // non-hex char
	assert.False(t, isCommitSHA("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2deadbeef")) // 48 chars
}

// --- generateGitSyncScript tests ---

func TestGenerateGitSyncScript(t *testing.T) {
	t.Run("should generate clone for public repo", func(t *testing.T) {
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL: "https://github.com/team/config.git",
				Ref: "main",
				Items: []clawv1alpha1.GitItem{
					{RepoPath: "SOUL.md", Path: "SOUL.md"},
				},
			},
		})
		assert.Contains(t, script, "set -e")
		assert.Contains(t, script, "combined-ca.crt")
		assert.Contains(t, script, "export GIT_SSL_CAINFO=/tmp/combined-ca.crt")
		assert.Contains(t, script, "CLONE_URL='https://github.com/team/config.git'")
		assert.Contains(t, script, "git clone --depth 1 --branch 'main'")
		assert.Contains(t, script, "/git-sources/0")
		assert.NotContains(t, script, "GIT_TOKEN")
		assert.NotContains(t, script, "ASKPASS")
	})

	t.Run("should use GIT_ASKPASS for private repo", func(t *testing.T) {
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL: "https://git.corp.com/team/config.git",
				Ref: "v2.0",
				SecretRef: &clawv1alpha1.SecretRefEntry{
					Name: "git-creds",
					Key:  "token",
				},
				Items: []clawv1alpha1.GitItem{
					{RepoPath: "SOUL.md", Path: "SOUL.md"},
				},
			},
		})
		assert.Contains(t, script, "ASKPASS_0=$(mktemp)")
		assert.Contains(t, script, "GIT_TOKEN_0")
		assert.Contains(t, script, "chmod +x")
		assert.Contains(t, script, "CLONE_URL='https://oauth2@git.corp.com/team/config.git'")
		assert.Contains(t, script, "GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=\"$ASKPASS_0\"")
		assert.Contains(t, script, "--branch 'v2.0'")
		assert.Contains(t, script, "rm -f \"$ASKPASS_0\"")
		assert.NotContains(t, script, "sed", "token must not be injected into clone URL via sed")
		assert.NotContains(t, script, "oauth2:${TOKEN}@", "token must not appear in clone URL")
	})

	t.Run("should omit --branch when ref is empty", func(t *testing.T) {
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL:   "https://github.com/team/config.git",
				Items: []clawv1alpha1.GitItem{{RepoPath: "a.md", Path: "a.md"}},
			},
		})
		assert.Contains(t, script, "git clone --depth 1 \"${CLONE_URL}\"")
		assert.NotContains(t, script, "--branch")
	})

	t.Run("should use fetch for commit SHA ref", func(t *testing.T) {
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL: "https://github.com/team/config.git",
				Ref: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				Items: []clawv1alpha1.GitItem{
					{RepoPath: "SOUL.md", Path: "SOUL.md"},
				},
			},
		})
		assert.Contains(t, script, "git init '/git-sources/0'")
		assert.Contains(t, script, "git -C '/git-sources/0' fetch --depth 1")
		assert.Contains(t, script, "'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2'")
		assert.Contains(t, script, "git -C '/git-sources/0' checkout FETCH_HEAD")
		assert.NotContains(t, script, "--branch")
	})

	t.Run("should use fetch for commit SHA ref with private repo", func(t *testing.T) {
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL: "https://git.corp.com/team/config.git",
				Ref: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
				SecretRef: &clawv1alpha1.SecretRefEntry{
					Name: "git-creds",
					Key:  "token",
				},
				Items: []clawv1alpha1.GitItem{
					{RepoPath: "SOUL.md", Path: "SOUL.md"},
				},
			},
		})
		assert.Contains(t, script, "ASKPASS_0=$(mktemp)")
		assert.Contains(t, script, "git init '/git-sources/0'")
		assert.Contains(t, script, "GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=\"$ASKPASS_0\" git -C '/git-sources/0' fetch --depth 1")
		assert.Contains(t, script, "checkout FETCH_HEAD")
		assert.Contains(t, script, "rm -f \"$ASKPASS_0\"")
		assert.NotContains(t, script, "--branch")
	})

	t.Run("should shell-escape single quotes in URL and ref", func(t *testing.T) {
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL: "https://example.com/it's-a-repo.git",
				Ref: "it's-a-tag",
				Items: []clawv1alpha1.GitItem{
					{RepoPath: "a.md", Path: "a.md"},
				},
			},
		})
		assert.Contains(t, script, "'https://example.com/it'\\''s-a-repo.git'")
		assert.Contains(t, script, "'it'\\''s-a-tag'")
	})
}

func TestInjectGitSyncInitContainer(t *testing.T) {
	makeDeploymentWithInitContainers := func() *unstructured.Unstructured {
		dep := makeTestDeployment()
		dep.Object["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["initContainers"] = []any{
			map[string]any{"name": "init-volume"},
			map[string]any{"name": "init-config"},
			map[string]any{"name": "wait-for-proxy"},
		}
		return dep
	}

	t.Run("should be no-op when no git sources", func(t *testing.T) {
		dep := makeDeploymentWithInitContainers()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := injectGitSyncInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultGitSyncImage, "http://proxy:8080")
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		assert.Len(t, initContainers, 3, "should not add init-git-sync")
	})

	t.Run("should insert init-git-sync after wait-for-proxy", func(t *testing.T) {
		dep := makeDeploymentWithInitContainers()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					GitSources: []clawv1alpha1.GitSource{
						{
							URL: "https://github.com/team/config.git",
							Ref: "main",
							Items: []clawv1alpha1.GitItem{
								{RepoPath: "SOUL.md", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		err := injectGitSyncInitContainer([]*unstructured.Unstructured{dep}, instance, "alpine/git:2.47", "http://proxy:8080")
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		require.Len(t, initContainers, 4)

		names := make([]string, len(initContainers))
		for i, c := range initContainers {
			cm := c.(map[string]any)
			names[i], _, _ = unstructured.NestedString(cm, "name")
		}
		assert.Equal(t, []string{"init-volume", "init-config", "wait-for-proxy", ClawGitSyncContainerName}, names)

		gitSync := initContainers[3].(map[string]any)
		image, _, _ := unstructured.NestedString(gitSync, "image")
		assert.Equal(t, "alpine/git:2.47", image)
	})

	t.Run("should add emptyDir volumes for each git source", func(t *testing.T) {
		dep := makeDeploymentWithInitContainers()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					GitSources: []clawv1alpha1.GitSource{
						{
							URL:   "https://example.com/repo1.git",
							Items: []clawv1alpha1.GitItem{{RepoPath: "a.md", Path: "a.md"}},
						},
						{
							URL:   "https://example.com/repo2.git",
							Items: []clawv1alpha1.GitItem{{RepoPath: "b.md", Path: "b.md"}},
						},
					},
				},
			},
		}
		err := injectGitSyncInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultGitSyncImage, "http://proxy:8080")
		require.NoError(t, err)

		volumes, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "volumes")
		// Original config volume + 2 git emptyDirs
		require.Len(t, volumes, 3)
		assert.Equal(t, "ws-git-0", volumes[1].(map[string]any)["name"])
		assert.Equal(t, "ws-git-1", volumes[2].(map[string]any)["name"])
	})

	t.Run("should set proxy env vars and CA cert mount", func(t *testing.T) {
		dep := makeDeploymentWithInitContainers()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					GitSources: []clawv1alpha1.GitSource{
						{
							URL:   "https://example.com/repo.git",
							Items: []clawv1alpha1.GitItem{{RepoPath: "a.md", Path: "a.md"}},
						},
					},
				},
			},
		}
		err := injectGitSyncInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultGitSyncImage, "http://test-proxy:8080")
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		gitSync := initContainers[3].(map[string]any)

		envVars, _, _ := unstructured.NestedSlice(gitSync, "env")
		envMap := map[string]string{}
		for _, e := range envVars {
			em := e.(map[string]any)
			if v, ok := em["value"].(string); ok {
				envMap[em["name"].(string)] = v
			}
		}
		assert.Equal(t, "http://test-proxy:8080", envMap["HTTP_PROXY"])
		assert.Equal(t, "http://test-proxy:8080", envMap["HTTPS_PROXY"])

		vMounts, _, _ := unstructured.NestedSlice(gitSync, "volumeMounts")
		mountNames := map[string]bool{}
		for _, vm := range vMounts {
			vmm := vm.(map[string]any)
			mountNames[vmm["name"].(string)] = true
		}
		assert.True(t, mountNames["proxy-ca"], "should mount proxy CA cert")
	})

	t.Run("should inject secretKeyRef for private repo tokens", func(t *testing.T) {
		dep := makeDeploymentWithInitContainers()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					GitSources: []clawv1alpha1.GitSource{
						{
							URL: "https://git.corp.com/team/config.git",
							SecretRef: &clawv1alpha1.SecretRefEntry{
								Name: "git-creds",
								Key:  "token",
							},
							Items: []clawv1alpha1.GitItem{{RepoPath: "a.md", Path: "a.md"}},
						},
					},
				},
			},
		}
		err := injectGitSyncInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultGitSyncImage, "http://proxy:8080")
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		gitSync := initContainers[3].(map[string]any)

		envVars, _, _ := unstructured.NestedSlice(gitSync, "env")
		var tokenEnv map[string]any
		for _, e := range envVars {
			em := e.(map[string]any)
			if em["name"] == "GIT_TOKEN_0" {
				tokenEnv = em
				break
			}
		}
		require.NotNil(t, tokenEnv, "GIT_TOKEN_0 env var should exist")

		valueFrom := tokenEnv["valueFrom"].(map[string]any)
		secretRef := valueFrom["secretKeyRef"].(map[string]any)
		assert.Equal(t, "git-creds", secretRef["name"])
		assert.Equal(t, "token", secretRef["key"])
	})
}

// --- injectSeedInitContainer tests ---

func TestInjectSeedInitContainer(t *testing.T) {
	makeDeploymentWithWaitProxy := func() *unstructured.Unstructured {
		dep := makeTestDeployment()
		dep.Object["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["initContainers"] = []any{
			map[string]any{"name": "init-volume"},
			map[string]any{"name": "init-config"},
			map[string]any{"name": "wait-for-proxy"},
		}
		return dep
	}

	t.Run("should still inject for builtins even with no user sources", func(t *testing.T) {
		dep := makeDeploymentWithWaitProxy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec:       clawv1alpha1.ClawSpec{},
		}
		err := injectSeedInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultOpenClawImage)
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		assert.Len(t, initContainers, 4, "should inject init-seed for builtin files")
	})

	t.Run("should insert after wait-for-proxy when no git sources", func(t *testing.T) {
		dep := makeDeploymentWithWaitProxy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "soul"},
					},
				},
			},
		}
		err := injectSeedInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultOpenClawImage)
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		require.Len(t, initContainers, 4)

		names := make([]string, len(initContainers))
		for i, c := range initContainers {
			cm := c.(map[string]any)
			names[i], _, _ = unstructured.NestedString(cm, "name")
		}
		assert.Equal(t, []string{"init-volume", "init-config", "wait-for-proxy", ClawSeedContainerName}, names)
	})

	t.Run("should insert after init-git-sync when present", func(t *testing.T) {
		dep := makeDeploymentWithWaitProxy()
		// Simulate init-git-sync already injected
		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		initContainers = append(initContainers, map[string]any{"name": ClawGitSyncContainerName})
		_ = unstructured.SetNestedSlice(dep.Object, initContainers, "spec", "template", "spec", "initContainers")

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "soul"},
					},
				},
			},
		}
		err := injectSeedInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultOpenClawImage)
		require.NoError(t, err)

		initContainers, _, _ = unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		require.Len(t, initContainers, 5)

		names := make([]string, len(initContainers))
		for i, c := range initContainers {
			cm := c.(map[string]any)
			names[i], _, _ = unstructured.NestedString(cm, "name")
		}
		assert.Equal(t, []string{"init-volume", "init-config", "wait-for-proxy", ClawGitSyncContainerName, ClawSeedContainerName}, names)
	})

	t.Run("should mount config, PVC, configMap sources, and git volumes", func(t *testing.T) {
		dep := makeDeploymentWithWaitProxy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "soul"},
					},
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team"},
							Items:        []clawv1alpha1.ConfigMapItem{{Key: "k", Path: "p"}},
						},
					},
					GitSources: []clawv1alpha1.GitSource{
						{
							URL:   "https://example.com/repo.git",
							Items: []clawv1alpha1.GitItem{{RepoPath: "a", Path: "a"}},
						},
					},
				},
			},
		}
		err := injectSeedInitContainer([]*unstructured.Unstructured{dep}, instance, DefaultOpenClawImage)
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		seedContainer := initContainers[3].(map[string]any)

		vMounts, _, _ := unstructured.NestedSlice(seedContainer, "volumeMounts")
		mountNames := map[string]bool{}
		for _, vm := range vMounts {
			vmm := vm.(map[string]any)
			mountNames[vmm["name"].(string)] = true
		}

		assert.True(t, mountNames["config"], "should mount config volume")
		assert.True(t, mountNames["claw-home"], "should mount PVC")
		assert.True(t, mountNames["ws-cm-team"], "should mount configMap source")
		assert.True(t, mountNames["ws-git-0"], "should mount git emptyDir")
	})

	t.Run("should use gateway image", func(t *testing.T) {
		dep := makeDeploymentWithWaitProxy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "soul"},
					},
				},
			},
		}
		err := injectSeedInitContainer([]*unstructured.Unstructured{dep}, instance, "custom-image:latest")
		require.NoError(t, err)

		initContainers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "initContainers")
		seedContainer := initContainers[3].(map[string]any)
		image, _, _ := unstructured.NestedString(seedContainer, "image")
		assert.Equal(t, "custom-image:latest", image)
	})
}

// --- Integration tests ---

func TestWorkspaceIntegration(t *testing.T) {
	t.Run("should inject workspace and skill keys into ConfigMap after reconcile", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					SkipBootstrap: true,
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "IDENTITY.md", Content: "# Identity\nName: Test User"},
					},
				},
				Skills: map[string]string{
					"quote-builder": "# Quote Builder\nBuild quotes...",
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		var cm corev1.ConfigMap
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, &cm))

		assert.Equal(t, "# Identity\nName: Test User", cm.Data["_ws_IDENTITY.md"],
			"workspace file should be present in ConfigMap")
		assert.Equal(t, "# Quote Builder\nBuild quotes...", cm.Data["_skill_quote-builder"],
			"skill file should be present in ConfigMap")

		// Verify skipBootstrap is in operator.json
		assert.Contains(t, cm.Data["operator.json"], "skipBootstrap")
	})

	t.Run("should inject skipBootstrap without workspace files or skills", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					SkipBootstrap: true,
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		var cm corev1.ConfigMap
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, &cm))

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		agents, ok := config["agents"].(map[string]any)
		require.True(t, ok, "operator.json should have agents key")
		defaults, ok := agents["defaults"].(map[string]any)
		require.True(t, ok, "agents should have defaults key")
		assert.Equal(t, true, defaults["skipBootstrap"],
			"skipBootstrap should be true in operator.json")

		for k := range cm.Data {
			assert.NotContains(t, k, "_ws_", "no workspace keys should be present")
			assert.NotContains(t, k, "_skill_", "no skill keys should be present")
		}
	})

	t.Run("should fail reconcile with invalid workspace path", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "../../etc/passwd", Content: "bad content"},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      testInstanceName,
				Namespace: namespace,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `must not contain ".."`)
	})

	t.Run("should fail reconcile with invalid skill name", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Skills: map[string]string{
					"platform": "should not be allowed",
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      testInstanceName,
				Namespace: namespace,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicts with builtin operator skill")
	})

	t.Run("should inject seed manifest and _ws_ keys for inlineSources after reconcile", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "# Custom Soul", Mode: clawv1alpha1.SeedModeOverwrite},
						{Path: "docs/guide.md", Content: "# Guide"},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		var cm corev1.ConfigMap
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, &cm))

		assert.Equal(t, "# Custom Soul", cm.Data["_ws_SOUL.md"])
		assert.Equal(t, "# Guide", cm.Data["_ws_docs--guide.md"])

		var manifest []seedManifestEntry
		require.NoError(t, json.Unmarshal([]byte(cm.Data[seedManifestKey]), &manifest))
		assert.True(t, len(manifest) >= 5, "manifest should have 3 builtins + 2 inline sources")

		byTarget := map[string]seedManifestEntry{}
		for _, e := range manifest {
			byTarget[e.Target] = e
		}
		assert.Equal(t, "overwrite", byTarget["SOUL.md"].Mode)
		assert.Equal(t, "overwrite", byTarget["docs/guide.md"].Mode)
		assert.Equal(t, "seedIfMissing", byTarget["AGENTS.md"].Mode, "builtin should be seedIfMissing")
	})

	t.Run("should add ConfigMap volumes for configMapSources after reconcile", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		wsCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "team-config-int", Namespace: namespace},
			Data:       map[string]string{"soul.md": "# Team Soul"},
		}
		require.NoError(t, k8sClient.Create(ctx, wsCM))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "team-config-int"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "soul.md", Path: "custom-soul.md"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))
		t.Cleanup(func() {
			_ = k8sClient.Delete(ctx, wsCM)
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		var cm corev1.ConfigMap
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, &cm))

		var manifest []seedManifestEntry
		require.NoError(t, json.Unmarshal([]byte(cm.Data[seedManifestKey]), &manifest))
		byTarget := map[string]seedManifestEntry{}
		for _, e := range manifest {
			byTarget[e.Target] = e
		}
		require.Contains(t, byTarget, "custom-soul.md")
		assert.Equal(t, "/configmap-sources/ws-cm-team-config-int/soul.md", byTarget["custom-soul.md"].Source)
	})

	t.Run("should add git sync init container and emptyDir volumes for gitSources after reconcile", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					GitSources: []clawv1alpha1.GitSource{
						{
							URL: "https://git.example.com/team/config.git",
							Ref: "main",
							Items: []clawv1alpha1.GitItem{
								{RepoPath: "configs/SOUL.md", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		var cm corev1.ConfigMap
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, &cm))

		var manifest []seedManifestEntry
		require.NoError(t, json.Unmarshal([]byte(cm.Data[seedManifestKey]), &manifest))
		byTarget := map[string]seedManifestEntry{}
		for _, e := range manifest {
			byTarget[e.Target] = e
		}
		require.Contains(t, byTarget, "SOUL.md")
		assert.Equal(t, "/git-sources/0/configs/SOUL.md", byTarget["SOUL.md"].Source)
	})

	t.Run("should normalize deprecated files to inline sources after reconcile", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					Files: map[string]string{ //nolint:staticcheck // testing deprecated field
						"IDENTITY.md": "# Identity\nDeprecated field test",
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		var cm corev1.ConfigMap
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, &cm))

		assert.Equal(t, "# Identity\nDeprecated field test", cm.Data["_ws_IDENTITY.md"],
			"deprecated files should be normalized and injected as _ws_ keys")

		var manifest []seedManifestEntry
		require.NoError(t, json.Unmarshal([]byte(cm.Data[seedManifestKey]), &manifest))
		byTarget := map[string]seedManifestEntry{}
		for _, e := range manifest {
			byTarget[e.Target] = e
		}
		require.Contains(t, byTarget, "IDENTITY.md")
		assert.Equal(t, "seedIfMissing", byTarget["IDENTITY.md"].Mode,
			"migrated files should use seedIfMissing mode")
	})

	t.Run("should fail reconcile with path conflict across source types", func(t *testing.T) {
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		wsCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "conflict-cm", Namespace: namespace},
			Data:       map[string]string{"soul": "from configmap"},
		}
		require.NoError(t, k8sClient.Create(ctx, wsCM))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Workspace: &clawv1alpha1.WorkspaceSpec{
					InlineSources: []clawv1alpha1.InlineSource{
						{Path: "SOUL.md", Content: "inline soul"},
					},
					ConfigMapSources: []clawv1alpha1.ConfigMapSource{
						{
							ConfigMapRef: clawv1alpha1.ConfigMapRef{Name: "conflict-cm"},
							Items: []clawv1alpha1.ConfigMapItem{
								{Key: "soul", Path: "SOUL.md"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))
		t.Cleanup(func() {
			_ = k8sClient.Delete(ctx, wsCM)
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      testInstanceName,
				Namespace: namespace,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SOUL.md")
		assert.Contains(t, err.Error(), "inline source")
	})
}
