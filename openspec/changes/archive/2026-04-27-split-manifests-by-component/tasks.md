## 1. Reorganize manifest files

- [x] 1.1 Create `internal/assets/manifests/claw/` directory
- [x] 1.2 Create `internal/assets/manifests/claw-proxy/` directory
- [x] 1.3 Move claw resources to `claw/` subdirectory (deployment.yaml, service.yaml, route.yaml, configmap.yaml, pvc.yaml, networkpolicy.yaml, ingress-networkpolicy.yaml)
- [x] 1.4 Move proxy resources to `claw-proxy/` subdirectory (proxy-deployment.yaml, proxy-service.yaml, proxy-configmap.yaml)
- [x] 1.5 Create `kustomization.yaml` in `claw/` with commonLabels and resource list for claw component
- [x] 1.6 Create `kustomization.yaml` in `claw-proxy/` with commonLabels and resource list for proxy component
- [x] 1.7 Delete top-level `internal/assets/manifests/kustomization.yaml`

## 2. Update controller kustomize build logic

- [x] 2.1 Extract helper function `buildKustomizeFromPath(fsys fs.FS, path string)` to build kustomize from a subdirectory
- [x] 2.2 Update `buildKustomizedObjects` to call `buildKustomizeFromPath` for `manifests/claw/`
- [x] 2.3 Update `buildKustomizedObjects` to call `buildKustomizeFromPath` for `manifests/claw-proxy/`
- [x] 2.4 Merge both object lists in `buildKustomizedObjects` before returning
- [x] 2.5 Ensure error handling returns early if either build fails

## 3. Verify and fix tests

- [x] 3.1 Run unit tests to identify any failures due to manifest path changes
- [x] 3.2 Update test code if needed to reference new directory structure
- [x] 3.3 Verify all tests pass with `make test`

## 4. Validation

- [x] 4.1 Run `make manifests` and `make generate` to ensure CRD generation still works
- [x] 4.2 Run `make build` to ensure operator compiles successfully
- [x] 4.3 Manually test reconciliation in dev environment to verify both components deploy correctly
- [x] 4.4 Verify all resources still have `app.kubernetes.io/name: claw` label
