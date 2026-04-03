## Context

OpenClaw currently deploys an Nginx proxy to handle LLM API requests, but there's no standardized way for users to configure API keys for their chosen providers (OpenAI, Anthropic, etc.). The proxy needs these credentials to authenticate upstream requests.

Current state: The controller creates all OpenClaw resources via Kustomize, including a proxy Deployment and ConfigMap, but no Secret for API credentials.

## Goals / Non-Goals

**Goals:**
- Add a mandatory `APIKey` field to the OpenClawSpec as a string field
- Inject the API key into the proxy's configuration via a Secret
- Enable basic LLM provider authentication

**Non-Goals:**
- Secret reference support (deferred to future work)
- Multiple API key support (one provider at a time)
- Automatic key rotation or expiration handling
- Support for other credential types (OAuth, tokens, etc.)

## Decisions

### Decision 1: Direct String Storage (Temporary)

**Chosen:** APIKey field stores the key directly as a string in the CR

**Rationale:** 
- Simplest implementation to unblock LLM provider integration
- Minimizes scope of initial implementation
- Secret reference support can be added later without breaking changes

**Alternatives considered:**
- SecretKeySelector reference: More secure and follows Kubernetes best practices, but adds complexity (validation webhook, Secret watching, error handling) - **deferred to future iteration**

**Migration path:** When Secret support is added, the field can be made optional with validation to require either `APIKey` (direct) OR `APIKeyFrom` (Secret reference).

### Decision 2: Proxy Secret Injection

**Chosen:** Controller creates/updates a Secret (`openclaw-proxy-secrets`) containing the API key under the `GEMINI_API_KEY` data entry, mounted by the proxy Deployment

**Rationale:**
- Keeps the API key out of ConfigMaps (Secrets have better RBAC)
- `GEMINI_API_KEY` naming aligns with current LLM provider (can be extended for other providers later)
- Secret is created if it doesn't exist, updated if it does (idempotent operation via server-side apply)
- Proxy configuration doesn't need to change when switching to Secret references later
- Standard pattern for credential injection

**Implementation details:**
- Secret name: `openclaw-proxy-secrets`
- Data key: `GEMINI_API_KEY`
- Controller behavior: Create secret if missing, update if exists

### Decision 3: Mandatory Field

**Chosen:** Make APIKey a required field with kubebuilder validation

**Rationale:**
- Enforces correct configuration upfront
- Prevents deployment failures from missing credentials
- Clear contract in CRD schema

## Risks / Trade-offs

**[Risk]** API key stored in plain text in the OpenClaw CR spec
→ **Mitigation:** Document security implications; add Secret reference support in follow-up work
→ **Note:** CR contents are still stored in etcd (can be encrypted at rest via cluster configuration)

**[Trade-off]** Direct storage vs Secret reference
→ **Benefit:** Simpler initial implementation, faster iteration
→ **Cost:** API key visible via `kubectl get openclaw -o yaml`
→ **Future:** Add SecretKeySelector support without breaking existing CRs

**[Trade-off]** Single API key per instance vs multiple providers
→ **Future:** Can extend with map of provider → key/Secret references if needed
