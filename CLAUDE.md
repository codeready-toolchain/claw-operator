# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kubernetes operator (Go, Kubebuilder/Operator SDK) that manages OpenClaw instances on OpenShift/Kubernetes.

**CRDs:**
- `Claw` in API group `claw.sandbox.redhat.com/v1alpha1` — Main CRD for OpenClaw instances
- `ClawDevicePairingRequest` in API group `claw.sandbox.redhat.com/v1alpha1` — CRD for device pairing requests

### Claw CRD

**Spec Fields:**
- `configMode` (ConfigMode, optional, default="merge"): Controls how operator config is applied on pod start. `merge` deep-merges operator settings into the existing user config (preserving user-owned keys like plugins, channels, agent customizations). `overwrite` fully replaces `openclaw.json` on every pod start with operator-managed config.
- `credentials` ([]CredentialSpec, optional): Array of credential configurations for proxy credential injection per domain. Each entry defines a credential with:
  - `name` (string, required, minLength=1): Unique identifier for this credential
  - `type` (CredentialType, required): Credential injection mechanism. Enum: `apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none`, `kubernetes`
  - `secretRef` (SecretRef, optional): Reference to Kubernetes Secret holding credential value. Required for all types except `none`. SecretRef has fields `name` (Secret name, minLength=1) and `key` (data key, minLength=1)
  - `domain` (string, optional): Domain to match against request Host header. Exact match: "api.github.com". Suffix match: ".googleapis.com" (leading dot). Optional for known providers (google, anthropic) and gcp type — the operator infers the default
  - `defaultHeaders` (map[string]string, optional): Headers injected on every proxied request (e.g., "anthropic-version: 2023-06-01")
  - `apiKey` (APIKeyConfig, optional): Required when type is `apiKey`. Fields: `header` (header name, minLength=1), `valuePrefix` (optional, e.g., "Bot ")
  - `gcp` (GCPConfig, optional): Required when type is `gcp`. Fields: `project` (GCP project ID, minLength=1), `location` (GCP region, minLength=1)
  - `pathToken` (PathTokenConfig, optional): Required when type is `pathToken`. Fields: `prefix` (URL path prefix, minLength=1)
  - `oauth2` (OAuth2Config, optional): Required when type is `oauth2`. Fields: `clientID` (minLength=1), `tokenURL` (minLength=1), `scopes` ([]string, optional)
  - `provider` (string, optional): Maps this credential to an OpenClaw LLM provider (e.g., "google", "anthropic", "openai", "openrouter", "xai"). When set, the controller configures gateway routing, dynamically generates the provider entry in `operator.json`, and populates the model catalog (`agents.defaults.models`) from known models for that provider. When omitted, the credential is used for MITM forward proxy only. For `provider: "google"` with `type: apiKey`, the controller uses the Gemini REST API upstream (`generativelanguage.googleapis.com/v1beta`). For `provider: "google"` with `type: gcp`, it uses Vertex AI upstream (`{location}-aiplatform.googleapis.com`).
  - `allowedPaths` ([]string, optional): Restricts which URL paths the proxy permits for this domain. Each entry is a path prefix (e.g., "/v1/api/"). If empty, all paths are allowed. Used by builtin passthrough domains (e.g., `raw.githubusercontent.com` is restricted to `/BerriAI/litellm/` and `/WhiskeySockets/Baileys/`) and available for user-defined credentials.

**Status Fields:**
- `gatewayTokenSecretRef` (string, optional): Name of the Secret containing the gateway authentication token (`<instance>-gateway-token`)
- `url` (string, optional): HTTPS URL for accessing the Claw instance (includes gateway token as URL fragment `#token=...` when ready)
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
- `CredentialTypeKubernetes = "kubernetes"` — Kubernetes API server auth via kubeconfig (token-based only). The Secret holds a standard kubeconfig file; the operator parses it to extract cluster servers, contexts, and tokens. A sanitized kubeconfig (tokens replaced with placeholder) is mounted on the gateway pod. The proxy injects real tokens per `hostname:port`

**Validation Rules:**
- CE validation ensures type-specific config fields are present when required
- `apiKey` config required when `type == "apiKey"` and `provider` is not set (known providers infer defaults)
- `gcp` config required when `type == "gcp"`
- `pathToken` config required when `type == "pathToken"`
- `oauth2` config required when `type == "oauth2"`
- `secretRef` required for all types except `none`

### ClawDevicePairingRequest CRD

**Spec Fields:**
- `requestID` (string, required, minLength=1): Unique identifier for the pairing request
- `selector` (metav1.LabelSelector, required): Label selector that specifies which Claw pod should process this device pairing request. The controller uses this selector to query for pods in the same namespace. The selector must match exactly one pod for the pairing request to be processed successfully.

**Status Fields:**
- `conditions` ([]metav1.Condition, optional): Standard Kubernetes condition array tracking approval state. Condition types:
  - `Ready`: Indicates whether the device pairing request has been processed
    - `Status=False, Reason=InvalidSelector`: The selector cannot be converted to a valid labels.Selector
    - `Status=False, Reason=NoMatchingPod`: No pods match the selector
    - `Status=False, Reason=MultipleMatchingPods`: More than one pod matches the selector
    - `Status=True, Reason=Ready`: Exactly one pod matches and the request can be processed

**Printcolumns:**
- `RequestID`: Shows request ID via JSONPath `.spec.requestID`
- `Age`: Shows creation timestamp via JSONPath `.metadata.creationTimestamp`

**Migration from NodePairingRequestApproval (Breaking Change):**
The CRD was renamed from `NodePairingRequestApproval` to `ClawDevicePairingRequest` to align with device pairing terminology. This is a breaking change requiring manual migration:
1. Export existing CRs: `kubectl get nodepairingrequestapprovals -o yaml > backup.yaml`
2. Edit the YAML to change `kind: NodePairingRequestApproval` to `kind: ClawDevicePairingRequest`
3. Delete old CRs: `kubectl delete nodepairingrequestapprovals --all`
4. Apply updated CRD: `make install` (or `kubectl apply -f config/crd/bases/`)
5. Reapply edited CRs: `kubectl apply -f backup.yaml`

**Adding selector field to existing ClawDevicePairingRequest resources (Breaking Change):**
The `selector` field was added as a required field in the ClawDevicePairingRequest CRD.

**Migration steps for adding selector field:**
1. Export existing ClawDevicePairingRequest resources as a backup: `kubectl get clawdevicepairingrequests --all-namespaces -o yaml > backup.yaml`
2. Upgrade the CRD: `make install` (or `kubectl apply -f config/crd/bases/`). Existing resources remain in the cluster but will fail validation on update until patched.
3. For each resource, add a `selector` field to `spec` and apply:
   ```yaml
   apiVersion: claw.sandbox.redhat.com/v1alpha1
   kind: ClawDevicePairingRequest
   metadata:
     name: my-pairing-request
     namespace: default
   spec:
     requestID: "existing-request-id"
     selector:
       matchLabels:
         app: claw
         claw.sandbox.redhat.com/instance: instance  # Or your instance name
   ```
   Apply each updated resource: `kubectl apply -f <updated-resource>.yaml`

**Selector usage example:**
To create a device pairing request targeting a specific Claw instance pod:
```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: ClawDevicePairingRequest
metadata:
  name: my-device-pairing
  namespace: claw-operator
spec:
  requestID: "device-abc-123"
  selector:
    matchLabels:
      app.kubernetes.io/name: claw
      claw.sandbox.redhat.com/instance: instance
```

The controller will:
- Convert the selector to a labels.Selector
- Query for pods in the same namespace matching the selector
- Set a condition indicating success (exactly 1 match) or failure (0 or multiple matches)

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
make wait-ready NS=my-claw                         # Wait for ready, print URL + token (CLAW=instance by default)
make approve-pairing NS=my-claw                    # List & approve a device pairing request (CLAW=instance by default)
make dev-cleanup                                  # Tear down
```

## Architecture

### Unified Kustomize-based controller

The operator uses a **single unified controller** that manages all resources using Kustomize and server-side apply:

**ClawResourceReconciler** (`internal/controller/claw_resource_controller.go`):
- Reconciles any `Claw` CR in the watched namespace
- Creates gateway Secret (`<instance>-gateway-token`) with randomly-generated authentication token
- Validates user-provided credentials (array of CredentialSpec in CR's `credentials` field) and referenced Secrets
- Creates all resources: PVC, ConfigMap, Deployment, Services (2), NetworkPolicies (3: ingress for gateway, egress for claw, egress for proxy), proxy Deployment/ConfigMap, and Route (OpenShift only)
- Uses **three-phase reconciliation** to dynamically inject Route host into ConfigMap for CORS configuration
- Uses server-side apply for idempotent, conflict-free resource management
- Automatically labels all resources with `app.kubernetes.io/name: claw`
- Gracefully skips resources whose CRDs aren't registered (e.g., Route on vanilla Kubernetes)
- Updates status conditions (Ready, CredentialsResolved, ProxyConfigured) based on Deployment readiness and credential validation
- Supports proxy image override via `ProxyImage` field (set from `PROXY_IMAGE` env var on the manager)
- Supports kubectl image override via `KubectlImage` field (set from `KUBECTL_IMAGE` env var on the manager, default `quay.io/openshift/origin-cli:4.21`); when kubernetes credentials are present, adds an init container that copies kubectl into a shared emptyDir volume mounted at `/opt/kube-tools` on the gateway container
- Supports image pull policy override via `ImagePullPolicy` field (set from `IMAGE_PULL_POLICY` env var on the manager; valid values: `Always`, `IfNotPresent`, `Never`)
- Configures proxy with multi-credential support for different LLM API domains
- Supports **multi-instance** deployments: resource names are derived from the Claw CR name via `CLAW_INSTANCE_NAME` template replacement, and instance labels (`claw.sandbox.redhat.com/instance`) are injected into all selectors for discrimination

**Key benefits:**
- Simplified codebase: 1 controller managing all resources
- Dynamic CORS configuration: Route host automatically injected into ConfigMap at deployment time
- Field ownership: server-side apply tracks which controller owns which fields
- Consistent labeling: queryable with `kubectl get all -l app.kubernetes.io/name=claw`
- Graceful fallback: localhost CORS origin on vanilla Kubernetes (no Route CRD)
- Future-proof: adding new resources only requires updating kustomization.yaml

### Config deep-merge at init time

The `<instance>-config` ConfigMap uses a **deep-merge** approach to preserve user and application changes across reconciles. No `$include` directives — OpenClaw sees a plain JSON file with no write barriers. All standard config mutations (plugin install, `config.patch`, channel setup) work normally.

**Init container** (`init-config`):
Uses the gateway image (`ghcr.io/openclaw/openclaw`) for Node.js. Runs `node /config/merge.js` on every pod start. Kustomize's `images` transformer pins both `init-config` and the main gateway container to the same image tag. The `CLAW_CONFIG_MODE` env var (from `spec.configMode`) controls merge behavior.

**Config merge modes** (controlled by `spec.configMode`):
- **`merge`** (default): Deep-merges `operator.json` into the existing PVC `openclaw.json`. Objects merge recursively (operator keys win), arrays and primitives from operator replace user values. User-owned keys (plugins, channels, `agents.list`, cron) survive restarts. Operator-managed keys (`agents.defaults.models`, `agents.defaults.model.primary`) are overwritten, except primary is preserved by `merge.js` after the first run so the user's model choice persists across restarts.
- **`overwrite`**: Deep-merges `operator.json` into the ConfigMap `openclaw.json` seed (ignoring PVC). User edits are wiped on every restart. Primary model is NOT preserved in overwrite mode.

**ConfigMap keys** (static keys in `configmap.yaml` template + dynamically injected):
- `operator.json` — Operator-managed: gateway settings, CORS origins, providers, model catalog (`agents.defaults.models`), primary model (`agents.defaults.model.primary`). Rewritten every reconcile. Read from ConfigMap mount at `/config/`, not written to PVC.
- `openclaw.json` — User-owned seed: `agents.list`, workspace path. No `$include`, no `gateway` section, no hardcoded models (models are dynamically generated in `operator.json`). Seeded to PVC on first run only (merge mode) or used as base every restart (overwrite mode).
- `merge.js` — The init container merge script. Stored in ConfigMap, executed via `node /config/merge.js`. Updated automatically with operator upgrades.
- `AGENTS.md` — System prompt seed. Copied to PVC if missing.
- `PLATFORM.md` — Platform overview and integration skill. Always copied to `skills/platform/SKILL.md`.
- `KUBERNETES.md` — *(dynamically injected by controller when k8s credentials present, not in static template)*. Kubernetes skill with OpenClaw skill frontmatter. Always copied to `skills/kubernetes/SKILL.md`.

**Who owns what on the PVC:**
| Config section | Owner | Behavior on pod restart (merge mode) |
|---|---|---|
| `gateway.*` | Operator | Overwritten — CORS origins, auth mode, bind, port |
| `models.providers` | Operator | Overwritten — tracks credentials in Claw CR |
| `agents.defaults.models` | Operator | Overwritten — dynamic model catalog from configured providers |
| `agents.defaults.model.primary` | Operator (first run) / User (after) | Set by operator on first run; preserved by `merge.js` on restarts |
| `agents.list` | User | Preserved — agent configurations |
| `plugins.*` | User | Preserved — plugin installs, allow/deny lists |
| `channels.*` | User | Preserved — WhatsApp, Telegram, etc. |
| `tools.*` | User | Preserved |
| `cron.*` | User | Preserved — scheduled tasks |

### Reconciliation flow

The controller uses a **three-phase reconciliation** approach to enable dynamic Route host injection into ConfigMap:

```
Reconcile(ctx, req) called
  ↓
1. Fetch Claw CR
  ↓
PHASE 1: Gateway Secret + Credentials + Proxy Config
2. applyGatewaySecret(ctx, instance)
   ├─ Check if <instance>-gateway-token Secret already exists
   ├─ If exists and has token, preserve existing token
   ├─ If not exists or missing token, generate new 64-char hex token using crypto/rand
   ├─ Create/update Secret with token data entry
   ├─ Set owner reference (for garbage collection)
   └─ Server-side apply Secret (Patch with Apply)
  ↓
3. resolveAndApplyCredentials(ctx, instance)
   ├─ resolveProviderDefaults for each credential
   │  ├─ Known apiKey providers (google, anthropic): fill domain and apiKey.header if not set
   │  ├─ GCP credentials: fill domain ".googleapis.com" if not set
   │  ├─ Kubernetes credentials: skip domain requirement (domains from kubeconfig)
   │  ├─ Explicit values are preserved (escape hatch)
   │  └─ Error if domain or apiKey still missing after defaults
   ├─ resolveCredentials: validate Secrets, parse kubeconfig for kubernetes type
   │  └─ Returns []resolvedCredential with parsed kubeconfigData
   ├─ applySanitizedKubeconfig (if any kubernetes credentials):
   │  ├─ Replace all tokens with "proxy-managed-token" placeholder
   │  ├─ Create/update ConfigMap "<instance>-kube-config" with sanitized kubeconfig
   │  └─ Set owner reference for garbage collection
   └─ applyProxyCA: generate self-signed ECDSA CA for MITM proxy (stored as Secret)
  ↓
4. applyProxyResources(ctx, instance, resolvedCreds)
   ├─ generateProxyConfig: build proxy config JSON (routes, injectors, gateway paths)
   │  ├─ usesVertexSDK credentials skip gateway route (no PathPrefix/Upstream)
   │  └─ Kubernetes routes include `caCert` (base64-encoded CA PEM) from kubeconfig's `certificate-authority-data`
   ├─ applyProxyConfigMap: server-side apply proxy config ConfigMap (controller-managed, not in Kustomize)
   ├─ applyVertexADCConfigMap (if any Vertex SDK credentials exist):
   │  ├─ Create ConfigMap "<instance>-vertex-adc" with stub authorized_user ADC JSON
   │  ├─ Stub allows google-auth-library to attempt token refresh via oauth2.googleapis.com
   │  ├─ Proxy intercepts and returns dummy tokens (token vending)
   │  └─ Set owner reference for garbage collection
   └─ Return proxy config JSON for hash stamping
  ↓
5. buildKustomizedObjects(instance)
   ├─ Write embedded manifests to in-memory filesystem with CLAW_INSTANCE_NAME replaced
   ├─ Build both claw/ and claw-proxy/ Kustomize components
   ├─ Parse YAML into unstructured objects
   └─ injectInstanceLabels: add claw.sandbox.redhat.com/instance label to all resources and selectors
  ↓
6. configureDeployments(instance, objects, resolvedCreds)
   ├─ configureProxyImage: override proxy container image if ProxyImage is set
   ├─ configureImagePullPolicy: override imagePullPolicy on all containers if set
   ├─ configureProxyForCredentials: add credential env vars and volume mounts to proxy deployment
   ├─ configureClawDeploymentForVertex: add Vertex AI env vars and ADC volume mount when needed
   ├─ configureClawDeploymentForKubernetes: add kubeconfig mount, PATH, and init-kubectl container
   └─ configureClawDeploymentConfigMode: set CLAW_CONFIG_MODE env var on init-config container
  ↓
7. stampProxyConfigHash: hash proxy config JSON and stamp on proxy pod template annotation
  ↓
8. stampSecretVersionAnnotation: fetch credential Secrets' ResourceVersions, stamp on proxy pod template
  ↓
PHASE 2: Route Application and Host Resolution
9. applyRouteOnly(ctx, objects, instance)
   ├─ Filter for Route resources from already-built objects
   ├─ Set namespace and owner reference
   └─ Server-side apply Route (skips with NoMatchError on vanilla Kubernetes)
  ↓
10. getRouteURL(ctx, instance)
   ├─ Fetch Route resource
   ├─ Extract .status.ingress[0].host (authoritative source populated by OpenShift router)
   ├─ If status not yet populated: requeue with 5s backoff
   ├─ If Route CRD not registered: continue with empty routeHost (localhost fallback)
   └─ Return https://<route-host> or empty string
  ↓
PHASE 3: ConfigMap Injection and Remaining Resources
11. injectRouteHostIntoConfigMap(objects, routeHost, instanceName)
   ├─ Find <instance>-config ConfigMap in objects
   ├─ Extract data["operator.json"] string
   ├─ Replace "OPENCLAW_ROUTE_HOST" placeholder with routeHost (https://...)
   ├─ If routeHost is empty (vanilla Kubernetes): use "http://localhost:18789" fallback
   └─ Set modified JSON back into ConfigMap
  ↓
12. injectProvidersIntoConfigMap(objects, instance)
   ├─ Filter credentials with Provider set (skip pathToken type)
   ├─ For gateway-routed providers: resolveProviderInfo(cred) → upstream + basePath as baseUrl
   ├─ For Vertex SDK providers (GCP + non-Google, e.g., anthropic): map to "{provider}-vertex" key with real Vertex AI URL, api mapping, and "gcp-vertex-credentials" apiKey
   ├─ Write providers into data["operator.json"]
   └─ Credentials without Provider are MITM-only (no provider entry)
  ↓
13. injectModelCatalogIntoConfigMap(objects, instance)
   ├─ Iterate credentials with Provider set (same filters as injectProvidersIntoConfigMap)
   ├─ Derive provider key: usesVertexSDK → "{provider}-vertex", else "{provider}"
   ├─ Strip "-vertex" suffix for logical provider name, look up modelCatalog map
   ├─ Emit model entries as "{providerKey}/{modelName}" with aliases into agents.defaults.models
   ├─ Set agents.defaults.model.primary from first provider's first model (first provider with catalog entry)
   ├─ Write into data["operator.json"] (agents.defaults section)
   └─ No-op when no credentials have a catalog entry (e.g., openrouter-only)
  ↓
14. injectKubernetesSkill(objects, resolvedCreds, instanceName)
   ├─ Collect contexts from all kubernetes credentials
   ├─ Write data["KUBERNETES.md"] in ConfigMap with OpenClaw skill frontmatter
   ├─ Init container copies to skills/kubernetes/SKILL.md for auto-discovery
   └─ No-op when no kubernetes credentials present
  ↓
15. injectKubePortsIntoNetworkPolicy(objects, resolvedCreds, instanceName)
   ├─ Collect unique non-443 ports from kubernetes credential clusters
   ├─ Append port entries to <instance>-proxy-egress NetworkPolicy egress[0].ports[]
   └─ No-op when all ports are 443 or no kubernetes credentials present
  ↓
16. stampGatewayConfigHash(objects, instanceName)
   ├─ Hash entire ConfigMap data (operator.json, openclaw.json, etc.)
   ├─ Stamp SHA-256 hash as annotation on gateway pod template
   └─ Triggers gateway pod rollout when ConfigMap content changes (e.g., after operator upgrade)
  ↓
17. Filter out Route (applied in phase above) and proxy ConfigMap (controller-managed)
  ↓
18. Set namespace and owner references on remaining objects
  ↓
19. applyResources(ctx, remainingObjects)
   └─ Server-side apply each resource (ConfigMap, PVC, Deployments, Services, NetworkPolicies)
  ↓
20. updateStatus(ctx, instance)
   ├─ checkDeploymentsReady: check both claw and claw-proxy Deployments
   ├─ Set Ready condition based on deployment readiness
   ├─ Set GatewayTokenSecretRef to gateway Secret name
   ├─ If ready: build URL with Route host + gateway token as fragment (token=...)
   ├─ If not ready: clear URL
   └─ Update status via status subresource (client.Status().Update)
  ↓
21. Return success
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

**Key methods** (organized by file):

`claw_resource_controller.go`:
- `Reconcile()` — main entry point, orchestrates reconciliation (secret → credentials → proxy config → build objects → configure deployments → stamp hashes → Route → inject ConfigMap → apply remaining → status)
- `resolveAndApplyCredentials()` — orchestrates credential resolution: resolves provider defaults, validates credentials (returning `[]resolvedCredential`), applies sanitized kubeconfig ConfigMap for kubernetes credentials, and applies proxy CA
- `applyProxyResources()` — generates proxy config, applies proxy ConfigMap and Vertex ADC ConfigMap. Returns proxy config JSON for hash stamping
- `buildKustomizedObjects(instance)` — writes embedded manifests to in-memory filesystem (replacing `CLAW_INSTANCE_NAME` with CR name), builds both claw/ and claw-proxy/ Kustomize components, injects instance labels
- `configureDeployments()` — orchestrates all deployment overrides (proxy image, pull policy, credentials, vertex, kubernetes, config mode)
- `applyRouteOnly()` — filters Route from pre-built objects, sets namespace/owner ref, applies via SSA
- `injectRouteHostIntoConfigMap()` — replaces `OPENCLAW_ROUTE_HOST` placeholder in `operator.json` with Route host (or localhost fallback)
- `injectProvidersIntoConfigMap()` — dynamically builds `models.providers` in `operator.json`. Skips pathToken type. Provider baseUrl points to real upstream URLs; the MITM proxy injects credentials transparently. Vertex SDK providers get the real Vertex AI URL
- `injectModelCatalogIntoConfigMap()` — dynamically builds `agents.defaults.models` and `agents.defaults.model.primary` in `operator.json`. Uses `modelCatalog` map. Providers not in the catalog (e.g., openrouter) are silently skipped
- `injectKubernetesSkill()` — writes `KUBERNETES.md` key into ConfigMap with OpenClaw skill frontmatter. Init container copies to `skills/kubernetes/SKILL.md`
- `injectKubePortsIntoNetworkPolicy()` — adds non-443 ports from kubeconfig to proxy egress NetworkPolicy
- `injectInstanceLabels()` — adds `claw.sandbox.redhat.com/instance` label to all resources and injects into Deployment/Service/NetworkPolicy selectors
- `applyResources()` — applies list of unstructured objects using server-side apply. Returns count of applied resources
- `findClawsReferencingSecret()` — maps Secret events to Claw reconcile requests (for Secret watching)
- `parseYAMLToObjects()` — converts multi-doc YAML to unstructured objects
- `readEmbeddedFile()` — reads files from embedded filesystem
- `sanitizeLabelValue()` — truncates label values to 63 chars with hash suffix for uniqueness

`claw_credentials.go`:
- `generateGatewayToken()` — generates cryptographically secure 64-char hex token using crypto/rand
- `applyGatewaySecret()` — creates/updates gateway Secret with token (preserves existing token)
- `resolveCredentials()` — validates all credentials and referenced Secrets. For `kubernetes` type, parses kubeconfig with `client-go/tools/clientcmd`, validates token-only auth, extracts cluster/context metadata
- `parseAndValidateKubeconfig()` — parses kubeconfig bytes, validates all users use token-based auth (rejects client certs, exec, auth provider, token-file, basic auth), validates unique token per server `hostname:port`
- `sanitizeKubeconfig()` — replaces all user tokens with `"proxy-managed-token"` placeholder
- `applySanitizedKubeconfig()` — creates/updates the kubeconfig ConfigMap. Cleans up orphaned ConfigMap when no kubernetes credentials present
- `hasKubernetesCredentials()` — returns true when any resolved credential has type `kubernetes`

`claw_providers.go`:
- `resolveProviderDefaults()` — fills missing `domain` and `apiKey` fields for known providers (google, anthropic) before validation. Skips domain requirement for `kubernetes` type
- `resolveProviderInfo()` — returns upstream host and base path for a credential's provider. GCP credentials route through Vertex AI. Google + apiKey uses Gemini REST API. All others fall back to domain
- `usesVertexSDK()` — returns true when type == gcp && provider != "" && provider != "google"

`claw_proxy.go`:
- `generateProxyConfig()` — builds proxy config JSON from resolved credentials. Includes builtin passthrough domains. Exact-match before suffix-match ordering
- `applyProxyConfigMap()` — server-side apply proxy config ConfigMap (controller-managed, not in Kustomize)
- `configureProxyImage()` — overrides proxy Deployment container image if `ProxyImage` is set; no-op when empty
- `configureProxyForCredentials()` — adds credential env vars and volume mounts to proxy deployment in-place
- `stampProxyConfigHash()` — hashes proxy config JSON and stamps on proxy pod template annotation
- `stampSecretVersionAnnotation()` — stamps Secret ResourceVersion annotations on proxy pod template
- `applyProxyCA()` — generates and applies self-signed ECDSA CA Secret for MITM proxy

`claw_deployment.go`:
- `configureImagePullPolicy()` — overrides imagePullPolicy on all containers in all Deployments
- `configureClawDeploymentForVertex()` — adds GOOGLE_APPLICATION_CREDENTIALS, ANTHROPIC_VERTEX_PROJECT_ID env vars and stub ADC volume mount
- `configureClawDeploymentForKubernetes()` — mounts kubeconfig, sets env vars, adds init-kubectl init container
- `configureClawDeploymentConfigMode()` — sets `CLAW_CONFIG_MODE` env var on init-config init container
- `stampGatewayConfigHash()` — hashes gateway ConfigMap data and stamps on gateway pod template annotation
- `applyVertexADCConfigMap()` — creates/updates stub ADC ConfigMap. Cleans up orphaned ConfigMap when no Vertex credentials

`claw_status.go`:
- `getDeploymentAvailableStatus()` — fetches Deployment and checks its Available condition
- `checkDeploymentsReady()` — checks if both claw and claw-proxy Deployments are ready
- `setCondition()` — generic helper to set a condition on the Claw instance
- `setReadyCondition()` — sets Ready condition on Claw based on deployment states
- `getRouteURL()` — fetches Route and extracts `.status.ingress[0].host`
- `buildClawURL()` — constructs status URL with Route host and gateway token as URL fragment
- `updateStatus()` — updates Claw status conditions, GatewayTokenSecretRef, and URL field via status subresource

**Shared constants** (`internal/controller/claw_resource_controller.go`):
- `GatewayTokenKeyName = "token"` — data key for gateway token in gateway Secret
- `ClawProxyContainerName = "proxy"` — container name in the proxy deployment
- `ClawGatewayContainerName = "gateway"` — container name in the claw deployment
- `ClawInitConfigContainerName = "init-config"` — init container that deep-merges operator config into `openclaw.json`
- `ClawConfigModeEnvVar = "CLAW_CONFIG_MODE"` — env var controlling merge vs overwrite behavior
- `DefaultKubectlImage = "quay.io/openshift/origin-cli:4.21"` — default image for the init-kubectl init container; copies `oc` and `kubectl` to shared volume (overridable via `KUBECTL_IMAGE` env var)
- Kind constants: `RouteKind`, `DeploymentKind`, `ConfigMapKind`, `NetworkPolicyKind`, `ServiceKind`, `PersistentVolumeClaimKind`

**Condition and annotation constants** (`api/v1alpha1/claw_types.go`):
- Condition types: `ConditionTypeReady`, `ConditionTypeCredentialsResolved`, `ConditionTypeProxyConfigured`
- Condition reasons: `ConditionReasonReady`, `ConditionReasonProvisioning`, `ConditionReasonResolved`, `ConditionReasonValidationFailed`, `ConditionReasonConfigured`, `ConditionReasonConfigFailed`
- Annotation keys: `AnnotationKeyProxyConfigHash`, `AnnotationKeyGatewayConfigHash`, `AnnotationPrefixSecretVersion`, `AnnotationSuffixSecretVersion`

**Resource naming helpers** (`internal/controller/claw_resource_controller.go`):
Resource names are derived from the Claw CR name for multi-instance support:
- `getClawDeploymentName(name)` → `name` (gateway deployment)
- `getProxyDeploymentName(name)` → `name + "-proxy"`
- `getGatewaySecretName(name)` → `name + "-gateway-token"`
- `getConfigMapName(name)` → `name + "-config"`
- `getPVCName(name)` → `name + "-home-pvc"`
- `getServiceName(name)` → `name` (gateway service)
- `getProxyServiceName(name)` → `name + "-proxy"`
- `getRouteName(name)` → `name`
- `getProxyCAConfigMapName(name)` → `name + "-proxy-ca"`
- `getVertexADCConfigMapName(name)` → `name + "-vertex-adc"`
- `getKubeConfigMapName(name)` → `name + "-kube-config"`
- `getIngressNetworkPolicyName(name)` → `name + "-ingress"`
- `getEgressNetworkPolicyName(name)` → `name + "-egress"`
- `getProxyEgressNetworkPolicyName(name)` → `name + "-proxy-egress"`
- `getProxyConfigMapName(name)` → `name + "-proxy-config"`

**Model catalog** (`internal/controller/claw_models.go`):
- `modelEntry` struct: `Name` (API model name), `Alias` (human-readable display name)
- `modelCatalog` variable: `map[string][]modelEntry` keyed by logical provider name (google, anthropic, openai, xai). Order within each slice matters — first entry becomes the default primary model. Providers not in the catalog (e.g., openrouter) are silently skipped by `injectModelCatalogIntoConfigMap`

**Known providers** (`internal/controller/claw_credentials.go`):
- `knownProviders` map: google, anthropic, openai, openrouter, xai — validated during `resolveCredentials`

**ClawDevicePairingRequestReconciler** (`internal/controller/claw_device_pairing_request_controller.go`):
- Reconciles `ClawDevicePairingRequest` CRs
- Converts the `spec.selector` to a `labels.Selector` and queries for pods in the same namespace
- Handles three scenarios: no matching pods (`NoMatchingPod`), multiple matches (`MultipleMatchingPods`), or exactly one match (`Ready`)
- Sets status conditions accordingly; TODO remains for the actual device pairing approval logic after pod match
- RBAC includes permissions for CR, status management, and pod listing

### Kustomize-based manifest generation

Kubernetes manifests are embedded via `//go:embed` as a complete directory in `internal/assets/manifests.go`:

```go
//go:embed manifests
var ManifestsFS embed.FS
```

The `internal/assets/manifests/` directory is organized into two Kustomize components:

**`manifests/claw/`** — Gateway component (7 files):
- **kustomization.yaml** — defines labels (`app.kubernetes.io/name: claw`), resource list, and images transformer (pins `ghcr.io/openclaw/openclaw` tag for both init-config and gateway containers)
- **configmap.yaml** — OpenClaw configuration (operator.json for operator-managed settings, openclaw.json as user-owned seed, merge.js for init-time deep-merge, AGENTS.md seed, PLATFORM.md for platform overview skill). KUBERNETES.md is dynamically injected by the controller when k8s credentials are present
- **pvc.yaml** — persistent storage (10Gi ReadWriteOnce)
- **deployment.yaml** — OpenClaw application pods (`init-config` uses the gateway image with Node.js to deep-merge operator config into `openclaw.json`; `wait-for-proxy` init container ensures proxy is ready before gateway starts; PVC subpath mounts at `~/.local`, `~/.cache`, `~/.config` for persistent tool state; `OPENCLAW_PROXY_ACTIVE=1` env var enables native managed proxy support)
- **service.yaml** — ClusterIP service exposing OpenClaw gateway (port 18789)
- **route.yaml** — OpenShift Route for external HTTPS access (skipped on non-OpenShift)
- **network-policy.yaml** — Ingress NetworkPolicy restricting gateway access to OpenShift router namespace

**`manifests/claw-proxy/`** — Proxy component (4 files + 1 unused):
- **kustomization.yaml** — defines labels (`app.kubernetes.io/name: claw`), resource list
- **proxy-deployment.yaml** — Go MITM proxy (env vars reference user-managed Secrets; controller patches credential env vars in-memory before applying)
- **proxy-service.yaml** — ClusterIP service for proxy (port 8080)
- **network-policies.yaml** — Two NetworkPolicies: OpenClaw egress (claw → proxy + DNS only) and proxy egress (proxy → HTTPS + DNS)
- **_proxy-configmap.yaml** — Template for proxy ConfigMap (prefixed with `_` so excluded from kustomization.yaml; proxy ConfigMap is applied directly by the controller via `applyProxyConfigMap`)

Resource names in manifests use `CLAW_INSTANCE_NAME` placeholder, replaced at build time with the Claw CR name for multi-instance support.

**Runtime process:**
1. Controller reads embedded manifest files and writes them to in-memory filesystem with `CLAW_INSTANCE_NAME` replaced by the Claw CR name
2. Kustomize API (`krusty.MakeKustomizer`) builds each component (`claw/`, `claw-proxy/`) separately
3. Labels from kustomization.yaml are applied automatically to all resources
4. Instance labels (`claw.sandbox.redhat.com/instance`) are injected into all selectors for multi-instance discrimination
5. Controller sets namespace and owner references dynamically
6. Server-side apply sends all resources to API server with field manager "claw-operator"
7. Kubernetes handles resource creation/updates idempotently

### Key directories

- `api/v1alpha1/` — CRD type definitions
  - `claw_types.go` — Claw CRD (ClawSpec, ClawStatus, CredentialSpec, credential types and configs)
  - `clawdevicepairingrequest_types.go` — ClawDevicePairingRequest CRD
  - `groupversion_info.go` — API group registration (`claw.sandbox.redhat.com/v1alpha1`)
- `internal/controller/` — ClawResourceReconciler and ClawDevicePairingRequestReconciler implementations and tests (separate test files per resource type for readability). Key files: `claw_models.go` (model catalog map per provider), `claw_providers.go` (provider defaults and routing), `claw_credentials.go` (credential validation and gateway secret), `claw_proxy.go` (proxy config generation and deployment configuration), `claw_deployment.go` (gateway deployment configuration — vertex, kubernetes, config mode, image pull policy, config hash stamping), `claw_status.go` (status updates, route URL, conditions)
- `internal/proxy/` — MITM proxy library. Credential-injecting forward proxy with two CONNECT modes:
  - **MITM** (`ConnectMitm`): TLS interception for credential injection, path filtering, and header injection. Used for all injector types except pure `none` passthrough
  - **Direct tunnel** (`ConnectAccept`): Plain CONNECT passthrough without TLS interception. Used for `none` routes with no `allowedPaths` or `defaultHeaders`. Required for protocols that fail under MITM (e.g., WhatsApp Noise handshake, WebSocket tunnels)
  - `Route.NeedsMITM()` determines the mode: returns true unless injector is `"none"` with no path or header restrictions
- `internal/assets/manifests/` — Embedded Kustomize directory with two components: `claw/` (7 files: gateway resources) and `claw-proxy/` (4 files: proxy resources). 12 files total
- `cmd/main.go` — Manager entrypoint, wires up controllers. Contains package-level `version` and `buildTime` variables set via LDFLAGS during build, logged at startup. Reads `PROXY_IMAGE`, `KUBECTL_IMAGE`, and `IMAGE_PULL_POLICY` env vars
- `cmd/proxy/main.go` — Proxy binary entrypoint
- `config/` — Kustomize overlays for CRDs, RBAC, manager deployment

### Code generation

After modifying API types in `api/v1alpha1/` (`claw_types.go`, `clawdevicepairingrequest_types.go`), run both:
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
- **Test CRs:** Test Claw instances can include the optional `credentials` field for credential testing (e.g., `instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{{Name: "gemini", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", SecretRef: &clawv1alpha1.SecretRef{Name: "test-secret", Key: "api-key"}}}` — domain and apiKey are inferred for known providers)
- **Test files:** Separate test files per resource type (`claw_configmap_test.go`, `claw_credentials_test.go`, `claw_status_test.go`, `claw_deployment_test.go`, `claw_proxy_test.go`, `claw_providers_test.go`, `claw_instance_label_test.go`, `claw_kubernetes_test.go`, `claw_merge_test.go`, `claw_resource_controller_test.go`, `claw_device_pairing_request_controller_test.go`)
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
- Pod security: non-root (uid 65532), restricted seccomp, all capabilities dropped. The `wait-for-proxy` init container and proxy use `readOnlyRootFilesystem: true`. The `init-config` init container and gateway container do not — `init-config` runs Node.js which needs writable `/tmp`, and the gateway runs dynamic AI agent tools that write to unpredictable `$HOME` paths. PVC subpath mounts at `~/.local`, `~/.cache`, `~/.config` provide persistent writable storage for tool state
- Linting config in `.golangci.yml` — notable: `lll`, `dupl` enabled
- License header required (template in `hack/boilerplate.go.txt`)
