## Context

Currently, the system has a single `OpenClawInstanceReconciler` that handles both ConfigMap and Deployment creation in a single reconciliation loop. The controller creates the ConfigMap first, then waits for it to exist before creating the Deployment.

This works but couples two independent concerns (ConfigMap lifecycle and Deployment lifecycle) in one controller. As the operator grows, this coupling will make the code harder to maintain and test.

## Goals / Non-Goals

**Goals:**
- Separate ConfigMap and Deployment management into dedicated controllers
- Each controller has a single, clear responsibility
- Maintain existing behavior: ConfigMap created before Deployment
- Improve testability through focused test files
- Keep RBAC permissions scoped to each controller's needs

**Non-Goals:**
- Changing the order of resource creation (ConfigMap still first, Deployment second)
- Adding coordination logic between controllers (they operate independently)
- Modifying the manifests or resource specifications
- Changing how resources are embedded

## Decisions

### Decision 1: Two independent controllers watching the same resource

**Choice:** Both `OpenClawConfigMapController` and `OpenClawDeploymentController` watch the same `OpenClawInstance` resource.

**Rationale:** Each controller needs to know when an OpenClawInstance is created/updated/deleted to manage its respective resource. Having both watch the same resource is simpler than introducing inter-controller communication.

**Alternative considered:** Single controller with helper functions for ConfigMap and Deployment.
- **Rejected because:** Doesn't achieve separation of concerns. Tests would still be coupled.

### Decision 2: No explicit ordering mechanism between controllers

**Choice:** Controllers operate independently. The natural Kubernetes event flow ensures ConfigMap is created first because OpenClawConfigMapController creates it immediately, while OpenClawDeploymentController can check for ConfigMap existence.

**Rationale:** Kubernetes controllers are designed to be eventually consistent. Adding explicit ordering (e.g., through status fields) adds complexity without clear benefit.

**Alternative considered:** Add status field to OpenClawInstance to coordinate creation order.
- **Rejected because:** Over-engineering for the current need. The existing approach (check if ConfigMap exists) is simpler.

### Decision 3: Deployment controller checks for ConfigMap existence

**Choice:** `OpenClawDeploymentController` checks for the 'openclaw-config' ConfigMap before creating the Deployment, same as the current implementation.

**Rationale:** Maintains the desired behavior where Deployment is only created after ConfigMap exists, without requiring coordination between controllers.

### Decision 4: Each controller has focused RBAC permissions

**Choice:** 
- `OpenClawConfigMapController`: permissions for OpenClawInstance (get/list/watch) and ConfigMap (create/get/list/watch)
- `OpenClawDeploymentController`: permissions for OpenClawInstance (get/list/watch), ConfigMap (get), and Deployment (create/get/list/watch)

**Rationale:** Follows principle of least privilege. Each controller only gets permissions for resources it manages.

### Decision 5: Separate test files mirror controller separation

**Choice:** 
- `openclawconfigmap_controller_test.go` tests ConfigMap controller
- `openclawdeployment_controller_test.go` tests Deployment controller
- Shared test suite setup can remain

**Rationale:** Makes tests easier to navigate and maintain. Each test file focuses on one controller's behavior.

## Risks / Trade-offs

**Risk:** Both controllers reconciling simultaneously could cause race conditions.
→ **Mitigation:** Kubernetes reconciliation is idempotent. Both controllers check for resource existence before creating. Multiple reconciliations are safe.

**Risk:** Splitting increases total lines of code (two controllers vs one).
→ **Accepted trade-off:** Code is more maintainable and testable. Each controller is simpler to understand.

**Risk:** Deployment controller depends on ConfigMap controller completing first.
→ **Mitigation:** Deployment controller explicitly checks for ConfigMap existence, same as current behavior. If ConfigMap doesn't exist, it returns success and waits for the next reconciliation.

**Trade-off:** More controllers to register in main.go.
→ **Accepted:** Clear structure worth the minor verbosity.
