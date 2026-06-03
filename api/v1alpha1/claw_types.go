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
	networkingv1 "k8s.io/api/networking/v1"
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
	ConditionTypeWebSearchConfigured     = "WebSearchConfigured"
	ConditionTypeIdle                    = "Idle"
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
	ConditionReasonIdle             = "Idle"
	ConditionReasonIdledByRequest   = "IdledByRequest"
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
// +kubebuilder:validation:XValidation:rule="has(self.type) || has(self.channel) || (has(self.provider) && self.provider in ['google', 'anthropic', 'openai', 'xai', 'openrouter'])",message="type is required (inferred only for known providers: google, anthropic, openai, xai, openrouter)"
// +kubebuilder:validation:XValidation:rule="!has(self.provider) || !has(self.channel)",message="provider and channel are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="has(self.channel) || (has(self.type) && self.type == 'none') || has(self.secretRef)",message="secretRef is required unless type is none or channel is set"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'apiKey' || has(self.apiKey) || (has(self.provider) && self.provider in ['google', 'anthropic']) || has(self.channel)",message="apiKey config is required when type is apiKey without inferred defaults"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'gcp' || has(self.gcp) || has(self.channel)",message="gcp config is required when type is gcp without inferred defaults"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'pathToken' || has(self.pathToken) || has(self.channel)",message="pathToken config is required when type is pathToken without inferred defaults"
// +kubebuilder:validation:XValidation:rule="!has(self.type) || self.type != 'oauth2' || has(self.oauth2) || has(self.channel)",message="oauth2 config is required when type is oauth2 without inferred defaults"
type CredentialSpec struct {
	// Name uniquely identifies this credential entry.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type selects the credential injection mechanism.
	// Optional when channel is set or provider is a known provider — the operator infers
	// the type from channel or provider defaults.
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
// +kubebuilder:validation:XValidation:rule="!has(self.transport) || has(self.url)",message="transport is only allowed for HTTP MCP servers (url)"
// +kubebuilder:validation:XValidation:rule="!has(self.credentialRef) || has(self.url)",message="credentialRef is only allowed for HTTP MCP servers (url)"
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

	// CredentialRef is the name of a credential in spec.credentials that
	// handles proxy routing and authentication for this MCP server's domain.
	// Only valid for HTTP MCP servers (url). The proxy injects credentials
	// so the gateway never sees raw tokens.
	// +optional
	CredentialRef string `json:"credentialRef,omitempty"`
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

// WebSearchSpec configures the operator-managed web search provider.
// +kubebuilder:validation:XValidation:rule="self.provider in ['duckduckgo','gemini'] || has(self.secretRef)",message="secretRef is required for API-keyed search providers"
type WebSearchSpec struct {
	// Provider selects the web search provider.
	// Known values: brave, tavily, duckduckgo, gemini.
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// SecretRef references a Secret key holding the search API key.
	// Required for API-keyed providers (brave, tavily).
	// Not needed for key-free (duckduckgo) or LLM-as-search (gemini).
	// +optional
	SecretRef *SecretRefEntry `json:"secretRef,omitempty"`

	// Config is provider-specific configuration merged into
	// plugins.entries.<provider>.config.webSearch in operator.json.
	// Use for provider-specific tuning (mode, maxResults, etc.).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *runtime.RawExtension `json:"config,omitempty"`
}

// WebFetchSpec configures the web_fetch tool.
type WebFetchSpec struct {
	// Enabled activates the web_fetch tool. Fetched URLs are gated by
	// the proxy allowlist.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// AuthMode selects the gateway authentication mechanism.
// +kubebuilder:validation:Enum=token;password
type AuthMode string

const (
	AuthModeToken    AuthMode = "token"
	AuthModePassword AuthMode = "password"
)

// AuthSpec configures gateway authentication.
// +kubebuilder:validation:XValidation:rule="self.mode != 'password' || has(self.passwordSecretRef)",message="passwordSecretRef is required when mode is password"
type AuthSpec struct {
	// Mode selects the authentication mechanism: "token" (default) uses an
	// auto-generated token, "password" uses a shared password from a Secret.
	// +optional
	// +kubebuilder:default=token
	Mode AuthMode `json:"mode,omitempty"`

	// PasswordSecretRef references a Secret key holding the shared password.
	// Required when mode is "password".
	// +optional
	PasswordSecretRef *SecretRefEntry `json:"passwordSecretRef,omitempty"`

	// DisableDevicePairing disables browser device identity checks
	// (maps to gateway.controlUi.dangerouslyDisableDeviceAuth upstream).
	// Defaults to true (device pairing is disabled by default).
	// Set to false to enable device pairing.
	// +optional
	DisableDevicePairing *bool `json:"disableDevicePairing,omitempty"`
}

// ConfigSpec defines user-provided OpenClaw configuration.
type ConfigSpec struct {
	// Raw is inline openclaw.json configuration as arbitrary JSON.
	// Keys set here are merged into operator.json before the enrichment
	// pipeline runs. User-set keys take precedence over operator defaults
	// for non-security-critical settings.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Raw *RawConfig `json:"raw,omitempty"`

	// MergeMode controls how operator config is applied on pod start.
	// "merge" (default) deep-merges operator settings into the existing
	// user config, preserving user-owned keys. "overwrite" fully replaces
	// the config on every pod start.
	// +optional
	// +kubebuilder:validation:Enum=merge;overwrite
	// +kubebuilder:default=merge
	MergeMode ConfigMode `json:"mergeMode,omitempty"`
}

// RawConfig holds arbitrary JSON configuration for openclaw.json.
// +kubebuilder:pruning:PreserveUnknownFields
type RawConfig struct {
	runtime.RawExtension `json:",inline"`
}

// WorkspaceSpec configures workspace file seeding.
type WorkspaceSpec struct {
	// SkipBootstrap suppresses the OpenClaw first-run questionnaire.
	// Default: false.
	// +optional
	SkipBootstrap bool `json:"skipBootstrap,omitempty"`

	// Files maps workspace-relative paths to file content.
	// Each file is seeded once (seedIfMissing) — user edits via the
	// OpenClaw UI are preserved across restarts.
	// +optional
	Files map[string]string `json:"files,omitempty"`
}

// MetricsSpec configures Prometheus metrics collection via an OTel Collector sidecar.
type MetricsSpec struct {
	// Enabled activates the OTel Collector sidecar and diagnostics.otel.metrics
	// config injection.
	Enabled bool `json:"enabled,omitempty"`

	// Port for the Prometheus metrics endpoint on the OTel Collector sidecar.
	// Default: 9464.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port *int32 `json:"port,omitempty"`

	// ServiceMonitor configures Prometheus Operator ServiceMonitor creation.
	// +optional
	ServiceMonitor *ServiceMonitorSpec `json:"serviceMonitor,omitempty"`
}

// ServiceMonitorSpec configures the Prometheus Operator ServiceMonitor resource.
type ServiceMonitorSpec struct {
	// Enabled controls whether a ServiceMonitor is created. Default: true
	// (when metrics.enabled is true).
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Interval is the Prometheus scrape interval. Default: "30s".
	// +optional
	Interval string `json:"interval,omitempty"`
}

// NetworkSpec configures network behavior for the gateway pod.
type NetworkSpec struct {
	// InClusterBypass controls whether the gateway pod can directly reach
	// in-cluster Kubernetes services, bypassing the MITM proxy.
	// When false (default), all egress goes through the proxy.
	// When true, .svc and .svc.cluster.local traffic bypasses the proxy.
	// +optional
	// +kubebuilder:default=false
	InClusterBypass *bool `json:"inClusterBypass,omitempty"`

	// AdditionalEgress appends raw NetworkPolicy egress rules to the
	// gateway's egress policy for targets the operator can't auto-detect:
	// tracing collectors, databases, webhooks, etc.
	// Rules are appended to {instance}-egress.
	// +optional
	AdditionalEgress []networkingv1.NetworkPolicyEgressRule `json:"additionalEgress,omitempty"`
}

// CustomProviderAPI selects the wire format for a custom provider.
// +kubebuilder:validation:Enum=openai-completions;openai-responses;anthropic-messages;ollama
type CustomProviderAPI string

const (
	CustomProviderAPIOpenAICompletions CustomProviderAPI = "openai-completions"
	CustomProviderAPIOpenAIResponses   CustomProviderAPI = "openai-responses"
	CustomProviderAPIAnthropicMessages CustomProviderAPI = "anthropic-messages"
	CustomProviderAPIOllama            CustomProviderAPI = "ollama"
)

// CustomModelEntry defines a single model on a custom provider.
type CustomModelEntry struct {
	// Name is the model identifier as the endpoint knows it (e.g., "qwen3-14b").
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Alias is the human-friendly display name shown in the model picker.
	// +optional
	Alias string `json:"alias,omitempty"`
}

// CustomProviderSpec defines a custom OpenAI-compatible model provider.
type CustomProviderSpec struct {
	// Name is the provider key used in models.providers and as the model prefix
	// (e.g., "my-vllm" -> models are referenced as "my-vllm/model-name").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9-]*$`
	Name string `json:"name"`

	// BaseUrl is the full base URL for the OpenAI-compatible API endpoint,
	// including any path prefix (e.g., "https://llm.mycompany.com/v1").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^https?://`
	BaseUrl string `json:"baseUrl"`

	// API selects the wire format / request adapter OpenClaw uses when talking
	// to this provider. Defaults to "openai-completions" (standard /v1/chat/completions).
	// +optional
	// +kubebuilder:validation:Enum=openai-completions;openai-responses;anthropic-messages;ollama
	API CustomProviderAPI `json:"api,omitempty"`

	// CredentialRef is the name of a credential in spec.credentials that
	// handles proxy routing and authentication for this provider's domain.
	// The referenced credential does not need provider set — this field
	// establishes the linkage.
	// +kubebuilder:validation:MinLength=1
	CredentialRef string `json:"credentialRef"`

	// Models lists the models available on this endpoint.
	// Each model is registered in agents.defaults.models with the provider
	// name prefix (e.g., "my-vllm/qwen3-14b").
	// +kubebuilder:validation:MinItems=1
	Models []CustomModelEntry `json:"models"`
}

// ClawSpec defines the desired state of Claw
type ClawSpec struct {
	// Config provides user-supplied OpenClaw configuration and merge behavior.
	// +optional
	Config *ConfigSpec `json:"config,omitempty"`

	// Auth configures gateway authentication. Defaults to token-based
	// authentication with device pairing disabled. Set mode to "password" for
	// shared-password access, or set disableDevicePairing to false to enable
	// device pairing.
	// +optional
	Auth *AuthSpec `json:"auth,omitempty"`

	// Credentials configures proxy credential injection per domain.
	// +optional
	Credentials []CredentialSpec `json:"credentials,omitempty"`

	// CustomProviders declares custom OpenAI-compatible model providers.
	// Each entry generates a models.providers entry and registers its models
	// in the model picker. The referenced credential handles proxy routing.
	// +optional
	// +listType=map
	// +listMapKey=name
	CustomProviders []CustomProviderSpec `json:"customProviders,omitempty"`

	// McpServers declares MCP servers injected into OpenClaw's config.
	// Map keys are server names as they appear in the mcp.servers config.
	// +optional
	McpServers map[string]McpServerSpec `json:"mcpServers,omitempty"`

	// WebSearch configures the web search provider for the OpenClaw agent.
	// +optional
	WebSearch *WebSearchSpec `json:"webSearch,omitempty"`

	// WebFetch enables the web_fetch tool for arbitrary URL fetching.
	// Fetched URLs are gated by the proxy allowlist — only domains
	// permitted by credentials, search providers, or builtins are reachable.
	// +optional
	WebFetch *WebFetchSpec `json:"webFetch,omitempty"`

	// Metrics configures Prometheus metrics collection via an OTel Collector sidecar.
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`

	// Network configures network behavior for the gateway pod.
	// Controls in-cluster proxy bypass and additional egress rules.
	// MCP server egress rules are auto-generated from spec.mcpServers URLs.
	// +optional
	Network *NetworkSpec `json:"network,omitempty"`

	// Plugins lists OpenClaw plugins to install via an init container before
	// the gateway starts. Each entry is a package name (e.g. "@openclaw/matrix").
	// The operator runs `openclaw plugins install clawhub:<pkg>` for each entry.
	// +optional
	Plugins []string `json:"plugins,omitempty"`

	// Workspace configures workspace file seeding and bootstrap behavior.
	// Files are seeded once (seedIfMissing) — user edits are preserved.
	// +optional
	Workspace *WorkspaceSpec `json:"workspace,omitempty"`

	// Skills maps skill names to SKILL.md content. Each entry creates
	// workspace/skills/<name>/SKILL.md, overwritten on every pod restart
	// (operator-managed).
	// +optional
	Skills map[string]string `json:"skills,omitempty"`

	// Idle, when set to true, instructs the operator to scale all managed
	// Deployments to zero replicas. Set to false (or omit) to run normally.
	// +optional
	// +kubebuilder:default=false
	Idle bool `json:"idle,omitempty"`
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

	// Deprecated: Use GatewayURL instead. Will be removed in a future version.
	// URL is the HTTPS URL for accessing the Claw instance
	// +optional
	URL string `json:"url,omitempty"`

	// GatewayURL is the HTTPS URL for accessing the Claw gateway, including the auth token fragment when applicable
	// +optional
	GatewayURL string `json:"gatewayURL,omitempty"`

	// DevicePairingURL is the HTTPS URL for the device pairing application.
	// Only populated when device pairing is enabled (disableDevicePairing is not true).
	// +optional
	DevicePairingURL string `json:"devicePairingURL,omitempty"`
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
