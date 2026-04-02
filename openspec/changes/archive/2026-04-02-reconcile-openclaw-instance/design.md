## Context

The OpenClawInstance controller was scaffolded by Operator SDK as a no-op skeleton. It currently logs reconciliation events but performs no actual resource management. The deployment manifest at `internal/manifests/deployment.yaml` contains the complete specification for an OpenClaw instance (including init containers, config volumes, and security settings), but is not yet used by the controller.

## Goals / Non-Goals

**Goals:**
- Implement basic reconciliation logic to create a Deployment when an OpenClawInstance is created
- Establish ownership relationship for automatic garbage collection
- Use the existing deployment manifest without modification
- Add necessary RBAC permissions for managing Deployments

**Non-Goals:**
- Status management or conditions (empty status struct for now)
- Deployment updates or configuration customization (use manifest as-is)
- ConfigMap, Secret, or PVC creation (assume these exist)
- Multi-instance support or advanced reconciliation patterns (single Deployment per OpenClawInstance)

## Decisions

### Decision 1: Embed deployment manifest using go:embed

**Choice:** Use `//go:embed` directive to embed `internal/manifests/deployment.yaml` into the controller binary at compile time.

**Rationale:**
- Simplifies deployment - no external file dependencies at runtime
- Manifest is static and doesn't change per instance
- Standard Go pattern for Kubernetes operators
- Avoids file I/O and path resolution issues

**Alternative considered:** Read manifest from filesystem at runtime
- Rejected: Adds complexity and potential failure modes (file not found, permissions)

### Decision 2: Parse manifest using controller-runtime's serializer

**Choice:** Use `scheme.Codecs.UniversalDeserializer().Decode()` to parse YAML into `appsv1.Deployment` struct.

**Rationale:**
- Standard approach in controller-runtime ecosystem
- Handles API version conversion automatically
- Validates manifest structure at parse time
- Same method used by kubectl and other controllers

### Decision 3: Set controller reference for ownership

**Choice:** Call `controllerutil.SetControllerReference()` to establish owner-dependent relationship before creating Deployment.

**Rationale:**
- Enables automatic garbage collection when OpenClawInstance is deleted
- Standard Kubernetes ownership pattern
- Prevents orphaned Deployments
- Controller-runtime provides helper function

### Decision 4: Use server-side apply semantics

**Choice:** Use `client.Create()` with error checking for `AlreadyExists`, then skip creation if Deployment exists.

**Rationale:**
- Simplest approach for initial implementation
- Idempotent behavior (safe to reconcile multiple times)
- Avoids complexity of update logic for now
- Deployment is created exactly as specified in manifest

**Alternative considered:** Use server-side apply (SSA)
- Deferred: SSA is more complex and not needed for initial implementation

## Risks / Trade-offs

**[Risk]** Deployment creation fails due to missing ConfigMap or Secret referenced in manifest  
→ **Mitigation:** Document prerequisite resources; consider adding validation or status conditions in future iteration

**[Risk]** Embedded manifest becomes stale if `internal/manifests/deployment.yaml` is updated but controller not rebuilt  
→ **Mitigation:** Standard operator development practice - rebuild on manifest changes; clear in code that manifest is embedded

**[Trade-off]** No customization per OpenClawInstance (all instances use identical Deployment spec)  
→ **Accepted:** This is v1 - spec is empty struct by design; customization can be added later when spec fields are defined

**[Risk]** Controller creates Deployment but doesn't track readiness or errors  
→ **Mitigation:** Future work - status conditions and deployment observability can be added when status struct is populated
