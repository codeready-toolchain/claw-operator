# ADR-0009: Web Search and Web Fetch Support

**Status:** Implemented
**Date:** 2026-05-12

## Overview

Add operator-managed web search and web fetch configuration to the Claw CRD. OpenClaw has a built-in `web_search` agent tool backed by 12 bundled provider plugins and a `web_fetch` tool for arbitrary URL fetching. Today the operator has no way to configure either — users would have to manually edit `openclaw.json` inside the pod, which is fragile, not declarative, and doesn't integrate with the operator's security model.

This feature enables users to declare a web search provider and/or enable web fetch in the Claw CR. The operator injects the necessary configuration into `operator.json`, sets up proxy credential injection and domain allowlisting, and manages secrets — all through the same reconciliation pipeline used for credentials, channels, and MCP servers.

## Design Principles

1. **Security first:** Search API keys flow through the MITM proxy's credential injection — secrets never reach the gateway container. The proxy domain allowlist is the L7 gate. No new egress surface is opened by default.

2. **Thin operator, smart app:** The operator sets `tools.web.search.provider`, maps secrets to proxy routes, and injects a placeholder API key into the config. It does *not* replicate OpenClaw's plugin config schemas. Provider-specific tuning uses an opaque `config` field, keeping the operator decoupled from upstream changes.

3. **Consistent patterns:** Follow the same reconciliation, ConfigMap injection, proxy route, and condition patterns as credentials, channels, and MCP servers.

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | How should search API keys be delivered to the gateway? | Proxy credential injection (secret stays on proxy) | All target providers use header-based auth compatible with existing `api_key` and `bearer` injectors. Keeps secrets off the gateway, consistent with the operator's core security model. LLM-as-search providers need no new secret handling. |
| 2 | Which providers should the operator support in phase 1? | Brave + Tavily + DuckDuckGo + Gemini | Covers standalone APIs, key-free, and LLM-as-search categories. Additional providers are trivial to add later — one table entry each. |
| 3 | Should search domains get proxy passthrough or credential injection? | Credential injection for API-keyed, passthrough for key-free, no new route for LLM-as-search | Direct consequence of Q1. Proxy acts as both L7 domain gate and credential injector — single enforcement point. |
| 4 | Single web search provider or allow a list? | Single provider struct (`spec.webSearch`) | Matches OpenClaw's one-active-provider model. Deterministic: the user declares a provider and that's what gets used. |
| 5 | Dedicated status condition or reuse existing? | New `WebSearchConfigured` condition | Follows established pattern. Only present when the feature is configured, so no noise for non-users. |
| 6 | Should the operator validate LLM-as-search providers have a matching credential? | Validate and fail with `WebSearchConfigured=False` | Fail-fast with a clear message. Per-category: `secretRef` existence for API-keyed, LLM credential cross-reference for LLM-as-search, none for key-free. |
| 7 | How should `web.fetch` be handled? | Separate `spec.webFetch` field | Clean separation of concerns. Each has distinct security characteristics. Users can enable fetch without search and vice versa. |
| 8 | What should `spec.webFetch` look like? | Simple boolean toggle | Proxy allowlist already controls reachable domains. Users add passthrough domains via `spec.credentials` with `type: none`. |
| 9 | How should the placeholder API key work? | Static placeholder `"ah-ah-ah-you-didnt-say-the-magic-word"` | Same pattern as LLM providers. Proxy strips and replaces regardless of placeholder format. |

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
3. The operator sets a static placeholder API key in the gateway config via `plugins.entries.<provider>.config.webSearch.apiKey`
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

```yaml
spec:
  webSearch:
    provider: brave           # required; known: brave, tavily, duckduckgo, gemini
    secretRef:                # required for API-keyed providers (brave, tavily)
      name: brave-search-key
      key: api-key
    config: {}                # optional; provider-specific tuning (merged into plugins.entries)
  webFetch:
    enabled: true             # activates web_fetch tool; gated by proxy allowlist
```

### CEL Validation

On `WebSearchSpec`:
- `secretRef` is required when `provider` is `brave` or `tavily`

The operator intentionally does *not* reject `secretRef` on key-free/LLM-as-search providers. Hard-coding provider categorization into CEL rules would be fragile when adding new providers. The reconciler validation handles the semantics per category.

### Reconciler Validation

- **Brave, Tavily:** Secret referenced by `secretRef` must exist and contain the specified key
- **Gemini:** A credential with `provider: "google"` must exist in `spec.credentials`
- **DuckDuckGo:** No validation needed
- Failures set `WebSearchConfigured=False` with a descriptive message and `Ready=False`

## Proxy Configuration

For API-keyed providers, the operator adds the search domain as a credential injection route in the proxy config. Known provider mapping:

| Provider | Domain | Injector | Header |
|----------|--------|----------|--------|
| Brave | `api.search.brave.com` | `api_key` | `X-Subscription-Token` |
| Tavily | `api.tavily.com` | `bearer` | — |
| DuckDuckGo | `html.duckduckgo.com` | `none` | — |
| Gemini | *(no entry — reuses existing google route)* | — | — |

The search secret is mounted on the proxy container as a `secretKeyRef` env var named `CRED_WEBSEARCH`. `stampSecretVersionAnnotation` also stamps the web search secret's `ResourceVersion` on the proxy pod template to trigger rollouts on Secret changes.

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
   Only injected for Brave and Tavily. User-provided `spec.webSearch.config` is deep-merged. For DuckDuckGo, no plugin entry is needed. For Gemini, no `apiKey` is injected — the Gemini search provider falls back to `models.providers.google.apiKey` already set for the google LLM provider. User-provided config for Gemini is merged into `plugins.entries.google.config.webSearch`.

3. **`tools.web.fetch`** — when `spec.webFetch` is set:
   ```json
   { "enabled": true }
   ```

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

## Future Considerations

- Additional search providers (Exa, Firecrawl, Perplexity, Grok) — one table entry each
- Firecrawl as a `webFetch` provider for JS-heavy pages
- SearXNG support (self-hosted, requires base URL and potentially a sidecar)
