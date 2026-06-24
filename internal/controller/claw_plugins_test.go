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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const testGatewayImage = "ghcr.io/openclaw/openclaw:2026.6.10"

func makeTestDeploymentForPlugins() []*unstructured.Unstructured {
	dep := &unstructured.Unstructured{}
	dep.SetKind(DeploymentKind)
	dep.SetName(getClawDeploymentName(testInstanceName))
	dep.Object["spec"] = map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":         ClawGatewayContainerName,
						"image":        testGatewayImage,
						"env":          []any{},
						"volumeMounts": []any{},
					},
				},
				"initContainers": []any{
					map[string]any{"name": "init-volume"},
					map[string]any{"name": "init-config"},
					map[string]any{"name": "wait-for-proxy"},
				},
				"volumes": []any{
					map[string]any{"name": "claw-home"},
					map[string]any{"name": "proxy-ca"},
					map[string]any{"name": "tmp-volume"},
				},
			},
		},
	}
	return []*unstructured.Unstructured{dep}
}

func testClawWithPlugins(plugins []string) *clawv1alpha1.Claw {
	return &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		Spec: clawv1alpha1.ClawSpec{
			Plugins: plugins,
		},
	}
}

// --- pluginsEnabled tests ---

func TestPluginsEnabled(t *testing.T) {
	t.Run("should return true when plugins are specified", func(t *testing.T) {
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})
		assert.True(t, pluginsEnabled(instance))
	})

	t.Run("should return false when plugins are empty", func(t *testing.T) {
		instance := testClawWithPlugins(nil)
		assert.False(t, pluginsEnabled(instance))
	})

	t.Run("should return false when plugins are zero-length slice", func(t *testing.T) {
		instance := testClawWithPlugins([]string{})
		assert.False(t, pluginsEnabled(instance))
	})
}

// --- generatePluginInstallScript tests ---

func TestGeneratePluginInstallScript(t *testing.T) {
	t.Run("should contain manifest cleanup phase", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, `MANIFEST="$EXT/.operator-managed"`)
		assert.Contains(t, script, `if [ -f "$MANIFEST" ]; then`)
		assert.Contains(t, script, `rm -rf -- "$target"`)
		assert.Contains(t, script, `rm -f "$MANIFEST"`)
	})

	t.Run("should contain pre-install snapshot phase", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, `mkdir -p "$EXT"`)
		assert.Contains(t, script, `ls "$EXT" 2>/dev/null | sort > /tmp/before-plugins.txt`)
	})

	t.Run("should contain diff-record phase", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, `ls "$EXT" | sort | comm -13 /tmp/before-plugins.txt - > "$MANIFEST"`)
	})

	t.Run("should generate install command for single plugin", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/matrix'")
	})

	t.Run("should generate install commands for multiple plugins", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix", "@openclaw/diagnostics-otel"})
		assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/matrix'")
		assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/diagnostics-otel'")
	})

	t.Run("should escape single quotes in plugin names", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"foo'bar"})
		assert.Contains(t, script, "openclaw plugins install clawhub:'foo'\\''bar'")
	})

	t.Run("should escape shell metacharacters", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"foo; rm -rf /"})
		assert.Contains(t, script, "'foo; rm -rf /'")
	})

	t.Run("should not contain blanket rm -rf extensions", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.NotContains(t, script, "rm -rf /home/node/.openclaw/extensions")
	})

	t.Run("should clean all extension dirs when no manifest exists", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, "else")
		assert.Contains(t, script, `find "$EXT" -mindepth 1 -maxdepth 1 -type d -exec rm -rf {} +`)
	})

	t.Run("should guard against path traversal in manifest entries", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, `""|.|..|*/*|*..*)`)
		assert.Contains(t, script, `[ -e "$target" ] || continue`)
	})

	t.Run("should order phases correctly: cleanup before snapshot before install before record", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		cleanupIdx := strings.Index(script, `rm -rf -- "$target"`)
		snapshotIdx := strings.Index(script, `/tmp/before-plugins.txt`)
		installIdx := strings.Index(script, "openclaw plugins install")
		recordIdx := strings.Index(script, `comm -13`)

		require.Greater(t, cleanupIdx, 0, "cleanup phase should be present")
		require.Greater(t, snapshotIdx, 0, "snapshot phase should be present")
		require.Greater(t, installIdx, 0, "install phase should be present")
		require.Greater(t, recordIdx, 0, "record phase should be present")

		assert.Less(t, cleanupIdx, snapshotIdx, "cleanup should come before snapshot")
		assert.Less(t, snapshotIdx, installIdx, "snapshot should come before install")
		assert.Less(t, installIdx, recordIdx, "install should come before record")
	})

	t.Run("should use consistent extensions path variable across all phases", func(t *testing.T) {
		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		assert.Contains(t, script, `EXT="/home/node/.openclaw/extensions"`)
		assert.Contains(t, script, `target="$EXT/$dir"`)
		assert.Contains(t, script, `mkdir -p "$EXT"`)
		assert.Contains(t, script, `ls "$EXT"`)
	})
}

// --- configurePluginsInitContainer tests ---

func TestConfigurePluginsInitContainer(t *testing.T) {
	t.Run("should add init-plugins container with correct spec", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		require.Len(t, initContainers, 4, "should have 3 existing + 1 new init container")

		pluginInit := initContainers[3].(map[string]any)
		assert.Equal(t, PluginsInitContainerName, pluginInit["name"])
		assert.Equal(t, testGatewayImage, pluginInit["image"])

		command := pluginInit["command"].([]any)
		assert.Equal(t, "sh", command[0])
		assert.Equal(t, "-c", command[1])
		assert.Contains(t, command[2], "openclaw plugins install clawhub:'@openclaw/matrix'")
	})

	t.Run("should set proxy environment variables", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		envVars := pluginInit["env"].([]any)

		envMap := make(map[string]string)
		for _, e := range envVars {
			entry := e.(map[string]any)
			envMap[entry["name"].(string)] = entry["value"].(string)
		}

		expectedProxy := "http://" + testInstanceName + "-proxy:8080"
		assert.Equal(t, "/home/node", envMap["HOME"])
		assert.Equal(t, "/home/node/.cache/npm", envMap["NPM_CONFIG_CACHE"])
		assert.Equal(t, expectedProxy, envMap["HTTP_PROXY"])
		assert.Equal(t, expectedProxy, envMap["HTTPS_PROXY"])
		assert.Equal(t, "localhost,127.0.0.1", envMap["NO_PROXY"])
		assert.Equal(t, "/etc/proxy-ca/ca.crt", envMap["NODE_EXTRA_CA_CERTS"])
	})

	t.Run("should include .svc in NO_PROXY when inClusterBypass is true", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})
		instance.Spec.Network = &clawv1alpha1.NetworkSpec{InClusterBypass: ptr.To(true)}

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		envVars := pluginInit["env"].([]any)
		envMap := make(map[string]string)
		for _, e := range envVars {
			entry := e.(map[string]any)
			envMap[entry["name"].(string)] = entry["value"].(string)
		}
		assert.Equal(t, "localhost,127.0.0.1,.svc,.svc.cluster.local", envMap["NO_PROXY"])
	})

	t.Run("should mount PVC subpaths, proxy-ca, and tmp-volume", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		volumeMounts := pluginInit["volumeMounts"].([]any)
		require.Len(t, volumeMounts, 5)

		mountPaths := make(map[string]string)
		for _, vm := range volumeMounts {
			m := vm.(map[string]any)
			mountPaths[m["mountPath"].(string)] = m["name"].(string)
		}

		assert.Equal(t, "claw-home", mountPaths["/home/node/.openclaw"])
		assert.Equal(t, "claw-home", mountPaths["/home/node/.local"])
		assert.Equal(t, "claw-home", mountPaths["/home/node/.cache"])
		assert.Equal(t, "proxy-ca", mountPaths["/etc/proxy-ca"])
		assert.Equal(t, "tmp-volume", mountPaths["/tmp"])
	})

	t.Run("should set resource requests and limits", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		resources := pluginInit["resources"].(map[string]any)

		requests := resources["requests"].(map[string]any)
		assert.Equal(t, "128Mi", requests["memory"])
		assert.Equal(t, "100m", requests["cpu"])

		limits := resources["limits"].(map[string]any)
		assert.Equal(t, "512Mi", limits["memory"])
		assert.Equal(t, "500m", limits["cpu"])
	})

	t.Run("should set imagePullPolicy to IfNotPresent", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		assert.Equal(t, "IfNotPresent", pluginInit["imagePullPolicy"])
	})

	t.Run("should set security context without readOnlyRootFilesystem", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		secCtx := pluginInit["securityContext"].(map[string]any)
		assert.Equal(t, false, secCtx["allowPrivilegeEscalation"])
		_, hasReadOnly := secCtx["readOnlyRootFilesystem"]
		assert.False(t, hasReadOnly, "should not set readOnlyRootFilesystem")
	})

	t.Run("should generate correct script for multiple plugins", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix", "@openclaw/diagnostics-otel"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		command := pluginInit["command"].([]any)
		script := command[2].(string)
		assert.Contains(t, script, "set -e")
		assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/matrix'")
		assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/diagnostics-otel'")
	})

	t.Run("should include manifest-tracked cleanup in init container script", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		pluginInit := initContainers[3].(map[string]any)
		command := pluginInit["command"].([]any)
		script := command[2].(string)
		assert.Contains(t, script, `.operator-managed`)
		assert.Contains(t, script, `comm -13 /tmp/before-plugins.txt`)
		assert.NotContains(t, script, "rm -rf /home/node/.openclaw/extensions")
	})

	t.Run("should no-op when plugins are empty", func(t *testing.T) {
		objects := makeTestDeploymentForPlugins()
		instance := testClawWithPlugins(nil)

		require.NoError(t, configurePluginsInitContainer(objects, instance, instance.Spec.Plugins))

		initContainers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "initContainers",
		)
		assert.Len(t, initContainers, 3, "should not add init container when no plugins")
	})

	t.Run("should return error when deployment not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		err := configurePluginsInitContainer(objects, instance, instance.Spec.Plugins)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})

	t.Run("should return error when gateway container has no image", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": ClawGatewayContainerName,
						},
					},
				},
			},
		}
		instance := testClawWithPlugins([]string{"@openclaw/matrix"})

		err := configurePluginsInitContainer([]*unstructured.Unstructured{dep}, instance, instance.Spec.Plugins)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gateway container image not found")
	})
}

// --- effectivePlugins and requiredProviderPlugins tests ---

func TestRequiredProviderPlugins(t *testing.T) {
	t.Run("returns vertex plugin for anthropic GCP credential", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "anthropic-vertex",
						Type:     clawv1alpha1.CredentialTypeGCP,
						Provider: "anthropic",
						GCP:      &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"},
					},
				},
			},
		}
		plugins := requiredProviderPlugins(instance)
		require.Len(t, plugins, 1)
		assert.Equal(t, "@openclaw/anthropic-vertex-provider@2026.6.10", plugins[0])
	})

	t.Run("returns empty for google GCP credential", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "vertex",
						Type:     clawv1alpha1.CredentialTypeGCP,
						Provider: "google",
						GCP:      &clawv1alpha1.GCPConfig{Project: "p", Location: "us-central1"},
					},
				},
			},
		}
		assert.Empty(t, requiredProviderPlugins(instance))
	})

	t.Run("returns empty for anthropic apiKey credential", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "claude",
						Type:     clawv1alpha1.CredentialTypeAPIKey,
						Provider: "anthropic",
						Domain:   "api.anthropic.com",
					},
				},
			},
		}
		assert.Empty(t, requiredProviderPlugins(instance))
	})

	t.Run("deduplicates when multiple anthropic vertex credentials exist", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "a1", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
						GCP: &clawv1alpha1.GCPConfig{Project: "p1", Location: "us-east5"}},
					{Name: "a2", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
						GCP: &clawv1alpha1.GCPConfig{Project: "p2", Location: "europe-west1"}},
				},
			},
		}
		plugins := requiredProviderPlugins(instance)
		assert.Len(t, plugins, 1)
	})
}

func TestEffectivePlugins(t *testing.T) {
	t.Run("returns only spec plugins when no implicit plugins needed", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Plugins: []string{"@openclaw/matrix"},
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "g", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google"},
				},
			},
		}
		assert.Equal(t, []string{"@openclaw/matrix"}, effectivePlugins(instance))
	})

	t.Run("merges implicit vertex plugin with spec plugins", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Plugins: []string{"@openclaw/matrix"},
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "vertex", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
						GCP: &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"}},
				},
			},
		}
		plugins := effectivePlugins(instance)
		assert.Contains(t, plugins, "@openclaw/matrix")
		assert.Contains(t, plugins, "@openclaw/anthropic-vertex-provider@2026.6.10")
		assert.Len(t, plugins, 2)
	})

	t.Run("does not duplicate if spec already declares the plugin", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Plugins: []string{"@openclaw/anthropic-vertex-provider"},
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "vertex", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
						GCP: &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"}},
				},
			},
		}
		plugins := effectivePlugins(instance)
		assert.Equal(t, []string{"@openclaw/anthropic-vertex-provider"}, plugins)
	})

	t.Run("returns implicit plugins when spec.plugins is empty", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "vertex", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
						GCP: &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"}},
				},
			},
		}
		plugins := effectivePlugins(instance)
		require.Len(t, plugins, 1)
		assert.Equal(t, "@openclaw/anthropic-vertex-provider@2026.6.10", plugins[0])
	})
}

// --- stampGatewayConfigHash with plugins tests ---

func TestStampGatewayConfigHashWithPlugins(t *testing.T) {
	makeHashObjects := func() []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{
			"operator.json": `{"gateway":{}}`,
		}

		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": ClawGatewayContainerName},
					},
				},
			},
		}
		return []*unstructured.Unstructured{cm, dep}
	}

	getHash := func(objects []*unstructured.Unstructured) string {
		ann, _, _ := unstructured.NestedStringMap(objects[1].Object, "spec", "template", "metadata", "annotations")
		return ann[clawv1alpha1.AnnotationKeyGatewayConfigHash]
	}

	t.Run("should produce different hashes when plugins differ", func(t *testing.T) {
		objects1 := makeHashObjects()
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, []string{"@openclaw/matrix"}))

		objects2 := makeHashObjects()
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, []string{"@openclaw/diagnostics-otel"}))

		assert.NotEqual(t, getHash(objects1), getHash(objects2))
	})

	t.Run("should produce different hash with plugins vs without", func(t *testing.T) {
		objects1 := makeHashObjects()
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, nil))

		objects2 := makeHashObjects()
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, []string{"@openclaw/matrix"}))

		assert.NotEqual(t, getHash(objects1), getHash(objects2))
	})

	t.Run("should produce identical hash regardless of plugin order", func(t *testing.T) {
		objects1 := makeHashObjects()
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, []string{"@openclaw/matrix", "@openclaw/diagnostics-otel"}))

		objects2 := makeHashObjects()
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, []string{"@openclaw/diagnostics-otel", "@openclaw/matrix"}))

		assert.Equal(t, getHash(objects1), getHash(objects2),
			"plugin order should not affect hash")
	})
}

// --- Integration tests ---

func TestPluginsIntegration(t *testing.T) {
	t.Run("should add init-plugins container when plugins specified", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Plugins = []string{"@openclaw/matrix"}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		var found bool
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			if ic.Name == PluginsInitContainerName {
				found = true
				assert.Contains(t, ic.Command[2], "openclaw plugins install clawhub:'@openclaw/matrix'")

				envMap := make(map[string]string)
				for _, e := range ic.Env {
					envMap[e.Name] = e.Value
				}
				assert.Contains(t, envMap["HTTP_PROXY"], "-proxy:8080")
				assert.Contains(t, envMap["HTTPS_PROXY"], "-proxy:8080")
				assert.Equal(t, "/etc/proxy-ca/ca.crt", envMap["NODE_EXTRA_CA_CERTS"])

				mountPaths := make(map[string]string)
				for _, vm := range ic.VolumeMounts {
					mountPaths[vm.MountPath] = vm.Name
				}
				assert.Equal(t, "claw-home", mountPaths["/home/node/.openclaw"])
				assert.Equal(t, "proxy-ca", mountPaths["/etc/proxy-ca"])
				assert.Equal(t, "tmp-volume", mountPaths["/tmp"])
				break
			}
		}
		assert.True(t, found, "Deployment should have init-plugins container")
	})

	t.Run("should not add init-plugins container when no plugins", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			assert.NotEqual(t, PluginsInitContainerName, ic.Name,
				"Deployment should not have init-plugins container when no plugins specified")
		}
	})

	t.Run("should install multiple plugins", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Plugins = []string{"@openclaw/matrix", "@openclaw/diagnostics-otel"}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			if ic.Name == PluginsInitContainerName {
				script := ic.Command[2]
				assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/matrix'")
				assert.Contains(t, script, "openclaw plugins install clawhub:'@openclaw/diagnostics-otel'")
				return
			}
		}
		t.Fatal("init-plugins container not found")
	})

	t.Run("should coexist with metrics sidecar", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Plugins = []string{"@openclaw/diagnostics-otel"}
		smDisabled := false
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{
			Enabled:        true,
			ServiceMonitor: &clawv1alpha1.ServiceMonitorSpec{Enabled: &smDisabled},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		var hasPluginsInit, hasOtelSidecar bool
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			if ic.Name == PluginsInitContainerName {
				hasPluginsInit = true
			}
		}
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == OTelCollectorContainerName {
				hasOtelSidecar = true
			}
		}
		assert.True(t, hasPluginsInit, "should have init-plugins container")
		assert.True(t, hasOtelSidecar, "should have otel-collector sidecar")
	})

	t.Run("should change config hash when plugins change", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Plugins = []string{"@openclaw/matrix"}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		hash1 := deployment.Spec.Template.Annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		require.NotEmpty(t, hash1)

		// Update plugins
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		instance.Spec.Plugins = []string{"@openclaw/matrix", "@openclaw/diagnostics-otel"}
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getClawDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))

		hash2 := deployment.Spec.Template.Annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		require.NotEmpty(t, hash2)
		assert.NotEqual(t, hash1, hash2, "config hash should change when plugins change")
	})
}
