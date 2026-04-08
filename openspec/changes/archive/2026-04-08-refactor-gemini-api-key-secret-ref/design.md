## Context

The OpenClaw operator currently accepts the Gemini API key as a plain string in the `OpenClawSpec.APIKey` field. The controller copies this value into the `openclaw-proxy-secrets` Secret under the `GEMINI_API_KEY` data entry, which is then mounted into the `openclaw-proxy` Deployment.

This approach has several issues:
- API keys are visible in CR manifests stored in version control
- No support for external secret management (Vault, Sealed Secrets, ESO)
- Secret rotation requires updating the CR manifest
- Violates Kubernetes principle of separating configuration from secrets

The new design follows the standard Kubernetes pattern where the CRD references a user-managed Secret (similar to `imagePullSecrets`, `tlsSecretName`, etc.).

**Constraints:**
- This is a breaking change requiring v1alpha1 → v1alpha2 migration or an in-place breaking change with clear migration docs
- Must maintain backward compatibility during transition or provide clear upgrade path
- The referenced Secret must be in the same namespace as the OpenClaw CR (standard Kubernetes scoping)

## Goals / Non-Goals

**Goals:**
- Replace `APIKey` string field with `GeminiAPIKey` Secret reference (name + key)
- Controller reads the referenced Secret and copies its value to `openclaw-proxy-secrets`
- Support runtime secret updates (when the referenced Secret changes, propagate to proxy)
- Clear error messages when referenced Secret is missing or key is not found

**Non-Goals:**
- Supporting cross-namespace secret references (security concern)
- Automatic secret creation (user/UI responsibility)
- Supporting multiple LLM providers in this change (focus on Gemini refactor only)
- Implementing secret rotation policies or expiration (external concern)

## Decisions

### Decision 1: Secret reference structure

**Choice:** Use a struct with `name` (required) and `key` (required) fields.

```go
type SecretKeySelector struct {
    Name string `json:"name"`
    Key  string `json:"key"`
}
```

**Alternatives considered:**
- **LocalObjectReference + separate key field**: More verbose, but follows `corev1.SecretKeySelector` pattern
- **String field with "secret/key" format**: Requires parsing, error-prone

**Rationale:** Using a struct similar to `corev1.SecretKeySelector` (but without `Optional` field) aligns with Kubernetes conventions and provides clear validation. We'll use `corev1.SecretKeySelector` directly to avoid reinventing the wheel.

### Decision 2: Breaking change vs versioned migration

**Choice:** Implement as breaking change in v1alpha1 with migration documentation.

**Alternatives considered:**
- **v1alpha2 with conversion webhook**: More complex, requires webhook server, conversion logic
- **Support both fields temporarily**: Tech debt, confusing API surface

**Rationale:** Since we're in alpha (v1alpha1), breaking changes are acceptable. Document migration steps clearly. Users are expected to be early adopters who can handle manual migration.

### Decision 3: Secret watch and reconciliation

**Choice:** Add Secret to controller's watch list, reconcile when referenced Secret changes.

**Alternatives considered:**
- **Polling**: Inefficient, delays propagation
- **No watching**: Requires manual CR updates to trigger reconciliation

**Rationale:** Watching Secrets enables automatic propagation of rotated keys without user intervention. Use predicate to filter only watched Secrets (avoid reconciling every Secret in namespace).

### Decision 4: Error handling for missing secrets

**Choice:** Set `Available=False, Reason=SecretNotFound` condition when referenced Secret is missing.

**Alternatives considered:**
- **Block deployment creation**: Orphans existing deployments
- **Create empty secret**: Fails at runtime anyway

**Rationale:** Clear status condition allows users to diagnose misconfiguration. Existing deployments remain running (may have stale keys but don't get deleted).

## Risks / Trade-offs

**[Risk] Breaking change disrupts existing users**
→ **Mitigation:** Provide migration guide with example Secret manifests and kubectl commands. Consider a pre-upgrade validation tool or script.

**[Risk] Secret in different namespace causes confusion**
→ **Mitigation:** Add kubebuilder validation marker if possible, clear error message in status condition if Secret is not found.

**[Risk] Watch predicate complexity**
→ **Mitigation:** Only reconcile OpenClaw CRs when a Secret they reference changes. Use `handler.EnqueueRequestsFromMapFunc` to map Secret → owning OpenClaw CR.

**[Risk] Timing: Secret created after OpenClaw CR**
→ **Mitigation:** Controller reconciles periodically and on Secret creation. Status condition shows clear error until Secret exists.

**[Trade-off] More setup steps for users (must create Secret first)**
→ **Accepted:** This is standard Kubernetes UX (e.g., TLS secrets). Document clearly in examples.

## Migration Plan

**User migration steps:**
1. Create Secret with Gemini API key:
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: gemini-api-key
     namespace: openclaw-system
   type: Opaque
   stringData:
     api-key: "YOUR_GEMINI_API_KEY"
   ```

2. Update OpenClaw CR to reference Secret:
   ```yaml
   spec:
     geminiAPIKey:
       name: gemini-api-key
       key: api-key
   ```

3. Delete old `apiKey` field (will cause validation error if left in place)

**Rollback strategy:**
- If issues arise, users can downgrade operator version (CRD schema change is backward compatible for reads)
- Existing `openclaw-proxy-secrets` Secret remains unchanged until controller reconciles

**Deployment approach:**
- Update CRD (run `make install`)
- Deploy new operator version
- Update existing OpenClaw CRs following migration steps above

## Open Questions

- **Q:** Should we support an optional `namespace` field in `SecretKeySelector` for cross-namespace references?
  **A:** No - security concern and not standard Kubernetes pattern.

- **Q:** Should we validate that the Secret exists before creating proxy Deployment?
  **A:** No - use status conditions to report errors, but don't block existing deployments.

- **Q:** Should we auto-generate the Secret if not provided?
  **A:** No - secret management is user responsibility (may come from external source like Vault).
