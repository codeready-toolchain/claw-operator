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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CredentialType selects the credential injection mechanism used by the proxy.
// +kubebuilder:validation:Enum=apiKey;bearer;gcp;pathToken;oauth2;none
type CredentialType string

const (
	CredentialTypeAPIKey    CredentialType = "apiKey"
	CredentialTypeBearer    CredentialType = "bearer"
	CredentialTypeGCP       CredentialType = "gcp"
	CredentialTypePathToken CredentialType = "pathToken"
	CredentialTypeOAuth2    CredentialType = "oauth2"
	CredentialTypeNone      CredentialType = "none"
)

// Condition types for Claw status.
const (
	ConditionTypeReady               = "Ready"
	ConditionTypeCredentialsResolved = "CredentialsResolved"
	ConditionTypeProxyConfigured     = "ProxyConfigured"
)

// Annotation keys used on proxy pod templates.
const (
	AnnotationKeyProxyConfigHash  = "claw.sandbox.redhat.com/proxy-config-hash"
	AnnotationPrefixSecretVersion = "claw.sandbox.redhat.com/"
	AnnotationSuffixSecretVersion = "-secret-version"
)

// Condition reasons for Claw status.
const (
	ConditionReasonReady            = "Ready"
	ConditionReasonProvisioning     = "Provisioning"
	ConditionReasonResolved         = "Resolved"
	ConditionReasonValidationFailed = "ValidationFailed"
	ConditionReasonConfigured       = "Configured"
	ConditionReasonConfigFailed     = "ConfigFailed"
)

// SecretRef references a specific key in a Secret.
type SecretRef struct {
	// Name is the name of the Secret
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the key in the Secret's data map
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// APIKeyConfig configures injection of a secret value into a custom header.
type APIKeyConfig struct {
	// Header name where the API key is injected (e.g., "x-goog-api-key", "x-api-key")
	// +kubebuilder:validation:MinLength=1
	Header string `json:"header"`

	// ValuePrefix is prepended to the secret value before injection.
	// Examples: "Bot " (Discord), "Basic " (pre-encoded basic auth).
	// +optional
	ValuePrefix string `json:"valuePrefix,omitempty"`
}

// GCPConfig configures GCP service account credential injection with OAuth2 token refresh.
type GCPConfig struct {
	// Project is the GCP project ID
	// +kubebuilder:validation:MinLength=1
	Project string `json:"project"`

	// Location is the GCP region (e.g., us-central1)
	// +kubebuilder:validation:MinLength=1
	Location string `json:"location"`
}

// PathTokenConfig configures token injection into the URL path.
type PathTokenConfig struct {
	// Prefix is prepended before the token in the URL path (e.g., "/bot" for Telegram)
	// +kubebuilder:validation:MinLength=1
	Prefix string `json:"prefix"`
}

// OAuth2Config configures client credentials token exchange.
type OAuth2Config struct {
	// ClientID for the OAuth2 client credentials flow
	// +kubebuilder:validation:MinLength=1
	ClientID string `json:"clientID"`

	// TokenURL is the OAuth2 token endpoint
	// +kubebuilder:validation:MinLength=1
	TokenURL string `json:"tokenURL"`

	// Scopes requested during token exchange
	// +optional
	Scopes []string `json:"scopes,omitempty"`
}

// CredentialSpec defines a single credential entry for proxy injection.
// +kubebuilder:validation:XValidation:rule="self.type != 'apiKey' || has(self.apiKey) || has(self.provider)",message="apiKey config is required when type is apiKey and provider is not set"
// +kubebuilder:validation:XValidation:rule="self.type != 'gcp' || has(self.gcp)",message="gcp config is required when type is gcp"
// +kubebuilder:validation:XValidation:rule="self.type != 'pathToken' || has(self.pathToken)",message="pathToken config is required when type is pathToken"
// +kubebuilder:validation:XValidation:rule="self.type != 'oauth2' || has(self.oauth2)",message="oauth2 config is required when type is oauth2"
// +kubebuilder:validation:XValidation:rule="self.type == 'none' || has(self.secretRef)",message="secretRef is required for this credential type"
type CredentialSpec struct {
	// Name uniquely identifies this credential entry.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type selects the credential injection mechanism
	Type CredentialType `json:"type"`

	// SecretRef references the Kubernetes Secret holding the credential value.
	// Not required for type "none" (proxy allowlist, no auth).
	// +optional
	SecretRef *SecretRef `json:"secretRef,omitempty"`

	// Domain the proxy matches against the request Host header.
	// Exact match: "api.github.com". Suffix match: ".googleapis.com" (leading dot).
	// Optional for known providers (google, anthropic) — the operator infers the default domain.
	// +optional
	Domain string `json:"domain,omitempty"`

	// DefaultHeaders are injected on every proxied request for this credential,
	// in addition to the credential itself (e.g., "anthropic-version: 2023-06-01").
	// +optional
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`

	// APIKey configures custom header injection. Required when type is "apiKey".
	// +optional
	APIKey *APIKeyConfig `json:"apiKey,omitempty"`

	// GCP configures GCP service account credential injection. Required when type is "gcp".
	// +optional
	GCP *GCPConfig `json:"gcp,omitempty"`

	// PathToken configures URL path token injection. Required when type is "pathToken".
	// +optional
	PathToken *PathTokenConfig `json:"pathToken,omitempty"`

	// OAuth2 configures client credentials token exchange. Required when type is "oauth2".
	// +optional
	OAuth2 *OAuth2Config `json:"oauth2,omitempty"`

	// Provider maps this credential to an OpenClaw LLM provider (e.g., "google", "anthropic", "openai", "openrouter").
	// When set, the controller configures gateway routing and generates the provider entry in openclaw.json.
	// When omitted, the credential is used for MITM forward proxy only (no provider entry).
	// +optional
	Provider string `json:"provider,omitempty"`
}

// ClawSpec defines the desired state of Claw
type ClawSpec struct {
	// Credentials configures proxy credential injection per domain.
	// +optional
	Credentials []CredentialSpec `json:"credentials,omitempty"`
}

// ClawStatus defines the observed state of Claw
type ClawStatus struct {
	// GatewayTokenSecretRef is the name of the Secret containing the gateway authentication token
	// +optional
	GatewayTokenSecretRef string `json:"gatewayTokenSecretRef,omitempty"`

	// Conditions represent the latest available observations of the Claw's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// URL is the HTTPS URL for accessing the Claw instance
	// +optional
	URL string `json:"url,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=claws,scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].reason"

// Claw is the Schema for the claws API
type Claw struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClawSpec   `json:"spec,omitempty"`
	Status ClawStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClawList contains a list of Claw
type ClawList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Claw `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Claw{}, &ClawList{})
}
