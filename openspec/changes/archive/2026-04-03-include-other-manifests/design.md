## Context

The operator uses a unified Kustomize-based controller pattern where all resources are embedded via `//go:embed` and applied atomically using server-side apply. Currently, only 4 manifests are embedded (configmap.yaml, deployment.yaml, pvc.yaml, kustomization.yaml), but `internal/manifests/` contains 6 additional manifests that provide critical features:

- **networkpolicy.yaml**: Two NetworkPolicy resources for egress control
- **proxy-configmap.yaml**: Nginx configuration for LLM API proxy
- **proxy-deployment.yaml**: Nginx proxy deployment with credential injection
- **proxy-service.yaml**: ClusterIP service for the proxy
- **route.yaml**: OpenShift Route for external access
- **service.yaml**: ClusterIP service for OpenClaw gateway

The embedded manifest directory structure is defined in `internal/assets/manifests.go` using `//go:embed manifests`, and the controller loads the entire directory at runtime using Kustomize's in-memory filesystem.

## Goals / Non-Goals

**Goals:**
- Include all 6 missing manifests in the embedded assets directory
- Ensure Kustomize automatically applies all resources as part of unified reconciliation
- Grant OpenClawReconciler RBAC permissions for new resource types (NetworkPolicy, Service, Route)
- Maintain atomic all-or-nothing resource creation behavior

**Non-Goals:**
- Modifying the unified controller architecture or reconciliation logic
- Changing the server-side apply mechanism
- Updating individual manifest contents (copy as-is from source)
- Adding validation or configuration options for proxy/networking features

## Decisions

### Decision 1: Copy manifests verbatim from source directory

**Rationale**: The manifests in `internal/manifests/` are already production-ready with correct labels, security contexts, and configurations. Copying them as-is ensures consistency and avoids introducing errors.

**Alternatives considered**:
- Generating manifests programmatically → Rejected: adds complexity without benefit when static manifests already exist
- Symlinking source manifests → Rejected: `//go:embed` doesn't follow symlinks

### Decision 2: Use existing kustomization.yaml resource list from source

**Rationale**: The source `internal/manifests/kustomization.yaml` already has the complete resource list in the correct order. Using this list ensures all resources are included and Kustomize processes them correctly.

**Alternatives considered**:
- Manually constructing the resource list → Rejected: error-prone and duplicates existing work
- Auto-discovering resources in the directory → Rejected: Kustomize requires explicit resource declarations in kustomization.yaml

### Decision 3: Add RBAC markers to OpenClawReconciler for new types

**Rationale**: The controller uses Kubebuilder RBAC markers (`// +kubebuilder:rbac:...`) to generate permissions. Adding markers for NetworkPolicy, Service, and Route ensures the operator can create these resources.

**Alternatives considered**:
- Manually editing `config/rbac/role.yaml` → Rejected: generated file, changes would be overwritten by `make manifests`
- Creating separate controller for each resource type → Rejected: contradicts the unified controller architecture

### Decision 4: Preserve embedded directory structure with labels

**Rationale**: The current kustomization.yaml includes a `labels` section that applies `app.kubernetes.io/name: openclaw` to all resources. This must be preserved when copying the new kustomization.yaml to maintain consistent labeling.

**Alternatives considered**:
- Removing labels and applying them programmatically → Rejected: Kustomize labels already work correctly
- Using different labels for new resources → Rejected: breaks queryability with `kubectl get all -l app.kubernetes.io/name=openclaw`

## Risks / Trade-offs

**[Risk]** Proxy deployment references Secrets that may not exist (e.g., `openclaw-proxy-secrets`, `openclaw-gcp-credentials`)  
→ **Mitigation**: All Secret references use `optional: true`, so pods will start even if Secrets are missing

**[Risk]** Route resource is OpenShift-specific and will fail on vanilla Kubernetes  
→ **Mitigation**: Server-side apply will skip the Route on non-OpenShift clusters (CRD not registered). Operator should log but not fail reconciliation.

**[Risk]** NetworkPolicy egress rules may conflict with cluster network policies  
→ **Mitigation**: Policies are permissive enough (allow DNS, specific pod selectors) to work in most clusters. Users can customize via CR spec in future iterations.

**[Trade-off]** All 10 resources are created atomically on every reconciliation  
→ Benefit: Prevents partial state and race conditions  
→ Cost: Slightly higher API server load during reconciliation (acceptable for low-frequency reconciles)
