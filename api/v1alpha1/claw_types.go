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
	"k8s.io/apimachinery/pkg/runtime"
)

// CredentialType selects the credential injection mechanism used by the proxy.
// +kubebuilder:validation:Enum=apiKey;bearer;gcp;pathToken;oauth2;none;kubernetes
type CredentialType string

const (
	CredentialTypeAPIKey     CredentialType = "apiKey"
	CredentialTypeBearer     CredentialType = "bearer"
	CredentialTypeGCP        CredentialType = "gcp"
	CredentialTypePathToken  CredentialType = "pathToken"
	CredentialTypeOAuth2     CredentialType = "oauth2"
	CredentialTypeNone       CredentialType = "none"
	CredentialTypeKubernetes CredentialType = "kubernetes"
)

// ConfigMode controls how operator.json is merged into the user's openclaw.json
// at pod start time.
// +kubebuilder:validation:Enum=merge;overwrite
type ConfigMode string

const (
	ConfigModeMerge     ConfigMode = "merge"
	ConfigModeOverwrite ConfigMode = "overwrite"
)

// McpTransport selects the HTTP transport type for remote MCP servers.
// +kubebuilder:validation:Enum=streamable-http;sse
type McpTransport string

const (
	McpTransportStreamableHTTP McpTransport = "streamable-http"
	McpTransportSSE            McpTransport = "sse"
)

// Condition types for Claw status.
const (
	ConditionTypeReady                   = "Ready"
	ConditionTypeCredentialsResolved     = "CredentialsResolved"
	ConditionTypeProxyConfigured         = "ProxyConfigured"
	ConditionTypeDevicePairingConfigured = "DevicePairingConfigured"
	ConditionTypeMcpServersConfigured    = "McpServersConfigured"
)

// Annotation keys used on pod templates to trigger rollouts on config changes.
const (
	AnnotationKeyProxyConfigHash     = "claw.sandbox.redhat.com/proxy-config-hash"
	AnnotationKeyGatewayConfigHash   = "claw.sandbox.redhat.com/gateway-config-hash"
	AnnotationPrefixSecretVersion    = "claw.sandbox.redhat.com/"
	AnnotationSuffixSecretVersion    = "-secret-version"
	AnnotationPrefixMcpSecretVersion = "claw.sandbox.redhat.com/mcp-"
	AnnotationSuffixMcpSecretVersion = "-secret-version"
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

// SecretRefEntry references a specific key in a Secret.
type SecretRefEntry struct {
	// Name is the name of the Secret
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the key in the Secret's data map
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// Role distinguishes multiple secrets for the same credential.
	// Required when multiple secretRef entries are present (e.g., Slack botToken/appToken).
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Role string `json:"role,omitempty"`
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
// +kubebuilder:validation:XValidation:rule="has(self.type) || has(self.channel)",message="either type or channel must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.provider) || !has(self.channel)",message="provider and channel are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="has(self.channel) || self.type == 'none' || has(self.secretRef)",message="secretRef is required unless type is none or channel is set"
// +kubebuilder:validation:XValidation:rule="self.type != 'apiKey' || has(self.apiKey) || has(self.provider) || has(self.channel)",message="apiKey config is required when type is apiKey without inferred defaults"
// +kubebuilder:validation:XValidation:rule="self.type != 'gcp' || has(self.gcp) || has(self.channel)",message="gcp config is required when type is gcp without inferred defaults"
// +kubebuilder:validation:XValidation:rule="self.type != 'pathToken' || has(self.pathToken) || has(self.channel)",message="pathToken config is required when type is pathToken without inferred defaults"
// +kubebuilder:validation:XValidation:rule="self.type != 'oauth2' || has(self.oauth2) || has(self.channel)",message="oauth2 config is required when type is oauth2 without inferred defaults"
type CredentialSpec struct {
	// Name uniquely identifies this credential entry.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type selects the credential injection mechanism.
	// Optional when channel is set — the operator infers the type from the channel defaults.
	// +optional
	Type CredentialType `json:"type,omitempty"`

	// SecretRef references Kubernetes Secrets holding credential values.
	// For single-secret credentials, use a one-element array.
	// For multi-secret channels (e.g., Slack), use role to distinguish entries.
	// Not required for type "none" (proxy allowlist, no auth) or channels that use
	// non-secret auth (e.g., WhatsApp QR pairing).
	// +optional
	SecretRef []SecretRefEntry `json:"secretRef,omitempty"`

	// Domain the proxy matches against the request Host header.
	// Exact match: "api.github.com". Suffix match: ".googleapis.com" (leading dot).
	// Optional for known providers and channels — the operator infers the default domain.
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
	// Mutually exclusive with channel.
	// +optional
	Provider string `json:"provider,omitempty"`

	// Channel declares this credential as a messaging channel integration.
	// When set, the operator enables the channel in OpenClaw's config and
	// infers proxy defaults (type, domain, injection config, companion routes).
	// Known values: telegram, discord, slack, whatsapp.
	// Mutually exclusive with provider.
	// +optional
	Channel string `json:"channel,omitempty"`

	// ChannelConfig is opaque JSON deep-merged into the channel's config block
	// in operator.json. Use for channel-specific settings (dmPolicy, allowFrom, etc.).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	ChannelConfig *runtime.RawExtension `json:"channelConfig,omitempty"`

	// AllowedPaths restricts which URL paths the proxy permits for this domain.
	// Each entry is a path prefix (e.g., "/v1/api/"). If empty, all paths are allowed.
	// +optional
	AllowedPaths []string `json:"allowedPaths,omitempty"`
}

// McpServerSpec defines an MCP server the operator injects into OpenClaw's config.
// +kubebuilder:validation:XValidation:rule="has(self.command) || has(self.url)",message="either command (stdio) or url (HTTP) must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.command) || !has(self.url)",message="command and url are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!has(self.url) || !has(self.envFrom) || size(self.envFrom) == 0",message="envFrom is only allowed for stdio MCP servers (command), not HTTP (url)"
type McpServerSpec struct {
	// Command is the executable for a stdio MCP server.
	// +optional
	Command string `json:"command,omitempty"`

	// Args are command-line arguments for the stdio server.
	// +optional
	Args []string `json:"args,omitempty"`

	// URL is the endpoint for an HTTP MCP server.
	// +optional
	URL string `json:"url,omitempty"`

	// Transport selects the HTTP transport type ("streamable-http" or "sse").
	// Only valid when url is set.
	// +optional
	Transport McpTransport `json:"transport,omitempty"`

	// Env are plain environment variables passed to the stdio server process
	// and written into the MCP server config in operator.json.
	// Use for non-secret values and tier-2 placeholder tokens.
	// +optional
	Env map[string]string `json:"env,omitempty"`

	// EnvFrom are secret-backed environment variables mounted on the gateway
	// container and inherited by the stdio server subprocess (tier 3).
	// Use only when the proxy-placeholder pattern (tier 2) is not viable.
	// +optional
	EnvFrom []McpEnvFromSecret `json:"envFrom,omitempty"`
}

// McpEnvFromSecret maps a Kubernetes Secret key to an environment variable
// on the gateway container for tier 3 MCP secret injection.
type McpEnvFromSecret struct {
	// Name is the environment variable name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// SecretRef references a key in a Kubernetes Secret.
	SecretRef SecretRefEntry `json:"secretRef"`
}

// ClawSpec defines the desired state of Claw
type ClawSpec struct {
	// ConfigMode controls how operator config is applied on pod start.
	// "merge" (default) deep-merges operator settings into the existing
	// user config, preserving user-owned keys. "overwrite" fully replaces
	// the config on every pod start.
	// +optional
	// +kubebuilder:default=merge
	ConfigMode ConfigMode `json:"configMode,omitempty"`

	// Credentials configures proxy credential injection per domain.
	// +optional
	Credentials []CredentialSpec `json:"credentials,omitempty"`

	// McpServers declares MCP servers injected into OpenClaw's config.
	// Map keys are server names as they appear in the mcp.servers config.
	// +optional
	McpServers map[string]McpServerSpec `json:"mcpServers,omitempty"`
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
