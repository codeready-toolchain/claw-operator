# Claw Operator

An OpenShift-oriented Kubernetes operator that manages [OpenClaw](https://github.com/openclaw/openclaw) instances. It handles deployment, credential injection for LLM providers, HTTPS routing, and gateway authentication through a single `Claw` custom resource. While the operator can run on vanilla Kubernetes, it is designed for OpenShift where the restricted Security Context Constraint (SCC) provides the primary pod security boundary -- non-root UID enforcement, SELinux confinement, seccomp filtering, and privilege escalation prevention are all handled by the platform.

## Security

The operator applies multiple layers of defense:

- **OpenShift restricted SCC** -- each pod runs under the restricted SCC which enforces non-root UIDs, SELinux labels, seccomp `RuntimeDefault`, and blocks privilege escalation. All containers additionally drop all Linux capabilities and disable service account token mounting.
- **Secret isolation** -- OpenClaw pods never see API keys or tokens. Credentials are stored in user-managed Kubernetes Secrets and injected by the proxy (a separate Deployment) into outbound requests.
- **External secret management** -- credential Secrets are user-managed and fully compatible with [External Secrets Operator](https://external-secrets.io/), Sealed Secrets, or HashiCorp Vault. Using an external secret manager is recommended for production.
- **Network isolation** -- OpenClaw pods cannot reach the internet directly; all outbound traffic is forced through the credential proxy via NetworkPolicy. The proxy only allows HTTPS (port 443) egress and rejects any domain not explicitly configured.
- **Ingress restriction** -- only the OpenShift router namespace can reach the gateway port (NetworkPolicy on ingress).
- **Gateway authentication** -- a cryptographically random 256-bit token is auto-generated per instance and required for all gateway access.
- **Device pairing** -- remote browser connections require a one-time approval via CLI before they can interact with the instance.

## Quick Start

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

```sh
oc create secret generic gemini-api-key \
  --from-literal=api-key=YOUR_GEMINI_API_KEY \
  -n $NS
```

Get your API key from [Google AI Studio](https://aistudio.google.com/apikey).

### 4. Create a Claw Instance

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
        name: gemini-api-key
        key: api-key
      provider: google
EOF
```

For known providers (`google`, `anthropic`), the operator infers `domain` and `apiKey` automatically. For other LLM providers (Anthropic Claude, Vertex AI, and more), see the [Provider Setup Guide](docs/provider-setup.md).

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

### 6. Pair Your Device

On first connection you'll see "pairing required". With the browser tab open, approve the request:

```sh
make approve-pairing NS=$NS
# or, for a non-default instance name:
# make approve-pairing NS=$NS CLAW=my-instance
```

This picks the first pending request and asks for confirmation.

Refresh the browser after approval. The device is remembered across sessions.

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
