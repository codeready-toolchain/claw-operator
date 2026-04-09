## Context

Users currently need to manually query the Route resource to find the URL for accessing their OpenClaw instance. The operator already reconciles the Route but doesn't expose its URL in the OpenClaw resource status.

The controller uses a unified reconciliation approach with server-side apply and already has a `updateStatus` method that checks deployment readiness. This change extends that method to also populate a URL field.

## Goals / Non-Goals

**Goals:**
- Add `URL` field to `OpenClaw.Status`
- Populate URL from Route when both deployments are ready
- Handle non-OpenShift clusters gracefully (where Route CRD doesn't exist)

**Non-Goals:**
- Validating Route accessibility or performing health checks
- Supporting multiple Routes or custom domain names
- Backward compatibility for external tools (this is a status-only addition)

## Decisions

### Decision 1: Add URL to status struct
**Choice**: Add `URL string` field to `OpenClawStatus` in `api/v1alpha1/openclaw_types.go`

**Rationale**: Status is the correct place for runtime-derived information. The URL is discovered, not configured.

**Alternatives considered**:
- Add as annotation: Rejected because status fields are the idiomatic Kubernetes pattern for runtime state
- Add to spec: Rejected because spec is for desired state, not observed state

### Decision 2: Populate URL in updateStatus method
**Choice**: Extend the existing `updateStatus` method to fetch the Route and populate URL after checking deployment readiness

**Rationale**: 
- The `updateStatus` method already checks if both deployments are ready
- Keeps all status updates in one place
- URL should only be shown when the instance is actually accessible (deployments ready)

**Alternatives considered**:
- Separate reconcile loop: Rejected because it adds complexity and potential race conditions
- Always populate URL regardless of readiness: Rejected because it would show a URL before the service is actually available

### Decision 3: Graceful handling for non-OpenShift
**Choice**: Use `client.Get()` to fetch Route; if not found (404 or CRD missing), leave URL empty

**Rationale**: The Route resource is OpenShift-specific. On vanilla Kubernetes, the operator already skips creating the Route via server-side apply. Empty URL on non-OpenShift is acceptable.

**Alternatives considered**:
- Detect platform and conditionally fetch: Rejected as over-engineered; simple error handling is cleaner
- Use Ingress fallback: Rejected because current design doesn't create Ingress resources

## Risks / Trade-offs

**Risk**: URL field shows empty on non-OpenShift clusters
→ **Mitigation**: This is expected behavior; users on vanilla Kubernetes would need to configure Ingress separately (out of scope)

**Risk**: Route exists but is not yet admitted (DNS not propagated)
→ **Mitigation**: The URL reflects what's configured in the Route, not DNS availability. This matches Kubernetes semantics (status shows desired state from resources, not external validation)

**Trade-off**: URL appears/disappears if deployments transition from ready to not-ready
→ **Accepted**: This is consistent with the Available condition behavior; URL visibility tied to readiness is desirable

## Migration Plan

1. Update `api/v1alpha1/openclaw_types.go` to add `URL` field
2. Run `make manifests` to regenerate CRD YAML with new field
3. Update `internal/controller/openclaw_resource_controller.go` to populate URL in `updateStatus`
4. Run `make install` to update CRD in test cluster
5. Existing OpenClaw instances will show empty URL until next reconcile, then auto-populate

**Rollback**: CRD addition is backward-compatible (adding optional status field). No rollback needed.
