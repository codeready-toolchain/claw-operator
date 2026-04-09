## Why

The current OpenClaw CRD stores the Gemini API key directly in the `apiKey` spec field, exposing sensitive credentials in the CR manifest. This violates Kubernetes security best practices and prevents proper secret rotation workflows. Moving to a Secret reference pattern enables secure credential management and follows the Kubernetes principle of separation between configuration and secrets.

## What Changes

- **BREAKING**: Remove `OpenClawSpec.APIKey` string field
- Add `OpenClawSpec.GeminiAPIKey` field with Secret reference structure (name + namespace)
- Controller reads the referenced Secret's `key` entry and copies its value to `openclaw-proxy-secrets` under `GEMINI_API_KEY`
- Controller watches the referenced Secret and automatically updates `openclaw-proxy-secrets` when the API key changes
- Update CRD validation to require the Secret reference field

## Capabilities

### New Capabilities
- `secret-reference-watching`: Watch referenced Secrets for changes and propagate updates to proxy secrets

### Modified Capabilities
- `api-key-field`: Change from direct string field to Secret reference structure

## Impact

- **API**: OpenClaw CRD v1alpha1 schema (breaking change - requires migration path or version bump)
- **Controller**: OpenClawResourceReconciler must watch Secrets in addition to OpenClaw CRs
- **Tests**: All test fixtures must create Secrets instead of setting apiKey directly
- **Documentation**: Users need guidance on creating the API key Secret before creating OpenClaw instances
