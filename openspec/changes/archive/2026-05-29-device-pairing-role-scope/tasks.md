## 1. Manifest Changes

- [x] 1.1 Rename `internal/assets/manifests/claw-device-pairing/clusterrole.yaml` to `role.yaml` and change `kind: ClusterRole` to `kind: Role` (keep rules unchanged)
- [x] 1.2 Update `internal/assets/manifests/claw-device-pairing/rolebinding.yaml` to set `roleRef.kind` to `Role` instead of `ClusterRole`
- [x] 1.3 Update `internal/assets/manifests/claw-device-pairing/kustomization.yaml` to reference `role.yaml` instead of `clusterrole.yaml`

## 2. Controller Code Changes

- [x] 2.1 Update the embedded file reference in `claw_resource_controller.go` from `clusterrole.yaml` to `role.yaml`
- [x] 2.2 Replace `newDevicePairingClusterRoleUnstructured` with `newDevicePairingRoleUnstructured` that creates a namespace-scoped `Role` (with namespace set)
- [x] 2.3 Update `cleanupDevicePairingResources` to use the new Role unstructured builder
- [x] 2.4 Remove `isClusterScoped` function and its usage in the owner-reference loop (the Role is now namespace-scoped, so it gets owner references like all other resources)
- [x] 2.5 Remove `ClusterRoleKind` constant if no longer used anywhere

## 3. RBAC Marker Update

- [x] 3.1 Update the kubebuilder RBAC marker to reference `roles` instead of `clusterroles` (verify no other code path requires `clusterroles` first)
- [x] 3.2 Run `make manifests` to regenerate RBAC and CRD manifests

## 4. Validation

- [x] 4.1 Run `make build` to verify compilation
- [x] 4.2 Run `make lint` to check for lint issues
- [x] 4.3 Run `make test` to verify existing tests pass
