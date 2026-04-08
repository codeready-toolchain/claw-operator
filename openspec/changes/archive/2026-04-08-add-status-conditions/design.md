## Context

The OpenClaw operator currently creates Deployment resources for both the main application (`openclaw`) and the API proxy (`openclaw-proxy`), but provides no feedback to users about when the instance is fully provisioned and ready for use. Kubernetes users expect CRDs to expose status conditions following the standard metav1.Condition pattern used throughout the ecosystem.

The unified Kustomize controller applies resources atomically but doesn't wait for or monitor their readiness. Users must manually check Deployment status with `kubectl get deployments` to determine if OpenClaw is usable.

## Goals / Non-Goals

**Goals:**
- Add standard Kubernetes status conditions to OpenClaw CRD
- Report `Available` condition reflecting readiness of both deployments
- Follow established Kubernetes condition patterns (type/status/reason/message)
- Maintain atomic resource application without introducing watch-based complexity
- Enable tooling (GitOps, operators, CLIs) to programmatically check OpenClaw readiness

**Non-Goals:**
- Detailed per-resource status tracking (only monitor Deployments, not ConfigMaps/PVCs/Services)
- Custom condition types beyond `Available` (can be added later if needed)
- Real-time event-driven status updates (polling during reconciliation is sufficient)
- Status conditions for individual pods or container crashes (Deployment status is the abstraction)

## Decisions

### Decision 1: Use standard metav1.Condition
Use `metav1.Condition` from `k8s.io/apimachinery` rather than custom condition types.

**Rationale:** Standard conditions are understood by kubectl, ArgoCD, Flux, and other tooling. The type includes LastTransitionTime, ObservedGeneration, and other fields expected by the ecosystem.

**Alternatives considered:**
- Custom condition struct: Would require teaching every tool about our schema
- Simple boolean Ready field: Doesn't capture reason/message or transition history

### Decision 2: Single Available condition initially
Implement only the `Available` condition type for MVP. Condition is `True` when both deployments are ready, `False` during provisioning.

**Rationale:** Available is the standard condition type for "is this thing ready to use?" questions. Other types (Progressing, Degraded) can be added later if needed.

**Alternatives considered:**
- Multiple condition types (Available, Progressing, Degraded): More information but higher complexity for first iteration
- Separate conditions per deployment: Over-engineering for current use case

### Decision 3: Check deployment status during reconciliation
After applying resources, fetch both Deployment statuses and update OpenClaw status in the same reconciliation loop.

**Rationale:** Leverages existing reconciliation flow without introducing new watches. Status checks happen naturally whenever reconciliation triggers (create, update, periodic resync).

**Alternatives considered:**
- Watch Deployments and trigger status updates: More responsive but adds complexity and potential for status-only reconciliation loops
- Separate status reconciler: Splits responsibility but duplicates watch/trigger logic

### Decision 4: Use status subresource
Update OpenClaw status via the status subresource (`client.Status().Update()`), not the main resource.

**Rationale:** Kubernetes best practice. Prevents status updates from triggering spec reconciliation. Allows different RBAC for spec vs status.

**Alternatives considered:**
- Update main resource: Causes unnecessary reconciliation loops and violates Kubernetes conventions

### Decision 5: Check Deployment Available condition
Read the `Available` condition from Deployment status rather than checking replica counts directly.

**Rationale:** Deployments already implement the Available condition following the same metav1.Condition pattern. Reusing it avoids reimplementing readiness logic.

**Alternatives considered:**
- Compare replicas == readyReplicas: Lower-level check, doesn't account for Deployment-specific readiness gates or conditions

## Risks / Trade-offs

**[Risk: Status updates may lag behind actual state]**
Mitigation: Status is updated during reconciliation, which triggers on create/update/delete plus periodic resync (default 10h). For most use cases this is acceptable. If real-time status is needed, resync interval can be reduced.

**[Risk: Failed status update doesn't block resource creation]**
Mitigation: Resource application and status update are separate operations. If status update fails, reconciliation will retry on next trigger. This is standard Kubernetes controller pattern.

**[Trade-off: Only monitoring Deployments, not all resources]**
Rationale: Deployments represent application readiness. ConfigMaps, PVCs, and Services either succeed immediately or fail during application. Monitoring them adds complexity without meaningful user value.

**[Trade-off: Status reads increase API server load]**
Mitigation: Status checks only happen during reconciliation (not constantly). Two GET requests per reconciliation is negligible for typical operator load.
