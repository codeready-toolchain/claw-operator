# ADR-0017: Custom Provider Support

**Status:** Implemented
**Date:** 2026-05-29

---

## Problem

The operator accepts arbitrary `provider` strings in `spec.credentials[].provider`, but `injectProviders()` unconditionally replaces `models.providers` (always-win tier), and `resolveProviderDefaults()` only auto-fills domain/header defaults for `google` and `anthropic` (via `knownAPIKeyProviders`). Unknown providers require explicit `domain` and `apiKey` config, but even with those set, users cannot control the `baseUrl` path or register models in the catalog — `injectProviders` derives `baseUrl` as `https://<domain>` with no path component.

This blocks:
- Self-hosted models (vLLM, Ollama, TGI, LiteLLM) that need a path prefix (e.g., `/v1`)
- Populating the model catalog for providers outside the hardcoded `modelCatalog` (`google`, `anthropic`, `openai`, `xai`)
- The Dev Sandbox dashboard's "Custom / Self-Hosted" provider option

---

## Design

Two complementary changes that together give full custom provider support:

1. **Accept arbitrary `provider` strings on credentials** — any credential with a `provider` value automatically generates both a proxy route and a `models.providers` entry. `resolveProviderDefaults()` auto-fills domain/header only for `google` and `anthropic` (`knownAPIKeyProviders`); all other providers require explicit `domain` (and `apiKey` config when `type: apiKey`).

2. **Add `spec.customProviders` CRD field** — a typed, validated struct for custom OpenAI-compatible providers with explicit `baseUrl`, model list, and credential linkage. This is the primary interface for dashboards and GitOps.

### How they compose

```
┌──────────────────────────────────────────────────────────┐
│                      Claw CR Spec                        │
│                                                          │
│  spec.credentials[] ─────────► Proxy routing             │
│    (domain, type, secretRef)    (domain allowlist + auth)│
│    provider: "custom-llm" ──┐                            │
│                             │                            │
│  spec.customProviders[] ────┼──► models.providers{}      │
│    (baseUrl, models)        │    (OpenClaw model routing)│
│    credentialRef ───────────┘                            │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

**Separation of concerns:**
- `spec.credentials[]` answers: "Can traffic reach this domain? How is it authenticated?"
- `spec.customProviders[]` answers: "How does OpenClaw talk to this model endpoint? What models does it serve?"

When `spec.customProviders` is used, the credential does NOT need `provider` set — the custom provider config handles model routing, and the credential just handles proxy access.

When `provider` is set directly on a credential (the quick path), `injectProviders()` auto-generates the `models.providers` entry — useful for quick `oc apply` workflows where the user doesn't want to declare a separate `customProviders` block.

---

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Provider validation | Arbitrary `provider` strings with explicit requirements | `resolveProviderDefaults()` auto-fills only for `knownAPIKeyProviders` (`google`, `anthropic`); others require explicit `domain` and type-specific config |
| Custom provider API | Dedicated `spec.customProviders` field | Typed, validated struct is dashboard-friendly and GitOps-friendly; avoids fighting the always-overwrite tier via `spec.config.raw` |
| Wire format selection | Include optional `api` enum field | Negligible cost; covers Ollama native, Anthropic endpoints, and OpenAI Responses without falling back to raw config that fights the operator's overwrite behavior |
| API enum values | `openai-completions`, `openai-responses`, `anthropic-messages`, `ollama` | Maps directly to OpenClaw's supported wire formats; forward-compatible (new values are non-breaking) |
| Credential linkage | `credentialRef` string referencing `spec.credentials[].name` | Keeps credential management in one place; custom providers compose with existing proxy routing |
| Duplicate detection | Controller rejects name collisions between `customProviders` and `credentials[].provider` | Prevents ambiguous provider routing |

---

## API Surface

### `spec.customProviders[]`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string (pattern: `^[a-z][a-z0-9-]*$`) | Yes | Provider key in `models.providers` and model prefix (e.g., `my-vllm/model-name`) |
| `baseUrl` | string (pattern: `^https?://`) | Yes | Full base URL for the OpenAI-compatible API endpoint |
| `api` | enum | No | Wire format: `openai-completions` (default), `openai-responses`, `anthropic-messages`, `ollama` |
| `credentialRef` | string | Yes | Name of a credential in `spec.credentials` that handles proxy routing |
| `models` | array (min 1) | Yes | Models available on this endpoint |
| `models[].name` | string | Yes | Model identifier as the endpoint knows it |
| `models[].alias` | string | No | Display name shown in the model picker |

### `spec.credentials[].provider` — relaxed

The `provider` field accepts any string. `resolveProviderDefaults()` auto-fills domain and header only for providers in `knownAPIKeyProviders` (currently `google` and `anthropic`) when `type: apiKey`. All other providers pass through but require explicit `domain` (and `apiKey` config when `type: apiKey`). The model catalog (`modelCatalog`) populates the model picker for `google`, `anthropic`, `openai`, and `xai`; providers not in the catalog are silently skipped for model registration.

---

## Usage Examples

### Quick path (arbitrary provider on credential)

For power users doing `oc apply`, just set `provider` to an arbitrary value:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: my-vllm
      type: bearer
      provider: my-vllm
      domain: llm.mycompany.com
      secretRef:
        - name: vllm-key
          key: api-key
  config:
    raw:
      agents:
        defaults:
          model:
            primary: my-vllm/qwen3-14b
          models:
            my-vllm/qwen3-14b:
              alias: Qwen 3 14B
```

The operator generates:
- Proxy route: `domain: llm.mycompany.com`, `injector: bearer`, `envVar: CRED_MY_VLLM`
- Provider entry: `models.providers.my-vllm = {baseUrl: "https://llm.mycompany.com", apiKey: "placeholder"}`

Limitation: `baseUrl` is `https://<domain>` (no path). Works for endpoints where the API is at the root. Custom provider IDs (not bundled in OpenClaw) also require user-specified models via `spec.config.raw` since the operator cannot auto-populate the provider's model catalog. Prefer `spec.customProviders` for custom endpoints — it handles both limitations.

### Typed path (`spec.customProviders`)

For dashboards and GitOps with full control over baseUrl:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: my-vllm
      type: bearer
      domain: llm.mycompany.com
      secretRef:
        - name: vllm-key
          key: api-key
  customProviders:
    - name: my-vllm
      baseUrl: "https://llm.mycompany.com/v1"
      credentialRef: my-vllm
      models:
        - name: qwen3-14b
          alias: Qwen 3 14B
        - name: llama-4-scout
          alias: Llama 4 Scout
```

The operator generates:
- Proxy route: `domain: llm.mycompany.com`, `injector: bearer`
- Provider entry: `models.providers.my-vllm = {baseUrl: "https://llm.mycompany.com/v1", apiKey: "placeholder", models: [{id: "qwen3-14b", ...}, ...]}`
- Model entries: `my-vllm/qwen3-14b`, `my-vllm/llama-4-scout` in the model picker
- Primary set to `my-vllm/qwen3-14b` (first model of first provider if no catalog provider comes first)

### No-auth self-hosted model (Ollama native API)

```yaml
spec:
  credentials:
    - name: local-ollama
      type: none
      domain: ollama.internal.corp
  customProviders:
    - name: ollama
      baseUrl: "http://ollama.internal.corp:11434"
      api: ollama
      credentialRef: local-ollama
      models:
        - name: llama3.3
          alias: Llama 3.3 70B
```

### Dashboard use case

A web dashboard that manages Claw instances would create:

```yaml
# Secret
apiVersion: v1
kind: Secret
metadata:
  name: llm-key
stringData:
  api-key: <user-provided-key>
---
# Claw CR
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: custom-llm
      type: bearer
      domain: "llm.mycompany.com"   # extracted from user's URL
      secretRef:
        - name: llm-key
          key: api-key
  customProviders:
    - name: custom-llm
      baseUrl: "https://llm.mycompany.com/v1"  # user's full URL
      credentialRef: custom-llm
      models:
        - name: qwen3-14b           # user-provided model name
          alias: "Qwen 3 14B"       # user-provided display name
```

---

## Validation Rules

| Rule | Enforcement |
|------|-------------|
| `customProviders[].name` must be unique across all custom providers | Controller validation |
| `customProviders[].name` must not collide with any `credentials[].provider` | Controller validation |
| `customProviders[].credentialRef` must reference an existing credential name | Controller validation |
| `customProviders[].baseUrl` must be a valid HTTP(S) URL | CEL + controller |
| `customProviders[].models` must have at least one entry | CEL |
| Credentials with arbitrary `provider` still require explicit `domain` and `type` | Existing `resolveProviderDefaults()` logic |

---

## Backward Compatibility

- Existing CRs with any `provider` value continue to work identically — `injectProviders()` already accepted arbitrary strings.
- `resolveProviderDefaults()` still auto-infers domain/header for `google` and `anthropic` (via `knownAPIKeyProviders`) — this logic is untouched.
- The model catalog still populates models for `google`, `anthropic`, `openai`, and `xai`.
- `spec.customProviders` is optional with no default — existing CRs are unaffected.
