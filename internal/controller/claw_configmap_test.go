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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Vertex AI base URL tests ---

func TestVertexAIBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		location string
		want     string
	}{
		{
			name:     "global uses plain hostname",
			location: "global",
			want:     "https://aiplatform.googleapis.com",
		},
		{
			name:     "regional location uses prefix",
			location: "us-east5",
			want:     "https://us-east5-aiplatform.googleapis.com",
		},
		{
			name:     "another region uses prefix",
			location: "europe-west1",
			want:     "https://europe-west1-aiplatform.googleapis.com",
		},
		{
			name:     "us-central1 uses prefix",
			location: "us-central1",
			want:     "https://us-central1-aiplatform.googleapis.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, vertexAIBaseURL(tt.location))
		})
	}
}

// --- Vertex SDK helper tests ---

func TestUsesVertexSDK(t *testing.T) {
	tests := []struct {
		name string
		cred clawv1alpha1.CredentialSpec
		want bool
	}{
		{
			name: "GCP + anthropic uses Vertex SDK",
			cred: clawv1alpha1.CredentialSpec{Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic"},
			want: true,
		},
		{
			name: "GCP + google does not use Vertex SDK",
			cred: clawv1alpha1.CredentialSpec{Type: clawv1alpha1.CredentialTypeGCP, Provider: "google"},
			want: false,
		},
		{
			name: "GCP without provider does not use Vertex SDK",
			cred: clawv1alpha1.CredentialSpec{Type: clawv1alpha1.CredentialTypeGCP},
			want: false,
		},
		{
			name: "apiKey + anthropic does not use Vertex SDK",
			cred: clawv1alpha1.CredentialSpec{Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "anthropic"},
			want: false,
		},
		{
			name: "GCP + meta uses Vertex SDK",
			cred: clawv1alpha1.CredentialSpec{Type: clawv1alpha1.CredentialTypeGCP, Provider: "meta"},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, usesVertexSDK(tt.cred))
		})
	}
}

// --- Proxy config Vertex SDK tests ---

func TestGenerateProxyConfigVertexSDK(t *testing.T) {
	t.Run("should not create gateway route for GCP anthropic credential", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "vertex-sa",
					Key:  "sa.json",
				},
				Domain: ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, ".googleapis.com")
		assert.Equal(t, "gcp", route.Injector)
		assert.Empty(t, route.PathPrefix, "Vertex SDK provider should not have gateway path prefix")
		assert.Empty(t, route.Upstream, "Vertex SDK provider should not have gateway upstream")
	})

	t.Run("should create gateway route for GCP google but not GCP anthropic", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "gcp-secret",
					Key:  "sa.json",
				},
				Domain: ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-central1",
				},
			},
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "vertex-sa",
					Key:  "sa.json",
				},
				Domain: ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		var googleRoute, anthropicRoute *proxyRoute
		for i := range cfg.Routes {
			if cfg.Routes[i].SAFilePath == "/etc/proxy/credentials/gemini-vertex/sa-key.json" {
				googleRoute = &cfg.Routes[i]
			}
			if cfg.Routes[i].SAFilePath == "/etc/proxy/credentials/anthropic-vertex/sa-key.json" {
				anthropicRoute = &cfg.Routes[i]
			}
		}

		require.NotNil(t, googleRoute, "google GCP route should exist")
		assert.Equal(t, "/gemini-vertex", googleRoute.PathPrefix, "google GCP should have gateway prefix")
		assert.NotEmpty(t, googleRoute.Upstream, "google GCP should have gateway upstream")

		require.NotNil(t, anthropicRoute, "anthropic GCP route should exist")
		assert.Empty(t, anthropicRoute.PathPrefix, "anthropic GCP should not have gateway prefix")
		assert.Empty(t, anthropicRoute.Upstream, "anthropic GCP should not have gateway upstream")
	})
}

// --- Provider injection Vertex SDK tests ---

func TestInjectProvidersVertexSDK(t *testing.T) {
	makeConfigMap := func(jsonContent string) []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{
			"operator-models.json": jsonContent,
		}
		return []*unstructured.Unstructured{cm}
	}

	getConfig := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator-models.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		return config
	}

	getProviders := func(t *testing.T, config map[string]any) map[string]any {
		t.Helper()
		return config["providers"].(map[string]any)
	}

	t.Run("should map GCP anthropic to anthropic-vertex provider key", func(t *testing.T) {
		objects := makeConfigMap(`{"providers":{}}`)
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
		objects := makeConfigMap(`{"providers":{}}`)
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
		objects := makeConfigMap(`{"providers":{}}`)
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
		objects := makeConfigMap(`{"providers":{}}`)
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
