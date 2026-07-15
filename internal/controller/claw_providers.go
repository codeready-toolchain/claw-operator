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
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// modelEntry defines a single model with its API name and human-readable alias.
type modelEntry struct {
	Name  string
	Alias string
}

// providerDefaults centralizes all per-provider knowledge for the operator.
// Adding a new first-class provider means adding a single entry here instead
// of updating multiple independent maps.
type providerDefaults struct {
	// CredType is the default credential type inferred when the user omits it.
	// Empty means the operator will not infer a type (user must specify).
	CredType clawv1alpha1.CredentialType

	// Credential defaults (apiKey type only; ignored for other cred types).
	Domain string // e.g. "generativelanguage.googleapis.com"
	Header string // e.g. "x-goog-api-key"

	// OpenClaw wire format API identifier. Empty means the provider uses the
	// OpenClaw default (openai-completions when baseUrl is set).
	API string // direct path, e.g. "google-generative-ai"

	// Wire format for the Vertex AI SDK path. Only used when usesVertexSDK()
	// is true (non-Google GCP credentials).
	VertexAPI string // e.g. "anthropic-messages"

	// BasePath is appended to the upstream host in resolveProviderInfo().
	// Non-empty only for providers whose native API sits under a sub-path
	// (e.g. "/v1beta" for Google's Gemini REST API).
	BasePath string

	// Companions are additional provider entries auto-injected alongside
	// this provider. Each companion must itself be defined in knownProviders.
	Companions []string

	// VertexPlugin is the ClawHub package spec for the OpenClaw plugin
	// required when this provider is used via the Vertex AI SDK path.
	// Empty means no plugin is needed (provider is handled natively).
	VertexPlugin string

	// Models is the hardcoded model catalog for this provider.
	// Order matters: the first model becomes the default primary when this
	// provider is the first configured credential in the Claw CR; remaining
	// models become the fallback chain.
	Models []modelEntry

	// EmbeddingAdapter is the OpenClaw memory search adapter ID for this
	// provider (e.g. "openai", "gemini"). Empty means the provider does not
	// support embeddings and is ineligible for automatic memory search config.
	EmbeddingAdapter string
}

// knownProviders is the single source of truth for all per-provider
// configuration in the operator. Credential defaults, wire format, model
// catalog, companion relationships, and routing info are all defined here.
//
// Providers not in this map (e.g., custom providers) still work -- they
// just get no defaults, no API override (OpenClaw defaults to
// openai-completions), and no model catalog.
var knownProviders = map[string]providerDefaults{
	"google": {
		CredType:         clawv1alpha1.CredentialTypeAPIKey,
		Domain:           "generativelanguage.googleapis.com",
		Header:           "x-goog-api-key",
		API:              "google-generative-ai",
		BasePath:         "/v1beta",
		EmbeddingAdapter: "gemini",
		Models: []modelEntry{
			{Name: "gemini-3.5-flash", Alias: "Gemini 3.5 Flash"},
			{Name: "gemini-3.1-pro-preview", Alias: "Gemini 3.1 Pro"},
			{Name: "gemini-3.1-flash-lite", Alias: "Gemini 3.1 Flash Lite"},
		},
	},
	"anthropic": {
		CredType:     clawv1alpha1.CredentialTypeAPIKey,
		Domain:       "api.anthropic.com",
		Header:       "x-api-key",
		API:          "anthropic-messages",
		VertexAPI:    "anthropic-messages",
		VertexPlugin: "@openclaw/anthropic-vertex-provider",
		Models: []modelEntry{
			{Name: "claude-sonnet-4-6", Alias: "Claude Sonnet 4.6"},
			// claude-sonnet-5 is a real, currently-shipping Anthropic model, but
			// OpenClaw's Anthropic thinking-profile resolver doesn't recognize it
			// yet: supportsClaudeAdaptiveThinking() in
			// refs/openclaw/packages/llm-core/src/model-contracts/anthropic.ts only
			// allowlists claude-{fable-5,mythos-preview,opus-4-(6|7|8),sonnet-4-6}
			// for adaptive-only thinking. Anthropic's Sonnet 5 API requires adaptive
			// thinking and rejects manual thinking config with a 400 error, so
			// without a matching entry OpenClaw would fall back to sending a manual
			// thinking level (e.g. "medium") and every request would fail. Re-enable
			// once a future OpenClaw release adds sonnet-5 to that allowlist (or
			// otherwise ships native support) -- check refs/openclaw CHANGELOG.md /
			// the anthropic plugin manifest for "sonnet-5" during the next
			// check-openclaw-release pass.
			// {Name: "claude-sonnet-5", Alias: "Claude Sonnet 5"},
			{Name: "claude-opus-4-8", Alias: "Claude Opus 4.8"},
			{Name: "claude-opus-4-7", Alias: "Claude Opus 4.7"},
			{Name: "claude-opus-4-6", Alias: "Claude Opus 4.6"},
			{Name: "claude-fable-5", Alias: "Claude Fable 5"},
			{Name: "claude-haiku-4-5", Alias: "Claude Haiku 4.5"},
		},
	},
	"openai": {
		CredType:         clawv1alpha1.CredentialTypeBearer,
		Domain:           "api.openai.com",
		BasePath:         "/v1",
		Companions:       []string{"openai-codex"},
		EmbeddingAdapter: "openai",
		Models: []modelEntry{
			{Name: "gpt-5.5", Alias: "GPT-5.5"},
			{Name: "gpt-5.4", Alias: "GPT-5.4"},
			{Name: "gpt-5.4-mini", Alias: "GPT-5.4 Mini"},
		},
	},
	"openai-codex": {
		API: "openai-chatgpt-responses",
	},
	"xai": {
		CredType: clawv1alpha1.CredentialTypeBearer,
		Domain:   "api.x.ai",
		BasePath: "/v1",
		API:      "openai-responses",
		Models: []modelEntry{
			{Name: "grok-4.3", Alias: "Grok 4.3"},
			{Name: "grok-4.20", Alias: "Grok 4.20"},
		},
	},
	"openrouter": {
		CredType: clawv1alpha1.CredentialTypeBearer,
		Domain:   "openrouter.ai",
		BasePath: "/api/v1",
		Models: []modelEntry{
			{Name: "openai/gpt-5.5", Alias: "GPT-5.5"},
			{Name: "anthropic/claude-sonnet-4-6", Alias: "Claude Sonnet 4.6"},
			{Name: "google/gemini-3.5-flash", Alias: "Gemini 3.5 Flash"},
		},
	},
}

// imagePluginVersion extracts the OpenClaw release version from a container
// image tag for use as an npm package version suffix.
//
//	"ghcr.io/openclaw/openclaw:2026.7.1"             → "2026.7.1"
//	"ghcr.io/openclaw/openclaw:2026.7.1-slim-arm64"  → "2026.7.1"
//	"ghcr.io/openclaw/openclaw:slim"                  → ""  (latest)
//	"ghcr.io/openclaw/openclaw:latest"                → ""  (latest)
//	"ghcr.io/openclaw/openclaw"                       → "2026.7.1"  (or whatever is derived from default image)
//	"ghcr.io/openclaw/openclaw@sha256:abc"            → "sha256:abc" (must be a valid digest)
//	"ghcr.io/openclaw/openclaw@sha256:invalid"        → error
func imagePluginVersion(image string) (string, error) {
	if image == "" {
		image = DefaultOpenClawImage
	}
	ref, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return "", err
	}
	identifier := ref.Identifier()
	if identifier == "latest" || identifier == "slim" {
		return "", nil
	}
	if version, _, ok := strings.Cut(identifier, "-slim"); ok {
		return version, nil
	}
	return identifier, nil
}

// usesVertexSDK returns true when a credential should use the native Vertex AI SDK
// instead of a gateway proxy route. This applies to non-Google GCP providers (e.g.,
// Anthropic via Vertex AI), where the provider's SDK format doesn't match Vertex AI's
// URL structure and the native @anthropic-ai/vertex-sdk handles it correctly.
func usesVertexSDK(cred clawv1alpha1.CredentialSpec) bool {
	return cred.Type == clawv1alpha1.CredentialTypeGCP && cred.Provider != "" && cred.Provider != "google"
}

// vertexAIBaseURL returns the Vertex AI REST base URL for the given location.
// The "global" location uses a plain hostname without a region prefix.
func vertexAIBaseURL(location string) string {
	if location == "global" {
		return "https://aiplatform.googleapis.com"
	}
	return "https://" + location + "-aiplatform.googleapis.com"
}

// gcpAIPlatformDomain returns the bare hostname for the Vertex AI API
// in the given location, derived from vertexAIBaseURL as the single
// source of truth.
func gcpAIPlatformDomain(location string) string {
	return strings.TrimPrefix(vertexAIBaseURL(location), "https://")
}

// buildProviderEntry constructs an OpenClaw provider config entry with the
// correct wire format API baked in. This avoids the "build map then mutate"
// pattern that led to the missing api field bug.
func buildProviderEntry(provider, baseURL, apiKey string) map[string]any {
	entry := map[string]any{
		"baseUrl": baseURL,
		"apiKey":  apiKey,
		"models":  []any{},
	}
	if api := knownProviders[provider].API; api != "" {
		entry["api"] = api
	}
	return entry
}

// resolveProviderDefaults fills in missing Type, Domain, and APIKey for known providers.
// Explicit values are preserved (escape hatch). Returns an error if required fields
// are still missing after applying defaults (unknown provider without domain/apiKey).
func resolveProviderDefaults(cred *clawv1alpha1.CredentialSpec) error {
	if cred.Type == "" {
		if defaults, ok := knownProviders[cred.Provider]; ok && defaults.CredType != "" {
			cred.Type = defaults.CredType
		} else {
			return fmt.Errorf("credential %q: type is required (no default for provider %q)",
				cred.Name, cred.Provider)
		}
	}

	switch cred.Type {
	case clawv1alpha1.CredentialTypeAPIKey:
		if defaults, ok := knownProviders[cred.Provider]; ok && defaults.Domain != "" {
			if cred.Domain == "" {
				cred.Domain = defaults.Domain
			}
			if cred.APIKey == nil && defaults.Header != "" {
				cred.APIKey = &clawv1alpha1.APIKeyConfig{Header: defaults.Header}
			}
		}

	case clawv1alpha1.CredentialTypeBearer:
		if defaults, ok := knownProviders[cred.Provider]; ok && defaults.Domain != "" {
			if cred.Domain == "" {
				cred.Domain = defaults.Domain
			}
		}

	case clawv1alpha1.CredentialTypeGCP:
		if cred.Domain == "" && cred.GCP != nil {
			cred.Domain = gcpAIPlatformDomain(cred.GCP.Location)
		}

	case clawv1alpha1.CredentialTypeKubernetes:
		return nil
	}

	if cred.Domain == "" {
		return fmt.Errorf("credential %q: domain is required (no default for provider %q with type %q)",
			cred.Name, cred.Provider, cred.Type)
	}
	if cred.Type == clawv1alpha1.CredentialTypeAPIKey && cred.APIKey == nil {
		return fmt.Errorf("credential %q: apiKey config is required (no default for provider %q)",
			cred.Name, cred.Provider)
	}
	return nil
}

// providerInfo holds the resolved upstream host and base path for a provider's gateway route.
type providerInfo struct {
	Upstream string
	BasePath string
}

// resolveProviderInfo returns the upstream and base path for a credential's provider.
// GCP credentials route through Vertex AI with the provider name as the publisher
// (works for google, anthropic, meta, etc.). Providers with a BasePath in
// knownProviders use their native API endpoint. All others: upstream = domain.
func resolveProviderInfo(cred clawv1alpha1.CredentialSpec) providerInfo {
	if cred.Type == clawv1alpha1.CredentialTypeGCP && cred.GCP != nil {
		return providerInfo{
			Upstream: vertexAIBaseURL(cred.GCP.Location),
			BasePath: "/v1/projects/" + cred.GCP.Project + "/locations/" + cred.GCP.Location + "/publishers/" + cred.Provider,
		}
	}

	if defaults, ok := knownProviders[cred.Provider]; ok && defaults.BasePath != "" {
		domain := cred.Domain
		if domain == "" || strings.HasPrefix(domain, ".") {
			domain = defaults.Domain
		}
		return providerInfo{
			Upstream: "https://" + domain,
			BasePath: defaults.BasePath,
		}
	}

	domain := strings.TrimPrefix(cred.Domain, ".")
	return providerInfo{Upstream: "https://" + domain}
}
