## Why

Users need to provide API keys to authenticate with their chosen LLM providers (OpenAI, Anthropic, etc.) when using OpenClaw. Currently, there's no standardized way to configure this authentication in the OpenClaw CR, making it difficult for users to connect to their preferred models.

## What Changes

- Add a mandatory `APIKey` string field to the `OpenClawSpec` structure
- Update the controller to create/update an operator-managed Secret named `openclaw-proxy-secrets` with the API key stored under the `GEMINI_API_KEY` data entry
- Configure the proxy Deployment to mount this Secret and use the API key for upstream authentication

**Note:** Secret reference support (using SecretKeySelector) is intentionally deferred to future work to minimize initial scope.

## Capabilities

### New Capabilities
- `api-key-field`: Defines the new mandatory APIKey field in the OpenClaw CRD, including validation, secure storage references, and how it integrates with the proxy configuration

### Modified Capabilities

## Impact

- **API types**: `api/v1alpha1/openclaw_types.go` - adds new string field to OpenClawSpec
- **Controller**: `internal/controller/openclaw_controller.go` - reconciliation logic to create/update `openclaw-proxy-secrets` Secret with `GEMINI_API_KEY` data entry
- **Manifests**: `internal/assets/manifests/proxy-deployment.yaml` - updated to mount `openclaw-proxy-secrets` Secret
- **Tests**: `internal/controller/*_test.go` - reconciliation tests for Secret creation, updates, and data entry validation
- **CRD manifests**: `config/crd/bases/` - regenerated CRD YAML with new required field
