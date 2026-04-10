# openclaw-operator

An operator for managing OpenClaw instances in Red Hat's OpenShift.

## Description

The OpenClaw Operator manages the lifecycle of OpenClaw instances through Kubernetes custom resources. It provides a declarative API for creating, configuring, and managing OpenClaw deployments within Kubernetes clusters.

## Custom Resource Definition

### OpenClaw

The `OpenClaw` custom resource represents a deployment of OpenClaw in your cluster.

**API Group:** `openclaw.sandbox.redhat.com/v1alpha1`

**Example:**
```yaml
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: OpenClaw
metadata:
  name: instance
  namespace: default
spec:
  # Required: Reference to a Secret containing the Gemini API key
  geminiAPIKey:
    name: gemini-api-key
    key: api-key
status:
  # Status fields are populated by the controller
  conditions:
    - type: Available
      status: "True"
      reason: Ready
      message: OpenClaw instance is ready
      lastTransitionTime: "2026-04-08T12:00:00Z"
      observedGeneration: 1
```

#### Spec Fields

- `geminiAPIKey` (SecretKeySelector, required): Reference to a Kubernetes Secret containing the Gemini API key. The controller reads this Secret and propagates the key to the proxy. The Secret must exist in the same namespace as the OpenClaw instance.
  - `name` (string, required): Name of the Secret containing the API key
  - `key` (string, required): Key within the Secret data that contains the API key value

#### Status Fields

The controller automatically populates status conditions to track the deployment readiness of your OpenClaw instance:

- `conditions` (array): Standard Kubernetes conditions following the [metav1.Condition](https://kubernetes.io/docs/reference/kubernetes-api/common-definitions/condition/) format. Currently includes:
  - **Available**: Indicates whether the OpenClaw instance is ready for use
    - `status: "False"`, `reason: Provisioning` — Deployments are being created or are not yet ready
    - `status: "True"`, `reason: Ready` — Both `openclaw` and `openclaw-proxy` Deployments are available

**Checking instance status:**

```sh
# View instance with status columns
kubectl get openclaw
# NAME       READY   REASON
# instance   True    Ready

# View full status
kubectl get openclaw instance -o yaml

# Check if instance is ready
kubectl get openclaw instance -o jsonpath='{.status.conditions[?(@.type=="Available")].status}'
```

The `kubectl get openclaw` output includes:
- **Ready** column: Shows the Available condition status (True/False/Unknown)
- **Reason** column: Shows why the instance is in its current state (Provisioning/Ready)

**Important:** Only OpenClaw instances named `instance` will be reconciled by the controller.

## Configuration

### Secrets Management

The OpenClaw operator manages authentication through two types of Secrets:

#### 1. Gateway Authentication Token (`openclaw-secrets`)

The controller automatically generates and manages a secure authentication token for the OpenClaw gateway:
- **Secret name:** `openclaw-secrets`
- **Data entry:** `OPENCLAW_GATEWAY_TOKEN` - A cryptographically secure 64-character hex string (256-bit entropy)
- **Generation:** Automatically created on first reconciliation using Go's `crypto/rand` package
- **Persistence:** Token is preserved across reconciliations (never regenerated unless the Secret is deleted)
- **Lifecycle:** Automatically deleted when the OpenClaw instance is removed (via owner references)

**Example retrieval:**
```sh
kubectl get secret openclaw-secrets -n openclaw-system -o jsonpath='{.data.OPENCLAW_GATEWAY_TOKEN}' | base64 -d
```

#### 2. LLM API Key (User-Managed Secret)

The `geminiAPIKey` field in the OpenClaw CR references a user-managed Secret containing your Gemini API key. The controller:
1. Validates the Secret referenced in `spec.geminiAPIKey` exists in the same namespace
2. Verifies the Secret contains the specified key with a non-empty value
3. Configures the `openclaw-proxy` deployment to reference your Secret directly via environment variable
4. Kubernetes automatically propagates Secret updates to the proxy pods (enables key rotation without controller intervention)

**Creating the API key Secret:**
```sh
kubectl create secret generic gemini-api-key \
  --from-literal=api-key="YOUR_GEMINI_API_KEY" \
  -n openclaw-system
```

Or using a YAML manifest (see `config/samples/gemini-api-key-secret.yaml`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gemini-api-key
  namespace: openclaw-system
type: Opaque
stringData:
  api-key: "YOUR_GEMINI_API_KEY"
```

**How it works:**
- The `openclaw-proxy` deployment's `GEMINI_API_KEY` environment variable uses `valueFrom.secretKeyRef` pointing to your Secret
- No intermediate Secret is created - the controller directly references your Secret
- The proxy pod receives the API key value from Kubernetes, which automatically updates if you change the Secret value

**Security Considerations:**
- The LLM API key is stored only in your user-managed Secret (not visible in the OpenClaw CR)
- Fully compatible with external secret management tools (Sealed Secrets, External Secrets Operator, Vault, etc.)
- The controller never copies or stores API key values - it only validates and references your Secret
- The gateway token is never exposed in the CR, only in the operator-managed Secret
- Consider enabling encryption at rest for etcd in your cluster

### Migration from Direct API Key to Secret Reference

**Breaking Change:** The `apiKey` field has been replaced with `geminiAPIKey` Secret reference in v1alpha1.

If you have an existing OpenClaw instance using the old `apiKey` field, follow these steps to migrate:

1. **Create a Secret with your existing API key:**
   ```sh
   # Extract the current API key from your OpenClaw CR
   CURRENT_KEY=$(kubectl get openclaw instance -n openclaw-system -o jsonpath='{.spec.apiKey}')
   
   # Create a Secret with the API key
   kubectl create secret generic gemini-api-key \
     --from-literal=api-key="$CURRENT_KEY" \
     -n openclaw-system
   ```

2. **Update your OpenClaw CR to use the Secret reference:**
   ```sh
   kubectl patch openclaw instance -n openclaw-system --type='json' -p='[
     {"op": "remove", "path": "/spec/apiKey"},
     {"op": "add", "path": "/spec/geminiAPIKey", "value": {"name": "gemini-api-key", "key": "api-key"}}
   ]'
   ```

3. **Verify the migration:**
   ```sh
   # Check that the OpenClaw instance is Available
   kubectl get openclaw instance -n openclaw-system
   
   # Verify the proxy deployment references your Secret
   kubectl get deployment openclaw-proxy -n openclaw-system -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="GEMINI_API_KEY")].valueFrom.secretKeyRef}'
   ```

**Rollback:** If you need to rollback to the old operator version, the CRD schema change is backward compatible for reads (the old apiKey field will appear empty).

**Note:** After migration, the old `openclaw-proxy-secrets` Secret (if it exists) is no longer used and can be safely deleted. The proxy deployment now references your user-managed Secret directly.

## Version Information

The operator logs its version and build time during startup for troubleshooting and deployment tracking:

```
INFO	setup	Starting OpenClaw Operator	{"version": "fc7c72b0", "buildTime": "2026-04-07T13:17:26Z"}
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
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/openclaw-operator:tag
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
make deploy IMG=<some-registry>/openclaw-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
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
make build-installer IMG=<some-registry>/openclaw-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/openclaw-operator/<tag or branch>/dist/install.yaml
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

