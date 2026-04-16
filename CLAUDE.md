# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kubernetes operator (Go, Kubebuilder/Operator SDK) that manages OpenClaw instances on OpenShift/Kubernetes.

**CRDs:**
- `Claw` in API group `claw.sandbox.redhat.com/v1alpha1` — Main CRD for OpenClaw instances
- `NodePairingRequestApproval` in API group `claw.sandbox.redhat.com/v1alpha1` — CRD for node pairing requests

### Claw CRD

**Spec Fields:**
- `credentials` ([]CredentialSpec, optional): Array of credential configurations for proxy credential injection per domain. Each entry defines a credential with:
  - `name` (string, required, minLength=1): Unique identifier for this credential
  - `type` (CredentialType, required): Credential injection mechanism. Enum: `apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none`
  - `secretRef` (SecretRef, optional): Reference to Kubernetes Secret holding credential value. Required for all types except `none`. SecretRef has fields `name` (Secret name, minLength=1) and `key` (data key, minLength=1)
  - `domain` (string, required, minLength=1): Domain to match against request Host header. Exact match: "api.github.com". Suffix match: ".googleapis.com" (leading dot)
  - `defaultHeaders` (map[string]string, optional): Headers injected on every proxied request (e.g., "anthropic-version: 2023-06-01")
  - `apiKey` (APIKeyConfig, optional): Required when type is `apiKey`. Fields: `header` (header name, minLength=1), `valuePrefix` (optional, e.g., "Bot ")
  - `gcp` (GCPConfig, optional): Required when type is `gcp`. Fields: `project` (GCP project ID, minLength=1), `location` (GCP region, minLength=1)
  - `pathToken` (PathTokenConfig, optional): Required when type is `pathToken`. Fields: `prefix` (URL path prefix, minLength=1)
  - `oauth2` (OAuth2Config, optional): Required when type is `oauth2`. Fields: `clientID` (minLength=1), `tokenURL` (minLength=1), `scopes` ([]string, optional)
  - `provider` (string, optional): Maps this credential to an OpenClaw LLM provider (e.g., "google", "anthropic", "openai", "openrouter"). When set, the controller configures gateway routing and dynamically generates the provider entry in `openclaw.json`. When omitted, the credential is used for MITM forward proxy only. For `provider: "google"` with `type: apiKey`, the controller uses the Gemini REST API upstream (`generativelanguage.googleapis.com/v1beta`). For `provider: "google"` with `type: gcp`, it uses Vertex AI upstream (`{location}-aiplatform.googleapis.com`).

**Status Fields:**
- `gatewayTokenSecretRef` (string, optional): Name of the Secret containing the gateway authentication token (`claw-gateway-token`)
- `url` (string, optional): HTTPS URL for accessing the Claw instance
- `conditions` ([]metav1.Condition, optional): Standard Kubernetes condition array tracking instance state. Condition types:
  - `Ready`: Indicates whether the Claw instance is ready for use
    - `Status=False, Reason=Provisioning`: Deployments are not yet ready
    - `Status=True, Reason=Ready`: Both `claw` and `claw-proxy` Deployments are available
  - `CredentialsResolved`: Tracks credential validation status
    - `Status=True, Reason=Resolved`: All credentials validated successfully
    - `Status=False, Reason=ValidationFailed`: Credential validation failed
  - `ProxyConfigured`: Tracks proxy configuration status
    - `Status=True, Reason=Configured`: Proxy configured successfully
    - `Status=False, Reason=ConfigFailed`: Proxy configuration failed

**Printcolumns:**
- `Ready`: Shows Ready condition status via JSONPath `.status.conditions[?(@.type=="Ready")].status`
- `Reason`: Shows Ready condition reason via JSONPath `.status.conditions[?(@.type=="Ready")].reason`

**Credential Type Constants:**
- `CredentialTypeAPIKey = "apiKey"` — Custom header injection
- `CredentialTypeBearer = "bearer"` — Bearer token (Authorization header)
- `CredentialTypeGCP = "gcp"` — GCP service account with OAuth2 token refresh
- `CredentialTypePathToken = "pathToken"` — Token injection into URL path
- `CredentialTypeOAuth2 = "oauth2"` — Client credentials token exchange
- `CredentialTypeNone = "none"` — Proxy allowlist, no authentication

**Validation Rules:**
- CE validation ensures type-specific config fields are present when required
- `apiKey` config required when `type == "apiKey"`
- `gcp` config required when `type == "gcp"`
- `pathToken` config required when `type == "pathToken"`
- `oauth2` config required when `type == "oauth2"`
- `secretRef` required for all types except `none`

### NodePairingRequestApproval CRD

**Spec Fields:**
- `requestID` (string, required, minLength=1): Unique identifier for the pairing request

**Status Fields:**
- `conditions` ([]metav1.Condition, optional): Standard Kubernetes condition array tracking approval state

**Printcolumns:**
- `RequestID`: Shows request ID via JSONPath `.spec.requestID`
- `Age`: Shows creation timestamp via JSONPath `.metadata.creationTimestamp`

**Version Logging:**
The operator logs version and build time at startup: `version` (short commit SHA) and `buildTime` (RFC3339). Injected via LDFLAGS during `container-build`. Local builds show defaults (`dev`/`unknown`).

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
go test ./internal/controller -run TestOpenClawConfigMap -v

# E2E (requires Kind)
make setup-test-e2e     # Create Kind cluster
make test-e2e           # Run e2e tests
make cleanup-test-e2e   # Tear down Kind cluster

# Container
make container-build IMG=<registry>/claw-operator:tag

# Dev deployment (OpenShift/Kubernetes)
make dev-setup REGISTRY=quay.io/myuser           # Build + push + deploy (one command)
make dev-build dev-push dev-deploy REGISTRY=...   # Iterate after code changes
make wait-ready NS=claw-operator                  # Wait for ready, print URL + token
make approve-pairing NS=claw-operator             # List & approve a device pairing request
make dev-cleanup                                  # Tear down
```

## Architecture

### Unified Kustomize-based controller

The operator uses a **single unified controller** that manages all resources using Kustomize and server-side apply:

**ClawResourceReconciler** (`internal/controller/claw_resource_controller.go`):
- Reconciles `Claw` CRs named exactly `"instance"` (skips all others)
- Creates gateway Secret (`claw-gateway-token`) with randomly-generated authentication token
- Validates user-provided credentials (array of CredentialSpec in CR's `credentials` field) and referenced Secrets
- Creates all resources: PVC, ConfigMap, Deployment, Services (2), NetworkPolicies (3: egress for claw, egress for proxy, ingress for gateway), proxy Deployment/ConfigMap, and Route (OpenShift only)
- Uses **three-phase reconciliation** to dynamically inject Route host into ConfigMap for CORS configuration
- Uses server-side apply for idempotent, conflict-free resource management
- Automatically labels all resources with `app.kubernetes.io/name: claw`
- Gracefully skips resources whose CRDs aren't registered (e.g., Route on vanilla Kubernetes)
- Updates status conditions (Ready, CredentialsResolved, ProxyConfigured) based on Deployment readiness and credential validation
- Supports proxy image override via `ProxyImage` field (set from `PROXY_IMAGE` env var on the manager)
- Configures proxy with multi-credential support for different LLM API domains

**Key benefits:**
- Simplified codebase: 1 controller managing all resources
- Dynamic CORS configuration: Route host automatically injected into ConfigMap at deployment time
- Field ownership: server-side apply tracks which controller owns which fields
- Consistent labeling: queryable with `kubectl get all -l app.kubernetes.io/name=claw`
- Graceful fallback: localhost CORS origin on vanilla Kubernetes (no Route CRD)
- Future-proof: adding new resources only requires updating kustomization.yaml

### Reconciliation flow

The controller uses a **three-phase reconciliation** approach to enable dynamic Route host injection into ConfigMap:

```
Reconcile(ctx, req) called
  ↓
1. Fetch Claw CR
  ↓
2. Filter by name (only "instance")
  ↓
PHASE 1: Gateway Secret
3. applyGatewaySecret(ctx, instance)
   ├─ Check if claw-gateway-token Secret already exists
   ├─ If exists and has token, preserve existing token
   ├─ If not exists or missing token, generate new 64-char hex token using crypto/rand
   ├─ Create/update claw-gateway-token Secret with token data entry
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
 │  ├─ Find claw-proxy Deployment in parsed objects
 │  ├─ Navigate to spec.template.spec.containers[].env[]
 │  ├─ Configure credential-specific environment variables based on instance.Spec.Credentials
 │  ├─ Update valueFrom.secretKeyRef to point to user's Secrets (name and key from each CredentialSpec.SecretRef)
 │  └─ Modify in-place BEFORE applying (so pod template changes trigger automatic pod restarts on Secret ref changes)
 ├─ stampSecretVersionAnnotation(ctx, objects, instance)
 │  ├─ Fetch user's Secrets to get their ResourceVersions
 │  ├─ Find claw-proxy Deployment in parsed objects
   │  ├─ Add annotations to pod template: claw.sandbox.redhat.com/<credential-name>-secret-version=<ResourceVersion>
   │  └─ This triggers pod restarts when Secret data changes (ResourceVersion updates), not just Secret reference changes
   └─ Return parsed objects
  ↓
7. injectRouteHostIntoConfigMap(objects, routeHost)
   ├─ Find claw-config ConfigMap in objects
   ├─ Extract data["openclaw.json"] string
   ├─ Replace "OPENCLAW_ROUTE_HOST" placeholder with routeHost (https://...)
   ├─ If routeHost is empty (vanilla Kubernetes): use "http://localhost:18789" fallback
   └─ Set modified JSON back into ConfigMap
  ↓
7b. injectProvidersIntoConfigMap(objects, instance.Spec.Credentials)
   ├─ Filter credentials with Provider set
   ├─ For each: resolveProviderInfo(cred) → upstream + basePath
   ├─ Build baseUrl: http://claw-proxy:8080/<credName><basePath>
   ├─ Parse openclaw.json, replace models.providers with generated entries
   └─ Credentials without Provider are MITM-only (no provider entry)
  ↓
8. Filter for remaining resources (kind != "Route")
  ↓
9. Set namespace and owner references on remaining objects
  ↓
10. applyResources(ctx, remainingObjects)
   └─ Server-side apply each resource (ConfigMap, Deployments, Services, NetworkPolicies)
  ↓
11. updateStatus(ctx, instance)
   ├─ Fetch claw Deployment and check Available condition
   ├─ Fetch claw-proxy Deployment and check Available condition
   ├─ Set Claw Ready condition based on both deployment statuses
   ├─ Set GatewayTokenSecretRef to gateway Secret name
   ├─ Populate instance.Status.URL with Route URL (if available)
   ├─ Update LastTransitionTime only if condition status changes
   └─ Update status via status subresource (client.Status().Update)
  ↓
12. Return success
```

**Route Status Polling:**
- If Route is applied but `.status.ingress[0].host` is not yet populated, reconciliation requeues with 5-second backoff
- OpenShift router typically populates Route status within 5-10 seconds
- Indefinite requeue: cluster-level Route issues should be diagnosed via `kubectl describe route claw`

**Vanilla Kubernetes Fallback:**
- On clusters without Route CRD (vanilla Kubernetes), Route application fails with `meta.IsNoMatchError`
- Controller logs "Route CRD not registered, using localhost fallback for CORS"
- ConfigMap receives `http://localhost:18789` as `allowedOrigins` value
- Control UI accessible via port-forward: `kubectl port-forward svc/claw 18789:18789`

**Key methods:**
- `Reconcile()` — main entry point, orchestrates three-phase reconciliation (gateway Secret → Route → ConfigMap injection + remaining resources)
- `generateGatewayToken()` — generates cryptographically secure 64-char hex token using crypto/rand
- `applyGatewaySecret()` — creates/updates claw-gateway-token Secret with gateway token (preserves existing token)
- `applyRouteOnly()` — applies only Route resource from Kustomize manifests (Phase 2)
- `getRouteURL()` — fetches Route and extracts `.status.ingress[0].host`, returns empty string if status not populated
- `buildKustomizedObjects()` — builds Kustomize manifests, configures proxy deployment, stamps Secret version, returns parsed objects
- `injectRouteHostIntoConfigMap()` — replaces `OPENCLAW_ROUTE_HOST` placeholder in ConfigMap with Route host (or localhost fallback)
- `injectProvidersIntoConfigMap()` — dynamically builds `models.providers` in `openclaw.json` from credentials that have `Provider` set, generating `baseUrl` entries pointing to the proxy's gateway routes
- `resolveProviderInfo()` — returns upstream host and base path for a credential's provider (handles Google Gemini vs Vertex AI, unknown providers fall back to domain)
- `applyResources()` — applies list of unstructured objects using server-side apply
- `configureProxyImage()` — overrides proxy Deployment container image if `ProxyImage` is set (from `PROXY_IMAGE` env var); no-op when empty (preserves embedded default for `make run`)
- `configureProxyDeployment()` — modifies claw-proxy deployment manifest in-place to reference user's Secret BEFORE applying (ensures pod template changes trigger restarts when Secret reference changes)
- `stampSecretVersionAnnotation()` — adds Secret ResourceVersion annotation to pod template BEFORE applying (ensures pod template changes trigger restarts when Secret data changes, not just reference)
- `getDeploymentAvailableStatus()` — fetches Deployment and checks its Available condition
- `checkDeploymentsReady()` — checks if both claw and claw-proxy Deployments are ready
- `setReadyCondition()` — sets Ready condition on Claw based on deployment states
- `updateStatus()` — updates Claw status conditions, GatewayTokenSecretRef, and URL field via status subresource
- `parseYAMLToObjects()` — converts multi-doc YAML to unstructured objects
- `readEmbeddedFile()` — reads files from embedded filesystem
- `findClawsReferencingSecret()` — maps Secret events to Claw reconcile requests (for Secret watching)

**Shared constants** (`internal/controller/claw_resource_controller.go`):
- `ClawInstanceName = "instance"` — only this name is reconciled
- `ClawConfigMapName = "claw-config"`
- `ClawPVCName = "claw-home-pvc"`
- `ClawDeploymentName = "claw"`
- `ClawGatewaySecretName = "claw-gateway-token"` — Secret containing gateway authentication token
- `ClawIngressNetworkPolicyName = "claw-ingress"` — ingress NetworkPolicy restricting gateway access to OpenShift router
- `GatewayTokenKeyName = "token"` — data key for gateway token in gateway Secret

**NodePairingRequestApprovalReconciler** (`internal/controller/nodepairingrequestapproval_controller.go`):
- Reconciles `NodePairingRequestApproval` CRs
- Currently a minimal implementation that logs reconciliation events
- Designed for future node pairing approval workflow
- RBAC includes permissions for CR and status management

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
- **deployment.yaml** — OpenClaw application pods (init container with full security context hardening)
- **service.yaml** — ClusterIP service exposing OpenClaw gateway (port 18789)
- **route.yaml** — OpenShift Route for external HTTPS access (skipped on non-OpenShift)
- **proxy-configmap.yaml** — Nginx configuration for LLM API proxy
- **proxy-deployment.yaml** — Nginx proxy (env vars reference user-managed Secrets; controller patches GEMINI_API_KEY after applying)
- **proxy-service.yaml** — ClusterIP service for proxy (port 8080)
- **networkpolicy.yaml** — Two NetworkPolicies for egress control (OpenClaw → proxy, proxy → internet)
- **ingress-networkpolicy.yaml** — Ingress NetworkPolicy restricting gateway access to OpenShift router namespace

**Runtime process:**
1. Controller loads entire `manifests/` directory into in-memory filesystem
2. Kustomize API (`krusty.MakeKustomizer`) builds resources from kustomization.yaml
3. Labels from kustomization.yaml are applied automatically to all resources
4. Controller sets namespace and owner references dynamically
5. Server-side apply sends all resources to API server with field manager "claw-operator"
6. Kubernetes handles resource creation/updates idempotently

### Key directories

- `api/v1alpha1/` — CRD type definitions
  - `claw_types.go` — Claw CRD (ClawSpec, ClawStatus, CredentialSpec, credential types and configs)
  - `nodepairingrequestapproval_types.go` — NodePairingRequestApproval CRD
  - `groupversion_info.go` — API group registration (`claw.sandbox.redhat.com/v1alpha1`)
- `internal/controller/` — ClawResourceReconciler and NodePairingRequestApprovalReconciler implementations and tests (separate test files per resource type for readability)
- `internal/assets/manifests/` — Embedded Kustomize directory with all manifests (11 total: kustomization.yaml, core resources, networking, and proxy components)
- `cmd/main.go` — Manager entrypoint, wires up controllers. Contains package-level `version` and `buildTime` variables set via LDFLAGS during build, logged at startup
- `config/` — Kustomize overlays for CRDs, RBAC, manager deployment

### Code generation

After modifying API types in `api/v1alpha1/` (`claw_types.go`, `nodepairingrequestapproval_types.go`), run both:
```bash
make manifests   # regenerate CRD YAML in config/crd/bases/
make generate    # regenerate zz_generated.deepcopy.go
```

RBAC is generated from `// +kubebuilder:rbac:...` markers on reconciler methods.

## Testing

- **Framework:** Go standard library `testing` package with testify/require and testify/assert, using `envtest` (real API server, no full cluster needed)
- **Shared setup:** `internal/controller/suite_test.go` boots envtest via `TestMain(m *testing.M)`
- **Assertions:** Use `require.NoError(t, err)` for setup/fatal errors, `assert.Equal(t, expected, actual)` for value comparisons
- **Pattern:** `Test*` functions with `t.Run()` subtests; uses `t.Cleanup()` for cleanup; uses `waitFor()` helper for async assertions (10s timeout, 250ms poll)
- **Polling helper:** `waitFor(t, timeout, interval, condition, message)` — custom helper for async checks (replaces Gomega's `Eventually()`)
- **Table-driven tests:** Use standard Go pattern with struct slices and `t.Run(tt.name, ...)` for parameterized tests
- **Test CRs:** Test Claw instances can include the optional `credentials` field for credential testing (e.g., `instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", SecretRef: &clawv1alpha1.SecretRef{Name: "test-secret", Key: "api-key"}, Domain: ".googleapis.com", APIKey: &clawv1alpha1.APIKeyConfig{Header: "x-goog-api-key"}}}`)
- **Test files:** Separate test files per resource type (`claw_configmap_test.go`, `claw_credentials_test.go`, `claw_status_test.go`, etc.)
- **E2E:** `test/e2e/` — runs against a Kind cluster, validates metrics and full deployment 

### Testing Patterns

**TestMain setup:**
```go
func TestMain(m *testing.M) {
    // Setup envtest
    testEnv = &envtest.Environment{...}
    cfg, err = testEnv.Start()
    // ... setup client, scheme
    
    code := m.Run()
    
    // Cleanup
    testEnv.Stop()
    os.Exit(code)
}
```

**Error handling with testify/require:**
```go
// Setup failures (fatal - can't continue)
require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw")

// Unexpected errors in happy path
_, err := reconciler.Reconcile(ctx, req)
require.NoError(t, err, "reconcile failed")

// Expected errors
require.Error(t, err, "should fail validation")
require.Contains(t, err.Error(), "missing required field")
```

**Value assertions with testify/assert:**
```go
// Equality checks (expected first in testify)
assert.Equal(t, "expected", actual)
assert.NotEqual(t, unwanted, actual)

// Emptiness checks
assert.Empty(t, str)
assert.NotEmpty(t, str)

// Container checks
assert.Contains(t, haystack, needle)
assert.True(t, strings.HasPrefix(url, "https://"))
```

**Async polling:**
```go
waitFor(t, timeout, interval, func() bool {
    err := k8sClient.Get(ctx, key, obj)
    return err == nil
}, "object should be created")
```

**Table-driven tests:**
```go
tests := []struct {
    name  string
    input X
    want  Y
}{
    {name: "scenario1", input: ..., want: ...},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test logic
        assert.Equal(t, tt.want, got)
    })
}
```

**Cleanup:**
```go
t.Cleanup(func() {
    deleteAndWait(ctx, &Type{}, key)
})
```

## Conventions

- Owner references are set on all created resources via `controllerutil.SetControllerReference`
- Pod security: non-root (uid 65532), restricted seccomp, all capabilities dropped (both init and main containers)
- Linting config in `.golangci.yml` — notable: `lll`, `dupl` enabled
- License header required (template in `hack/boilerplate.go.txt`)
