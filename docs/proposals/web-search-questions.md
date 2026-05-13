# Web Search Support — Design Questions

**Status:** All decisions made
**Related:** [Design document](web-search-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

---

## Q1: How should search API keys be delivered to the gateway?

OpenClaw's `web_search` tool makes HTTP calls directly from the gateway process (Node.js), reading the API key from environment variables (e.g., `BRAVE_API_KEY`) or from `plugins.entries.<provider>.config.webSearch.apiKey` in config. This is different from LLM provider traffic, which flows through the MITM proxy for credential injection.

The key tension: our security model keeps secrets off the gateway container whenever possible (the proxy injects them), but OpenClaw's search implementation reads the key from the gateway's own env/config.

### Option B: Proxy credential injection (secret stays on proxy)

Add the search provider's domain as a credential route in the proxy config. The proxy injects the API key header on outbound requests. Set a placeholder value in `plugins.entries.<provider>.config.webSearch.apiKey` so OpenClaw thinks it has a key and makes the call.

- **Pro:** Consistent with the operator's core security model. Secret never touches the gateway.
- **Pro:** The proxy's L7 allowlist already controls which domains are reachable — adding credential injection for the search domain is natural.
- **Pro:** Source code analysis confirms all major providers use header-based auth that the proxy can inject:
  - Brave: `X-Subscription-Token` header → `api_key` injector
  - Tavily: `Authorization: Bearer` header (via `postTrustedWebToolsJson`) → `bearer` injector
  - Exa, Firecrawl, Perplexity: `Authorization: Bearer` header → `bearer` injector
- **Pro:** LLM-as-search providers (Gemini, Grok) reuse existing LLM credentials already proxy-injected — no new handling needed.

**Decision:** Option B — proxy credential injection for API-keyed providers. Source analysis confirmed all target providers use header-based auth compatible with the proxy's existing `api_key` and `bearer` injectors. Secrets stay off the gateway, consistent with the operator's core security model. LLM-as-search providers (Gemini) need no new secret handling since their LLM credential is already proxy-injected.

_Considered and rejected: Option A — gateway env var (breaks "no secrets on gateway" principle, unnecessary given header-based auth works), Option C — hybrid (two code paths for no benefit since all providers are header-injectable)_

---

## Q2: Which providers should the operator support in phase 1?

The operator needs a known-provider mapping table (provider name → API domain, env var name, injector type). Supporting all 12 OpenClaw providers upfront is a large surface. We need to pick a useful initial set.

### Option: Brave + Tavily + DuckDuckGo + Gemini (four providers)

Covers three categories with minimal effort:

| Provider | Category | New proxy route | New secret | Effort |
|----------|----------|:-:|:-:|--------|
| Brave | Standalone API | Yes (`api.search.brave.com`) | Yes (proxy `api_key`) | Medium |
| Tavily | Standalone API | Yes (`api.tavily.com`) | Yes (proxy `bearer`) | Medium |
| DuckDuckGo | Key-free | Yes (`html.duckduckgo.com`) | No | Low |
| Gemini | LLM-as-search | No (reuses google credential) | No | Low |

- **Pro:** Covers the most common use cases: Brave (#1 popularity), Tavily (#2, popular in AI agent frameworks), DuckDuckGo (free fallback), Gemini (free for existing google credential users).
- **Pro:** Gemini is nearly zero implementation cost — pure config injection, no new proxy route or secret. Only needs cross-field validation that a `google` provider credential exists.
- **Pro:** Demonstrates all three provider categories, making it easy to add more providers later.

**Decision:** Brave + Tavily + DuckDuckGo + Gemini. Covers standalone APIs, key-free, and LLM-as-search categories. Additional providers (Exa, Firecrawl, Perplexity, Grok) are trivial to add later — one table entry each for standalone APIs, or the same config-only pattern as Gemini for LLM-as-search.

_Considered and rejected: Option A — Brave + DuckDuckGo only (misses Tavily and Gemini), Option D — all 12 providers (unnecessary scope, many are niche)_

---

## Q3: Should search domains get proxy passthrough or credential injection?

The MITM proxy controls which domains the gateway can reach. When adding a search provider's domain, we choose between a simple passthrough (no credential manipulation) and a credential injection route.

This follows directly from Q1. Since we chose proxy credential injection for API keys, the proxy must do credential injection (not just passthrough) for API-keyed providers.

### Option B: Credential injection on proxy

The proxy injects the search API key into outbound requests to the search domain. Per-provider behavior:

- **Brave:** `api_key` injector with `header: "X-Subscription-Token"` on `api.search.brave.com`
- **Tavily:** `bearer` injector on `api.tavily.com`
- **DuckDuckGo:** `none` injector (passthrough) on `html.duckduckgo.com` — key-free
- **Gemini:** No new route — traffic already covered by existing `google` provider credential on `.googleapis.com`

- **Pro:** Secret stays off gateway. Consistent with Q1 decision.
- **Pro:** Source analysis confirms all target providers use header-based auth the proxy already supports.
- **Pro:** Proxy acts as both L7 domain gate and credential injector — single enforcement point.

**Decision:** Option B — credential injection for API-keyed providers (Brave, Tavily), passthrough for key-free (DuckDuckGo), no new route for LLM-as-search (Gemini). Direct consequence of Q1 decision.

_Considered and rejected: Option A — passthrough only (would require putting the secret on the gateway as an env var, contradicts Q1 decision)_

---

## Q4: Single web search provider or allow a list?

OpenClaw supports configuring one `tools.web.search.provider` at a time (with auto-detection fallback when no provider is explicitly set). Should the operator mirror this or allow multiple?

### Option A: Single provider (`spec.webSearch` is a struct)

```yaml
spec:
  webSearch:
    provider: brave
    secretRef: ...
```

- **Pro:** Matches OpenClaw's runtime behavior — only one provider is active at a time.
- **Pro:** Simpler CRD, simpler validation, simpler implementation.
- **Pro:** Clear intent — the user knows exactly which provider will be used.
- **Con:** Can't configure a fallback provider (e.g., Brave primary, DuckDuckGo fallback). OpenClaw's auto-detect fallback chain only works when `provider` is not explicitly set.

**Decision:** Option A — single provider struct. Matches OpenClaw's one-active-provider model. Deterministic: the user declares a provider and that's what gets used. Simple CRD and validation.

_Considered and rejected: Option B — ordered list (OpenClaw only accepts one provider value; operator-side fallback adds complexity for no runtime benefit), Option C — auto-detect fallback flag (over-engineering; auto-detect is OpenClaw's default when no provider is set, not something the operator should toggle)_

---

## Q5: Dedicated status condition or reuse existing?

The operator uses status conditions to signal feature-specific health. Should web search get its own condition?

### Option A: New `WebSearchConfigured` condition

- **Pro:** Consistent with `CredentialsResolved`, `ProxyConfigured`, `McpServersConfigured`. Clear signal for web search issues.
- **Pro:** Condition is only present when `spec.webSearch` is set, reducing noise.

**Decision:** Option A — new `WebSearchConfigured` condition. Follows the established pattern. Only present when the feature is configured, so no noise for users who don't use web search. Consistent with `McpServersConfigured`.

_Considered and rejected: Option B — fold into CredentialsResolved (conflates LLM credential and search secret failures), Option C — no dedicated condition (no way to distinguish search issues from other problems)_

---

## Q6: Should the operator validate that LLM-as-search providers have a matching credential?

When a user sets `provider: gemini`, the operator could check that a `google` provider credential exists in `spec.credentials`. Without it, Gemini search grounding won't work at runtime (no API key available). Validation requirements vary by provider category:

- **API-keyed** (Brave, Tavily): validate `secretRef` is set and the referenced Secret exists
- **LLM-as-search** (Gemini): validate that the corresponding LLM provider credential exists in `spec.credentials`
- **Key-free** (DuckDuckGo): no validation needed — no secret, no dependency

### Option A: Validate and fail

Check requirements per category. Set `WebSearchConfigured=False` if missing.

- **Pro:** Fails fast with a clear message. User doesn't have to debug why search silently doesn't work.
- **Pro:** The operator already validates credentials — this is a natural extension.
- **Pro:** Cross-reference is trivial: a static map (`gemini → google`, future `grok → xai`).

**Decision:** Option A — validate and fail. Per-category validation: `secretRef` existence for API-keyed providers, LLM credential cross-reference for LLM-as-search providers, no validation for key-free providers. Fail-fast with `WebSearchConfigured=False` is consistent with how `resolveCredentials` works today.

_Considered and rejected: Option B — validate and warn (soft failures are easy to miss), Option C — don't validate (silent runtime failure when Gemini has no google credential)_

---

## Q7: How should `web.fetch` be handled?

OpenClaw has a separate `web_fetch` tool (lightweight URL fetching, distinct from `web_search`). It can optionally use Firecrawl as a provider. NemoClaw enables `web.fetch` alongside search. Should the operator configure `tools.web.fetch` as well?

### Option B: Separate `spec.webFetch` field (designed and implemented now)

Add a dedicated `spec.webFetch` field alongside `spec.webSearch`, keeping the two concerns cleanly separated in the CRD while implementing both in the same change.

- **Pro:** Clean separation of concerns. Each has distinct security characteristics (known API endpoints vs. arbitrary URLs).
- **Pro:** Users can enable fetch without search and vice versa.
- **Pro:** Implementing alongside search reuses the same ConfigMap injection pattern and proxy infrastructure.

**Decision:** Option B — separate `spec.webFetch` field, designed and implemented alongside web search. Clean CRD separation, but delivered in the same change.

_Considered and rejected: Option A — enable fetch implicitly alongside search (conflates security profiles; users may want search without arbitrary URL access), Option C — boolean toggle on webSearch spec (same conflation problem)_

---

## Q8: What should `spec.webFetch` look like?

`web_fetch` in OpenClaw allows agents to fetch arbitrary URLs. Unlike `web_search` (which calls a known API endpoint), `web_fetch` can target any URL the agent provides. This has significant security implications since all outbound traffic goes through the MITM proxy, which acts as an L7 domain allowlist.

### Option A: Simple boolean toggle

```yaml
spec:
  webFetch:
    enabled: true
```

The operator sets `tools.web.fetch.enabled: true` in `operator.json`. No new proxy routes — `web_fetch` uses OpenClaw's built-in HTTP client, which goes through the proxy. Requests to domains not in the proxy allowlist will be blocked (403).

- **Pro:** Simplest possible design. One field.
- **Pro:** The proxy allowlist is the security gate — only domains already permitted (LLM providers, search APIs, builtin passthroughs) are fetchable.
- **Con:** Very limited usefulness — the agent can only fetch URLs on domains already allowed for other reasons. Can't fetch arbitrary documentation sites, GitHub issues, etc.

**Decision:** Option A — simple boolean toggle. The proxy allowlist already controls reachable domains, and users can add passthrough domains via existing `spec.credentials` entries with `type: none`. Firecrawl support can be added later as a provider option.

_Considered and rejected: Option B — Firecrawl provider (scope creep, can be added later), Option C — allowedDomains list (overlaps with existing `credentials` type: none pattern)_

---

## Q9: How should the placeholder API key work for proxy credential injection?

With Q1 decided as proxy credential injection, the gateway needs *something* in its config that looks like a valid API key — otherwise OpenClaw won't attempt the search call at all. But the real key lives on the proxy, not the gateway.

### Option A: Static placeholder string

Set `plugins.entries.<provider>.config.webSearch.apiKey` to a fixed placeholder like `"proxy-injected"`. The gateway sees a non-empty key, makes the HTTP call, and the proxy strips the placeholder and injects the real key.

- **Pro:** Simple. Same pattern used for LLM provider `apiKey: "ah-ah-ah-you-didnt-say-the-magic-word"` in `injectProvidersIntoConfigMap`.
- **Pro:** Proxy already handles this — it replaces whatever auth header it finds with the real credential.
- **Con:** If OpenClaw ever validates key format (e.g., checks for a `BSA` prefix on Brave keys), the placeholder might fail client-side validation.

**Decision:** Option A — static placeholder string `"ah-ah-ah-you-didnt-say-the-magic-word"`, matching the existing LLM provider pattern. The proxy strips and replaces regardless of placeholder format.

_Considered and rejected: Option B — provider-specific placeholder format (unnecessary; the proxy strips and replaces regardless of what the gateway sends)_
