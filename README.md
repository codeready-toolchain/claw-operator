# Claw Operator

An operator for managing OpenClaw instances in Red Hat's OpenShift.

## Description

The Claw Operator manages the lifecycle of OpenClaw instances through Kubernetes custom resources. It provides a declarative API for creating, configuring, and managing OpenClaw deployments within Kubernetes clusters.

## Custom Resource Definition

### Claw

The `Claw` custom resource represents a deployment of OpenClaw in your cluster.

**API Group:** `claw.sandbox.redhat.com/v1alpha1`

**Example:**
```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        name: gemini-api-key
        key: api-key
      domain: ".googleapis.com"
      apiKey:
        header: x-goog-api-key
```

#### Spec Fields

- `credentials` ([]CredentialSpec, optional): Configures proxy credential injection per domain. Each entry defines how the proxy authenticates outbound requests to a specific API provider.
  - `name` (string, required): Unique identifier for this credential entry
  - `type` (enum, required): Credential injection mechanism — one of `apiKey`, `bearer`, `gcp`, `pathToken`, `oauth2`, `none`
  - `secretRef` (SecretRef, required for all types except `none`): Reference to a Kubernetes Secret holding the credential value
    - `name` (string, required): Name of the Secret
    - `key` (string, required): Key within the Secret's data map
  - `domain` (string, required): Domain the proxy matches against the request Host header. Exact match (e.g., `api.github.com`) or suffix match with leading dot (e.g., `.googleapis.com`)
  - `defaultHeaders` (map[string]string, optional): Headers injected on every proxied request for this credential
  - `apiKey` (APIKeyConfig, required when type is `apiKey`): Custom header injection config
    - `header` (string): Header name (e.g., `x-goog-api-key`, `x-api-key`)
    - `valuePrefix` (string, optional): Prepended to the secret value before injection
  - `gcp` (GCPConfig, required when type is `gcp`): GCP service account credential injection
    - `project` (string): GCP project ID
    - `location` (string): GCP region (e.g., `us-central1`)
  - `pathToken` (PathTokenConfig, required when type is `pathToken`): URL path token injection
    - `prefix` (string): Prepended before the token in the URL path (e.g., `/bot` for Telegram)
  - `oauth2` (OAuth2Config, required when type is `oauth2`): Client credentials token exchange
    - `clientID` (string): OAuth2 client ID
    - `tokenURL` (string): OAuth2 token endpoint
    - `scopes` ([]string, optional): Scopes requested during token exchange

#### Status Fields

The controller automatically populates status conditions to track the deployment readiness of your Claw instance:

- `conditions` (array): Standard Kubernetes conditions following the [metav1.Condition](https://kubernetes.io/docs/reference/kubernetes-api/common-definitions/condition/) format. Currently includes:
  - **Ready**: Indicates whether the Claw instance is ready for use
    - `status: "False"`, `reason: Provisioning` — Deployments are being created or are not yet ready
    - `status: "True"`, `reason: Ready` — Both `openclaw` and `openclaw-proxy` Deployments are available
  - **CredentialsResolved**: Indicates whether all credential Secrets have been validated
    - `status: "False"`, `reason: ValidationFailed` — One or more credential Secrets are missing or invalid
    - `status: "True"`, `reason: Resolved` — All credential Secrets are valid
  - **ProxyConfigured**: Indicates whether the proxy configuration was generated successfully
    - `status: "False"`, `reason: ConfigFailed` — Proxy config generation failed
    - `status: "True"`, `reason: Configured` — Proxy config generated successfully
- `gatewayTokenSecretRef` (string): Name of the Secret containing the gateway authentication token
- `url` (string): HTTPS URL for accessing the Claw instance (populated when deployments are ready and Route is available)

**Checking instance status:**

```sh
# View instance with status columns
oc get claw
# NAME       READY   REASON
# instance   True    Ready

# View full status
oc get claw instance -o yaml

# Check if instance is ready
oc get claw instance -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
```

The `oc get claw` output includes:
- **Ready** column: Shows the Ready condition status (True/False/Unknown)
- **Reason** column: Shows why the instance is in its current state (Provisioning/Ready)

**Important:** Only Claw instances named `instance` will be reconciled by the controller.

## Configuration

### Secrets Management

The operator manages two categories of Secrets:

#### 1. Gateway Authentication Token (`openclaw-gateway-token`)

The controller automatically generates and manages a secure authentication token for the OpenClaw gateway:
- **Secret name:** `openclaw-gateway-token`
- **Data entry:** `token` — A cryptographically secure 64-character hex string (256-bit entropy)
- **Generation:** Automatically created on first reconciliation using Go's `crypto/rand` package
- **Persistence:** Token is preserved across reconciliations (never regenerated unless the Secret is deleted)
- **Lifecycle:** Automatically deleted when the Claw instance is removed (via owner references)

**Example retrieval:**
```sh
oc get secret openclaw-gateway-token -o jsonpath='{.data.token}' | base64 -d
```

#### 2. Credential Secrets (User-Managed)

Each entry in `spec.credentials` references a user-managed Secret. The controller:
1. Validates that each referenced Secret exists in the same namespace
2. Verifies the Secret contains the specified key
3. Configures the `openclaw-proxy` deployment to reference Secrets directly (via env vars or volume mounts depending on credential type)
4. Generates a proxy config JSON with credential routing rules and applies it as a ConfigMap

**How credentials are injected into the proxy:**
- **apiKey, bearer, pathToken, oauth2** types: Secret value is injected as an environment variable via `valueFrom.secretKeyRef`
- **gcp** type: Secret is mounted as a volume at `/etc/proxy/credentials/<name>/sa-key.json`
- **none** type: No Secret required — used for proxy allowlist entries (no authentication)

**Security Considerations:**
- Credential values are stored only in your user-managed Secrets (never copied by the controller)
- Fully compatible with external secret management tools (Sealed Secrets, External Secrets Operator, Vault, etc.)
- The controller validates and references your Secrets but never reads or stores credential values
- The gateway token is never exposed in the CR, only in the operator-managed Secret
- Consider enabling encryption at rest for etcd in your cluster

## Version Information

The operator logs its version and build time during startup for troubleshooting and deployment tracking:

```
INFO	setup	Starting Claw Operator	{"version": "fc7c72b0", "buildTime": "2026-04-07T13:17:26Z"}
```

**Version fields:**
- `version`: Short commit SHA (7 characters) of the source code used to build the binary
- `buildTime`: RFC3339 timestamp indicating when the binary was built

**How it works:**
- Version information is injected at build time via LDFLAGS in the `docker-build` Makefile target
- Local development builds (e.g., `make run`) show default values: `version="dev"` and `buildTime="unknown"`
- Production container builds automatically capture the git commit SHA and build timestamp

This allows operators to quickly identify which version is running in any environment by checking the startup logs.

## Getting Started

### Prerequisites
- go version v1.25.0+
- docker version 17.03+.
- oc version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Dev Deployment (Quick Start)

Dev targets build both the operator and proxy images, push them, install CRDs, and deploy the controller in one command. You need a container registry accessible from your cluster (e.g., `quay.io`, `ghcr.io`, or an OpenShift internal registry).

**Full setup (build + push + deploy):**
```sh
make dev-setup REGISTRY=quay.io/myuser
```

**Or step by step:**
```sh
make dev-build  REGISTRY=quay.io/myuser   # Build operator + proxy images
make dev-push   REGISTRY=quay.io/myuser   # Push both images
make dev-deploy REGISTRY=quay.io/myuser   # Install CRDs + deploy controller
```

**Iterate after code changes:**
```sh
make dev-build dev-push dev-deploy REGISTRY=quay.io/myuser
```

**Tear down:**
```sh
make dev-cleanup
```

Images are tagged with `dev` by default. Override with `TAG`:
```sh
make dev-setup REGISTRY=quay.io/myuser TAG=my-branch
```

### To Deploy on the cluster (Manual)
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/claw-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/claw-operator:tag PROXY_IMG=<some-registry>/openclaw-proxy:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
oc apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### Creating an OpenClaw Instance

After deploying the operator, create an OpenClaw instance:

**1. Create a Secret with your Gemini API key:**

```sh
oc create secret generic gemini-api-key \
  --from-literal=api-key=YOUR_GEMINI_API_KEY \
  -n claw-operator
```

Get your API key from [Google AI Studio](https://aistudio.google.com/apikey).

**2. Create the Claw CR** (must be named `instance`):

```sh
oc apply -f config/samples/openclaw_v1alpha1_claw.yaml -n claw-operator
```

Or create it directly:

```yaml
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: claw-operator
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        name: gemini-api-key
        key: api-key
      domain: ".googleapis.com"
      apiKey:
        header: x-goog-api-key
```

**3. Watch the instance become ready:**

```sh
oc get claw instance -n claw-operator -w
```

The `Ready` column will transition from `Provisioning` to `Ready` once both the OpenClaw and proxy deployments are available.

**4. Access the instance:**

On **OpenShift**, a Route is created automatically. Get the URL from the Claw status:

```sh
oc get claw instance -n claw-operator -o jsonpath='{.status.url}'
```

On **vanilla Kubernetes** (no Route CRD), use port-forwarding:

```sh
oc port-forward svc/openclaw 18789:18789 -n claw-operator
```

Then open http://localhost:18789 in your browser.

**5. Authenticate with the gateway token:**

The operator generates a gateway authentication token stored in a Secret. Retrieve it with:

```sh
oc get secret openclaw-gateway-token -n claw-operator -o jsonpath='{.data.token}' | base64 -d
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
oc delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/claw-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'oc apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
oc apply -f https://raw.githubusercontent.com/<org>/claw-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
operator-sdk edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing

Contributions are welcome! Please ensure that:
- All tests pass with `make test`
- Code passes linting with `make lint`
- Changes are covered by appropriate unit or e2e tests
- Commits follow the repository's commit message conventions

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

