# Claw Operator

A Kubernetes operator that manages [OpenClaw](https://github.com/openclaw/openclaw) instances on OpenShift. It handles deployment, credential injection for LLM providers, HTTPS routing, and gateway authentication through a single `Claw` custom resource.

## Security

The operator applies multiple layers of defense:

- **Sandboxing** -- each Claw instance runs in its own namespace with OpenShift's restricted SCC enforced automatically (UID isolation, SELinux, seccomp).
- **Secret isolation** -- OpenClaw pods never see API keys or tokens. Credentials are only mounted in the proxy, which runs as a separate Deployment. The OpenClaw container has no Secret mounts and no environment variables containing credential values.
- **Network isolation** -- OpenClaw pods cannot reach the internet directly; all outbound traffic is forced through the credential proxy via NetworkPolicy. The proxy only allows HTTPS (port 443) egress and rejects any domain not explicitly configured.
- **Ingress restriction** -- only the OpenShift router namespace can reach the gateway port (NetworkPolicy on ingress).
- **Credential injection** -- API keys and tokens are stored in user-managed Kubernetes Secrets and referenced by the proxy only (`secretKeyRef`/volume mount). The operator never reads or copies credential values.
- **Gateway authentication** -- a cryptographically random 256-bit token is auto-generated per instance and required for all gateway access.
- **Device pairing** -- remote browser connections require a one-time approval via CLI before they can interact with the instance.
- **Pod hardening** -- all containers run as non-root, drop all Linux capabilities, use a read-only root filesystem, and set `seccompProfile: RuntimeDefault`.
- **Minimal privileges** -- service account tokens are not mounted (`automountServiceAccountToken: false`).
- **External secret management** -- credential Secrets are user-managed and fully compatible with [External Secrets Operator](https://external-secrets.io/), Sealed Secrets, or HashiCorp Vault. Using an external secret manager is recommended for production.

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

The operator runs in `claw-operator`, but user workloads (Claw instances, secrets) go in your own namespace. Set it once and all commands below will use it:

```sh
export NS=my-claw-namespace
oc create namespace $NS
```

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
      domain: "generativelanguage.googleapis.com"
      apiKey:
        header: x-goog-api-key
      provider: google
EOF
```

Wait for it to become ready and get the URL and gateway token:

```sh
make wait-ready NS=$NS
```

### 5. Log In

Open the URL printed above and enter the gateway token to log in.

On vanilla Kubernetes (no Route), use port-forwarding instead:

```sh
oc port-forward svc/claw 18789:18789 -n $NS
# Then open http://localhost:18789
```

### 6. Pair Your Device

On first connection you'll see "pairing required". With the browser tab open, approve the request:

```sh
make approve-pairing NS=$NS
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
| `make test` | Run unit tests |
| `make test-e2e` | Run e2e tests (requires Kind) |
| `make lint` | Run golangci-lint |
| `make build` | Build manager binary locally |
| `make run` | Run controller locally against cluster |
| `make manifests` | Regenerate CRD YAML and RBAC from markers |
| `make generate` | Regenerate DeepCopy methods |

Override the container tool with `CONTAINER_TOOL=docker` if needed. Default is `podman`.
