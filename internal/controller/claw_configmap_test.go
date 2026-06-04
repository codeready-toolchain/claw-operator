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
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func providersFromConfig(t *testing.T, config map[string]any) map[string]any {
	t.Helper()
	modelsVal, ok := config["models"]
	require.True(t, ok, "config should contain 'models' key")
	models, ok := modelsVal.(map[string]any)
	require.True(t, ok, "models should be map[string]any, got %T", modelsVal)
	providersVal, ok := models["providers"]
	require.True(t, ok, "models should contain 'providers' key")
	providers, ok := providersVal.(map[string]any)
	require.True(t, ok, "providers should be map[string]any, got %T", providersVal)
	return providers
}

// --- Provider injection Vertex SDK tests ---

func TestInjectProvidersVertexSDK(t *testing.T) {
	t.Run("should map GCP anthropic to anthropic-vertex provider key", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "anthropic-vertex")

		av := providers["anthropic-vertex"].(map[string]any)
		assert.Equal(t, "https://us-east5-aiplatform.googleapis.com", av["baseUrl"])
		assert.Equal(t, "gcp-vertex-credentials", av["apiKey"])
		assert.Equal(t, "anthropic-messages", av["api"])
		assert.Equal(t, 128000, av["maxTokens"])
	})

	t.Run("should use plain hostname for global location", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		av := providers["anthropic-vertex"].(map[string]any)
		assert.Equal(t, "https://aiplatform.googleapis.com", av["baseUrl"])
	})

	t.Run("should set maxTokens and no api for non-anthropic vertex provider", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "meta-vertex")
		mv := providers["meta-vertex"].(map[string]any)
		assert.Equal(t, "https://us-central1-aiplatform.googleapis.com", mv["baseUrl"])
		assert.Equal(t, "gcp-vertex-credentials", mv["apiKey"])
		assert.Equal(t, 128000, mv["maxTokens"])
		assert.NotContains(t, mv, "api", "meta has no VertexAPI in knownProviders")
	})

	t.Run("should reject duplicate vertex providers", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		err := injectProviders(config, testClawWithCredentials(credentials), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "anthropic-vertex")
	})
}

// --- Provider injection tests ---

func TestInjectProviders(t *testing.T) {
	t.Run("should inject google provider with correct baseUrl", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta", google["baseUrl"])
		assert.Equal(t, "google-generative-ai", google["api"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", google["apiKey"])
	})

	t.Run("should inject multiple providers", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		assert.Contains(t, providers, "google")
		assert.Contains(t, providers, "anthropic")
		anthropic := providers["anthropic"].(map[string]any)
		assert.Equal(t, "https://api.anthropic.com", anthropic["baseUrl"])
		assert.Equal(t, "anthropic-messages", anthropic["api"])
	})

	t.Run("should leave providers empty when no provider is set", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:   "telegram",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.telegram.org",
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		assert.Empty(t, providers)
	})

	t.Run("should use Vertex AI upstream for google gcp credential", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "https://europe-west1-aiplatform.googleapis.com/v1/projects/my-proj/locations/europe-west1/publishers/google", google["baseUrl"])
		assert.Equal(t, "google-generative-ai", google["api"])
	})

	t.Run("should set api for anthropic apiKey credential", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "claude",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "anthropic",
				Domain:   "api.anthropic.com",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "anthropic")
		anthropic := providers["anthropic"].(map[string]any)
		assert.Equal(t, "https://api.anthropic.com", anthropic["baseUrl"])
		assert.Equal(t, "anthropic-messages", anthropic["api"])
	})

	t.Run("should skip pathToken credentials even with provider set", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
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

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		assert.Empty(t, providers, "pathToken credentials should not generate provider entries")
	})

	t.Run("should reject duplicate providers", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini-1", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
			{Name: "gemini-2", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		err := injectProviders(config, testClawWithCredentials(credentials), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "google")
	})

	t.Run("should inject companion providers for openai", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "openai",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "openai",
				Domain:   "api.openai.com",
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "openai")
		require.Contains(t, providers, "openai-codex", "companion provider should be injected")
		codex := providers["openai-codex"].(map[string]any)
		openai := providers["openai"].(map[string]any)
		assert.Equal(t, "https://api.openai.com/v1", openai["baseUrl"])
		assert.Equal(t, openai["baseUrl"], codex["baseUrl"])
		assert.Equal(t, "openai-codex-responses", codex["api"])
		assert.NotContains(t, openai, "api", "OpenAI-compatible providers use OpenClaw default wire format")
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", codex["apiKey"])
	})

	t.Run("should reject explicit credential that collides with companion provider", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "codex", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "openai-codex", Domain: "api.openai.com"},
			{Name: "openai", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "openai", Domain: "api.openai.com"},
		}

		err := injectProviders(config, testClawWithCredentials(credentials), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "openai-codex")
	})

	t.Run("should set api and baseUrl with /v1 for xai", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "grok",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "xai",
				Domain:   "api.x.ai",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "Authorization"},
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "xai")
		xai := providers["xai"].(map[string]any)
		assert.Equal(t, "https://api.x.ai/v1", xai["baseUrl"])
		assert.Equal(t, "openai-responses", xai["api"])
	})

	t.Run("should handle openrouter with /api/v1 base path", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "or",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "openrouter",
				Domain:   "openrouter.ai",
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "openrouter")
		entry := providers["openrouter"].(map[string]any)
		assert.Equal(t, "https://openrouter.ai/api/v1", entry["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", entry["apiKey"])
		assert.NotContains(t, entry, "api", "openrouter should use OpenClaw default wire format")
	})

	t.Run("should handle unknown provider without api field", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				Domain:   "api.custom-llm.com",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "Authorization"},
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "custom-llm")
		entry := providers["custom-llm"].(map[string]any)
		assert.Equal(t, "https://api.custom-llm.com", entry["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", entry["apiKey"])
		assert.NotContains(t, entry, "api", "unknown providers should use OpenClaw default wire format")
	})

	t.Run("should inject codexOAuth provider with synthetic JWT apiKey", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "codex",
				Type:     clawv1alpha1.CredentialTypeCodexOAuth,
				Provider: "openai-oauth",
				Domain:   "chatgpt.com",
			},
		}
		resolvedCreds := []resolvedCredential{
			{
				CredentialSpec: credentials[0],
				CodexOAuth:     &codexOAuthData{AccountID: "acct_test123"},
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), resolvedCreds))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "openai-oauth")
		entry := providers["openai-oauth"].(map[string]any)
		assert.Equal(t, "https://chatgpt.com", entry["baseUrl"])
		assert.Equal(t, "openai-chatgpt-responses", entry["api"])

		apiKey, ok := entry["apiKey"].(string)
		require.True(t, ok, "apiKey should be a string")
		assert.NotEqual(t, placeholderAPIKey, apiKey, "codexOAuth should use synthetic JWT, not placeholder")
		assert.Contains(t, apiKey, ".", "apiKey should look like a JWT (contain dots)")
	})

	t.Run("should use placeholder when codexOAuth resolvedCreds is nil", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "codex",
				Type:     clawv1alpha1.CredentialTypeCodexOAuth,
				Provider: "openai-oauth",
				Domain:   "chatgpt.com",
			},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "openai-oauth")
		entry := providers["openai-oauth"].(map[string]any)
		assert.Equal(t, placeholderAPIKey, entry["apiKey"])
	})

	t.Run("should inject openai-oauth model catalog", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "codex", Type: clawv1alpha1.CredentialTypeCodexOAuth, Provider: "openai-oauth", Domain: "chatgpt.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Contains(t, models, "openai-oauth/gpt-5.4-mini")
		assert.Contains(t, models, "openai-oauth/gpt-5.5")
		assert.Contains(t, models, "openai-oauth/gpt-5.4")
		entry := models["openai-oauth/gpt-5.4-mini"].(map[string]any)
		assert.Equal(t, "GPT-5.4 Mini", entry["alias"])
	})

	t.Run("should inject both openai and openai-oauth providers independently", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "openai", Type: clawv1alpha1.CredentialTypeBearer, Provider: "openai", Domain: "api.openai.com"},
			{Name: "codex", Type: clawv1alpha1.CredentialTypeCodexOAuth, Provider: "openai-oauth", Domain: "chatgpt.com"},
		}
		resolvedCreds := []resolvedCredential{
			{CredentialSpec: credentials[0]},
			{CredentialSpec: credentials[1], CodexOAuth: &codexOAuthData{AccountID: "acct_dual"}},
		}

		require.NoError(t, injectProviders(config, testClawWithCredentials(credentials), resolvedCreds))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "openai")
		require.Contains(t, providers, "openai-oauth")

		openaiEntry := providers["openai"].(map[string]any)
		assert.Equal(t, placeholderAPIKey, openaiEntry["apiKey"],
			"openai should use placeholder, not synthetic JWT")

		oauthEntry := providers["openai-oauth"].(map[string]any)
		apiKey := oauthEntry["apiKey"].(string)
		assert.NotEqual(t, placeholderAPIKey, apiKey,
			"openai-oauth should use synthetic JWT")
		assert.Contains(t, apiKey, ".", "apiKey should be a JWT")
	})
}

// --- Model catalog injection tests ---

func TestInjectModelCatalog(t *testing.T) {
	t.Run("single provider google emits correct model entries", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Len(t, models, len(providerModelCatalog("google")))
		assert.Contains(t, models, "google/gemini-3.5-flash")
		entry := models["google/gemini-3.5-flash"].(map[string]any)
		assert.Equal(t, "Gemini 3.5 Flash", entry["alias"])
	})

	t.Run("multiple providers emit models for each", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
			{Name: "claude", Type: clawv1alpha1.CredentialTypeBearer, Provider: "anthropic", Domain: "api.anthropic.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		expectedCount := len(providerModelCatalog("google")) + len(providerModelCatalog("anthropic"))
		assert.Len(t, models, expectedCount)
		assert.Contains(t, models, "google/gemini-3.5-flash")
		assert.Contains(t, models, "anthropic/claude-sonnet-4-6")
	})

	t.Run("vertex credential emits provider-vertex prefix", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "anthropic-vertex", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
				Domain: ".googleapis.com", GCP: &clawv1alpha1.GCPConfig{Project: "my-project", Location: "us-east5"},
			},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Contains(t, models, "anthropic-vertex/claude-sonnet-4-6")
		assert.NotContains(t, models, "anthropic/claude-sonnet-4-6")
	})

	t.Run("both direct and vertex anthropic coexist", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "claude-direct", Type: clawv1alpha1.CredentialTypeBearer, Provider: "anthropic", Domain: "api.anthropic.com"},
			{
				Name: "claude-vertex", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
				Domain: ".googleapis.com", GCP: &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"},
			},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Contains(t, models, "anthropic/claude-sonnet-4-6")
		assert.Contains(t, models, "anthropic-vertex/claude-sonnet-4-6")
		expectedCount := len(providerModelCatalog("anthropic")) * 2
		assert.Len(t, models, expectedCount)
	})

	t.Run("primary set from first providers first model", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
			{Name: "claude", Type: clawv1alpha1.CredentialTypeBearer, Provider: "anthropic", Domain: "api.anthropic.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "google/gemini-3.5-flash", model["primary"])
	})

	t.Run("openrouter emits three-segment model keys", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "or", Type: clawv1alpha1.CredentialTypeBearer, Provider: "openrouter", Domain: "openrouter.ai"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Len(t, models, len(providerModelCatalog("openrouter")))
		assert.Contains(t, models, "openrouter/openai/gpt-5.5")
		assert.Contains(t, models, "openrouter/anthropic/claude-sonnet-4-6")
		assert.Contains(t, models, "openrouter/google/gemini-3.5-flash")
		entry := models["openrouter/openai/gpt-5.5"].(map[string]any)
		assert.Equal(t, "GPT-5.5", entry["alias"])

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "openrouter/openai/gpt-5.5", model["primary"])
		fallbacks := model["fallbacks"].([]any)
		require.Len(t, fallbacks, 2)
		assert.Equal(t, "openrouter/anthropic/claude-sonnet-4-6", fallbacks[0])
		assert.Equal(t, "openrouter/google/gemini-3.5-flash", fallbacks[1])
	})

	t.Run("primary set from first provider with catalog", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "custom", Type: clawv1alpha1.CredentialTypeBearer, Provider: "custom-llm", Domain: "api.custom-llm.com"},
			{Name: "claude", Type: clawv1alpha1.CredentialTypeBearer, Provider: "anthropic", Domain: "api.anthropic.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "anthropic/claude-sonnet-4-6", model["primary"])
	})

	t.Run("no providers means no models or primary", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "passthrough", Type: clawv1alpha1.CredentialTypeNone, Domain: "example.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		_, hasAgents := config["agents"]
		assert.False(t, hasAgents, "agents section should not exist when no models are emitted")
	})

	t.Run("pathToken credentials skipped", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "telegram", Type: clawv1alpha1.CredentialTypePathToken, Provider: "telegram",
				Domain: "api.telegram.org", PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"},
			},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		_, hasAgents := config["agents"]
		assert.False(t, hasAgents, "pathToken credentials should not generate model entries")
	})

	t.Run("provider not in catalog silently skipped", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "custom", Type: clawv1alpha1.CredentialTypeBearer, Provider: "custom-llm", Domain: "api.custom-llm.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		_, hasAgents := config["agents"]
		assert.False(t, hasAgents, "unknown provider should not generate model entries")
	})

	t.Run("user model entry wins on collision", func(t *testing.T) {
		config := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"models": map[string]any{
						"google/gemini-3.5-flash": map[string]any{"alias": "My Custom Alias"},
					},
				},
			},
		}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		entry := models["google/gemini-3.5-flash"].(map[string]any)
		assert.Equal(t, "My Custom Alias", entry["alias"])
		assert.Len(t, models, len(providerModelCatalog("google")))
	})

	t.Run("user primary wins over catalog default", func(t *testing.T) {
		config := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"model": map[string]any{
						"primary": "anthropic/claude-sonnet-4-6",
					},
				},
			},
		}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "anthropic/claude-sonnet-4-6", model["primary"])
	})

	t.Run("fallbacks set from remaining catalog models", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		fallbacks, ok := model["fallbacks"].([]any)
		require.True(t, ok, "fallbacks should be set")
		require.Len(t, fallbacks, len(providerModelCatalog("google"))-1, "fallbacks should contain all non-primary models")
		for i, fb := range fallbacks {
			assert.Equal(t, "google/"+providerModelCatalog("google")[i+1].Name, fb)
		}
	})

	t.Run("fallbacks use same provider as primary", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
			{Name: "claude", Type: clawv1alpha1.CredentialTypeBearer, Provider: "anthropic", Domain: "api.anthropic.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		fallbacks, ok := model["fallbacks"].([]any)
		require.True(t, ok)
		for _, f := range fallbacks {
			assert.Contains(t, f.(string), "google/", "fallbacks should only contain models from the primary provider")
		}
	})

	t.Run("vertex credential fallbacks use provider-vertex prefix", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "claude-vertex", Type: clawv1alpha1.CredentialTypeGCP, Provider: "anthropic",
				Domain: ".googleapis.com", GCP: &clawv1alpha1.GCPConfig{Project: "my-project", Location: "us-east5"},
			},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "anthropic-vertex/"+providerModelCatalog("anthropic")[0].Name, model["primary"])
		fallbacks, ok := model["fallbacks"].([]any)
		require.True(t, ok, "fallbacks should be set for vertex credentials")
		for _, f := range fallbacks {
			assert.Contains(t, f.(string), "anthropic-vertex/",
				"vertex fallbacks must use the provider-vertex prefix")
		}
	})

	t.Run("user fallbacks win over catalog default", func(t *testing.T) {
		config := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"model": map[string]any{
						"fallbacks": []any{"anthropic/claude-sonnet-4-6"},
					},
				},
			},
		}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		fallbacks := model["fallbacks"].([]any)
		require.Len(t, fallbacks, 1)
		assert.Equal(t, "anthropic/claude-sonnet-4-6", fallbacks[0])
	})

	t.Run("catalog fills gaps when user has some models", func(t *testing.T) {
		config := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"models": map[string]any{
						"google/gemini-3.1-pro-preview": map[string]any{"alias": "My Pro Override"},
					},
				},
			},
		}
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		injectModelCatalog(config, testClawWithCredentials(credentials))

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Len(t, models, len(providerModelCatalog("google")))
		assert.Contains(t, models, "google/gemini-3.5-flash")
		assert.Contains(t, models, "google/gemini-3.1-flash-lite")
		proEntry := models["google/gemini-3.1-pro-preview"].(map[string]any)
		assert.Equal(t, "My Pro Override", proEntry["alias"])
	})
}

// --- Custom provider injection tests ---

func testClawWithCustomProviders(
	credentials []clawv1alpha1.CredentialSpec,
	customProviders []clawv1alpha1.CustomProviderSpec,
) *clawv1alpha1.Claw {
	claw := testClawWithCredentials(credentials)
	claw.Spec.CustomProviders = customProviders
	return claw
}

func TestInjectCustomProviders(t *testing.T) {
	t.Run("should inject custom provider with correct baseUrl and models", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models: []clawv1alpha1.CustomModelEntry{
						{Name: "qwen3-14b"},
						{Name: "llama-4-scout"},
					},
				},
			},
		)

		require.NoError(t, injectProviders(config, claw, nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "my-vllm")
		vllm := providers["my-vllm"].(map[string]any)
		assert.Equal(t, "https://llm.mycompany.com/v1", vllm["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", vllm["apiKey"])
		models := vllm["models"].([]any)
		require.Len(t, models, 2)
		assert.Equal(t, "qwen3-14b", models[0].(map[string]any)["id"])
		assert.Equal(t, "llama-4-scout", models[1].(map[string]any)["id"])
	})

	t.Run("should set api field when specified", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "ollama", Type: clawv1alpha1.CredentialTypeNone, Domain: "ollama.internal.corp"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "ollama",
					BaseUrl:       "http://ollama.internal.corp:11434",
					API:           clawv1alpha1.CustomProviderAPIOllama,
					CredentialRef: "ollama",
					Models:        []clawv1alpha1.CustomModelEntry{{Name: "llama3.3"}},
				},
			},
		)

		require.NoError(t, injectProviders(config, claw, nil))

		providers := providersFromConfig(t, config)
		ollama := providers["ollama"].(map[string]any)
		assert.Equal(t, "ollama", ollama["api"])
	})

	t.Run("should omit api field when not specified", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models:        []clawv1alpha1.CustomModelEntry{{Name: "model-a"}},
				},
			},
		)

		require.NoError(t, injectProviders(config, claw, nil))

		providers := providersFromConfig(t, config)
		vllm := providers["my-vllm"].(map[string]any)
		assert.NotContains(t, vllm, "api")
	})

	t.Run("should coexist with credential-based providers", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{
					Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google",
					Domain: "generativelanguage.googleapis.com",
				},
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models:        []clawv1alpha1.CustomModelEntry{{Name: "model-a"}},
				},
			},
		)

		require.NoError(t, injectProviders(config, claw, nil))

		providers := providersFromConfig(t, config)
		assert.Contains(t, providers, "google")
		assert.Contains(t, providers, "my-vllm")
	})

	t.Run("should reject duplicate between credential provider and customProvider", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{
					Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google",
					Domain: "generativelanguage.googleapis.com",
				},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "google",
					BaseUrl:       "https://my-proxy.com/v1",
					CredentialRef: "gemini",
					Models:        []clawv1alpha1.CustomModelEntry{{Name: "model-a"}},
				},
			},
		)

		err := injectProviders(config, claw, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "google")
	})

	t.Run("should inject multiple custom providers", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "cred-a", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm-a.corp"},
				{Name: "cred-b", Type: clawv1alpha1.CredentialTypeNone, Domain: "ollama.corp"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name: "vllm", BaseUrl: "https://llm-a.corp/v1", CredentialRef: "cred-a",
					Models: []clawv1alpha1.CustomModelEntry{{Name: "qwen3-14b"}},
				},
				{
					Name: "ollama", BaseUrl: "http://ollama.corp:11434", CredentialRef: "cred-b",
					API:    clawv1alpha1.CustomProviderAPIOllama,
					Models: []clawv1alpha1.CustomModelEntry{{Name: "llama3.3"}},
				},
			},
		)

		require.NoError(t, injectProviders(config, claw, nil))

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "vllm")
		require.Contains(t, providers, "ollama")
		assert.Equal(t, "https://llm-a.corp/v1", providers["vllm"].(map[string]any)["baseUrl"])
		assert.Equal(t, "http://ollama.corp:11434", providers["ollama"].(map[string]any)["baseUrl"])
		assert.Equal(t, "ollama", providers["ollama"].(map[string]any)["api"])
		assert.NotContains(t, providers["vllm"].(map[string]any), "api")
	})
}

func TestInjectModelCatalogCustomProviders(t *testing.T) {
	t.Run("should add custom provider models to catalog", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models: []clawv1alpha1.CustomModelEntry{
						{Name: "qwen3-14b", Alias: "Qwen 3 14B"},
						{Name: "llama-4-scout"},
					},
				},
			},
		)

		injectModelCatalog(config, claw)

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		require.Contains(t, models, "my-vllm/qwen3-14b")
		require.Contains(t, models, "my-vllm/llama-4-scout")
		assert.Equal(t, "Qwen 3 14B", models["my-vllm/qwen3-14b"].(map[string]any)["alias"])
		assert.Equal(t, "llama-4-scout", models["my-vllm/llama-4-scout"].(map[string]any)["alias"])
	})

	t.Run("should set primary from custom provider when no catalog provider exists", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models: []clawv1alpha1.CustomModelEntry{
						{Name: "qwen3-14b"},
						{Name: "llama-4-scout"},
					},
				},
			},
		)

		injectModelCatalog(config, claw)

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "my-vllm/qwen3-14b", model["primary"])
	})

	t.Run("should prefer built-in catalog primary over custom provider", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{
					Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google",
					Domain: "generativelanguage.googleapis.com",
				},
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models:        []clawv1alpha1.CustomModelEntry{{Name: "qwen3-14b"}},
				},
			},
		)

		injectModelCatalog(config, claw)

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "google/gemini-3.5-flash", model["primary"])

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Contains(t, models, "my-vllm/qwen3-14b")
		assert.Contains(t, models, "google/gemini-3.1-pro-preview")
	})

	t.Run("should register models from multiple custom providers", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "cred-a", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm-a.corp"},
				{Name: "cred-b", Type: clawv1alpha1.CredentialTypeNone, Domain: "ollama.corp"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name: "vllm", BaseUrl: "https://llm-a.corp/v1", CredentialRef: "cred-a",
					Models: []clawv1alpha1.CustomModelEntry{{Name: "qwen3-14b", Alias: "Qwen 3"}},
				},
				{
					Name: "ollama", BaseUrl: "http://ollama.corp:11434", CredentialRef: "cred-b",
					Models: []clawv1alpha1.CustomModelEntry{{Name: "llama3.3", Alias: "Llama 3.3"}},
				},
			},
		)

		injectModelCatalog(config, claw)

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Contains(t, models, "vllm/qwen3-14b")
		assert.Contains(t, models, "ollama/llama3.3")
		assert.Equal(t, "Qwen 3", models["vllm/qwen3-14b"].(map[string]any)["alias"])
		assert.Equal(t, "Llama 3.3", models["ollama/llama3.3"].(map[string]any)["alias"])

		model := config["agents"].(map[string]any)["defaults"].(map[string]any)["model"].(map[string]any)
		assert.Equal(t, "vllm/qwen3-14b", model["primary"], "primary should come from first custom provider")
	})

	t.Run("should use model name as alias when alias is empty", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{"providers": map[string]any{}}}
		claw := testClawWithCustomProviders(
			[]clawv1alpha1.CredentialSpec{
				{Name: "my-cred", Type: clawv1alpha1.CredentialTypeBearer, Domain: "llm.mycompany.com"},
			},
			[]clawv1alpha1.CustomProviderSpec{
				{
					Name:          "my-vllm",
					BaseUrl:       "https://llm.mycompany.com/v1",
					CredentialRef: "my-cred",
					Models:        []clawv1alpha1.CustomModelEntry{{Name: "qwen3-14b"}},
				},
			},
		)

		injectModelCatalog(config, claw)

		models := config["agents"].(map[string]any)["defaults"].(map[string]any)["models"].(map[string]any)
		assert.Equal(t, "qwen3-14b", models["my-vllm/qwen3-14b"].(map[string]any)["alias"])
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

	t.Run("should inject customProviders into ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "my-cred",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    "llm.mycompany.com",
			},
		}
		instance.Spec.CustomProviders = []clawv1alpha1.CustomProviderSpec{
			{
				Name:          "my-vllm",
				BaseUrl:       "https://llm.mycompany.com/v1",
				CredentialRef: "my-cred",
				Models: []clawv1alpha1.CustomModelEntry{
					{Name: "qwen3-14b", Alias: "Qwen 3 14B"},
					{Name: "llama-4-scout", Alias: "Llama 4 Scout"},
				},
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

		providers := providersFromConfig(t, config)
		require.Contains(t, providers, "my-vllm")
		vllm := providers["my-vllm"].(map[string]any)
		assert.Equal(t, "https://llm.mycompany.com/v1", vllm["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", vllm["apiKey"])
		models := vllm["models"].([]any)
		require.Len(t, models, 2)
		assert.Equal(t, "qwen3-14b", models[0].(map[string]any)["id"])

		agents := config["agents"].(map[string]any)["defaults"].(map[string]any)
		catalogModels := agents["models"].(map[string]any)
		assert.Contains(t, catalogModels, "my-vllm/qwen3-14b")
		assert.Contains(t, catalogModels, "my-vllm/llama-4-scout")
		assert.Equal(t, "Qwen 3 14B", catalogModels["my-vllm/qwen3-14b"].(map[string]any)["alias"])

		model := agents["model"].(map[string]any)
		assert.Equal(t, "my-vllm/qwen3-14b", model["primary"])
	})

	t.Run("should inject model catalog into operator.json after reconciliation", func(t *testing.T) {
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

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		agents, ok := config["agents"].(map[string]any)
		require.True(t, ok, "operator.json should contain agents section")
		defaults := agents["defaults"].(map[string]any)

		catalogModels, hasModels := defaults["models"].(map[string]any)
		require.True(t, hasModels, "operator.json should contain agents.defaults.models")
		assert.Contains(t, catalogModels, "google/gemini-3.5-flash", "should have google model from catalog")

		model := defaults["model"].(map[string]any)
		assert.Equal(t, "google/gemini-3.5-flash", model["primary"], "primary should be first google model")
		fallbacks, ok := model["fallbacks"].([]any)
		assert.True(t, ok, "fallbacks should be set")
		assert.NotEmpty(t, fallbacks, "fallbacks should contain remaining catalog models")
	})
}
