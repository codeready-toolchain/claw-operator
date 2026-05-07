## 1. Kustomize Manifests

- [x] 1.1 Create `internal/assets/manifests/claw-device-pairing/kustomization.yaml` with resource list and `app.kubernetes.io/name: claw-device-pairing` label via the Kustomize `labels` directive
- [x] 1.2 Create `internal/assets/manifests/claw-device-pairing/serviceaccount.yaml` with name `CLAW_INSTANCE_NAME-device-pairing`
- [x] 1.3 Create `internal/assets/manifests/claw-device-pairing/deployment.yaml` with image `quay.io/xcoulon/claw-device-pairing:latest`, security hardening (non-root, drop all caps, readOnlyRootFilesystem, RuntimeDefault seccomp), and serviceAccountName `CLAW_INSTANCE_NAME-device-pairing`
- [x] 1.4 Create `internal/assets/manifests/claw-device-pairing/service.yaml` as ClusterIP targeting the device-pairing pod port
- [x] 1.5 Create `internal/assets/manifests/claw-device-pairing/route.yaml` with `OPENCLAW_ROUTE_HOST` as `.spec.host`, `.spec.path: /integration/device-pairing`, edge TLS termination, targeting `CLAW_INSTANCE_NAME-device-pairing` Service

## 2. Controller Integration

- [x] 2.1 Add device-pairing manifest entries to `buildKustomizedObjects()` — load files from `manifests/claw-device-pairing/`, write to in-memory filesystem, build via Kustomize, and merge with claw + proxy objects
- [x] 2.2 Add `injectRouteHostIntoDevicePairingRoute()` function to replace `OPENCLAW_ROUTE_HOST` placeholder in the device-pairing Route's `.spec.host` with the resolved Claw Route host
- [x] 2.3 Call `injectRouteHostIntoDevicePairingRoute()` in the `Reconcile()` function after `getRouteURL()` resolves the host, before `applyRouteOnly()`

## 3. Tests

- [x] 3.1 Add test verifying device-pairing manifests are included in `buildKustomizedObjects()` output (ServiceAccount, Deployment, Service, Route all present with correct names)
- [x] 3.2 Add test verifying `CLAW_INSTANCE_NAME` replacement works for device-pairing resources
- [x] 3.3 Add test verifying device-pairing Route host injection replaces `OPENCLAW_ROUTE_HOST` with the resolved host
- [x] 3.4 Add test verifying device-pairing Route has correct path `/integration/device-pairing`
- [x] 3.5 Add test verifying reconciliation applies device-pairing resources (ServiceAccount, Deployment, Service exist after reconcile)
- [x] 3.6 Run `make test` and `make lint` to verify all tests pass and no lint issues
