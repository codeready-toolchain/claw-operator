## Context

The OpenClawResourceReconciler currently creates two secrets during reconciliation:
1. `openclaw-proxy-secrets` - contains LLM API keys (e.g., `GEMINI_API_KEY`)
2. (New) `openclaw-secrets` - will contain the gateway authentication token

The reconciler uses server-side apply for all resources and follows the pattern:
- Early secret creation before applying Kustomized resources
- Owner references for garbage collection
- Idempotent operations (secrets not regenerated if they exist)

The existing `applyProxySecret()` method provides a template for secret creation.

## Goals / Non-Goals

**Goals:**
- Generate a cryptographically secure random token for gateway authentication
- Create and manage `openclaw-secrets` Secret with `OPENCLAW_GATEWAY_TOKEN` entry
- Preserve existing tokens (do not regenerate on reconciliation)
- Follow existing reconciler patterns (owner references, server-side apply, error handling)
- Add test coverage for secret creation

**Non-Goals:**
- Mounting the secret into the OpenClaw deployment (separate change)
- Implementing gateway authentication logic (separate change)
- Token rotation or expiration mechanisms
- Support for multiple tokens or token revocation

## Decisions

### 1. Token Generation Method
**Decision:** Use `crypto/rand.Read()` to generate 32 random bytes, then hex-encode to 64 characters.

**Rationale:** 
- `crypto/rand` is Go's cryptographically secure random number generator
- 32 bytes (256 bits) provides strong entropy for authentication tokens
- Hex encoding produces a 64-character string that's URL-safe and easy to handle
- Equivalent to `openssl rand -hex 32` as requested

**Alternatives considered:**
- Base64 encoding: Would be shorter (44 chars) but less human-readable and contains special characters
- UUID: Only 128 bits of entropy, less secure than 256 bits

### 2. Secret Creation Timing
**Decision:** Create `openclaw-secrets` via a new `applyGatewaySecret()` method called before `applyKustomizedResources()`.

**Rationale:**
- Mirrors the existing pattern for `applyProxySecret()`
- Ensures secret exists before other resources are applied
- Clear separation of concerns (secret generation vs. Kustomize-managed resources)
- Allows early failure if token generation fails

**Alternatives considered:**
- Include in Kustomize manifests: Would require pre-generating token or templating, adds complexity
- Create after Kustomized resources: Secret wouldn't be available if deployment needs it immediately

### 3. Idempotency Strategy
**Decision:** Check if secret exists and has `OPENCLAW_GATEWAY_TOKEN` entry. If yes, skip generation and use server-side apply to ensure owner reference is set.

**Rationale:**
- Preserves existing tokens across reconciliation loops
- Prevents token churn that would break active gateway sessions
- Consistent with proxy secret behavior
- Server-side apply updates metadata (owner references) without touching data

**Alternatives considered:**
- Always regenerate: Would break active sessions on every reconciliation
- Never update: Could leave orphaned secrets without owner references

### 4. Error Handling
**Decision:** Return error from `applyGatewaySecret()` to fail reconciliation if token generation or secret creation fails.

**Rationale:**
- Secret creation is critical for security
- Controller-runtime will requeue failed reconciliations with exponential backoff
- Consistent with existing error handling in reconciler
- Prevents partial state where OpenClaw is deployed without authentication

### 5. Testing Approach
**Decision:** Add test file `openclaw_gatewaysecret_controller_test.go` following existing secret test patterns.

**Rationale:**
- Consistent with existing test organization (one file per resource type)
- Can test token generation, secret creation, idempotency, and owner references
- Uses envtest for real API server interactions
- Follows Ginkgo/Gomega patterns from existing tests

## Risks / Trade-offs

### Risk: Token loss if secret is manually deleted
**Mitigation:** Owner reference ensures secret is recreated on next reconciliation. New token will be generated, requiring gateway clients to update.

### Risk: Insufficient randomness on low-entropy systems
**Mitigation:** `crypto/rand` on Linux uses `/dev/urandom` which is non-blocking and suitable for cryptographic use. On systems with insufficient entropy, `Read()` will return an error, failing the reconciliation.

### Risk: Token visible in memory/logs
**Mitigation:** Token is only generated in-memory during secret creation. Standard Kubernetes secret security applies (base64 encoded at rest, RBAC-protected). Avoid logging token values.

### Trade-off: No token rotation mechanism
**Implication:** Tokens persist indefinitely unless the secret is manually deleted. Acceptable for initial implementation; rotation can be added later if needed.

### Trade-off: Single token per instance
**Implication:** All gateway clients share the same token. Sufficient for current use case; per-client tokens would require more complex secret structure.

## Migration Plan

**Deployment:**
1. Reconciler code changes deployed via operator update
2. Existing OpenClaw instances will have `openclaw-secrets` created on next reconciliation
3. New instances will have secret created immediately
4. No CRD changes required (no cluster downtime)

**Rollback:**
- Revert operator deployment to previous version
- `openclaw-secrets` will remain but won't be updated
- Can manually delete secrets if needed (will not be recreated by old controller)

**Validation:**
- Verify `openclaw-secrets` secret exists after operator update
- Verify secret has `OPENCLAW_GATEWAY_TOKEN` entry with 64 hex characters
- Verify owner reference points to OpenClaw instance
- Verify token is not regenerated on subsequent reconciliations

## Open Questions

None - design is straightforward and follows existing patterns.
