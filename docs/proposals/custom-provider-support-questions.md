# Custom Provider Support — Design Questions

**Status:** Resolved
**Related:** [Design document](custom-provider-support.md)

---

## Q1: Should `CustomProviderSpec` include an `api` field for wire format selection?

OpenClaw supports multiple wire formats (`openai-completions`, `anthropic-messages`, `ollama`, `openai-responses`, etc.) via the `api` field in `models.providers`. When omitted, it defaults to `openai-completions`. The question was whether to include an optional `api` field now, or keep the design minimal and add it later.

### Option A: Include optional `api` field

```go
// +optional
// +kubebuilder:validation:Enum=openai-completions;openai-responses;anthropic-messages;ollama
API CustomProviderAPI `json:"api,omitempty"`
```

- **Pro:** Covers Ollama native API, Anthropic-format endpoints, and OpenAI Responses API without falling back to `spec.config.raw`
- **Pro:** Tiny addition — one optional field with an enum, no structural complexity
- **Pro:** The Ollama example becomes natural: `api: ollama` + `baseUrl: "http://host:11434"` (no `/v1` suffix needed)
- **Pro:** Forward-compatible — adding new enum values is non-breaking
- **Con:** Slightly expands the scope beyond "OpenAI-compatible" in the title
- **Con:** Enum must be updated when OpenClaw adds new API formats (maintenance burden)

**Decision:** Option A — negligible cost, significantly expands utility for common self-hosted scenarios (Ollama native, Anthropic endpoints), and the `spec.config.raw` escape hatch is actively hostile for this field due to `models.providers` being in the always-overwrite tier.

_Considered and rejected: Option B (omit field, strict OpenAI-only scope — forces awkward raw config workarounds that fight the operator's overwrite behavior)_
