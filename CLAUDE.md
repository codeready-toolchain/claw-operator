# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kubernetes operator (Go, Kubebuilder/Operator SDK) that manages OpenClaw instances on OpenShift/Kubernetes. CRD: `OpenClaw` in API group `openclaw.sandbox.redhat.com/v1alpha1`.

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

### Two-controller design

The operator uses **split reconcilers** (not one monolithic controller):

- **OpenClawConfigMapReconciler** (`internal/controller/openclaw_configmap_controller.go`) — creates a ConfigMap (`openclaw-config`) from an embedded manifest
- **OpenClawDeploymentReconciler** (`internal/controller/openclaw_deployment_controller.go`) — creates a Deployment (`openclaw`), but only after the ConfigMap exists (explicit dependency check)

Both controllers only reconcile `OpenClaw` resources named exactly `"instance"` — all other names are skipped.

### Embedded manifests

Kubernetes manifests are embedded via `//go:embed` in `internal/assets/manifests.go`. The actual YAML files live in `internal/assets/manifests/`. At runtime, manifests are decoded with the universal deserializer, then the namespace and owner reference are set dynamically.

### Key directories

- `api/v1alpha1/` — CRD type definitions (OpenClawSpec, OpenClawStatus)
- `internal/controller/` — Reconciler implementations and tests
- `internal/assets/manifests/` — Embedded ConfigMap and Deployment YAML
- `cmd/main.go` — Manager entrypoint, wires up both controllers
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
- **E2E:** `test/e2e/` — runs against a Kind cluster, validates metrics and full deployment

## Conventions

- Owner references are set on all created resources via `controllerutil.SetControllerReference`
- Pod security: non-root (uid 65532), restricted seccomp, all capabilities dropped
- Linting config in `.golangci.yml` — notable: `lll`, `dupl`, `ginkgolinter` enabled
- License header required (template in `hack/boilerplate.go.txt`)
