## Context

The Claw Operator currently manages a single `Claw` CRD for OpenClaw instances. This change introduces a second CRD (`NodePairingRequestApproval`) to handle node pairing request approvals. The operator uses Kubebuilder/Operator SDK framework with controller-runtime for reconciliation patterns.

Current state:
- Single CRD: `Claw` in `api/v1alpha1/`
- Controllers use server-side apply for resource management
- Tests use envtest with testify assertions
- RBAC generated from kubebuilder markers
- CRD manifests generated via `make manifests`, DeepCopy via `make generate`

## Goals / Non-Goals

**Goals:**
- Add NodePairingRequestApproval CRD with namespaced scope
- Implement minimal controller to watch and reconcile NodePairingRequestApproval resources
- Follow existing project patterns for API types, controllers, and tests
- Generate CRD manifests and RBAC using existing tooling
- Ensure resources serve as immutable audit records

**Non-Goals:**
- Defining full reconciliation logic for node pairing (out of scope for initial CRD setup)
- Integration with specific pairing backends or external systems
- Inter-namespace pairing or cluster-scoped variants
- Migration from any existing pairing mechanism (new capability)

## Decisions

### Decision 1: Use same API group and version as Claw CRD
**Rationale:** Consolidate all OpenClaw-related resources under `openclaw.sandbox.redhat.com/v1alpha1`. This simplifies RBAC aggregation and provides consistent API surface for users.

**Alternatives considered:**
- Separate API group (e.g., `pairing.openclaw.sandbox.redhat.com`) - Rejected: increases RBAC complexity and fragments the API for a tightly coupled operator

### Decision 2: Implement minimal controller initially
**Rationale:** Start with basic watch/reconcile scaffold that validates the resource exists. This allows CRD registration, type generation, and test infrastructure to be established. Full reconciliation logic can be added incrementally in follow-up changes.

**Alternatives considered:**
- Full implementation upfront - Rejected: increases risk and complexity for initial CRD setup

### Decision 3: Follow existing ClawResourceReconciler testing patterns
**Rationale:** The project uses envtest with testify assertions, shared `TestMain` setup in `suite_test.go`, and separate test files per concern. Reusing these patterns ensures consistency and leverages existing test infrastructure. Test helpers use `apierrors.IsNotFound(err)` for definitive deletion checks rather than `client.IgnoreNotFound` which can produce false positives.

**Alternatives considered:**
- Gomega-based testing - Rejected: project standardized on testify/require and testify/assert
- Integration tests only - Rejected: envtest provides faster feedback and better isolation
- Using `client.IgnoreNotFound` for wait conditions - Rejected: returns nil for both "exists" and "not found" states, making it unreliable for deletion verification

### Decision 4: Require both Spec field and RequestID
**Rationale:** The Spec field itself must be required (not optional) since it contains the RequestID which is essential for every NodePairingRequestApproval resource. Making both Spec and RequestID required ensures the API contract is enforced at admission time. RequestID serves as the unique identifier for pairing operations, and caller-provided IDs enable correlation with external systems.

**Implementation:**
- Spec field: `+kubebuilder:validation:Required` marker with JSON tag `json:"spec"` (no omitempty)
- RequestID field: `+kubebuilder:validation:Required` and `+kubebuilder:validation:MinLength=1`

**Alternatives considered:**
- Optional Spec with omitempty - Rejected: allows creation of resources without required configuration
- Optional RequestID - Rejected: every pairing request needs an identifier
- Auto-generated UUID in controller - Rejected: caller should control request identity for correlation with external systems

### Decision 5: Include Status subresource
**Rationale:** Following Kubernetes best practices, Status tracks reconciliation state separately from desired state (Spec). This enables the controller to report pairing progress without race conditions on Spec updates.

**Alternatives considered:**
- No Status subresource - Rejected: limits observability and violates Kubernetes conventions for managed resources

### Decision 6: Include Conditions array in Status
**Rationale:** Following Kubernetes API conventions and the existing Claw CRD pattern, include `[]metav1.Condition` in Status. This provides standardized condition tracking (type, status, reason, message, lastTransitionTime) for controller state. The specific condition types will be defined when pairing logic is implemented in a subsequent change.

**Alternatives considered:**
- Custom status fields instead of Conditions - Rejected: Conditions is the Kubernetes-standard pattern used by Claw CRD and other operators
- Defer Conditions to follow-up change - Rejected: easier to include the field structure now than add it later with API version bump

### Decision 7: Prohibit controller from deleting resources
**Rationale:** NodePairingRequestApproval resources should serve as immutable audit records of pairing requests. Once created, they provide a history trail that should not be automatically deleted by the controller. Removing the `delete` verb from controller RBAC prevents accidental or programmatic deletion while still allowing external actors (with appropriate RBAC) to manually delete resources if needed.

**Implementation:**
- Controller RBAC verbs: `get;list;watch;create;update;patch` (delete excluded)
- Controller can still modify status and spec fields
- Resources can only be deleted by users/services with explicit delete permissions

**Alternatives considered:**
- Allow deletion but add finalizers - Rejected: adds complexity for a simple immutability requirement
- Allow deletion with warnings in logs - Rejected: better to enforce immutability at RBAC level
- Include delete permission initially - Rejected: contradicts the audit trail use case

## Risks / Trade-offs

**[Risk]** New CRD increases API surface area → **Mitigation:** Use kubebuilder validation markers to enforce field constraints at admission time

**[Risk]** Controller without full logic may confuse users → **Mitigation:** Document in CRD printcolumns and status conditions that pairing logic is not yet implemented

**[Trade-off]** Minimal controller means deferred implementation → Accepted: allows foundational work to proceed while pairing logic is designed separately

## Migration Plan

1. Generate API types with kubebuilder markers (`api/v1alpha1/nodepairingrequestapproval_types.go`)
   - Include `+kubebuilder:validation:Required` on Spec field
   - Use JSON tag `json:"spec"` without omitempty
   - Include `+kubebuilder:validation:Required` and MinLength validation on RequestID
2. Run `make manifests` to generate CRD YAML
3. Run `make generate` to generate DeepCopy methods
4. Implement controller scaffold with watch setup
5. Add RBAC markers to controller (excluding delete verb)
6. Register controller in `cmd/main.go`
7. Write envtest-based controller tests with `apierrors.IsNotFound` for deletion checks
8. Run `make install` to install CRDs in development cluster
9. Verify CRD registration with `kubectl get crd nodepairingrequestapprovals.openclaw.sandbox.redhat.com`

**Rollback:** Remove CRD with `kubectl delete crd nodepairingrequestapprovals.openclaw.sandbox.redhat.com` and revert code changes. No data migration needed (new feature).

## Open Questions

- What specific condition types (e.g., Ready, Paired) should be used? (defer to follow-up change implementing pairing logic)
- Should RequestID have a validation regex pattern? (e.g., UUID format)
- Do we need printcolumns for `kubectl get nodepairingrequestapproval` output beyond RequestID and age?
