# Memory Search Proxy Routing

**Status:** Final

---

## Overview

OpenClaw's memory search feature uses embeddings for semantic recall across agent sessions. By default it expects an OpenAI API key directly accessible to the gateway container. In our operator's architecture, real credentials live only in the proxy sidecar — the gateway container never sees them. This causes memory search to fail with "No API key found for provider openai".

This design adds automatic memory search configuration to the operator's config enrichment pipeline, routing embedding requests through the existing credential-injecting proxy.

---

## Design Principles

1. **Zero-config for users** — If a user has a credential that supports embeddings, memory search works automatically.
2. **Proxy-first** — All credential-bearing traffic routes through the proxy. No API keys leak into the gateway container.
3. **User-overridable** — Users who set `agents.defaults.memorySearch` in `spec.config.raw` own it completely; the operator skips injection.
4. **Fail gracefully** — If no embedding-capable provider is configured, memory search is explicitly disabled rather than erroring at runtime.

---

## Architecture

### Data Flow

```
Gateway container
  └── memory_search tool invoked
       └── reads agents.defaults.memorySearch.provider (e.g. "openai" or "gemini")
       └── native adapter resolves API key from models.providers.<id>.apiKey (placeholder)
       └── adapter calls upstream URL (e.g. https://api.openai.com/v1/embeddings)
            └── Node.js fetch honors HTTPS_PROXY env var
                 └── request routes to proxy sidecar (localhost:8080)
                      └── proxy MITM matches domain, strips placeholder auth, injects real credentials
                           └── forwards to upstream
```

The `injectProviders` function already writes `models.providers.<id> = { baseUrl, apiKey: "placeholder", ... }` for each credential. The native adapters (openai, gemini) read apiKey from `models.providers.<id>.apiKey` and make requests to their hardcoded upstream URLs. Since the gateway has `HTTPS_PROXY` set to the proxy sidecar, all outbound HTTPS goes through the proxy regardless — the proxy matches by domain and injects real credentials via MITM.

---

## Core Concepts

### Embedding-Capable Providers

A new field `EmbeddingAdapter` on `providerDefaults` maps our credential provider names to OpenClaw's memory search adapter IDs:

| Credential Provider | Cred Type    | OpenClaw Adapter ID | Default Model (handled by OpenClaw) |
|--------------------|-------------|--------------------|------------------------------------|
| `openai`           | `bearer`    | `"openai"`         | `text-embedding-3-small`           |
| `google`           | `apiKey`    | `"gemini"`         | `gemini-embedding-001`             |

Providers without embedding support (`anthropic`, `xai`) are not eligible. Google credentials with `type: gcp` (Vertex AI) are also excluded — the `gemini` adapter expects API key auth to `generativelanguage.googleapis.com`, not Vertex AI OAuth2 tokens. Only `type: apiKey` Google credentials are eligible.

### Provider Selection

First embedding-capable credential in `spec.credentials` order wins. This matches the existing "first credential is primary model" pattern.

### User Override

If `agents.defaults.memorySearch` exists in the merged config (after `deepMerge(operatorTemplate, userRawConfig)`), the operator does not inject any memory search config. Since the operator's base template (`operator.json`) does not contain `memorySearch`, its presence in the merged config means the user put it there via `spec.config.raw`. The user owns it completely — they can point to a local model, a custom endpoint, or disable it.

### No-Provider Behavior

When no embedding-capable credential exists AND the user hasn't provided their own `memorySearch` config, the operator injects `memorySearch.enabled: false` to suppress noisy runtime errors.

---

## Implementation Plan

### 1. Extend `providerDefaults` with embedding info

Add an `EmbeddingAdapter` field to the `providerDefaults` struct:

```go
type providerDefaults struct {
    // ... existing fields ...
    EmbeddingAdapter string // OpenClaw memory search adapter ID (empty = no embedding support)
}
```

Set it for `openai` (`"openai"`) and `google` (`"gemini"`).

### 2. Add `injectMemorySearch` function

New function in the enrichment pipeline (called after `injectProviders`):

```go
func injectMemorySearch(config map[string]any, instance *clawv1alpha1.Claw) {
    // Check user override: if agents.defaults.memorySearch exists in merged config, skip.
    // Since operator.json template has no memorySearch, its presence means user set it.
    if userHasMemorySearchConfig(config) {
        return
    }

    // Find first embedding-capable credential.
    // GCP-type credentials (Vertex AI) are excluded — the gemini adapter expects
    // API key auth, not Vertex AI OAuth2 tokens.
    for _, cred := range instance.Spec.Credentials {
        if cred.Type == clawv1alpha1.CredentialTypeGCP {
            continue
        }
        if defaults, ok := knownProviders[cred.Provider]; ok && defaults.EmbeddingAdapter != "" {
            setNestedValue(config, defaults.EmbeddingAdapter,
                "agents", "defaults", "memorySearch", "provider")
            return
        }
    }

    // No embedding provider found — disable to suppress runtime errors
    setNestedValue(config, false, "agents", "defaults", "memorySearch", "enabled")
}
```

### 3. Wire into enrichment pipeline

Add the call in `enrichConfigAndNetworkPolicy` after `injectProviders`:

```go
injectModelCatalog(config, instance)
injectMemorySearch(config, instance)  // NEW
```

### 4. Unit tests

- OpenAI credential (type: bearer) → `memorySearch.provider: "openai"` injected
- Google credential (type: apiKey) → `memorySearch.provider: "gemini"` injected
- Anthropic-only → `memorySearch.enabled: false` injected
- Google credential (type: gcp / Vertex AI) → skipped, `memorySearch.enabled: false`
- Multiple credentials (Anthropic + OpenAI) → first embedding-capable wins (OpenAI)
- User raw config has `memorySearch` → operator skips injection entirely
- User raw config has `memorySearch.enabled: false` → operator skips (respects user)

---

## Custom Providers

Custom providers (`spec.customProviders`) are not auto-selected for memory search. Users with custom embedding endpoints (vLLM, Ollama, LiteLLM) configure via `spec.config.raw`:

```yaml
spec:
  config:
    raw:
      agents:
        defaults:
          memorySearch:
            provider: "openai-compatible"
            model: "my-embedding-model"
            remote:
              baseUrl: "http://my-endpoint/v1"
              apiKey: "placeholder"
```

This triggers the user-override skip behavior — the operator leaves their config untouched.
