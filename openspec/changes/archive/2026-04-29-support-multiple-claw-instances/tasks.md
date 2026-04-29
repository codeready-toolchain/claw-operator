## 1. Add resource naming helper functions

- [x] 1.1 Create helper function `getClawDeploymentName(instanceName string) string` that returns `instanceName`
- [x] 1.2 Create helper function `getProxyDeploymentName(instanceName string) string` that returns `instanceName + "-proxy"`
- [x] 1.3 Create helper function `getGatewaySecretName(instanceName string) string` that returns `instanceName + "-gateway-token"`
- [x] 1.4 Create helper function `getConfigMapName(instanceName string) string` that returns `instanceName + "-config"`
- [x] 1.5 Create helper function `getPVCName(instanceName string) string` that returns `instanceName + "-home-pvc"`
- [x] 1.6 Create helper function `getServiceName(instanceName string) string` that returns `instanceName`
- [x] 1.7 Create helper function `getProxyServiceName(instanceName string) string` that returns `instanceName + "-proxy"`
- [x] 1.8 Create helper function `getRouteName(instanceName string) string` that returns `instanceName`
- [x] 1.9 Create helper function `getProxyCAConfigMapName(instanceName string) string` that returns `instanceName + "-proxy-ca"`
- [x] 1.10 Create helper function `getVertexADCConfigMapName(instanceName string) string` that returns `instanceName + "-vertex-adc"`
- [x] 1.11 Create helper function `getKubeConfigMapName(instanceName string) string` that returns `instanceName + "-kube-config"`
- [x] 1.12 Create helper function `getIngressNetworkPolicyName(instanceName string) string` that returns `instanceName + "-ingress"`
- [x] 1.13 Create helper function `getEgressNetworkPolicyName(instanceName string) string` that returns `instanceName + "-egress"`
- [x] 1.14 Create helper function `getProxyEgressNetworkPolicyName(instanceName string) string` that returns `instanceName + "-proxy-egress"`

## 2. Remove instance name filter

- [x] 2.1 Remove constant `ClawInstanceName = "instance"` from controller
- [x] 2.2 Remove instance name check in `Reconcile()` method (lines checking `if instance.Name != ClawInstanceName`)
- [x] 2.3 Remove instance name check in `findClawsReferencingSecret()` method
- [x] 2.4 Update log messages that referenced "instance" name to use actual instance name

## 3. Update resource creation to use dynamic names

- [x] 3.1 Update `applyGatewaySecret()` to use `getGatewaySecretName(instance.Name)` instead of hardcoded `ClawGatewaySecretName`
- [x] 3.2 Update `applySanitizedKubeconfig()` to use `getKubeConfigMapName(instance.Name)`
- [x] 3.3 Update `applyProxyCA()` to use `getProxyCAConfigMapName(instance.Name)`
- [x] 3.4 Update `applyVertexADCConfigMap()` to use `getVertexADCConfigMapName(instance.Name)`
- [x] 3.5 Update `applyProxyConfigMap()` to use dynamic ConfigMap name
- [x] 3.6 Update `buildKustomizedObjects()` to set dynamic names on parsed resources after kustomize build
- [x] 3.7 Add logic to set deployment names using helper functions after kustomize build
- [x] 3.8 Add logic to set service names using helper functions after kustomize build
- [x] 3.9 Add logic to set route name using helper function after kustomize build
- [x] 3.10 Add logic to set ConfigMap name using helper function after kustomize build
- [x] 3.11 Add logic to set PVC name using helper function after kustomize build
- [x] 3.12 Add logic to set NetworkPolicy names using helper functions after kustomize build

## 4. Add instance label to resources

- [x] 4.1 Add instance label `claw.sandbox.redhat.com/instance` with value `instance.Name` to all resources after kustomize build
- [x] 4.2 Update NetworkPolicy pod selectors to include instance label
- [x] 4.3 Verify Service selectors target correct pods via instance label

## 5. Update status updates to use dynamic names

- [x] 5.1 Update `getDeploymentAvailableStatus()` to use `getClawDeploymentName(instance.Name)` for gateway deployment lookup
- [x] 5.2 Update `getDeploymentAvailableStatus()` to use `getProxyDeploymentName(instance.Name)` for proxy deployment lookup
- [x] 5.3 Update `getRouteURL()` to use `getRouteName(instance.Name)` for Route lookup
- [x] 5.4 Update `updateStatus()` to set `GatewayTokenSecretRef` using `getGatewaySecretName(instance.Name)`

## 6. Update tests

- [ ] 6.1 Update test fixtures to use arbitrary instance names instead of "instance"
- [ ] 6.2 Add test case for reconciling Claw instance with custom name (e.g., "my-openclaw")
- [ ] 6.3 Add test case for multiple Claw instances in same namespace
- [ ] 6.4 Verify test suite no longer assumes fixed resource names
- [ ] 6.5 Update test assertions to check dynamic resource names
- [ ] 6.6 Run `make test` and verify all tests pass

## 7. Validation

- [ ] 7.1 Run `make manifests` and `make generate` to ensure CRD generation still works
- [ ] 7.2 Run `make build` to ensure operator compiles successfully
- [ ] 7.3 Deploy operator to test cluster
- [ ] 7.4 Create Claw instance with custom name (e.g., "test-instance")
- [ ] 7.5 Verify all resources are created with correct dynamic names
- [ ] 7.6 Verify instance label `claw.sandbox.redhat.com/instance` is present on all resources
- [ ] 7.7 Create second Claw instance in same namespace with different name
- [ ] 7.8 Verify both instances coexist with separate resources
- [ ] 7.9 Verify `kubectl get all -l claw.sandbox.redhat.com/instance=test-instance` returns only first instance resources
- [ ] 7.10 Test backward compatibility: create Claw instance named "instance" and verify resources match old naming
