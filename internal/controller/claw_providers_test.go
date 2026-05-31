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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Registry consistency tests ---

func TestKnownProvidersConsistency(t *testing.T) {
	const googleProvider = "google"

	t.Run("companions must be defined in knownProviders", func(t *testing.T) {
		for provider, defaults := range knownProviders {
			for _, companion := range defaults.Companions {
				_, ok := knownProviders[companion]
				assert.True(t, ok,
					"provider %q declares companion %q which is not defined in knownProviders",
					provider, companion)
			}
		}
	})

	t.Run("companions must have explicit API set", func(t *testing.T) {
		for provider, defaults := range knownProviders {
			for _, companion := range defaults.Companions {
				cDefaults := knownProviders[companion]
				assert.NotEmpty(t, cDefaults.API,
					"provider %q declares companion %q which has no API — "+
						"companions use a non-standard wire format by definition, so API must be set",
					provider, companion)
			}
		}
	})

	t.Run("providers with Domain must also have Header", func(t *testing.T) {
		for provider, defaults := range knownProviders {
			if defaults.Domain != "" {
				assert.NotEmpty(t, defaults.Header,
					"provider %q has Domain %q but no Header for apiKey injection",
					provider, defaults.Domain)
			}
		}
	})

	t.Run("VertexAPI is only set on providers that are usable via Vertex SDK", func(t *testing.T) {
		for provider, defaults := range knownProviders {
			if defaults.VertexAPI != "" && provider == googleProvider {
				t.Errorf("provider %q has VertexAPI set but google uses Vertex AI directly, not through the SDK path", provider)
			}
		}
	})

	t.Run("Vertex-capable providers with non-default wire format must have VertexAPI", func(t *testing.T) {
		// Only check providers that users can configure with type: gcp.
		// Skip google (uses Vertex directly, not the SDK path) and
		// companion-only providers (never appear as cred.Provider on GCP creds).
		isCompanion := map[string]bool{}
		for _, defaults := range knownProviders {
			for _, c := range defaults.Companions {
				isCompanion[c] = true
			}
		}

		for provider, defaults := range knownProviders {
			if provider == googleProvider || isCompanion[provider] {
				continue
			}
			if defaults.API != "" && defaults.VertexAPI == "" {
				assert.Fail(t,
					"provider %q has API=%q (non-default wire format) but no VertexAPI — "+
						"if this provider is used via Vertex SDK, it will fall back to openai-completions",
					provider, defaults.API)
			}
		}
	})

	t.Run("model entries have non-empty Name and Alias", func(t *testing.T) {
		for provider, defaults := range knownProviders {
			for i, m := range defaults.Models {
				assert.NotEmpty(t, m.Name, "provider %q model[%d] has empty Name", provider, i)
				assert.NotEmpty(t, m.Alias, "provider %q model[%d] has empty Alias", provider, i)
			}
		}
	})
}

// --- buildProviderEntry tests ---

func TestBuildProviderEntry(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantAPI  string
	}{
		{name: "google uses native Gemini API", provider: "google", wantAPI: "google-generative-ai"},
		{name: "anthropic uses Messages API", provider: "anthropic", wantAPI: "anthropic-messages"},
		{name: "openai-codex uses Codex responses API", provider: "openai-codex", wantAPI: "openai-codex-responses"},
		{name: "openai uses OpenClaw default wire format", provider: "openai", wantAPI: ""},
		{name: "xai uses OpenClaw default wire format", provider: "xai", wantAPI: ""},
		{name: "unknown provider uses OpenClaw default", provider: "custom-llm", wantAPI: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := buildProviderEntry(tt.provider, "https://example.com", "test-key")
			assert.Equal(t, "https://example.com", entry["baseUrl"])
			assert.Equal(t, "test-key", entry["apiKey"])
			assert.Equal(t, []any{}, entry["models"])

			if tt.wantAPI == "" {
				assert.NotContains(t, entry, "api")
				return
			}
			assert.Equal(t, tt.wantAPI, entry["api"])
		})
	}
}

// --- providerModelCatalog tests ---

func TestProviderModelCatalog(t *testing.T) {
	t.Run("returns models for known provider", func(t *testing.T) {
		models := providerModelCatalog("google")
		require.NotEmpty(t, models)
		assert.Equal(t, "gemini-3.5-flash", models[0].Name)
	})

	t.Run("returns nil for unknown provider", func(t *testing.T) {
		assert.Nil(t, providerModelCatalog("openrouter"))
	})

	t.Run("returns nil for provider with no models", func(t *testing.T) {
		assert.Nil(t, providerModelCatalog("openai-codex"))
	})
}

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

// --- resolveProviderInfo tests ---

func TestResolveProviderInfo(t *testing.T) {
	tests := []struct {
		name         string
		cred         clawv1alpha1.CredentialSpec
		wantUpstream string
		wantBasePath string
	}{
		{
			name: "google apiKey uses Gemini REST API",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "google",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Domain:   "generativelanguage.googleapis.com",
			},
			wantUpstream: "https://generativelanguage.googleapis.com",
			wantBasePath: "/v1beta",
		},
		{
			name: "google gcp uses Vertex AI",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "google",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-central1",
				},
			},
			wantUpstream: "https://us-central1-aiplatform.googleapis.com",
			wantBasePath: "/v1/projects/my-project/locations/us-central1/publishers/google",
		},
		{
			name: "anthropic bearer uses domain directly",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "anthropic",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   "api.anthropic.com",
			},
			wantUpstream: "https://api.anthropic.com",
			wantBasePath: "",
		},
		{
			name: "anthropic gcp uses Vertex AI with anthropic publisher",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "anthropic",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
			wantUpstream: "https://us-east5-aiplatform.googleapis.com",
			wantBasePath: "/v1/projects/my-project/locations/us-east5/publishers/anthropic",
		},
		{
			name: "gcp global location uses plain hostname",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "anthropic",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "global",
				},
			},
			wantUpstream: "https://aiplatform.googleapis.com",
			wantBasePath: "/v1/projects/my-project/locations/global/publishers/anthropic",
		},
		{
			name: "unknown provider with exact domain",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "custom-llm",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   "api.custom-llm.com",
			},
			wantUpstream: "https://api.custom-llm.com",
			wantBasePath: "",
		},
		{
			name: "unknown provider with suffix domain strips dot",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "custom",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   ".custom.ai",
			},
			wantUpstream: "https://custom.ai",
			wantBasePath: "",
		},
		{
			name: "known provider without BasePath uses domain directly",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "openai",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Domain:   "api.openai.com",
			},
			wantUpstream: "https://api.openai.com",
			wantBasePath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := resolveProviderInfo(tt.cred)
			assert.Equal(t, tt.wantUpstream, info.Upstream)
			assert.Equal(t, tt.wantBasePath, info.BasePath)
		})
	}
}

// --- resolveProviderDefaults tests ---

func TestResolveProviderDefaults(t *testing.T) {
	tests := []struct {
		name       string
		cred       clawv1alpha1.CredentialSpec
		wantDomain string
		wantHeader string
		wantErr    string
	}{
		{
			name: "google apiKey fills domain and header",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
			},
			wantDomain: "generativelanguage.googleapis.com",
			wantHeader: "x-goog-api-key",
		},
		{
			name: "anthropic apiKey fills domain and header",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "anthropic",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "anthropic",
			},
			wantDomain: "api.anthropic.com",
			wantHeader: "x-api-key",
		},
		{
			name: "google gcp fills domain",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				GCP:      &clawv1alpha1.GCPConfig{Project: "p", Location: "us-central1"},
			},
			wantDomain: ".googleapis.com",
		},
		{
			name: "anthropic gcp fills domain",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				GCP:      &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"},
			},
			wantDomain: ".googleapis.com",
		},
		{
			name: "explicit domain preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "custom-proxy.internal",
			},
			wantDomain: "custom-proxy.internal",
			wantHeader: "x-goog-api-key",
		},
		{
			name: "explicit apiKey preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-custom-key"},
			},
			wantDomain: "generativelanguage.googleapis.com",
			wantHeader: "x-custom-key",
		},
		{
			name: "unknown provider with domain succeeds",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				Domain:   "api.custom-llm.com",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
			wantDomain: "api.custom-llm.com",
			wantHeader: "x-api-key",
		},
		{
			name: "unknown provider without domain errors",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
			wantErr: "domain is required",
		},
		{
			name: "unknown provider without apiKey errors",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				Domain:   "api.custom-llm.com",
			},
			wantErr: "apiKey config is required",
		},
		{
			name: "no provider with domain and apiKey succeeds",
			cred: clawv1alpha1.CredentialSpec{
				Name:   "legacy",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &clawv1alpha1.APIKeyConfig{Header: "x-token"},
			},
			wantDomain: "api.example.com",
			wantHeader: "x-token",
		},
		{
			name: "bearer type with no domain errors",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "custom-llm",
			},
			wantErr: "domain is required",
		},
		{
			name: "kubernetes type returns nil (no domain required)",
			cred: clawv1alpha1.CredentialSpec{
				Name: "k8s",
				Type: clawv1alpha1.CredentialTypeKubernetes,
			},
			wantDomain: "",
		},
		{
			name: "known provider without Domain defaults requires explicit domain",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "grok",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "xai",
			},
			wantErr: "domain is required",
		},
		{
			name: "known provider without Domain defaults succeeds with explicit values",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gpt",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "openai",
				Domain:   "api.openai.com",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "Authorization"},
			},
			wantDomain: "api.openai.com",
			wantHeader: "Authorization",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred := tt.cred
			err := resolveProviderDefaults(&cred)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDomain, cred.Domain)
			if tt.wantHeader != "" {
				require.NotNil(t, cred.APIKey)
				assert.Equal(t, tt.wantHeader, cred.APIKey.Header)
			}
		})
	}
}
