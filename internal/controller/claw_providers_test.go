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
