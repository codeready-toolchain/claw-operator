## 1. Add Constants and Imports

- [x] 1.1 Add `OpenClawGatewaySecretName = "openclaw-secrets"` constant to reconciler
- [x] 1.2 Add `GatewayTokenKeyName = "OPENCLAW_GATEWAY_TOKEN"` constant to reconciler
- [x] 1.3 Add required imports: `crypto/rand`, `encoding/hex`

## 2. Implement Token Generation

- [x] 2.1 Create `generateGatewayToken()` function that generates 32 random bytes using `crypto/rand.Read()`
- [x] 2.2 Hex-encode the 32 bytes to produce a 64-character string
- [x] 2.3 Add error handling for `crypto/rand.Read()` failures

## 3. Implement Gateway Secret Creation

- [x] 3.1 Create `applyGatewaySecret(ctx, instance)` method in reconciler
- [x] 3.2 Check if `openclaw-secrets` secret already exists in the namespace
- [x] 3.3 If secret exists and has `OPENCLAW_GATEWAY_TOKEN` entry, skip token generation
- [x] 3.4 If secret doesn't exist, generate new token using `generateGatewayToken()`
- [x] 3.5 Create Secret object with `OPENCLAW_GATEWAY_TOKEN` data entry
- [x] 3.6 Set owner reference on secret using `controllerutil.SetControllerReference()`
- [x] 3.7 Apply secret using server-side apply (Patch with FieldManager)
- [x] 3.8 Return error if secret creation fails

## 4. Integrate into Reconciliation Flow

- [x] 4.1 Call `applyGatewaySecret()` in `Reconcile()` method before `applyKustomizedResources()`
- [x] 4.2 Return error from `Reconcile()` if `applyGatewaySecret()` fails
- [x] 4.3 Add `// +kubebuilder:rbac:` marker for Secret get/create/update/patch permissions

## 5. Add Test Coverage

- [x] 5.1 Create `internal/controller/openclaw_gatewaysecret_controller_test.go` test file
- [x] 5.2 Add test: Secret is created when OpenClaw instance is reconciled
- [x] 5.3 Add test: Secret contains `OPENCLAW_GATEWAY_TOKEN` entry with 64 hex characters
- [x] 5.4 Add test: Token is not regenerated when secret already exists
- [x] 5.5 Add test: Token values are unique across different reconciliations (when secret is deleted)
- [x] 5.6 Add test: Owner reference is set correctly on secret
- [x] 5.7 Add test: Secret is deleted when OpenClaw instance is deleted (garbage collection)

## 6. Update Documentation

- [x] 6.1 Update CLAUDE.md to document `openclaw-secrets` Secret and its purpose
- [x] 6.2 Update CLAUDE.md with new constants (`OpenClawGatewaySecretName`, `GatewayTokenKeyName`)
- [x] 6.3 Update CLAUDE.md reconciliation flow to include `applyGatewaySecret()` step

## 7. Validation

- [x] 7.1 Run `make test` to verify all tests pass
- [x] 7.2 Run `make lint` to ensure code quality
- [x] 7.3 Run `make manifests` to regenerate RBAC manifests from kubebuilder markers
