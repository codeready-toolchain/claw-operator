## 1. API Type Updates

- [x] 1.1 Add `Selector` field to ClawDevicePairingRequestSpec in `api/v1alpha1/clawdevicepairingrequest_types.go`
- [x] 1.2 Add kubebuilder validation marker `+kubebuilder:validation:Required` for the Selector field
- [x] 1.3 Import `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` if not already present
- [x] 1.4 Run `make generate` to update generated deepcopy methods
- [x] 1.5 Run `make manifests` to regenerate CRD YAML with the new field

## 2. Controller Implementation

- [x] 2.1 Update `ClawDevicePairingRequestReconciler.Reconcile()` to convert Spec.Selector to labels.Selector using `metav1.LabelSelectorAsSelector()`
- [x] 2.2 Handle conversion errors by setting a condition with reason "InvalidSelector"
- [x] 2.3 Query for pods using the selector with `r.List(ctx, podList, &client.ListOptions{Namespace: req.Namespace, LabelSelector: selector})`
- [x] 2.4 Handle zero matching pods by setting a condition with reason "NoMatchingPod"
- [x] 2.5 Handle multiple matching pods by setting a condition with reason "MultipleMatchingPods"
- [x] 2.6 Process the pairing request when exactly one pod matches

## 3. Testing

- [x] 3.1 Add test for ClawDevicePairingRequest creation with valid selector
- [x] 3.2 Add test for ClawDevicePairingRequest creation without selector (should fail validation)
- [x] 3.3 Add test for controller handling zero matching pods
- [x] 3.4 Add test for controller handling multiple matching pods
- [x] 3.5 Add test for controller handling invalid selector
- [x] 3.6 Add test for controller successfully finding single matching pod
- [x] 3.7 Run `make test` to verify all tests pass

## 4. Documentation

- [x] 4.1 Update CLAUDE.md to document the selector field and its usage
- [x] 4.2 Add migration notes for existing ClawDevicePairingRequest resources
- [x] 4.3 Document that this is a breaking change requiring manual CR updates before upgrading
