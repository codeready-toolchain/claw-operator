# Claw Operator

[![Build](https://github.com/codeready-toolchain/claw-operator/actions/workflows/cd.yml/badge.svg)](https://github.com/codeready-toolchain/claw-operator/actions/workflows/cd.yml)

An OpenShift-oriented Kubernetes operator that manages [OpenClaw](https://github.com/openclaw/openclaw) instances. It handles deployment, credential injection for LLM providers, HTTPS routing, and gateway authentication through a single `Claw` custom resource. While the operator can run on vanilla Kubernetes, it is designed for OpenShift where the restricted Security Context Constraint (SCC) provides the primary pod security boundary -- non-root UID enforcement, SELinux confinement, seccomp filtering, and privilege escalation prevention are all handled by the platform.

## Security

The operator applies multiple layers of defense:

- **OpenShift restricted SCC** -- each pod runs under the restricted SCC which enforces non-root UIDs, SELinux labels, seccomp `RuntimeDefault`, and blocks privilege escalation. All containers additionally drop all Linux capabilities and disable service account token mounting.
- **Secret isolation** -- OpenClaw pods never see API keys or tokens. Credentials are stored in user-managed Kubernetes Secrets and injected by the proxy (a separate Deployment) into outbound requests.
- **External secret management** -- credential Secrets are user-managed and fully compatible with [External Secrets Operator](https://external-secrets.io/), Sealed Secrets, or HashiCorp Vault. Using an external secret manager is recommended for production.
- **Network isolation** -- OpenClaw pods cannot reach the internet directly; all outbound traffic is forced through the credential proxy via NetworkPolicy. The proxy only allows HTTPS (port 443) egress and rejects any domain not explicitly configured.
- **Ingress restriction** -- only the OpenShift router namespace can reach the gateway port (NetworkPolicy on ingress).
- **Gateway authentication** -- two modes: `token` (default) auto-generates a 256-bit token per instance; `password` uses a shared password from a Kubernetes Secret. See `spec.auth` in the [CRD reference](docs/adr/0011-password-auth-mode.md).
- **Device pairing** -- when enabled via `spec.auth.disableDevicePairing: false`, remote browser connections require a one-time approval via CLI before they can interact with the instance. Disabled by default.

## Installation (OLM)

The recommended way to install the operator on an OpenShift cluster with OLM.

### 1. Create the Operator Namespace

```sh
oc create namespace claw-operator
```

### 2. Create a CatalogSource

```sh
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: claw-operator-catalog
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: quay.io/codeready-toolchain/claw-operator-catalog:latest
  displayName: Claw Operator
  publisher: Red Hat
  updateStrategy:
    registryPoll:
      interval: 15m
EOF
```

### 3. Create an OperatorGroup

```sh
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: claw-operator
  namespace: claw-operator
spec:
  targetNamespaces:
    - claw-operator
EOF
```

### 4. Create a Subscription

```sh
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: claw-operator
  namespace: claw-operator
spec:
  channel: staging
  name: claw-operator
  source: claw-operator-catalog
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

### 5. Verify the Operator Is Running

```sh
oc get csv -n claw-operator
oc get pods -n claw-operator
```

Once the CSV phase shows `Succeeded` and the controller pod is running, proceed to [Set Up Your Namespace](#2-set-up-your-namespace) to create a Claw instance.

## Development Quick Start

### Prerequisites

- OpenShift cluster (or Kubernetes with manual port-forward)
- `oc` CLI logged into the cluster
- `podman` installed locally
- A container registry accessible from your cluster (e.g., `quay.io`)

### 1. Deploy the Operator

Log in to your container registry and OpenShift cluster:

```sh
podman login quay.io
oc login --server=https://api.your-cluster.example.com:6443
```

Make sure the `claw-operator` and `claw-proxy` repositories on quay.io are set to **public** (or configure a pull secret), so the cluster can pull the images.

Then build, push, and deploy in one command:

```sh
make dev-setup REGISTRY=quay.io/<your-user>
```

This builds both images (operator + proxy), pushes them, installs CRDs, and deploys the controller into the `claw-operator` namespace.

### 2. Set Up Your Namespace

The operator runs in `claw-operator`, but user workloads (Claw instances, secrets) go in your own namespace. Set `NS` once and all commands below will use it (Makefile targets also default to `my-claw`):

```sh
export NS=my-claw   # or pick your own name
oc create namespace $NS
```

> **Do not use the `default` namespace.** On OpenShift, the `default` namespace uses a different SCC assignment that may not inject a numeric `runAsUser` into pods. This causes containers whose image declares a non-numeric `USER` (e.g., `node`) to fail with `runAsNonRoot` verification errors. Always create a dedicated namespace for your Claw instance.

### 3. Create a Credential Secret

#### Google Gemini

Get your API key from [Google AI Studio](https://aistudio.google.com/apikey).

```sh
oc create secret generic gemini-api-key \
  --from-literal=api-key=YOUR_GEMINI_API_KEY \
  -n $NS
```

#### OpenAI

Get your API key from the [OpenAI Platform](https://platform.openai.com/api-keys).

```sh
oc create secret generic openai-api-key \
  --from-literal=api-key=YOUR_OPENAI_API_KEY \
  -n $NS
```

### 4. Create a Claw Instance

#### Google Gemini

Apply the sample CR, or use the inline version below:

```sh
oc apply -f config/samples/claw_v1alpha1_claw.yaml -n $NS
```

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
EOF
```

#### OpenAI

```sh
oc apply -f - <<EOF
apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
  namespace: $NS
spec:
  credentials:
    - name: openai
      type: bearer
      secretRef:
        - name: openai-api-key
          key: api-key
      provider: openai
EOF
```

For known providers (`google`, `anthropic`, `openai`, `xai`), the operator infers `domain` automatically. For other LLM providers, messaging channels, MCP servers, and more, see the [User Guide](docs/user-guide.md).

Wait for it to become ready and get the URL and gateway token:

```sh
make wait-ready NS=$NS
# or, for a non-default instance name:
# make wait-ready NS=$NS CLAW=my-instance
```

### 5. Log In

Open the URL printed above and enter the gateway token to log in.

On vanilla Kubernetes (no Route), use port-forwarding instead:

```sh
oc port-forward svc/instance 18789:18789 -n $NS
# Then open http://localhost:18789
# Replace "instance" with your Claw CR name if different
```

**Password mode:** If you prefer shared password access (useful for workshops, demos, or shared team instances), create a Secret with the password and set `spec.auth.mode: password` on the Claw CR. Users enter the password in the browser instead of using a token. See [ADR-0011](docs/adr/0011-password-auth-mode.md) for details.

### 6. Pair Your Device (opt-in)

Device pairing is disabled by default. To enable it, set `spec.auth.disableDevicePairing: false` on the Claw CR.

When enabled, on first connection you'll see "pairing required". With the browser tab open, approve the request:

```sh
make approve-pairing NS=$NS
# or, for a non-default instance name:
# make approve-pairing NS=$NS CLAW=my-instance
```

This picks the first pending request and asks for confirmation.

Refresh the browser after approval. The device is remembered across sessions.

> **Note:** Device pairing is disabled by default in all auth modes. Set `spec.auth.disableDevicePairing: false` to enable it.

## Makefile Targets

Run `make help` for a full list. Key targets:

| Target | Description |
|---|---|
| `make dev-setup REGISTRY=...` | Full dev cycle: build, push, deploy |
| `make dev-build dev-push dev-deploy REGISTRY=...` | Step-by-step dev iteration |
| `make dev-cleanup` | Tear down deployed controller and CRDs |
| `make wait-ready NS=... [CLAW=...]` | Wait for ready, print URL + token |
| `make approve-pairing NS=... [CLAW=...]` | List & approve a device pairing request |
| `make test` | Run unit tests |
| `make test-e2e` | Run e2e tests (requires Kind) |
| `make lint` | Run golangci-lint |
| `make build` | Build manager binary locally |
| `make run` | Run controller locally against cluster |
| `make manifests` | Regenerate CRD YAML and RBAC from markers |
| `make generate` | Regenerate DeepCopy methods |

Override the container tool with `CONTAINER_TOOL=docker` if needed. Default is `podman`.