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
  # Required: API key for authenticating with the LLM provider
  apiKey: "your-gemini-api-key-here"
status:
  # Status fields will be populated by the controller
```

#### Spec Fields

- `apiKey` (string, required): The API key for authenticating with your LLM provider (currently Gemini). This key is stored in a Kubernetes Secret (`openclaw-proxy-secrets`) and injected into the proxy for secure upstream authentication.

**Important:** Only OpenClaw instances named `instance` will be reconciled by the controller.

## Configuration

### Secrets Management

The OpenClaw operator automatically creates and manages two Kubernetes Secrets for authentication:

#### 1. Gateway Authentication Token (`openclaw-secrets`)

The controller automatically generates a secure, randomly-generated authentication token for the OpenClaw gateway:
- **Secret name:** `openclaw-secrets`
- **Data entry:** `OPENCLAW_GATEWAY_TOKEN` - A cryptographically secure 64-character hex string (256-bit entropy)
- **Generation:** Automatically created on first reconciliation using Go's `crypto/rand` package
- **Persistence:** Token is preserved across reconciliations (never regenerated unless the Secret is deleted)
- **Lifecycle:** Automatically deleted when the OpenClaw instance is removed (via owner references)

**Example retrieval:**
```sh
kubectl get secret openclaw-secrets -n openclaw-system -o jsonpath='{.data.OPENCLAW_GATEWAY_TOKEN}' | base64 -d
```

#### 2. LLM API Key (`openclaw-proxy-secrets`)

The `apiKey` field in the OpenClaw CR is mandatory and used for LLM provider authentication. The controller:
1. Reads the API key from the OpenClaw CR's `spec.apiKey` field
2. Creates or updates a Secret named `openclaw-proxy-secrets` with the key stored under `GEMINI_API_KEY`
3. The proxy deployment automatically mounts this Secret and uses it for upstream requests

**Security Considerations:**
- The LLM API key is stored directly in the OpenClaw CR (visible via `kubectl get openclaw -o yaml`)
- The gateway token is never exposed in the CR, only in the Secret
- Consider enabling encryption at rest for etcd in your cluster
- Secret reference support (using SecretKeySelector) is planned for future releases to improve security

**Example:**
```yaml
apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: OpenClaw
metadata:
  name: instance
  namespace: openclaw-system
spec:
  apiKey: "AIzaSyD-your-actual-gemini-api-key-here"
```

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
- go version v1.24.0+
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

