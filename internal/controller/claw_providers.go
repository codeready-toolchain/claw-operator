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

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

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

// vertexProviderAPIMapping maps provider names to their OpenClaw API identifiers for Vertex AI.
var vertexProviderAPIMapping = map[string]string{
	"anthropic": "anthropic-messages",
}

// apiKeyProviderDefault holds the default domain and header for a known provider using type: apiKey.
type apiKeyProviderDefault struct {
	Domain string
	Header string
}

// knownAPIKeyProviders maps provider names to their default domain and header.
var knownAPIKeyProviders = map[string]apiKeyProviderDefault{
	"google":    {Domain: "generativelanguage.googleapis.com", Header: "x-goog-api-key"},
	"anthropic": {Domain: "api.anthropic.com", Header: "x-api-key"},
}

// resolveProviderDefaults fills in missing Domain and APIKey fields for known providers.
// Explicit values are preserved (escape hatch). Returns an error if required fields
// are still missing after applying defaults (unknown provider without domain/apiKey).
func resolveProviderDefaults(cred *clawv1alpha1.CredentialSpec) error {
	switch cred.Type {
	case clawv1alpha1.CredentialTypeAPIKey:
		if defaults, ok := knownAPIKeyProviders[cred.Provider]; ok {
			if cred.Domain == "" {
				cred.Domain = defaults.Domain
			}
			if cred.APIKey == nil {
				cred.APIKey = &clawv1alpha1.APIKeyConfig{Header: defaults.Header}
			}
		}

	case clawv1alpha1.CredentialTypeGCP:
		if cred.Domain == "" {
			cred.Domain = ".googleapis.com"
		}

	case clawv1alpha1.CredentialTypeKubernetes:
		// Domains are derived from the kubeconfig, not the domain field
		return nil
	}

	if cred.Domain == "" {
		return fmt.Errorf("credential %q: domain is required (no default for provider %q with type %q)", cred.Name, cred.Provider, cred.Type)
	}
	if cred.Type == clawv1alpha1.CredentialTypeAPIKey && cred.APIKey == nil {
		return fmt.Errorf("credential %q: apiKey config is required (no default for provider %q)", cred.Name, cred.Provider)
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
// (works for google, anthropic, meta, etc.). Google + apiKey uses the Gemini REST API.
// All other combos: upstream = domain, basePath = "".
func resolveProviderInfo(cred clawv1alpha1.CredentialSpec) providerInfo {
	if cred.Type == clawv1alpha1.CredentialTypeGCP && cred.GCP != nil {
		return providerInfo{
			Upstream: vertexAIBaseURL(cred.GCP.Location),
			BasePath: "/v1/projects/" + cred.GCP.Project + "/locations/" + cred.GCP.Location + "/publishers/" + cred.Provider,
		}
	}

	if cred.Provider == "google" {
		return providerInfo{
			Upstream: "https://generativelanguage.googleapis.com",
			BasePath: "/v1beta",
		}
	}

	domain := strings.TrimPrefix(cred.Domain, ".")
	return providerInfo{Upstream: "https://" + domain}
}
