## 1. Copy manifests to embedded assets directory

- [x] 1.1 Copy `networkpolicy.yaml` from `internal/manifests/` to `internal/assets/manifests/`
- [x] 1.2 Copy `proxy-configmap.yaml` from `internal/manifests/` to `internal/assets/manifests/`
- [x] 1.3 Copy `proxy-deployment.yaml` from `internal/manifests/` to `internal/assets/manifests/`
- [x] 1.4 Copy `proxy-service.yaml` from `internal/manifests/` to `internal/assets/manifests/`
- [x] 1.5 Copy `route.yaml` from `internal/manifests/` to `internal/assets/manifests/`
- [x] 1.6 Copy `service.yaml` from `internal/manifests/` to `internal/assets/manifests/`

## 2. Update kustomization.yaml with all resources

- [x] 2.1 Update `internal/assets/manifests/kustomization.yaml` to include all 10 resources in the resource list
- [x] 2.2 Preserve the `labels` section with `app.kubernetes.io/name: openclaw` in kustomization.yaml
- [x] 2.3 Verify resource ordering matches source kustomization.yaml: pvc, configmap, deployment, service, route, proxy-configmap, proxy-deployment, proxy-service, networkpolicy

## 3. Add RBAC permissions for new resource types

- [x] 3.1 Add `// +kubebuilder:rbac` marker for NetworkPolicy resources (groups=networking.k8s.io, resources=networkpolicies, verbs=get;list;watch;create;update;patch;delete)
- [x] 3.2 Add `// +kubebuilder:rbac` marker for Service resources (groups="", resources=services, verbs=get;list;watch;create;update;patch;delete)
- [x] 3.3 Add `// +kubebuilder:rbac` marker for Route resources (groups=route.openshift.io, resources=routes, verbs=get;list;watch;create;update;patch;delete)
- [x] 3.4 Run `make manifests` to regenerate `config/rbac/role.yaml` with new permissions

## 4. Verify and test

- [x] 4.1 Run `make build` to verify compilation succeeds
- [x] 4.2 Run `make test` to verify existing unit tests pass
- [x] 4.3 Verify embedded filesystem includes all 10 manifest files at runtime
