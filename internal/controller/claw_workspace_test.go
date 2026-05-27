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

	t.Run("should inject _ws_ prefixed keys for workspace files", func(t *testing.T) {
		cm := makeTestConfigMap()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Workspace: &clawv1alpha1.WorkspaceSpec{
					Files: map[string]string{
						"IDENTITY.md": "# Identity\nName: Test",
						"AGENTS.md":   "# Custom Agents",
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
					Files: map[string]string{
						"docs/README.md": "readme content",
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
					Files: map[string]string{
						"../../etc/passwd": "bad content",
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
					Files: map[string]string{
						"IDENTITY.md": "# Identity\nName: Test User",
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
					Files: map[string]string{
						"../../etc/passwd": "bad content",
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
}
