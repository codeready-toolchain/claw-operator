## 1. API Type Definition Changes

- [x] 1.1 Replace `APIKey string` field with `GeminiAPIKey *corev1.SecretKeySelector` in OpenClawSpec (api/v1alpha1/openclaw_types.go)
- [x] 1.2 Add kubebuilder validation markers to require GeminiAPIKey field and its nested name/key fields
- [x] 1.3 Run `make manifests` to regenerate CRD YAML with new schema
- [x] 1.4 Run `make generate` to regenerate DeepCopy methods
- [x] 1.5 Verify generated CRD includes proper validation for GeminiAPIKey structure

## 2. Controller Reconciliation Logic

- [x] 2.1 Update `applyProxySecret` method to read API key from referenced Secret instead of CR spec field
- [x] 2.2 Add helper method `getAPIKeyFromSecret(ctx, instance)` to fetch API key value from referenced Secret
- [x] 2.3 Handle missing Secret case: return error and update status condition to Available=False, Reason=SecretNotFound
- [x] 2.4 Handle missing key in Secret case: return error and update status condition to Available=False, Reason=SecretKeyNotFound
- [x] 2.5 Update `updateStatus` to check for Secret-related errors and set appropriate status conditions

## 3. Secret Watching and Event Handling

- [x] 3.1 Add Secret watch to controller setup in `cmd/main.go` using `Watches()` with source `&corev1.Secret{}`
- [x] 3.2 Implement `handler.EnqueueRequestsFromMapFunc` to map Secret events to OpenClaw reconcile requests
- [x] 3.3 Create predicate function to filter Secret events (only reconcile if Secret is referenced by an OpenClaw CR)
- [x] 3.4 Add logic to find all OpenClaw CRs in the same namespace that reference a given Secret
- [ ] 3.5 Test Secret watch triggers reconciliation when referenced Secret is created/updated/deleted

## 4. Update Test Fixtures and Test Cases

- [x] 4.1 Update all OpenClaw test fixtures to use `geminiAPIKey` Secret reference instead of `apiKey` string
- [x] 4.2 Create helper function in tests to generate test Secrets with API keys
- [x] 4.3 Update `openclaw_secret_controller_test.go` tests to verify Secret reference resolution
- [x] 4.4 Add test case: Controller reconciles when referenced Secret does not exist (expect SecretNotFound status)
- [x] 4.5 Add test case: Controller reconciles when referenced Secret exists but key is missing (expect SecretKeyNotFound status)
- [x] 4.6 Add test case: Controller propagates API key from referenced Secret to proxy Secret
- [x] 4.7 Add test case: Controller updates proxy Secret when referenced Secret value changes
- [x] 4.8 Add test case: Controller reconciles when Secret is created after OpenClaw CR
- [x] 4.9 Add test case: Controller does not reconcile when unrelated Secret changes
- [x] 4.10 Verify all existing tests pass with updated fixtures

## 5. Example and Sample Updates

- [x] 5.1 Update `config/samples/openclaw_v1alpha1_openclaw.yaml` to use `geminiAPIKey` Secret reference
- [x] 5.2 Create example Secret manifest in `config/samples/` showing API key Secret structure
- [x] 5.3 Update README or docs with migration instructions for existing users

## 6. E2E Test Updates

- [x] 6.1 Update e2e test fixtures in `test/e2e/` to create Secret before creating OpenClaw CR
- [ ] 6.2 Verify e2e tests pass with new Secret reference pattern
- [x] 6.3 Add e2e test for Secret rotation scenario (update Secret value, verify proxy picks up change)

## 7. Cleanup and Validation

- [x] 7.1 Run `make lint` and fix any issues
- [x] 7.2 Run `make test` and ensure all unit tests pass
- [ ] 7.3 Run `make test-e2e` and ensure e2e tests pass
- [ ] 7.4 Manually test: create Secret, create OpenClaw CR, verify proxy Secret is populated
- [ ] 7.5 Manually test: update referenced Secret value, verify proxy Secret updates
- [ ] 7.6 Manually test: delete referenced Secret, verify status shows SecretNotFound
