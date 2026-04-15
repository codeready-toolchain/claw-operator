# Credential Examples (`spec.credentials[]`)

**Related:** [Sketch](security-hardening-sketch.md), [Design](security-hardening-design.md)

This document shows how real-world services map to entries in the `Claw` CRD's `spec.credentials[]` array. Each credential entry has a `name`, a `type` that selects the injection mechanism, a `secretRef` pointing to a Kubernetes Secret, a `domain` for proxy routing, and optional type-specific config blocks.

**Domain matching:** exact string (`api.github.com`) or suffix with leading dot (`.googleapis.com` matches `generativelanguage.googleapis.com`, `aiplatform.googleapis.com`, etc.). First match wins.

---

## Go Types

```go
type ClawSpec struct {
    // Credentials configures proxy credential injection per domain.
    // +optional
    Credentials []CredentialSpec `json:"credentials,omitempty"`
}

// CredentialSpec defines a single credential entry in spec.credentials[].
// +kubebuilder:validation:XValidation:rule="self.type != 'apiKey' || has(self.apiKey)",message="apiKey config is required when type is apiKey"
// +kubebuilder:validation:XValidation:rule="self.type != 'gcp' || has(self.gcp)",message="gcp config is required when type is gcp"
type CredentialSpec struct {
    // Name uniquely identifies this credential entry.
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // Type selects the credential injection mechanism
    // +kubebuilder:validation:Enum=apiKey;bearer;gcp;pathToken;oauth2;none
    Type CredentialType `json:"type"`

    // SecretRef references the Kubernetes Secret holding the credential value.
    // Not required for type "none" (proxy allowlist, no auth).
    // +optional
    SecretRef *SecretKeyRef `json:"secretRef,omitempty"`

    // Domain the proxy matches against the request Host header.
    // Exact match: "api.github.com". Suffix match: ".googleapis.com" (leading dot).
    Domain string `json:"domain"`

    // DefaultHeaders are injected on every proxied request for this credential,
    // in addition to the credential itself. Useful for required version headers
    // like "anthropic-version: 2023-06-01" (pattern from OpenShell).
    // +optional
    DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`

    // Shape-specific configuration — set the one matching Type.
    APIKey     *APIKeyConfig     `json:"apiKey,omitempty"`
    GCP        *GCPConfig        `json:"gcp,omitempty"`
    PathToken  *PathTokenConfig  `json:"pathToken,omitempty"`
    OAuth2     *OAuth2Config     `json:"oauth2,omitempty"`
    // Bearer and None need no extra config.

    // Provider maps this credential to an OpenClaw LLM provider (e.g., "google", "anthropic", "openai", "openrouter").
    // When set, the controller configures gateway routing and generates the provider entry in openclaw.json.
    // When omitted, the credential is used for MITM forward proxy only (no provider entry).
    // +optional
    Provider string `json:"provider,omitempty"`
}

type CredentialType string

const (
    CredentialTypeAPIKey      CredentialType = "apiKey"
    CredentialTypeBearer      CredentialType = "bearer"
    CredentialTypeGCP         CredentialType = "gcp"
    CredentialTypePathToken   CredentialType = "pathToken"
    CredentialTypeOAuth2      CredentialType = "oauth2"
    CredentialTypeNone        CredentialType = "none"        // proxy allowlist only, no credential injection — see "Proxy Allowlist" below
)

type SecretKeyRef struct {
    Name string `json:"name"`
    Key  string `json:"key"`
}

type APIKeyConfig struct {
    // Header name where the API key is injected (e.g., "x-goog-api-key", "x-api-key")
    Header string `json:"header"`
    // ValuePrefix is prepended to the secret value before injection.
    // Examples: "Bot " (Discord), "Basic " (pre-encoded basic auth), "Bearer " (custom bearer).
    // +optional
    ValuePrefix string `json:"valuePrefix,omitempty"`
}

type GCPConfig struct {
    Project  string `json:"project"`
    Location string `json:"location"`
}

type PathTokenConfig struct {
    // Prefix prepended before the token in the URL path (e.g., "/bot" for Telegram)
    Prefix string `json:"prefix"`
}

type OAuth2Config struct {
    ClientID string   `json:"clientID"`
    TokenURL string   `json:"tokenURL"`
    Scopes   []string `json:"scopes,omitempty"`
}

```

---

## LLM Providers

### Gemini (API key)

```yaml
- name: gemini
  type: apiKey
  secretRef:
    name: llm-keys
    key: GEMINI_API_KEY
  domain: "generativelanguage.googleapis.com"
  apiKey:
    header: "x-goog-api-key"
  provider: google  # Enables gateway mode + generates provider entry in openclaw.json
                    # baseUrl → http://claw-proxy:8080/gemini/v1beta
```

### Anthropic (API key + default headers)

Anthropic requires an `anthropic-version` header on every request. Use `defaultHeaders` to inject it alongside the API key.

```yaml
- name: anthropic
  type: apiKey
  secretRef:
    name: llm-keys
    key: ANTHROPIC_API_KEY
  domain: "api.anthropic.com"
  defaultHeaders:
    anthropic-version: "2023-06-01"
  apiKey:
    header: "x-api-key"
  provider: anthropic  # Gateway mode: baseUrl → http://claw-proxy:8080/anthropic
```

### OpenAI (Bearer token)

```yaml
- name: openai
  type: bearer
  secretRef:
    name: llm-keys
    key: OPENAI_API_KEY
  domain: "api.openai.com"
  provider: openai  # Gateway mode: baseUrl → http://claw-proxy:8080/openai
```

### OpenRouter (Bearer token, OpenAI-compatible)

```yaml
- name: openrouter
  type: bearer
  secretRef:
    name: llm-keys
    key: OPENROUTER_API_KEY
  domain: "openrouter.ai"
  provider: openrouter  # Gateway mode: baseUrl → http://claw-proxy:8080/openrouter
```

---

## Cloud AI

### Vertex AI (GCP service account → OAuth2)

The proxy loads the service account JSON, obtains a short-lived OAuth2 access token via `golang.org/x/oauth2/google`, caches and auto-refreshes it, and injects `Authorization: Bearer <token>`.

**GCP token vending** (pattern from paude-proxy): Google client SDKs inside OpenClaw try to obtain tokens from `oauth2.googleapis.com/token` via Application Default Credentials (ADC). The proxy intercepts these `POST` requests and returns a dummy access token, so the SDK is satisfied without ever seeing real credentials. The proxy then replaces the dummy token with the real one on subsequent API calls. This allows unmodified Google SDK code inside OpenClaw to work transparently with the proxy.

```yaml
- name: vertex-ai
  type: gcp
  secretRef:
    name: gcp-sa
    key: sa-key.json
  domain: ".googleapis.com"
  gcp:
    project: my-gcp-project
    location: us-central1
  provider: google  # Gateway mode: baseUrl → http://claw-proxy:8080/vertex-ai/v1/projects/my-gcp-project/locations/us-central1/publishers/google
                    # Upstream → https://us-central1-aiplatform.googleapis.com
```

**Domain:** `.googleapis.com` (suffix match) covers `aiplatform.googleapis.com`, `generativelanguage.googleapis.com`, and the token endpoint `oauth2.googleapis.com`.

**Provider resolution for Google:** When `provider: "google"` is set:
- With `type: gcp`, the controller resolves the Vertex AI upstream URL from `gcp.project` and `gcp.location` (e.g., `https://us-central1-aiplatform.googleapis.com`) and generates the appropriate base path
- With `type: apiKey`, the controller uses the Gemini REST API upstream (`https://generativelanguage.googleapis.com`) with base path `/v1beta`

---

## Channel Integrations

Channel integrations and platform APIs typically do not set `provider` — they use MITM forward proxy mode only, since they are not LLM providers that need `openclaw.json` entries.

### Telegram Bot (path token injection)

The Telegram Bot API places the token in the URL path: `/bot<TOKEN>/sendMessage`.

```yaml
- name: telegram
  type: pathToken
  secretRef:
    name: channel-tokens
    key: TELEGRAM_BOT_TOKEN
  domain: "api.telegram.org"
  pathToken:
    prefix: "/bot"
```

**Proxy behavior:** Incoming request to `api.telegram.org/sendMessage` becomes `api.telegram.org/bot<TOKEN>/sendMessage`.

### WhatsApp Business (Meta Cloud API, Bearer token)

The access token is a standard Bearer header. The phone number ID is part of the request URL constructed by the client, not a credential concern.

```yaml
- name: whatsapp
  type: bearer
  secretRef:
    name: meta-tokens
    key: WHATSAPP_ACCESS_TOKEN
  domain: "graph.facebook.com"
```

### Slack Bot (Bearer token)

Slack Bot tokens use `Authorization: Bearer xoxb-...`.

```yaml
- name: slack
  type: bearer
  secretRef:
    name: channel-tokens
    key: SLACK_BOT_TOKEN
  domain: "slack.com"
```

### Discord Bot (API key with value prefix)

Discord uses `Authorization: Bot <TOKEN>` — not standard Bearer. Modeled as an API key injection on the `Authorization` header with a `valuePrefix`.

```yaml
- name: discord
  type: apiKey
  secretRef:
    name: channel-tokens
    key: DISCORD_BOT_TOKEN
  domain: "discord.com"
  apiKey:
    header: "Authorization"
    valuePrefix: "Bot "
```

**Proxy behavior:** Sets `Authorization: Bot <TOKEN>`. The `valuePrefix` field also handles `Token <value>` and other non-standard authorization schemes.

---

## Platform APIs

### GitHub (Bearer token)

```yaml
- name: github
  type: bearer
  secretRef:
    name: platform-tokens
    key: GITHUB_TOKEN
  domain: "api.github.com"
```

### Jira Cloud (pre-encoded Basic auth)

Jira uses `Authorization: Basic base64(email:token)`. The user pre-computes the base64 value and stores it in the Secret.

```yaml
- name: jira
  type: apiKey
  secretRef:
    name: platform-tokens
    key: JIRA_BASIC_AUTH         # pre-encoded: base64("user@corp.com:api-token")
  domain: ".atlassian.net"
  apiKey:
    header: "Authorization"
    valuePrefix: "Basic "
```

---

## Kubernetes API (Deferred — Phase 3)

The gateway needs access to the Kubernetes API to manage resources in the user's namespace (deploy apps, debug workloads, etc.). The gateway pod's egress is locked to the proxy only, so Kubernetes API calls must also go through the proxy.

The `kubernetes` credential type was removed from the initial implementation because projected ServiceAccount token volumes only provide the proxy pod's own SA token, not a user-specified one. Several approaches are under consideration:

- **TokenRequest API** — the operator mints short-lived tokens for a user-specified ServiceAccount via the Kubernetes TokenRequest API and injects them into the proxy. Requires elevated RBAC permissions for the operator.
- **User-managed Secret** — the user provides a Secret containing a kubeconfig or long-lived token. Simpler but less secure (no automatic rotation).
- **Projected volumes with operator-created SA** — the operator creates a dedicated ServiceAccount and configures the proxy pod to project its token. Limited to the proxy pod's own namespace.

This will be designed and implemented when there is a concrete use case. See Phase 3 in the [design document](security-hardening-design.md).

---

## MCP Servers

### Remote MCP server with API key

```yaml
- name: mcp-web-search
  type: apiKey
  secretRef:
    name: mcp-secrets
    key: WEB_SEARCH_API_KEY
  domain: "search-mcp.tools.example.com"
  apiKey:
    header: "x-api-key"
```

### Remote MCP server with Bearer token

```yaml
- name: mcp-code-analysis
  type: bearer
  secretRef:
    name: mcp-secrets
    key: CODE_ANALYSIS_TOKEN
  domain: "code-mcp.tools.example.com"
```

### Enterprise MCP server with OAuth2 client credentials

The proxy exchanges the client secret for a short-lived access token at the configured token URL, caches and auto-refreshes it, and injects `Authorization: Bearer <token>`.

```yaml
- name: mcp-enterprise-crm
  type: oauth2
  secretRef:
    name: oauth-secrets
    key: crm-client-secret
  domain: "crm-mcp.corp.example.com"
  oauth2:
    clientID: "openclaw-agent"
    tokenURL: "https://sso.corp.example.com/oauth/token"
    scopes: ["crm:read", "crm:write"]
```

---

## Proxy Allowlist (`type: none`)

The proxy blocks all unknown domains with a 403. When the proxy should forward requests to a domain without injecting any credentials — for example, a service where authentication is handled at another layer (mTLS via service mesh, NetworkPolicy-based isolation, etc.) — `type: none` serves as a pure allowlist entry.

```yaml
- name: allowed-service
  type: none
  domain: "some-service.example.com"
```

The proxy passes the request through without modifying any headers. No `secretRef` is needed.

---

## Shape Summary

| Type | Injection mechanism | Services | Extra config | `provider` |
|------|-------------------|----------|--------------|------------|
| `apiKey` | Custom header with secret value | Gemini, Anthropic, Discord, Jira, MCP servers | `header`, `valuePrefix`, `defaultHeaders` | LLM providers: `google`, `anthropic`, etc. |
| `bearer` | `Authorization: Bearer <token>` | OpenAI, OpenRouter, GitHub, Slack, WhatsApp, MCP servers | `defaultHeaders` | LLM providers: `openai`, `openrouter`, etc. |
| `gcp` | SA JSON → OAuth2 token refresh + token vending → Bearer | Vertex AI, GCP APIs | `project`, `location` | `google` (Vertex AI) |
| `pathToken` | Token inserted into URL path | Telegram | `prefix` | Typically not set |
| `oauth2` | Client credentials → token exchange → Bearer | Enterprise MCP servers, corporate APIs | `clientID`, `tokenURL`, `scopes` | Optional |
| `none` | No auth (proxy allowlist only) | Services with auth at another layer | — | N/A |

When `provider` is set, the proxy uses **gateway mode** (path-prefix routing, dynamic `openclaw.json` generation). When omitted, the proxy uses **MITM forward proxy mode** only.

---

## Edge Cases and Notes

- **Discord `Bot` prefix:** Handled via `apiKey.valuePrefix: "Bot "` on the `Authorization` header.
- **Anthropic version header:** Handled via `defaultHeaders: { anthropic-version: "2023-06-01" }`. Any provider requiring extra static headers uses the same mechanism.
- **GCP token vending:** The `gcp` injector intercepts `POST oauth2.googleapis.com/token` and returns a dummy access token so Google SDK clients work with placeholder ADC credentials. Real token injection happens on subsequent API calls. The suffix domain `.googleapis.com` covers both API and token endpoints.
- **Domain matching precedence:** Routes are checked in config order; first match wins. Exact matches (`api.github.com`) should be listed before suffix matches (`.googleapis.com`) to avoid unintended catches.
- **Multiple credentials for the same domain:** Possible (e.g., two GCP projects). The proxy config should support this — route matching may need path-based disambiguation in the future.
- **Secret ownership:** The Secrets referenced by `spec.credentials` entries are user-created and user-owned. The operator reads but does not create or modify them. Only operator-managed Secrets (`openclaw-gateway-token`, proxy config) have owner references.
- **Injection failure:** If a route matches but credential injection fails (e.g., expired GCP SA, missing Secret), the proxy returns **502** with a descriptive error body — not a silent passthrough that would result in a confusing 401/403 from the upstream.
- **Credential redaction:** All proxy log output redacts credential values (Secret data, tokens) as `[REDACTED]`. Debug logging of request/response headers strips auth header values.
