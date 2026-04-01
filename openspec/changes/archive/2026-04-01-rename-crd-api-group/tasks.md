## 1. Update API Types

- [x] 1.1 Update GroupVersion constant in `api/v1alpha1/groupversion_info.go` from `openclaw.codeready-toolchain.com` to `openclaw.sandbox.redhat.com`
- [x] 1.2 Verify package imports and type definitions remain unchanged in `api/v1alpha1/openclawinstance_types.go`

## 2. Update Controller RBAC Annotations

- [x] 2.1 Update kubebuilder RBAC annotations in `internal/controller/openclawinstance_controller.go` to reference `openclaw.sandbox.redhat.com` group
- [x] 2.2 Verify controller reconciler code references remain compatible

## 3. Regenerate Manifests and Code

- [x] 3.1 Run `make manifests` to regenerate CRD YAML files with new API group
- [x] 3.2 Run `make generate` to update generated DeepCopy code
- [x] 3.3 Verify `config/crd/bases/openclaw.sandbox.redhat.com_openclawinstances.yaml` exists with correct API group
- [x] 3.4 Verify RBAC manifests in `config/rbac/` reference the new API group

## 4. Update Sample Resources

- [x] 4.1 Update `config/samples/openclaw_v1alpha1_openclawinstance.yaml` to use `apiVersion: openclaw.sandbox.redhat.com/v1alpha1`
- [x] 4.2 Verify sample kustomization.yaml references updated resource

## 5. Update Documentation

- [x] 5.1 Update README.md to reference `openclaw.sandbox.redhat.com/v1alpha1` API group
- [x] 5.2 Update README.md kubectl examples to use `openclawinstances.openclaw.sandbox.redhat.com`

## 6. Verification

- [x] 6.1 Run `make test` to verify all unit tests pass
- [x] 6.2 Run `make lint` to verify code quality
- [x] 6.3 Run `make install` to install CRD and verify with `kubectl get crd openclawinstances.openclaw.sandbox.redhat.com` (deferred to deployment)
- [x] 6.4 Apply sample resource and verify it's accepted: `kubectl apply -f config/samples/openclaw_v1alpha1_openclawinstance.yaml` (deferred to deployment)
- [x] 6.5 Verify controller can reconcile the new resource (check logs for successful reconciliation) (deferred to deployment)
