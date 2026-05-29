# Custom Provider Support

## Problem

The operator only accepts a hardcoded set of provider values (`google`, `anthropic`, `openai`, `openrouter`, `xai`) in `spec.credentials[].provider`. Additionally, `models.providers` is in the "always-win" tier — `injectProviders()` unconditionally replaces it, making it impossible for users to configure custom or self-hosted model endpoints via `spec.config.raw`.

This blocks:
- Self-hosted models (vLLM, Ollama, TGI, LiteLLM) running outside the cluster
- Hosted providers not in the hardcoded list (Together AI, Fireworks, Groq, Cerebras, etc.)
- The Dev Sandbox dashboard's "Custom / Self-Hosted" provider option

## Design

Two complementary changes that together give full custom provider support:

1. **Remove the `knownProviders` validation gate** — allow arbitrary `provider` strings on credentials, so that any credential with a `provider` value automatically generates both a proxy route and a `models.providers` entry.

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

## API Changes

### 1. CredentialSpec — relax `provider` validation

Remove the `knownProviders` allowlist check in `resolveCredentials()`. The field becomes a free-form string with documented known values.

```go
// Before (claw_credentials.go)
var knownProviders = map[string]bool{
    "google": true, "anthropic": true, "openai": true, "openrouter": true, "xai": true,
}

// After — remove the map, remove the validation check.
// Known providers still get auto-inferred defaults (domain, header) via resolveProviderDefaults().
// Unknown providers pass through with explicit domain/type required.
```

Existing controller-level validation in `resolveProviderDefaults()` already ensures that unknown providers without explicit `domain` (and `apiKey` config for type `apiKey`) are rejected — so removing the gate is safe.

### 2. New `CustomProviderSpec` type

```go
// CustomProviderSpec defines a custom OpenAI-compatible model provider.
type CustomProviderSpec struct {
    // Name is the provider key used in models.providers and as the model prefix
    // (e.g., "my-vllm" → models are referenced as "my-vllm/model-name").
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
    // Other values: "anthropic-messages", "ollama", "openai-responses".
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
```

### 3. ClawSpec — add the field

```go
type ClawSpec struct {
    // ... existing fields ...

    // CustomProviders declares custom OpenAI-compatible model providers.
    // Each entry generates a models.providers entry and registers its models
    // in the model picker. The referenced credential handles proxy routing.
    // +optional
    CustomProviders []CustomProviderSpec `json:"customProviders,omitempty"`
}
```

---

## Controller Changes

### `injectProviders()` — merge custom providers

After building the provider map from credentials, iterate `spec.customProviders` and add entries:

```go
func injectProviders(config map[string]any, instance *clawv1alpha1.Claw) error {
    providers := map[string]any{}

    // Existing: build from spec.credentials[].provider
    for _, cred := range instance.Spec.Credentials {
        if cred.Provider == "" || cred.Type == clawv1alpha1.CredentialTypePathToken {
            continue
        }
        // ... existing logic unchanged ...
    }

    // New: add from spec.customProviders
    for _, cp := range instance.Spec.CustomProviders {
        if _, exists := providers[cp.Name]; exists {
            return fmt.Errorf("duplicate provider %q: conflicts between credentials and customProviders", cp.Name)
        }
        models := make([]any, len(cp.Models))
        for i, m := range cp.Models {
            models[i] = map[string]any{"id": m.Name, "name": m.Name}
        }
        entry := map[string]any{
            "baseUrl": cp.BaseUrl,
            "apiKey":  "ah-ah-ah-you-didnt-say-the-magic-word",
            "models":  models,
        }
        if cp.API != "" {
            entry["api"] = string(cp.API)
        }
        providers[cp.Name] = entry
    }

    ensureNestedMap(config, "models")["providers"] = providers
    return nil
}
```

### `injectModelCatalog()` — include custom provider models

After the existing catalog logic, iterate `spec.customProviders` and add their models:

```go
// In injectModelCatalog, after existing catalog loop:
for _, cp := range instance.Spec.CustomProviders {
    for _, m := range cp.Models {
        key := cp.Name + "/" + m.Name
        alias := m.Alias
        if alias == "" {
            alias = m.Name
        }
        catalogModels[key] = map[string]any{"alias": alias}
    }
    // Set primary from first custom provider's first model if no catalog primary yet
    if catalogPrimary == "" && len(cp.Models) > 0 {
        catalogPrimary = cp.Name + "/" + cp.Models[0].Name
    }
}
```

### `resolveCredentials()` — remove knownProviders gate

```go
// Remove this block:
if !knownProviders[cred.Provider] {
    errs = append(errs, fmt.Errorf(...))
}

// Keep the duplicate-provider check (still useful).
```

### `resolveCredentials()` — validate customProviders credential refs

Add validation that each `customProviders[].credentialRef` matches a credential name in `spec.credentials`:

```go
credNames := map[string]bool{}
for _, cred := range instance.Spec.Credentials {
    credNames[cred.Name] = true
}
for _, cp := range instance.Spec.CustomProviders {
    if !credNames[cp.CredentialRef] {
        errs = append(errs, fmt.Errorf("customProvider %q: credentialRef %q not found in spec.credentials", cp.Name, cp.CredentialRef))
    }
}
```

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

## Files Changed

| File | Change |
|------|--------|
| `api/v1alpha1/claw_types.go` | Add `CustomProviderSpec`, `CustomProviderAPI`, `CustomModelEntry`, add `CustomProviders` to `ClawSpec` |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerated (`make generate`) |
| `config/crd/bases/` | Regenerated (`make manifests`) |
| `internal/controller/claw_credentials.go` | Remove `knownProviders` map and validation check; add `customProviders` ref validation |
| `internal/controller/claw_resource_controller.go` | Update `injectProviders()` and `injectModelCatalog()` to include custom providers |
| `internal/controller/claw_configmap_test.go` | Tests for custom provider injection |
| `internal/controller/claw_credentials_test.go` | Tests for arbitrary provider validation, credentialRef validation |
| `internal/controller/claw_proxy_test.go` | Tests confirming proxy routes still work with arbitrary providers |
| `docs/user-guide.md` | New "Custom / Self-Hosted Models" section |

---

## Backward Compatibility

- Existing CRs with `provider: google`, `anthropic`, `openai`, `openrouter`, `xai` continue to work identically.
- `resolveProviderDefaults()` still auto-infers domain/header for `google` and `anthropic` — this logic is untouched.
- The only behavioral change: CRs with unknown `provider` values that previously failed validation will now succeed. This is purely additive.
- `spec.customProviders` is optional with no default — existing CRs are unaffected.
