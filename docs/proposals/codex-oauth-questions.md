# Codex OAuth Provider — Design Questions

**Status:** Resolved — all decisions finalized

**Related:** [Design document](codex-oauth-design.md)

Each question has options with trade-offs and a recommendation. Go through them one by one to form the design, then update the design document.

---

## Q1: Credential type naming

The new credential type needs a name in the CRD's `CredentialType` enum. This affects the user-facing API and the proxy config wire format.

### Option A: New dedicated `codexOAuth` type
- **Pro:** Self-documenting — users immediately understand this is for Codex subscription auth
- **Pro:** Clean separation from the existing `oauth2` type (which is `client_credentials` grant)
- **Pro:** Codex-specific validation (auth.json format, `auth_mode: "chatgpt"`) belongs on its own type
- **Con:** Adds one more value to the `CredentialType` enum

**Decision:** Option A — dedicated `codexOAuth` type. The flow is sufficiently distinct (file-based bootstrap, hardcoded public client ID, Codex-specific headers, auth.json parsing) that it deserves its own type.

_Considered and rejected: Option B — extend existing `oauth2` with a `grantType` field (overloads `oauth2` with two different flows, complicates validation with conditional required fields)_

---

## Q2: Provider identity and wire format

When the operator generates the `models.providers` entry in `openclaw.json`, what provider name and API wire format should it use? Upstream OpenClaw has renamed `openai-codex` / `openai-codex-responses` to `openai` / `openai-chatgpt-responses`.

### Option C: Use a distinct provider name with the upstream wire format
- **Pro:** Avoids collision with the existing `openai` provider
- **Pro:** Uses the correct upstream wire format (`openai-chatgpt-responses`)
- **Con:** The provider name is operator-specific (upstream uses `openai` for everything, distinguishing by auth profile)

**Decision:** Option C — distinct provider name `openai-oauth` with the upstream `openai-chatgpt-responses` wire format. Upstream OpenClaw uses a single `openai` provider for both API key and OAuth, differentiating at the auth profile layer. Our operator needs a distinct key because `knownProviders` drives proxy routing, credential injection, and target domain — one entry can't serve both `api.openai.com` (bearer) and `chatgpt.com` (OAuth refresh). `openai-oauth` mirrors upstream's mental model (same provider, different auth shape) while remaining clearly distinct from the existing `openai` entry.

_Considered and rejected: Option A (legacy names may be deprecated upstream), Option B (collides with existing `openai` provider key at config, proxy route, and path prefix layers). Also considered `openai-chatgpt` and `chatgpt` as provider names; `openai-oauth` better reflects the auth-shape distinction that upstream uses._

---

## Q3: Relationship to existing `openai` credential

Users may want both Codex OAuth (for ChatGPT subscription models) and an OpenAI API key (for Platform API features like embeddings, DALL-E, or as a fallback). How should these coexist?

### Option A: Fully independent — user configures both separately
- **Pro:** Simple, explicit, no magic
- **Pro:** Each credential has its own provider entry, no ambiguity
- **Con:** User must configure two credentials for what is conceptually "OpenAI"

**Decision:** Option A — fully independent. Each credential type (`bearer` for `openai`, `codexOAuth` for `openai-oauth`) is configured separately. No companion relationship between them. The existing `openai` + `openai-codex` companion behavior is unchanged for API key users.

_Considered and rejected: Option B (companion pattern assumes shared credential, which is false for OAuth vs API key), Option C (adds complexity for marginal benefit — suppressing a companion is unnecessary when the provider names are already distinct)_

---

## Q4: OpenClaw auth-profiles.json configuration

OpenClaw's native Codex OAuth support uses `auth-profiles.json` to store OAuth tokens and `auth.order` in `openclaw.json` to route models through OAuth. Since our proxy handles all auth, we need to decide how to configure OpenClaw's provider to use the proxy-injected credentials instead.

### Option A: Configure as API-key provider (no auth-profiles.json)
- **Pro:** Simple — OpenClaw treats the Codex provider like any other API-key provider. It sends the placeholder key, proxy replaces it.
- **Pro:** No `auth-profiles.json` needed — zero risk of token leakage through config files
- **Pro:** Consistent with how all other providers work in the operator
- **Con:** OpenClaw might try to use the `openai-completions` or `openai-responses` wire format instead of `openai-chatgpt-responses` unless `api` is explicitly set

**Decision:** Option A — configure as API-key provider with placeholder key, explicit `api: "openai-chatgpt-responses"` and `baseUrl`. No `auth-profiles.json` generated. The proxy intercepts the placeholder key and injects the real OAuth token. Consistent with all other providers in the operator.

_Considered and rejected: Option B (dummy tokens in auth-profiles.json are confusing and OpenClaw may attempt to refresh them)_

---

## Q5: Network policy for auth.openai.com

The proxy needs to reach `auth.openai.com:443` to refresh tokens. The operator manages egress NetworkPolicies that restrict which domains the pod can access.

### Option A: Auto-add auth.openai.com as a builtin passthrough when codexOAuth is present
- **Pro:** Zero user config — the operator knows this is required and adds it
- **Pro:** Consistent with how GCP auto-adds `oauth2.googleapis.com`
- **Con:** None significant

**Decision:** Option A — auto-add `auth.openai.com:443` and `chatgpt.com:443` egress when any `codexOAuth` credential is present. Follows the GCP pattern.

_Considered and rejected: Option B (poor UX — forgetting causes silent token refresh failures, inconsistent with GCP behavior)_

---

## Q6: Account ID source

The `chatgpt-account-id` header requires the user's ChatGPT account ID. This value can come from multiple sources.

### Option A: Parse from auth.json at proxy startup
- **Pro:** Zero user config — the proxy reads `tokens.account_id` from the auth.json file
- **Pro:** Codex CLI always writes `account_id` to auth.json
- **Con:** If auth.json somehow lacks `account_id`, the proxy can't inject the header

**Decision:** Option A — parse `account_id` from auth.json at proxy startup. The Codex CLI always writes it. No additional CRD fields needed beyond `secretRef`. Proxy fails fast with a clear error if the field is missing.

_Considered and rejected: Option B (redundant — same info is in the Secret, bad UX to require manual extraction), Option C (over-engineered for a field that's always present)_

---

## Q7: Token vending for auth.openai.com

The GCP injector intercepts `POST oauth2.googleapis.com/token` and returns a dummy response so that Google SDK clients (which try to fetch their own tokens using placeholder ADC) get a valid-looking response. Should we do the same for `auth.openai.com`?

### Option B: No token vending — just passthrough for proxy's own refresh calls
- **Pro:** Simpler — no interception logic
- **Pro:** If we configure OpenClaw as API-key mode (Q4 decision), there are no token refresh attempts to intercept

**Decision:** Option B — no token vending. Since OpenClaw is configured in API-key mode (Q4), it won't attempt OAuth refresh. The proxy makes its own direct HTTP calls to `auth.openai.com` for refresh via `oauth2.TokenSource`, bypassing the proxy route table. No interception needed.

_Considered and rejected: Option A (unnecessary — OpenClaw doesn't know it's OAuth), Option C (over-engineered — blocking gateway from auth.openai.com requires splitting routes)_

---

## Q8: Model catalog for Codex OAuth

The operator maintains a hardcoded model catalog per provider in `knownProviders`. What models should the Codex OAuth provider expose?

### Option A: Dedicated Codex model catalog
- **Pro:** Accurate — Codex backend only supports specific models (gpt-5.5, gpt-5.4, gpt-5.4-mini, etc.)
- **Pro:** Users see the right models in the picker
- **Con:** Must be maintained as OpenAI adds/removes models

**Decision:** Option A — dedicated Codex model catalog in `knownProviders`. The catalog provides sensible defaults (3-5 models) for the model picker. Users can always add more models via `spec.config.raw`, which merges into the OpenClaw config.

_Considered and rejected: Option B (empty model picker by default, bad UX), Option C (model sets differ between Platform API and Codex backend)_

---

## Q9: Coexistence with API key OpenAI credential

Can a user have both a `bearer` credential with `provider: "openai"` (API key) and a `codexOAuth` credential with the Codex OAuth provider active simultaneously?

### Option A: Allow both — they are independent providers
- **Pro:** Users can use API key for embeddings/platform features and Codex OAuth for agent turns
- **Pro:** No collision if they use distinct provider names (per Q2)
- **Pro:** OpenClaw can use either based on model routing

**Decision:** Option A — allow both. They target different domains (`api.openai.com` vs `chatgpt.com`), use different auth mechanisms, and map to different providers. No technical reason to prevent coexistence.

_Considered and rejected: Option B (artificial restriction with no technical justification, limits flexibility)_
