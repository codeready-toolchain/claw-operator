/*
Copyright 2026 Red Hat.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- ConfigMap tests ---

func TestClawConfigMapController(t *testing.T) {

	t.Run("When reconciling an Claw named 'instance'", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should create ConfigMap for Claw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap)
				return err == nil
			}, "ConfigMap should be created")
		})

		t.Run("should set correct owner reference on ConfigMap", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap)
				if err != nil {
					return false
				}
				if len(configMap.OwnerReferences) == 0 {
					return false
				}
				ownerRef := configMap.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "ConfigMap should have correct owner reference")
		})

		t.Run("should have operator.json with gateway config and providers", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap) == nil
			}, "ConfigMap should be created")

			operatorJSON, ok := configMap.Data["operator.json"]
			require.True(t, ok, "operator.json key must exist")

			var config map[string]any
			require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

			_, hasGateway := config["gateway"]
			assert.True(t, hasGateway, "operator.json should contain gateway section")

			_, hasModels := config["models"]
			assert.True(t, hasModels, "operator.json should contain models section")

			_, hasAgents := config["agents"]
			assert.False(t, hasAgents, "operator.json must not contain agents section (user-owned)")
		})

		t.Run("should have openclaw.json seed with $include directive", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap) == nil
			}, "ConfigMap should be created")

			openclawJSON, ok := configMap.Data["openclaw.json"]
			require.True(t, ok, "openclaw.json key must exist")

			var config map[string]any
			require.NoError(t, json.Unmarshal([]byte(openclawJSON), &config))

			include, hasInclude := config["$include"]
			require.True(t, hasInclude, "openclaw.json must contain $include directive")
			assert.Equal(t, "./operator.json", include, "$include should reference operator.json")

			gateway, hasGateway := config["gateway"].(map[string]any)
			require.True(t, hasGateway, "openclaw.json seed must contain gateway section for config safety check")
			assert.Equal(t, "local", gateway["mode"], "gateway.mode must be present to survive OpenClaw write-back")

			agents, hasAgents := config["agents"].(map[string]any)
			require.True(t, hasAgents, "openclaw.json seed should contain agents section")

			defaults, hasDefaults := agents["defaults"].(map[string]any)
			require.True(t, hasDefaults, "agents should have defaults")

			model, hasModel := defaults["model"].(map[string]any)
			require.True(t, hasModel, "defaults should have model config")
			assert.NotEmpty(t, model["primary"], "should have a primary model set")
		})

		t.Run("should have AGENTS.md seed content", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap) == nil
			}, "ConfigMap should be created")

			agentsMd, ok := configMap.Data["AGENTS.md"]
			assert.True(t, ok, "AGENTS.md key must exist")
			assert.Contains(t, agentsMd, "OpenClaw Assistant")
		})

		t.Run("should have PROXY_SETUP.md skill content", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap) == nil
			}, "ConfigMap should be created")

			proxyMd, ok := configMap.Data["PROXY_SETUP.md"]
			assert.True(t, ok, "PROXY_SETUP.md key must exist")
			assert.Contains(t, proxyMd, "Proxy Architecture")
			assert.Contains(t, proxyMd, "type: none")
			assert.Contains(t, proxyMd, ".whatsapp.com")
			assert.Contains(t, proxyMd, ".whatsapp.net")
		})

		t.Run("should not have KUBERNETES.md when no kubernetes credentials", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap) == nil
			}, "ConfigMap should be created")

			_, hasKubeMd := configMap.Data["KUBERNETES.md"]
			assert.False(t, hasKubeMd, "KUBERNETES.md should not exist without kubernetes credentials")
		})
	})

	t.Run("When reconciling a Claw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should create ConfigMap for the named instance", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace, resourceName)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(resourceName),
					Namespace: namespace,
				}, configMap) == nil
			}, "ConfigMap should be created for the named instance")
		})
	})
}

// --- PVC tests ---

func TestOpenClawPersistentVolumeClaimController(t *testing.T) {

	t.Run("When reconciling an Claw named 'instance'", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should create PVC for Claw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getPVCName(testInstanceName),
					Namespace: namespace,
				}, pvc)
				return err == nil
			}, "PVC should be created")
		})

		t.Run("should set correct owner reference on PVC", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getPVCName(testInstanceName),
					Namespace: namespace,
				}, pvc)
				if err != nil {
					return false
				}
				if len(pvc.OwnerReferences) == 0 {
					return false
				}
				ownerRef := pvc.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "PVC should have correct owner reference")
		})
	})

	t.Run("When reconciling a Claw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should create PVC for the named instance", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace, resourceName)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getPVCName(resourceName),
					Namespace: namespace,
				}, pvc) == nil
			}, "PVC should be created for the named instance")
		})
	})
}

// --- Deployment tests ---

func TestOpenClawDeploymentController(t *testing.T) {

	t.Run("When reconciling an Claw named 'instance'", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should create Deployment for Claw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getClawDeploymentName(testInstanceName),
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")
		})

		t.Run("should set correct owner reference on Deployment", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getClawDeploymentName(testInstanceName),
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")

			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getClawDeploymentName(testInstanceName),
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				if len(deployment.OwnerReferences) == 0 {
					return false
				}
				ownerRef := deployment.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "Deployment should have correct owner reference")
		})

		t.Run("should create ingress NetworkPolicy with correct owner reference", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			np := &netv1.NetworkPolicy{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getIngressNetworkPolicyName(testInstanceName),
					Namespace: namespace,
				}, np)
				return err == nil
			}, "Ingress NetworkPolicy should be created")

			require.NotEmpty(t, np.OwnerReferences, "NetworkPolicy should have owner references")
			ownerRef := np.OwnerReferences[0]
			require.Equal(t, ClawResourceKind, ownerRef.Kind)
			require.Equal(t, resourceName, ownerRef.Name)
			require.NotNil(t, ownerRef.Controller)
			require.True(t, *ownerRef.Controller)
		})
	})

	t.Run("When reconciling a Claw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should create Deployment for the named instance", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace, resourceName)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{
					Name:      getClawDeploymentName(resourceName),
					Namespace: namespace,
				}, deployment) == nil
			}, "Deployment should be created for the named instance")
		})
	})
}

// --- Gateway Secret tests ---

func TestOpenClawGatewaySecretController(t *testing.T) {

	t.Run("When reconciling an Claw named 'instance'", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should create gateway Secret when Claw instance is reconciled", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				return err == nil
			}, "gateway Secret should be created")

			assert.Contains(t, secret.Data, GatewayTokenKeyName)
		})

		t.Run("should create token with exactly 64 hex characters", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				token, exists := secret.Data[GatewayTokenKeyName]
				if !exists {
					return false
				}
				hexPattern := regexp.MustCompile("^[0-9a-f]{64}$")
				return hexPattern.Match(token)
			}, "token should be exactly 64 hex characters")
		})

		t.Run("should not regenerate token when secret already exists", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, "initial token should be created")
			initialToken := string(secret.Data[GatewayTokenKeyName])

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			secret = &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				currentToken := string(secret.Data[GatewayTokenKeyName])
				return currentToken == initialToken
			}, "token should not be regenerated")
		})

		t.Run("should generate unique tokens for different reconciliations when secret is deleted", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, "first token should be created")
			firstToken := string(secret.Data[GatewayTokenKeyName])

			require.NoError(t, k8sClient.Delete(ctx, secret), "failed to delete Secret")

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			newSecret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, newSecret)
				if err != nil {
					return false
				}
				secondToken := string(newSecret.Data[GatewayTokenKeyName])
				return len(secondToken) > 0 && secondToken != firstToken
			}, "new unique token should be generated")
		})

		t.Run("should set correct owner reference on gateway Secret during initial creation", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "gateway Secret should have correct owner reference")
		})

		t.Run("should set correct owner reference on gateway Secret when it already existed", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			apiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, apiSecret), "failed to create Secret")

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.Credentials = testCredentials()
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw")

			gatewaySecret := createTestGatewaySecret(t, getGatewaySecretName(resourceName), namespace)
			require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway Secret")
			assert.Empty(t, gatewaySecret.OwnerReferences, "gateway Secret should not have owner references initially")

			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "gateway Secret should have correct owner reference after reconciliation")
		})

		t.Run("should have owner reference that enables garbage collection", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getGatewaySecretName(testInstanceName),
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "gateway Secret should have owner reference for garbage collection")
		})
	})
}

// --- Route configuration tests ---

func TestOpenClawRouteConfiguration(t *testing.T) {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret"
		apiKeySecretKey = "api-key"
	)

	t.Run("ConfigMap injection logic", func(t *testing.T) {
		t.Run("should replace OPENCLAW_ROUTE_HOST placeholder with Route host", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(getConfigMapName(testInstanceName))
			configMap.Object["data"] = map[string]any{
				"operator.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example-claw.apps.cluster.com"

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostsIntoConfigMap(testInstanceName, objects, routeHost, "")
			require.NoError(t, err, "injectRouteHostsIntoConfigMap failed")

			operatorJSON, found, err := unstructured.NestedString(configMap.Object, "data", "operator.json")
			require.NoError(t, err, "failed to get operator.json")
			assert.True(t, found, "operator.json not found in ConfigMap data")

			// Parse JSON and verify allowedOrigins contains the route
			var config map[string]any
			err = json.Unmarshal([]byte(operatorJSON), &config)
			require.NoError(t, err, "failed to parse operator.json")

			allowedOrigins, found, err := unstructured.NestedStringSlice(config, "gateway", "controlUi", "allowedOrigins")
			require.NoError(t, err, "failed to get allowedOrigins")
			assert.True(t, found, "allowedOrigins not found")
			assert.Contains(t, allowedOrigins, routeHost)
		})

		t.Run("should set allowedOrigins array with single route", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(getConfigMapName(testInstanceName))
			configMap.Object["data"] = map[string]any{
				"operator.json": `{"gateway":{"controlUi":{"allowedOrigins":[]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example.com"

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostsIntoConfigMap(testInstanceName, objects, routeHost, "")
			require.NoError(t, err, "injectRouteHostsIntoConfigMap failed")

			operatorJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "operator.json")
			var config map[string]any
			err = json.Unmarshal([]byte(operatorJSON), &config)
			require.NoError(t, err, "failed to parse operator.json")

			allowedOrigins, found, err := unstructured.NestedStringSlice(config, "gateway", "controlUi", "allowedOrigins")
			require.NoError(t, err, "failed to get allowedOrigins")
			assert.True(t, found, "allowedOrigins not found")
			assert.Equal(t, []string{routeHost}, allowedOrigins)
		})

		t.Run("should use localhost fallback when no routes provided", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(getConfigMapName(testInstanceName))
			configMap.Object["data"] = map[string]any{
				"operator.json": `{"gateway":{"controlUi":{"allowedOrigins":[]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostsIntoConfigMap(testInstanceName, objects, "", "")
			require.NoError(t, err, "injectRouteHostsIntoConfigMap failed")

			operatorJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "operator.json")
			var config map[string]any
			err = json.Unmarshal([]byte(operatorJSON), &config)
			require.NoError(t, err, "failed to parse operator.json")

			allowedOrigins, found, err := unstructured.NestedStringSlice(config, "gateway", "controlUi", "allowedOrigins")
			require.NoError(t, err, "failed to get allowedOrigins")
			assert.True(t, found, "allowedOrigins not found")
			assert.Equal(t, []string{"http://localhost:18789"}, allowedOrigins)
		})

		t.Run("should include both gateway and console route hosts in allowedOrigins", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(getConfigMapName(testInstanceName))
			configMap.Object["data"] = map[string]any{
				"operator.json": `{"gateway":{"controlUi":{"allowedOrigins":[]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://claw-gateway.apps.cluster.com"
			consoleRouteHost := "https://claw-console.apps.cluster.com"

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostsIntoConfigMap(testInstanceName, objects, routeHost, consoleRouteHost)
			require.NoError(t, err, "injectRouteHostsIntoConfigMap failed")

			operatorJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "operator.json")
			var config map[string]any
			err = json.Unmarshal([]byte(operatorJSON), &config)
			require.NoError(t, err, "failed to parse operator.json")

			allowedOrigins, found, err := unstructured.NestedStringSlice(config, "gateway", "controlUi", "allowedOrigins")
			require.NoError(t, err, "failed to get allowedOrigins")
			assert.True(t, found, "allowedOrigins not found")
			assert.ElementsMatch(t, []string{routeHost, consoleRouteHost}, allowedOrigins)
		})

		t.Run("should include only gateway route when console route is empty", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(getConfigMapName(testInstanceName))
			configMap.Object["data"] = map[string]any{
				"operator.json": `{"gateway":{"controlUi":{"allowedOrigins":[]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://claw-gateway.apps.cluster.com"
			consoleRouteHost := "" // Console route not available

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostsIntoConfigMap(testInstanceName, objects, routeHost, consoleRouteHost)
			require.NoError(t, err, "injectRouteHostsIntoConfigMap failed")

			operatorJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "operator.json")
			var config map[string]any
			err = json.Unmarshal([]byte(operatorJSON), &config)
			require.NoError(t, err, "failed to parse operator.json")

			allowedOrigins, found, err := unstructured.NestedStringSlice(config, "gateway", "controlUi", "allowedOrigins")
			require.NoError(t, err, "failed to get allowedOrigins")
			assert.True(t, found, "allowedOrigins not found")
			assert.Equal(t, []string{routeHost}, allowedOrigins, "should only include gateway route")
		})

		t.Run("should use localhost fallback when all routes are empty (vanilla Kubernetes)", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(getConfigMapName(testInstanceName))
			configMap.Object["data"] = map[string]any{
				"operator.json": `{"gateway":{"controlUi":{"allowedOrigins":[]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := ""
			consoleRouteHost := ""

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostsIntoConfigMap(testInstanceName, objects, routeHost, consoleRouteHost)
			require.NoError(t, err, "injectRouteHostsIntoConfigMap failed")

			operatorJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "operator.json")
			var config map[string]any
			err = json.Unmarshal([]byte(operatorJSON), &config)
			require.NoError(t, err, "failed to parse operator.json")

			allowedOrigins, found, err := unstructured.NestedStringSlice(config, "gateway", "controlUi", "allowedOrigins")
			require.NoError(t, err, "failed to get allowedOrigins")
			assert.True(t, found, "allowedOrigins not found")
			assert.Equal(t, []string{"http://localhost:18789"}, allowedOrigins)
		})
	})

	t.Run("When reconciling with Route CRD not registered", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should create ConfigMap with localhost fallback when Route CRD not available", func(t *testing.T) {
			instance := &clawv1alpha1.Claw{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance); err == nil {
				_ = k8sClient.Delete(ctx, instance)
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
					return err != nil
				}, "Claw should be deleted")
			}

			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance = &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace

			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
				{
					Name:     "gemini",
					Type:     clawv1alpha1.CredentialTypeAPIKey,
					Provider: "google",
					SecretRef: &clawv1alpha1.SecretRef{
						Name: apiKeySecret,
						Key:  apiKeySecretKey,
					},
					Domain: ".googleapis.com",
					APIKey: &clawv1alpha1.APIKeyConfig{
						Header: "x-goog-api-key",
					},
				},
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw")

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getConfigMapName(testInstanceName),
					Namespace: namespace,
				}, configMap)
				if err != nil {
					return false
				}
				operatorJSON, ok := configMap.Data["operator.json"]
				if !ok {
					return false
				}
				return strings.Contains(operatorJSON, "http://localhost:18789")
			}, "ConfigMap should contain localhost fallback in operator.json")
		})
	})

	t.Run("Proxy deployment configuration", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should build kustomized objects with proxy deployment", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)

			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			objects, err := reconciler.buildKustomizedObjects(instance)
			require.NoError(t, err, "buildKustomizedObjects failed")

			var proxyDeployment *unstructured.Unstructured
			for _, obj := range objects {
				if obj.GetKind() == DeploymentKind && obj.GetName() == getProxyDeploymentName(resourceName) {
					proxyDeployment = obj
					break
				}
			}
			require.NotNil(t, proxyDeployment, "proxy deployment not found in kustomized objects")

			containers, found, err := unstructured.NestedSlice(proxyDeployment.Object, "spec", "template", "spec", "containers")
			require.NoError(t, err, "failed to get containers")
			assert.True(t, found, "containers not found in proxy deployment")
			assert.NotEmpty(t, containers, "expected at least one container in proxy deployment")
		})
	})

	t.Run("Init container config seeding", func(t *testing.T) {
		t.Run("should always copy operator.json and conditionally seed user files", func(t *testing.T) {
			reconciler := createClawReconciler()
			instance := &clawv1alpha1.Claw{}
			instance.Name = testInstanceName
			instance.Namespace = namespace
			objects, err := reconciler.buildKustomizedObjects(instance)
			require.NoError(t, err)

			var deployment *unstructured.Unstructured
			for _, obj := range objects {
				if obj.GetKind() == DeploymentKind && obj.GetName() == getClawDeploymentName(testInstanceName) {
					deployment = obj
					break
				}
			}
			require.NotNil(t, deployment)

			initContainers, _, err := unstructured.NestedSlice(
				deployment.Object, "spec", "template", "spec", "initContainers",
			)
			require.NoError(t, err)

			var initConfigScript string
			for _, ic := range initContainers {
				container := ic.(map[string]any)
				if container["name"] == "init-config" {
					cmds := container["command"].([]any)
					initConfigScript = cmds[len(cmds)-1].(string)
					break
				}
			}
			require.NotEmpty(t, initConfigScript, "init-config container should exist")

			assert.Contains(t, initConfigScript, "cp /config/operator.json /home/node/.openclaw/operator.json",
				"operator.json should always be copied unconditionally")
			assert.Contains(t, initConfigScript, "[ -f /home/node/.openclaw/openclaw.json ] || cp",
				"openclaw.json should only be seeded if missing")
			assert.Contains(t, initConfigScript, "[ -f /home/node/.openclaw/workspace/AGENTS.md ] || cp",
				"AGENTS.md should only be seeded if missing")
			assert.Contains(t, initConfigScript, "cp /config/PROXY_SETUP.md /home/node/.openclaw/workspace/skills/proxy/SKILL.md",
				"PROXY_SETUP.md should always be copied to proxy skill directory")
			assert.Contains(t, initConfigScript, "if [ -f /config/KUBERNETES.md ]",
				"KUBERNETES.md should be copied only when present in ConfigMap")
		})
	})
}
