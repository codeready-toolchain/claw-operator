# CLAUDE.md

## Project Overview

Kubernetes operator (Go, Kubebuilder/Operator SDK) that manages OpenClaw instances on OpenShift/Kubernetes. Detailed architecture reference in `docs/architecture.md`.

**CRDs** (API group `claw.sandbox.redhat.com/v1alpha1`):
- `Claw` — Main CRD. Spec: `config` (ConfigSpec: raw RawExtension, mergeMode merge/overwrite/seedOnly), `credentials` ([]CredentialSpec), `auth` (AuthSpec: mode token/password, passwordSecretRef, disableDevicePairing). Status: `conditions`, `url`, `gatewayTokenSecretRef`
- `ClawDevicePairingRequest` — Device pairing. Spec: `requestID`, `selector` (LabelSelector). Controller matches exactly one pod
- `ClawOperatorConfig` — Cluster-admin policy singleton (name `cluster`, in the operator's own namespace). Spec: `allowedConfigModes` ([]ConfigMode, empty = unrestricted). Gates which `mergeMode` values `Claw` CRs may use; see [ADR-0021](docs/adr/0021-seed-only-config-mode.md)

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
make test-e2e           # Run e2e tests (creates/tears down Kind cluster automatically)

# Container
make container-build IMG=<registry>/claw-operator:tag

# Dev deployment (OpenShift/Kubernetes)
make dev-setup REGISTRY=quay.io/myuser           # Build + push + deploy (one command)
make dev-build dev-push dev-deploy REGISTRY=...   # Iterate after code changes
make wait-ready NS=my-claw                        # Wait for ready, print URL + token
make approve-pairing NS=my-claw                   # List & approve a device pairing request
make dev-cleanup                                  # Tear down
```

## Architecture

Single unified controller (`ClawResourceReconciler`) manages all resources via Kustomize and server-side apply. Three-phase reconciliation ensures the Route host is resolved before injecting it into the gateway ConfigMap for CORS. MITM proxy injects credentials transparently; known providers get auto-inferred defaults.

Detailed architecture reference: @docs/architecture.md

## Key Directories

- `api/v1alpha1/` -- CRD types (`claw_types.go`, `clawdevicepairingrequest_types.go`, `clawoperatorconfig_types.go`). Condition/annotation constants live here
- `internal/controller/` -- Reconciler + tests. Key files: `claw_resource_controller.go` (main reconciler), `claw_credentials.go` (credential validation, gateway secret), `claw_providers.go` (centralized `knownProviders` registry, provider defaults/routing), `claw_proxy.go` (proxy config), `claw_deployment.go` (deployment configuration), `claw_status.go` (status updates), `claw_models.go` (model catalog accessor, delegates to `knownProviders`), `claw_auth.go` (auth mode + device pairing), `claw_operator_config.go` (ClawOperatorConfig mergeMode gating)
- `internal/proxy/` -- MITM proxy library
- `internal/assets/manifests/` -- Embedded Kustomize manifests (two components: `claw/`, `claw-proxy/`)
- `cmd/main.go` -- Manager entrypoint (reads `PROXY_IMAGE`, `KUBECTL_IMAGE`, `IMAGE_PULL_POLICY`, `WATCH_NAMESPACE` env vars)
- `cmd/proxy/main.go` -- Proxy binary entrypoint
- `config/` -- Kustomize overlays for CRDs, RBAC, manager deployment

## Code Generation

After modifying API types in `api/v1alpha1/`, always run both:
```bash
make manifests   # regenerate CRD YAML in config/crd/bases/
make generate    # regenerate zz_generated.deepcopy.go
```
RBAC is generated from `// +kubebuilder:rbac:...` markers on reconciler methods.

## Testing

- **Framework**: testify/require + testify/assert with `envtest` (real API server, no full cluster)
- **Shared setup**: `suite_test.go` boots envtest via `TestMain(m *testing.M)`
- **Assertions**: `require` for fatal setup errors, `assert` for value comparisons
- **Pattern**: `Test*` with `t.Run()` subtests, `t.Cleanup()`, table-driven tests
- **Async**: `waitFor(t, timeout, interval, condition, message)` helper (10s timeout, 250ms poll)
- **Test files**: separate per resource type (e.g., `claw_configmap_test.go`, `claw_credentials_test.go`)
- **E2E**: `test/e2e/` runs against Kind cluster

## Conventions

- Owner references on all created resources via `controllerutil.SetControllerReference`
- Pod security: non-root (uid 65532), restricted seccomp, all capabilities dropped
- `readOnlyRootFilesystem: true` on proxy and `wait-for-proxy` containers (not on init-config or gateway -- Node.js and AI tools need writable paths)
- Linting: `.golangci.yml` with `lll`, `dupl` enabled
- License header required (template in `hack/boilerplate.go.txt`)
