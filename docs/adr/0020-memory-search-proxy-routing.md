# ADR-0020: Memory Search Proxy Routing

**Status:** Implemented
**Date:** 2026-06-01

---

## Overview

OpenClaw's memory search feature uses embeddings for semantic recall across agent sessions. By default it expects an OpenAI API key directly accessible to the gateway container. In the operator's architecture, real credentials live only in the proxy sidecar — the gateway container never sees them. This causes memory search to fail with "No API key found for provider openai".

This decision adds automatic memory search configuration to the operator's config enrichment pipeline, routing embedding requests through the existing credential-injecting proxy.

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

## Decisions

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | Which credential becomes the embedding provider? | First embedding-capable credential in `spec.credentials` order wins | Simple, deterministic, consistent with existing primary model selection pattern. Users who care can reorder credentials. |
| 2 | What happens when the user already has `memorySearch` in their raw config? | Skip injection entirely — user owns it completely | Cleanest escape hatch. Matches the "don't fight the user" principle. |
| 3 | Which OpenClaw embedding adapter ID to use? | Provider-native adapter IDs (`openai`, `gemini`) | OpenClaw handles model defaults, API format, and error handling natively. No need to specify model. Gemini's non-OpenAI-compatible embedding API works correctly. |
| 4 | Behavior when no embedding-capable provider is configured? | Inject `memorySearch.enabled: false` when no embedding provider exists and user hasn't configured their own | Eliminates noisy runtime errors for the common "Anthropic-only" case while preserving the user override escape hatch. |
| 5 | Should custom providers be eligible for memory search? | No automatic selection; users configure via `spec.config.raw` | Safe default. Users with custom embedding endpoints (vLLM, Ollama, LiteLLM) configure manually, which triggers the skip behavior. Can expand to auto-selection later if demanded. |

---

## Embedding-Capable Providers

| Credential Provider | Cred Type | OpenClaw Adapter ID | Default Model (handled by OpenClaw) |
|--------------------|-----------|--------------------|------------------------------------|
| `openai`           | `bearer`  | `"openai"`         | `text-embedding-3-small`           |
| `google`           | `apiKey`  | `"gemini"`         | `gemini-embedding-001`             |

Providers without embedding support (`anthropic`, `xai`) are not eligible. Google credentials with `type: gcp` (Vertex AI) are also excluded — the `gemini` adapter expects API key auth to `generativelanguage.googleapis.com`, not Vertex AI OAuth2 tokens.

---

## Custom Providers

Custom providers (`spec.customProviders`) are not auto-selected for memory search. Users with custom embedding endpoints configure via `spec.config.raw`:

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
