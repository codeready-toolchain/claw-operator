# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kubernetes operator (Go, Kubebuilder/Operator SDK) that manages OpenClaw instances on OpenShift/Kubernetes. CRD: `OpenClaw` in API group `openclaw.sandbox.redhat.com/v1alpha1`.

**CRD Spec Fields:**
- `geminiAPIKey` (SecretRef, required): Reference to a user-managed Secret containing the Gemini API key. SecretRef is a custom type with fields `name` (Secret name, minLength=1) and `key` (data key, minLength=1), both validated at admission time. Controller validates the Secret exists and configures the `openclaw-proxy` deployment to reference it directly via environment variable. Pod restarts are triggered automatically when: (1) Secret reference changes (name or key field), or (2) Secret data changes (controller stamps Secret ResourceVersion as pod template annotation).

**CRD Status Fields:**
- `conditions` ([]metav1.Condition, optional): Standard Kubernetes condition array tracking instance state. Currently includes:
  - `Available` condition: Indicates whether the OpenClaw instance is ready for use
    - `Status=False, Reason=Provisioning`: Deployments are not yet ready
    - `Status=True, Reason=Ready`: Both `openclaw` and `openclaw-proxy` Deployments are available

**CRD Printcolumns:**
The CRD defines custom printcolumns for `kubectl get openclaw` output:
- `Ready`: Shows Available condition status (True/False/Unknown) via JSONPath `.status.conditions[?(@.type=="Available")].status`
- `Reason`: Shows Available condition reason (Provisioning/Ready) via JSONPath `.status.conditions[?(@.type=="Available")].reason`

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

The operator uses a **single unified controller** that manages all resources using Kustomize and server-side apply:

**OpenClawResourceReconciler** (`internal/controller/openclaw_resource_controller.go`):
- Reconciles `OpenClaw` CRs named exactly `"instance"` (skips all others)
- Creates gateway Secret (`openclaw-secrets`) with randomly-generated authentication token
- Validates user-provided Secret containing Gemini API key (referenced in CR's `geminiAPIKey` field)
- Creates all resources: PVC, ConfigMap, Deployment, Services (2), NetworkPolicies (2), proxy Deployment/ConfigMap, and Route (OpenShift only)
- Uses **three-phase reconciliation** to dynamically inject Route host into ConfigMap for CORS configuration
- Uses server-side apply for idempotent, conflict-free resource management
- Automatically labels all resources with `app.kubernetes.io/name: openclaw`
- Gracefully skips resources whose CRDs aren't registered (e.g., Route on vanilla Kubernetes)
- Updates status conditions based on Deployment readiness after applying resources

**Key benefits:**
- Simplified codebase: 1 controller managing all resources
- Dynamic CORS configuration: Route host automatically injected into ConfigMap at deployment time
- Field ownership: server-side apply tracks which controller owns which fields
- Consistent labeling: queryable with `kubectl get all -l app.kubernetes.io/name=openclaw`
- Graceful fallback: localhost CORS origin on vanilla Kubernetes (no Route CRD)
- Future-proof: adding new resources only requires updating kustomization.yaml

### Reconciliation flow

The controller uses a **three-phase reconciliation** approach to enable dynamic Route host injection into ConfigMap:

```
Reconcile(ctx, req) called
  ↓
1. Fetch OpenClaw CR
  ↓
2. Filter by name (only "instance")
  ↓
PHASE 1: Gateway Secret
3. applyGatewaySecret(ctx, instance)
   ├─ Check if openclaw-secrets Secret already exists
   ├─ If exists and has OPENCLAW_GATEWAY_TOKEN, preserve existing token
   ├─ If not exists or missing token, generate new 64-char hex token using crypto/rand
   ├─ Create/update openclaw-secrets Secret with OPENCLAW_GATEWAY_TOKEN data entry
   ├─ Set owner reference (for garbage collection)
   └─ Server-side apply Secret (Patch with Apply)
  ↓
PHASE 2: Route Application and Host Resolution
4. applyRouteOnly(ctx, instance)
   ├─ Build Kustomize manifests
   ├─ Filter for Route resources only
   ├─ Set namespace and owner reference
   └─ Server-side apply Route (skips with NoMatchError on vanilla Kubernetes)
  ↓
5. getRouteURL(ctx, instance)
   ├─ Fetch Route resource
   ├─ Extract .status.ingress[0].host (authoritative source populated by OpenShift router)
   ├─ If status not yet populated: requeue with 5s backoff
   ├─ If Route CRD not registered: continue with empty routeHost (localhost fallback)
   └─ Return https://<route-host> or empty string
  ↓
PHASE 3: ConfigMap Injection and Remaining Resources
6. buildKustomizedObjects(ctx, instance)
   ├─ Build Kustomize in-memory from embedded manifests
   ├─ Parse YAML into unstructured objects
   ├─ configureProxyDeployment(objects, instance)
   │  ├─ Find openclaw-proxy Deployment in parsed objects
   │  ├─ Navigate to spec.template.spec.containers[].env[]
   │  ├─ Find GEMINI_API_KEY env var
   │  ├─ Update valueFrom.secretKeyRef to point to user's Secret (name and key from instance.Spec.GeminiAPIKey)
   │  └─ Modify in-place BEFORE applying (so pod template changes trigger automatic pod restarts on Secret ref changes)
   ├─ stampSecretVersionAnnotation(ctx, objects, instance)
   │  ├─ Fetch user's Secret to get its ResourceVersion
   │  ├─ Find openclaw-proxy Deployment in parsed objects
   │  ├─ Add annotation to pod template: openclaw.sandbox.redhat.com/gemini-secret-version=<ResourceVersion>
   │  └─ This triggers pod restarts when Secret data changes (ResourceVersion updates), not just Secret reference changes
   └─ Return parsed objects
  ↓
7. injectRouteHostIntoConfigMap(objects, routeHost)
   ├─ Find openclaw-config ConfigMap in objects
   ├─ Extract data["openclaw.json"] string
   ├─ Replace "OPENCLAW_ROUTE_HOST" placeholder with routeHost (https://...)
   ├─ If routeHost is empty (vanilla Kubernetes): use "http://localhost:18789" fallback
   └─ Set modified JSON back into ConfigMap
  ↓
8. Filter for remaining resources (kind != "Route")
  ↓
9. Set namespace and owner references on remaining objects
  ↓
10. applyResources(ctx, remainingObjects)
   └─ Server-side apply each resource (ConfigMap, Deployments, Services, NetworkPolicies)
  ↓
11. updateStatus(ctx, instance)
   ├─ Fetch openclaw Deployment and check Available condition
   ├─ Fetch openclaw-proxy Deployment and check Available condition
   ├─ Set OpenClaw Available condition based on both deployment statuses
   ├─ Populate instance.Status.URL with Route URL (if available)
   ├─ Update LastTransitionTime only if condition status changes
   └─ Update status via status subresource (client.Status().Update)
  ↓
12. Return success
```

**Route Status Polling:**
- If Route is applied but `.status.ingress[0].host` is not yet populated, reconciliation requeues with 5-second backoff
- OpenShift router typically populates Route status within 5-10 seconds
- Indefinite requeue: cluster-level Route issues should be diagnosed via `kubectl describe route openclaw`

**Vanilla Kubernetes Fallback:**
- On clusters without Route CRD (vanilla Kubernetes), Route application fails with `meta.IsNoMatchError`
- Controller logs "Route CRD not registered, using localhost fallback for CORS"
- ConfigMap receives `http://localhost:18789` as `allowedOrigins` value
- Control UI accessible via port-forward: `kubectl port-forward svc/openclaw 18789:18789`

**Key methods:**
- `Reconcile()` — main entry point, orchestrates three-phase reconciliation (gateway Secret → Route → ConfigMap injection + remaining resources)
- `generateGatewayToken()` — generates cryptographically secure 64-char hex token using crypto/rand
- `applyGatewaySecret()` — creates/updates openclaw-secrets Secret with gateway token (preserves existing token)
- `applyRouteOnly()` — applies only Route resource from Kustomize manifests (Phase 2)
- `getRouteURL()` — fetches Route and extracts `.status.ingress[0].host`, returns empty string if status not populated
- `buildKustomizedObjects()` — builds Kustomize manifests, configures proxy deployment, stamps Secret version, returns parsed objects
- `injectRouteHostIntoConfigMap()` — replaces `OPENCLAW_ROUTE_HOST` placeholder in ConfigMap with Route host (or localhost fallback)
- `applyKustomizedResources()` — builds and applies manifests via Kustomize + SSA with optional filter function
- `applyResources()` — applies list of unstructured objects using server-side apply
- `configureProxyDeployment()` — modifies openclaw-proxy deployment manifest in-place to reference user's Secret BEFORE applying (ensures pod template changes trigger restarts when Secret reference changes)
- `stampSecretVersionAnnotation()` — adds Secret ResourceVersion annotation to pod template BEFORE applying (ensures pod template changes trigger restarts when Secret data changes, not just reference)
- `getDeploymentAvailableStatus()` — fetches Deployment and checks its Available condition
- `checkDeploymentsReady()` — checks if both openclaw and openclaw-proxy Deployments are ready
- `setAvailableCondition()` — sets Available condition on OpenClaw based on deployment states
- `updateStatus()` — updates OpenClaw status conditions and URL field via status subresource
- `parseYAMLToObjects()` — converts multi-doc YAML to unstructured objects
- `readEmbeddedFile()` — reads files from embedded filesystem
- `findOpenClawsReferencingSecret()` — maps Secret events to OpenClaw reconcile requests (for Secret watching)

**Shared constants** (`internal/controller/openclaw_resource_controller.go`):
- `OpenClawInstanceName = "instance"` — only this name is reconciled
- `OpenClawConfigMapName = "openclaw-config"`
- `OpenClawPVCName = "openclaw-home-pvc"`
- `OpenClawDeploymentName = "openclaw"`
- `OpenClawGatewaySecretName = "openclaw-secrets"` — Secret containing gateway authentication token
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
- **proxy-deployment.yaml** — Nginx proxy (env vars reference user-managed Secrets; controller patches GEMINI_API_KEY after applying)
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
- **Test CRs:** All test OpenClaw instances must include the required `geminiAPIKey` field (e.g., `instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{Name: "test-secret", Key: "api-key"}`)
- **Test files:** Separate test files per resource type (`openclaw_configmap_controller_test.go`, `openclaw_secretref_controller_test.go`, `openclaw_status_controller_test.go`, etc.)
- **E2E:** `test/e2e/` — runs against a Kind cluster, validates metrics and full deployment

## Conventions

- Owner references are set on all created resources via `controllerutil.SetControllerReference`
- Pod security: non-root (uid 65532), restricted seccomp, all capabilities dropped
- Linting config in `.golangci.yml` — notable: `lll`, `dupl`, `ginkgolinter` enabled
- License header required (template in `hack/boilerplate.go.txt`)
