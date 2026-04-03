## 1. API Types and CRD Schema

- [x] 1.1 Add `APIKey` field to `OpenClawSpec` in `api/v1alpha1/openclaw_types.go` with type `string`
- [x] 1.2 Add kubebuilder validation marker `+kubebuilder:validation:Required` for APIKey field
- [x] 1.3 Add kubebuilder validation marker `+kubebuilder:validation:MinLength=1` to prevent empty strings
- [x] 1.4 Run `make generate` to update DeepCopy methods
- [x] 1.5 Run `make manifests` to regenerate CRD YAML with new field schema

## 2. Controller Logic

- [x] 2.1 Add RBAC markers for Secret create/update/get permissions in controller file
- [x] 2.2 Implement helper function to create `openclaw-proxy-secrets` Secret with `GEMINI_API_KEY` data entry from CR's APIKey field
- [x] 2.3 Add logic to create Secret if it doesn't exist, update if it does (in reconciliation flow before or after applyKustomizedResources)
- [x] 2.4 Set owner reference on Secret for garbage collection when OpenClaw CR is deleted
- [x] 2.5 Use server-side apply for Secret to ensure idempotent create/update operations

## 3. Proxy Deployment Configuration

- [x] 3.1 Update `internal/assets/manifests/proxy-deployment.yaml` to add Secret volume mount
- [x] 3.2 Add volume definition for `openclaw-proxy-secrets` Secret
- [x] 3.3 Mount Secret at appropriate path in proxy container (e.g., `/etc/openclaw/secrets`)
- [x] 3.4 Update proxy Nginx configuration to read `GEMINI_API_KEY` from mounted Secret file
- [x] 3.5 Configure proxy to inject API key in Authorization header for upstream requests to Gemini

## 4. Unit and Integration Tests

- [x] 4.1 Add controller tests for Secret creation in `internal/controller/secret_test.go`
- [x] 4.2 Test reconciliation creates `openclaw-proxy-secrets` Secret when it doesn't exist with `GEMINI_API_KEY` data entry
- [x] 4.3 Test reconciliation updates `openclaw-proxy-secrets` Secret when it already exists
- [x] 4.4 Test reconciliation updates Secret when APIKey field changes in CR
- [x] 4.5 Test Secret is recreated if deleted manually
- [x] 4.6 Test Secret has correct owner reference
- [x] 4.7 Update existing controller tests to include APIKey field in test OpenClaw CRs

## 5. Documentation and Examples

- [x] 5.1 Update `config/samples/openclaw_v1alpha1_openclaw.yaml` to include APIKey field with example value
- [x] 5.2 Add README section explaining the APIKey field and security considerations
- [x] 5.3 Document that Secret reference support is planned for future work
- [x] 5.4 Verify `kubectl explain openclaw.spec.apiKey` shows correct structure after CRD update
