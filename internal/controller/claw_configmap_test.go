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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Provider injection Vertex SDK tests ---

func TestInjectProvidersVertexSDK(t *testing.T) {
	makeConfigMap := func(jsonContent string) []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{
			"operator.json": jsonContent,
		}
		return []*unstructured.Unstructured{cm}
	}

	getConfig := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		return config
	}

	getProviders := func(t *testing.T, config map[string]any) map[string]any {
		t.Helper()
		models := config["models"].(map[string]any)
		return models["providers"].(map[string]any)
	}

	t.Run("should map GCP anthropic to anthropic-vertex provider key", func(t *testing.T) {
		objects := makeConfigMap(`{"models":{"providers":{}}}`)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		config := getConfig(t, objects)
		providers := getProviders(t, config)

		require.Contains(t, providers, "anthropic-vertex")

		av := providers["anthropic-vertex"].(map[string]any)
		assert.Equal(t, "https://us-east5-aiplatform.googleapis.com", av["baseUrl"])
		assert.Equal(t, "gcp-vertex-credentials", av["apiKey"])
		assert.Equal(t, "anthropic-messages", av["api"])
		assert.Equal(t, float64(128000), av["maxTokens"])
	})

	t.Run("should use plain hostname for global location", func(t *testing.T) {
		objects := makeConfigMap(`{"models":{"providers":{}}}`)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "global",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		config := getConfig(t, objects)
		providers := getProviders(t, config)
		av := providers["anthropic-vertex"].(map[string]any)
		assert.Equal(t, "https://aiplatform.googleapis.com", av["baseUrl"])
	})

	t.Run("should set maxTokens and no api for non-anthropic vertex provider", func(t *testing.T) {
		objects := makeConfigMap(`{"models":{"providers":{}}}`)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "meta-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "meta",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-central1",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		config := getConfig(t, objects)
		providers := getProviders(t, config)

		require.Contains(t, providers, "meta-vertex")
		mv := providers["meta-vertex"].(map[string]any)
		assert.Equal(t, "https://us-central1-aiplatform.googleapis.com", mv["baseUrl"])
		assert.Equal(t, "gcp-vertex-credentials", mv["apiKey"])
		assert.Equal(t, float64(128000), mv["maxTokens"])
		assert.NotContains(t, mv, "api", "meta has no api mapping in vertexProviderAPIMapping")
	})

	t.Run("should reject duplicate vertex providers", func(t *testing.T) {
		objects := makeConfigMap(`{"models":{"providers":{}}}`)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "claude-vertex-1", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
				Domain: ".googleapis.com", GCP: &clawv1alpha1.GCPConfig{Project: "p1", Location: "us-east5"},
			},
			{
				Name: "claude-vertex-2", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
				Domain: ".googleapis.com", GCP: &clawv1alpha1.GCPConfig{Project: "p2", Location: "us-east5"},
			},
		}

		err := injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "anthropic-vertex")
	})
}

// --- Provider injection into ConfigMap tests ---

func TestInjectProvidersIntoConfigMap(t *testing.T) {
	makeConfigMap := func(jsonContent string) []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{
			"operator.json": jsonContent,
		}
		return []*unstructured.Unstructured{cm}
	}

	baseJSON := `{"models":{"providers":{}},"gateway":{"port":18789}}`

	getProviders := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		models := config["models"].(map[string]any)
		return models["providers"].(map[string]any)
	}

	t.Run("should inject google provider with correct baseUrl", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta", google["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", google["apiKey"])
	})

	t.Run("should inject multiple providers", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
			{
				Name:     "claude",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "anthropic",
				Domain:   "api.anthropic.com",
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		assert.Contains(t, providers, "google")
		assert.Contains(t, providers, "anthropic")
		anthropic := providers["anthropic"].(map[string]any)
		assert.Equal(t, "https://api.anthropic.com", anthropic["baseUrl"])
	})

	t.Run("should leave providers empty when no provider is set", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:   "telegram",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.telegram.org",
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		assert.Empty(t, providers)
	})

	t.Run("should use Vertex AI upstream for google gcp credential", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-proj",
					Location: "europe-west1",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "https://europe-west1-aiplatform.googleapis.com/v1/projects/my-proj/locations/europe-west1/publishers/google", google["baseUrl"])
	})

	t.Run("should preserve other config sections", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(nil)))

		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		gateway := config["gateway"].(map[string]any)
		assert.Equal(t, float64(18789), gateway["port"])
	})

	t.Run("should skip pathToken credentials even with provider set", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "telegram",
				Type:     clawv1alpha1.CredentialTypePathToken,
				Provider: "telegram",
				Domain:   "api.telegram.org",
				PathToken: &clawv1alpha1.PathTokenConfig{
					Prefix: "/bot",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		assert.Empty(t, providers, "pathToken credentials should not generate provider entries")
	})

	t.Run("should reject duplicate providers", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini-1", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
			{Name: "gemini-2", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		err := injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "google")
	})
}

// --- Dynamic provider injection integration tests ---

func TestOpenClawDynamicProviders(t *testing.T) {
	ctx := context.Background()

	t.Run("should inject dynamic providers into ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		operatorJSON, ok := cm.Data["operator.json"]
		require.True(t, ok, "operator.json should exist")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

		models, ok := config["models"].(map[string]any)
		require.True(t, ok, "models section should exist")
		providers, ok := models["providers"].(map[string]any)
		require.True(t, ok, "providers section should exist")
		require.Contains(t, providers, "google", "google provider should be injected")

		google, ok := providers["google"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta", google["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", google["apiKey"])
	})

	t.Run("should have empty providers when no credentials have provider set", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:   "passthrough",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: "example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		models := config["models"].(map[string]any)
		providers := models["providers"].(map[string]any)
		assert.Empty(t, providers, "providers should be empty when no credentials have provider set")
	})

	t.Run("should have empty providers for MITM-only credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstanceMITMOnly(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		models := config["models"].(map[string]any)
		providers := models["providers"].(map[string]any)
		assert.Empty(t, providers, "providers should be empty for MITM-only credentials")
	})
}
