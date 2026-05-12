# Web Search and Web Fetch Support

**Status:** Final
**Date:** 2026-05-12
**Decisions:** [web-search-questions.md](web-search-questions.md)

## Overview

Add operator-managed web search and web fetch configuration to the Claw CRD. OpenClaw has a built-in `web_search` agent tool backed by 12 bundled provider plugins and a `web_fetch` tool for arbitrary URL fetching. Today the operator has no way to configure either — users would have to manually edit `openclaw.json` inside the pod, which is fragile, not declarative, and doesn't integrate with the operator's security model.

This feature enables users to declare a web search provider and/or enable web fetch in the Claw CR. The operator injects the necessary configuration into `operator.json`, sets up proxy credential injection and domain allowlisting, and manages secrets — all through the same reconciliation pipeline used for credentials, channels, and MCP servers.

## Design Principles

1. **Security first:** Search API keys flow through the MITM proxy's credential injection — secrets never reach the gateway container. The proxy domain allowlist is the L7 gate. No new egress surface is opened by default.

2. **Thin operator, smart app:** The operator sets `tools.web.search.provider`, maps secrets to proxy routes, and injects a placeholder API key into the config. It does *not* replicate OpenClaw's plugin config schemas. Provider-specific tuning uses an opaque `config` field, keeping the operator decoupled from upstream changes.

3. **Consistent patterns:** Follow the same reconciliation, ConfigMap injection, proxy route, and condition patterns as credentials, channels, and MCP servers.

## Architecture

### How OpenClaw Consumes Web Search Config

OpenClaw reads web search configuration from `openclaw.json`:

```json5
{
  tools: {
    web: {
      search: {
        enabled: true,
        provider: "brave"
      },
      fetch: {
        enabled: true
      }
    }
  },
  plugins: {
    entries: {
      brave: {
        config: {
          webSearch: {
            apiKey: "ah-ah-ah-you-didnt-say-the-magic-word"
          }
        }
      }
    }
  }
}
```

Each search provider also checks specific environment variables as credential fallbacks (`BRAVE_API_KEY`, `TAVILY_API_KEY`, etc.), but the operator uses the config path with a placeholder API key — the real key is injected by the proxy.

### Provider Categories

Phase 1 supports four providers across three categories:

| Provider | Category | Proxy Route | Auth Injection | Secret Required |
|----------|----------|-------------|----------------|:---:|
| **Brave** | Standalone API | `api.search.brave.com` | `api_key` (header: `X-Subscription-Token`) | Yes |
| **Tavily** | Standalone API | `api.tavily.com` | `bearer` | Yes |
| **DuckDuckGo** | Key-free | `html.duckduckgo.com` | `none` (passthrough) | No |
| **Gemini** | LLM-as-search | *(none — reuses google credential)* | *(existing google route)* | No |

**Category 1: Standalone search APIs** (Brave, Tavily) — dedicated API key, dedicated domain. The operator adds a proxy route with credential injection. A placeholder API key is set in the config; the proxy strips it and injects the real key.

**Category 2: Key-free** (DuckDuckGo) — no API key. The operator adds a passthrough proxy route and sets the provider in config. Zero secrets.

**Category 3: LLM-as-search** (Gemini) — reuses existing LLM provider credentials. Gemini search grounding sends a regular Gemini API call with `tools: [{ google_search }]`. Traffic flows through the existing `google` provider credential route. The operator only sets `tools.web.search.provider: "gemini"` — no new proxy route or secret.

### Security Model

**Search API keys** are handled via proxy credential injection (same as LLM providers):

1. The user's Secret is mounted as a `secretKeyRef` env var on the **proxy** container (not the gateway), using `credEnvVarName("websearch")` → `CRED_WEBSEARCH`
2. The operator adds a proxy route for the search domain with the appropriate injector (`api_key` or `bearer`) referencing that env var
3. The operator sets a static placeholder API key (`"ah-ah-ah-you-didnt-say-the-magic-word"`) in the gateway config via `plugins.entries.<provider>.config.webSearch.apiKey`
4. OpenClaw sees a non-empty key and makes the HTTP call through the proxy
5. The MITM proxy intercepts the request, strips the placeholder auth header, and injects the real credential from `CRED_WEBSEARCH`
6. The request reaches the upstream with the real key

The gateway container **never sees the real API key**.

The search secret's `ResourceVersion` is stamped on the proxy pod template (alongside existing credential stamps) to trigger rollouts when the search API key Secret changes.

**Web fetch** is gated by the proxy allowlist. When `spec.webFetch.enabled: true`, the operator sets `tools.web.fetch.enabled: true` in the config. The agent can only fetch URLs on domains already permitted by the proxy (LLM providers, search APIs, builtin passthroughs). Users can open additional domains via `spec.credentials` entries with `type: none`.

### Reconciliation Flow

```
Claw CR spec.webSearch / spec.webFetch
         │
         ▼
  validateWebSearchConfig()             ◄── validate provider name
         │                                   check secret exists (API-keyed)
         │                                   check google credential exists (gemini)
         │                                   set WebSearchConfigured condition
         │
         ▼
  applyProxyResources()
         ├─► generateProxyConfig()      ◄── add search domain as credential
         │                                   injection route (brave, tavily)
         │                                   or passthrough (duckduckgo)
         │                                   webSearch is passed alongside
         │                                   credentials and mcpServers
         │
         ▼
  buildKustomizedObjects()
         │
         ▼
  configureDeployments()
         ├─► configureProxyForWebSearch() ◄── mount search secret as
         │                                     CRED_WEBSEARCH env var on
         │                                     proxy container (secretKeyRef)
         │
         ▼
  stampSecretVersionAnnotation()        ◄── includes web search secret
         │                                   in proxy pod template stamps
         │
         ▼
  enrichConfigAndNetworkPolicy()
         ├─► injectWebSearchIntoConfigMap() ◄── operator.json:
         │                                       tools.web.search.provider
         │                                       tools.web.fetch.enabled
         │                                       plugins.entries.<id>.config.webSearch
         │
         ▼
  merge.js (init-config)                ◄── merged into PVC openclaw.json
```

## CRD Schema

### New Fields on ClawSpec

```go
type ClawSpec struct {
    ConfigMode  ConfigMode            `json:"configMode,omitempty"`
    Credentials []CredentialSpec      `json:"credentials,omitempty"`
    McpServers  map[string]McpServerSpec `json:"mcpServers,omitempty"`

    // WebSearch configures the web search provider for the OpenClaw agent.
    // +optional
    WebSearch *WebSearchSpec `json:"webSearch,omitempty"`

    // WebFetch enables the web_fetch tool for arbitrary URL fetching.
    // Fetched URLs are gated by the proxy allowlist — only domains
    // permitted by credentials, search providers, or builtins are reachable.
    // +optional
    WebFetch *WebFetchSpec `json:"webFetch,omitempty"`
}
```

### WebSearchSpec

```go
// WebSearchSpec configures the operator-managed web search provider.
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
```

### WebFetchSpec

```go
// WebFetchSpec configures the web_fetch tool.
type WebFetchSpec struct {
    // Enabled activates the web_fetch tool. Fetched URLs are gated by
    // the proxy allowlist.
    // +kubebuilder:default=true
    Enabled bool `json:"enabled"`
}
```

### CEL Validation

On `WebSearchSpec`:
- `secretRef` is required when `provider` is `brave` or `tavily`

```go
// +kubebuilder:validation:XValidation:rule="self.provider in ['duckduckgo','gemini'] || has(self.secretRef)",message="secretRef is required for API-keyed search providers"
```

Note: we intentionally do *not* reject `secretRef` on key-free/LLM-as-search providers. The operator ignores it for those providers, but refusing it at admission time would mean hard-coding the provider categorization into the CEL rule, making it fragile when adding new providers. The reconciler validation (below) already handles the semantics per category.

### Reconciler Validation

- **Brave, Tavily:** Secret referenced by `secretRef` must exist and contain the specified key
- **Gemini:** A credential with `provider: "google"` must exist in `spec.credentials`
- **DuckDuckGo:** No validation needed
- Failures set `WebSearchConfigured=False` with a descriptive message and `Ready=False`

## Proxy Configuration

For API-keyed providers, the operator adds the search domain as a credential injection route in the proxy config. This is handled alongside existing credential routes in `generateProxyConfig`.

```go
var knownSearchProviders = map[string]searchProviderInfo{
    "brave": {
        Domain:   "api.search.brave.com",
        Injector: "api_key",
        Header:   "X-Subscription-Token",
    },
    "tavily": {
        Domain:   "api.tavily.com",
        Injector: "bearer",
    },
    "duckduckgo": {
        Domain:   "html.duckduckgo.com",
        Injector: "none",
    },
    // gemini: no entry — reuses existing google provider credential route
}
```

**Proxy deployment changes:** The search secret is mounted on the proxy container (not the gateway) as a `secretKeyRef` env var named `CRED_WEBSEARCH` (using the existing `credEnvVarName` helper with `"websearch"` as the credential name). The proxy reads this env var at runtime to inject the real credential. This is done by a new `configureProxyForWebSearch` function, following the same pattern as `configureProxyForCredentials`.

**Secret version stamping:** `stampSecretVersionAnnotation` must also stamp the web search secret's `ResourceVersion` on the proxy pod template, so that Secret changes trigger a proxy rollout. This requires extending the function to also check `spec.webSearch.secretRef` alongside `spec.credentials`.

**`generateProxyConfig` signature:** The function currently takes `(credentials []resolvedCredential, mcpServers map[string]McpServerSpec)`. It needs to also accept `webSearch *WebSearchSpec` (or the full `ClawSpec`) to emit the search domain routes. The search provider route is appended to the routes list alongside credential routes, using the `knownSearchProviders` mapping table above.

## ConfigMap Injection

`injectWebSearchIntoConfigMap` sets up to three blocks in `operator.json`:

1. **`tools.web.search`** — provider selection:
   ```json
   { "enabled": true, "provider": "brave" }
   ```

2. **`plugins.entries.<provider>.config.webSearch`** — placeholder API key + user config (API-keyed providers only):
   ```json
   { "apiKey": "ah-ah-ah-you-didnt-say-the-magic-word" }
   ```
   Only injected for Brave and Tavily. User-provided `spec.webSearch.config` is deep-merged into this block. For **DuckDuckGo**, no plugin entry is needed (key-free). For **Gemini**, no `apiKey` is injected — the Gemini search provider falls back to `models.providers.google.apiKey` which the operator already sets for the google LLM provider. If the user provides `spec.webSearch.config` for Gemini, it is merged into `plugins.entries.google.config.webSearch` (OpenClaw's google extension id) for provider-specific tuning (model, baseUrl, etc.).

3. **`tools.web.fetch`** — when `spec.webFetch` is set:
   ```json
   { "enabled": true }
   ```

This follows the same pattern as `injectChannelsIntoConfigMap`, which sets both `channels` and `plugins.entries`.

## Status Condition

New `WebSearchConfigured` condition type:

- `True` — search provider validated and config injected
- `False` — validation failed (missing secret, unknown provider, missing google credential for gemini)
- Not set when `spec.webSearch` is nil

Failures also set `Ready=False`.

## Examples

### Brave Search (API key, proxy-injected)

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: my-instance
spec:
  credentials:
    - name: anthropic
      type: apiKey
      secretRef:
        - name: anthropic-api-key
          key: api-key
      provider: anthropic
  webSearch:
    provider: brave
    secretRef:
      name: brave-search-key
      key: api-key
  webFetch:
    enabled: true
```

### DuckDuckGo (key-free)

```yaml
spec:
  webSearch:
    provider: duckduckgo
```

### Gemini Search Grounding (reuses LLM credential)

```yaml
spec:
  credentials:
    - name: google
      type: apiKey
      secretRef:
        - name: google-api-key
          key: api-key
      provider: google
  webSearch:
    provider: gemini
```

### Tavily with custom config

```yaml
spec:
  webSearch:
    provider: tavily
    secretRef:
      name: tavily-key
      key: api-key
    config:
      maxResults: 10
```

### Web fetch only (no search)

```yaml
spec:
  credentials:
    - name: docs-site
      type: none
      domain: docs.python.org
  webFetch:
    enabled: true
```

## Implementation Plan

### Files to Change

| File | Change |
|------|--------|
| `api/v1alpha1/claw_types.go` | Add `WebSearchSpec`, `WebFetchSpec`, new fields on `ClawSpec`, `WebSearchConfigured` condition constant, CEL validation |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerated (`make generate`) |
| `config/crd/bases/` | Regenerated CRD YAML (`make manifests`) |
| New: `internal/controller/claw_web_search.go` | `validateWebSearchConfig`, `injectWebSearchIntoConfigMap`, `configureProxyForWebSearch` (mount search secret on proxy), known provider mapping table (`knownSearchProviders`) |
| `internal/controller/claw_proxy.go` | Extend `generateProxyConfig` to accept `*WebSearchSpec` and emit search domain routes; extend `stampSecretVersionAnnotation` to also stamp `spec.webSearch.secretRef` |
| `internal/controller/claw_resource_controller.go` | Wire validation into reconcile loop, call `configureProxyForWebSearch` in `configureDeployments`, call `injectWebSearchIntoConfigMap` in `enrichConfigAndNetworkPolicy`, pass `webSearch` to `generateProxyConfig` |
| New: `internal/controller/claw_web_search_test.go` | Tests for validation, ConfigMap injection, proxy route generation, proxy deployment mounting |
| `docs/provider-setup.md` | User-facing documentation for web search and web fetch setup |

### Steps

1. Add CRD types (`WebSearchSpec`, `WebFetchSpec`, condition constant, CEL rule) and run `make manifests generate`
2. Create `claw_web_search.go` with `knownSearchProviders` mapping, `validateWebSearchConfig`, `configureProxyForWebSearch`, and `injectWebSearchIntoConfigMap`
3. Extend `generateProxyConfig` to accept `*WebSearchSpec` and emit search domain routes
4. Extend `stampSecretVersionAnnotation` to stamp the web search secret's `ResourceVersion`
5. Wire into reconciler: validation → proxy config (with web search) → deployment mounting → secret stamping → ConfigMap injection → condition
6. Tests (validation, ConfigMap injection, proxy routes, proxy deployment mounting, secret version stamping)
7. Documentation

## Future Considerations

- Additional search providers (Exa, Firecrawl, Perplexity, Grok) — one table entry each
- Firecrawl as a `webFetch` provider for JS-heavy pages
- SearXNG support (self-hosted, requires base URL and potentially a sidecar)
