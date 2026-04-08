# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kubernetes operator (Go, Kubebuilder/Operator SDK) that manages OpenClaw instances on OpenShift/Kubernetes. CRD: `OpenClaw` in API group `openclaw.sandbox.redhat.com/v1alpha1`.

**CRD Spec Fields:**
- `apiKey` (string, required): API key for LLM provider authentication (currently Gemini). Injected into `openclaw-proxy-secrets` Secret under `GEMINI_API_KEY` data entry.

**CRD Status Fields:**
- `conditions` ([]metav1.Condition, optional): Standard Kubernetes condition array tracking instance state. Currently includes:
  - `Available` condition: Indicates whether the OpenClaw instance is ready for use
    - `Status=False, Reason=Provisioning`: Deployments are not yet ready
    - `Status=True, Reason=Ready`: Both `openclaw` and `openclaw-proxy` Deployments are available

**Version Logging:**
The operator logs version and build time at startup: `version` (short commit SHA) and `buildTime` (RFC3339). Injected via LDFLAGS during `docker-build`. Local builds show defaults (`dev`/`unknown`).

## Common Commands

```bash
make build              # Build manager binary
make test               # Run unit tests (envtest-based, excludes e2e)
make lint               # Run golangci-lint
make lint-fix           # Lint with auto-fix
make fmt                # go fmt
make vet                # go vet
make manifests          # Generate CRD YAML and RBAC from kubebuilder markers
make generate           # Generate DeepCopy methods
make install            # Install CRDs to cluster
make run                # Run controller locally against cluster

# Single test
go test ./internal/controller -run TestControllerSuite -v
# or with Ginkgo focus:
go test ./internal/controller -ginkgo.focus "ConfigMap" -v

# E2E (requires Kind)
make setup-test-e2e     # Create Kind cluster
make test-e2e           # Run e2e tests
make cleanup-test-e2e   # Tear down Kind cluster

# Docker
make docker-build IMG=<registry>/openclaw-operator:tag
```

## Architecture

### Unified Kustomize-based controller

The operator uses a **single unified controller** that manages all resources atomically using Kustomize and server-side apply:

**OpenClawResourceReconciler** (`internal/controller/openclaw_resource_controller.go`):
- Reconciles `OpenClaw` CRs named exactly `"instance"` (skips all others)
- Creates gateway Secret (`openclaw-secrets`) with randomly-generated authentication token
- Creates proxy Secret (`openclaw-proxy-secrets`) with API key from CR's `apiKey` field
- Creates all resources: PVC, ConfigMap, Deployment, Services (2), NetworkPolicies (2), proxy Deployment/ConfigMap, and Route (OpenShift only)
- All resources created atomically as a unit (no ordering dependencies or race conditions)
- Uses server-side apply for idempotent, conflict-free resource management
- Automatically labels all resources with `app.kubernetes.io/name: openclaw`
- Gracefully skips resources whose CRDs aren't registered (e.g., Route on vanilla Kubernetes)
- Updates status conditions based on Deployment readiness after applying resources

**Key benefits:**
- Simplified codebase: 1 controller (~200 LOC) vs 3 separate controllers (~400 LOC)
- Atomic updates: all-or-nothing resource creation prevents partial state
- Field ownership: server-side apply tracks which controller owns which fields
- Consistent labeling: queryable with `kubectl get all -l app.kubernetes.io/name=openclaw`
- Future-proof: adding new resources only requires updating kustomization.yaml

### Reconciliation flow

```
Reconcile(ctx, req) called
  ↓
1. Fetch OpenClaw CR
  ↓
2. Filter by name (only "instance")
  ↓
3. applyGatewaySecret(ctx, instance)
   ├─ Check if openclaw-secrets Secret already exists
   ├─ If exists and has OPENCLAW_GATEWAY_TOKEN, preserve existing token
   ├─ If not exists or missing token, generate new 64-char hex token using crypto/rand
   ├─ Create/update openclaw-secrets Secret with OPENCLAW_GATEWAY_TOKEN data entry
   ├─ Set owner reference (for garbage collection)
   └─ Server-side apply Secret (Patch with Apply)
  ↓
4. applyProxySecret(ctx, instance)
   ├─ Read apiKey from instance.Spec.APIKey
   ├─ Create/update openclaw-proxy-secrets Secret with GEMINI_API_KEY data entry
   ├─ Set owner reference (for garbage collection)
   └─ Server-side apply Secret (Patch with Apply)
  ↓
5. applyKustomizedResources(ctx, instance)
   ├─ Build Kustomize in-memory from embedded manifests
   ├─ Parse YAML into unstructured objects
   ├─ Set namespace = instance.Namespace on each resource
   ├─ Set owner reference (for garbage collection)
   └─ Server-side apply each resource (Patch with Apply)
  ↓
6. updateStatus(ctx, instance)
   ├─ Fetch openclaw Deployment and check Available condition
   ├─ Fetch openclaw-proxy Deployment and check Available condition
   ├─ Set OpenClaw Available condition based on both deployment statuses
   ├─ Update LastTransitionTime only if condition status changes
   └─ Update status via status subresource (client.Status().Update)
  ↓
7. Return success
```

**Key methods:**
- `Reconcile()` — main entry point, orchestration logic
- `generateGatewayToken()` — generates cryptographically secure 64-char hex token using crypto/rand
- `applyGatewaySecret()` — creates/updates openclaw-secrets Secret with gateway token (preserves existing token)
- `applyProxySecret()` — creates/updates openclaw-proxy-secrets Secret with API key from CR
- `applyKustomizedResources()` — builds and applies all manifests via Kustomize + SSA
- `getDeploymentAvailableStatus()` — fetches Deployment and checks its Available condition
- `checkDeploymentsReady()` — checks if both openclaw and openclaw-proxy Deployments are ready
- `setAvailableCondition()` — sets Available condition on OpenClaw based on deployment states
- `updateStatus()` — updates OpenClaw status conditions via status subresource
- `parseYAMLToObjects()` — converts multi-doc YAML to unstructured objects
- `readEmbeddedFile()` — reads files from embedded filesystem

**Shared constants** (`internal/controller/openclaw_resource_controller.go`):
- `OpenClawInstanceName = "instance"` — only this name is reconciled
- `OpenClawConfigMapName = "openclaw-config"`
- `OpenClawPVCName = "openclaw-home-pvc"`
- `OpenClawDeploymentName = "openclaw"`
- `OpenClawProxySecretName = "openclaw-proxy-secrets"` — Secret containing LLM API keys
- `OpenClawGatewaySecretName = "openclaw-secrets"` — Secret containing gateway authentication token
- `GeminiAPIKeyName = "GEMINI_API_KEY"` — data key for Gemini API key in proxy Secret
- `GatewayTokenKeyName = "OPENCLAW_GATEWAY_TOKEN"` — data key for gateway token in gateway Secret

### Kustomize-based manifest generation

Kubernetes manifests are embedded via `//go:embed` as a complete directory in `internal/assets/manifests.go`:

```go
//go:embed manifests
var ManifestsFS embed.FS
```

The `internal/assets/manifests/` directory contains:
- **kustomization.yaml** — defines labels and resource list
- **configmap.yaml** — OpenClaw configuration
- **pvc.yaml** — persistent storage (10Gi ReadWriteOnce)
- **deployment.yaml** — OpenClaw application pods
- **service.yaml** — ClusterIP service exposing OpenClaw gateway (port 18789)
- **route.yaml** — OpenShift Route for external HTTPS access (skipped on non-OpenShift)
- **proxy-configmap.yaml** — Nginx configuration for LLM API proxy
- **proxy-deployment.yaml** — Nginx proxy with credential injection from Secrets
- **proxy-service.yaml** — ClusterIP service for proxy (port 8080)
- **networkpolicy.yaml** — Two NetworkPolicies for egress control (OpenClaw → proxy, proxy → internet)

**Runtime process:**
1. Controller loads entire `manifests/` directory into in-memory filesystem
2. Kustomize API (`krusty.MakeKustomizer`) builds resources from kustomization.yaml
3. Labels from kustomization.yaml are applied automatically to all resources
4. Controller sets namespace and owner references dynamically
5. Server-side apply sends all resources to API server with field manager "openclaw-operator"
6. Kubernetes handles resource creation/updates idempotently

### Key directories

- `api/v1alpha1/` — CRD type definitions (OpenClawSpec, OpenClawStatus)
- `internal/controller/` — OpenClawReconciler implementation and tests (separate test files per resource type for readability)
- `internal/assets/manifests/` — Embedded Kustomize directory with all manifests (10 total: kustomization.yaml, core resources, networking, and proxy components)
- `cmd/main.go` — Manager entrypoint, wires up the unified OpenClawReconciler. Contains package-level `version` and `buildTime` variables set via LDFLAGS during build, logged at startup
- `config/` — Kustomize overlays for CRDs, RBAC, manager deployment

### Code generation

After modifying `api/v1alpha1/openclaw_types.go`, run both:
```bash
make manifests   # regenerate CRD YAML in config/crd/bases/
make generate    # regenerate zz_generated.deepcopy.go
```

RBAC is generated from `// +kubebuilder:rbac:...` markers on reconciler methods.

## Testing

- **Framework:** Ginkgo v2 + Gomega with `envtest` (real API server, no full cluster needed)
- **Shared setup:** `internal/controller/suite_test.go` boots envtest once per suite
- **Pattern:** Describe/Context/It blocks with `AfterEach` cleanup; uses `Eventually()` for async assertions (10s timeout, 250ms poll)
- **Test CRs:** All test OpenClaw instances must include the required `apiKey` field (e.g., `instance.Spec.APIKey = "test-api-key"`)
- **Test files:** Separate test files per resource type (`openclaw_configmap_controller_test.go`, `openclaw_secret_controller_test.go`, `openclaw_status_controller_test.go`, etc.)
- **E2E:** `test/e2e/` — runs against a Kind cluster, validates metrics and full deployment

## Conventions

- Owner references are set on all created resources via `controllerutil.SetControllerReference`
- Pod security: non-root (uid 65532), restricted seccomp, all capabilities dropped
- Linting config in `.golangci.yml` — notable: `lll`, `dupl`, `ginkgolinter` enabled
- License header required (template in `hack/boilerplate.go.txt`)
