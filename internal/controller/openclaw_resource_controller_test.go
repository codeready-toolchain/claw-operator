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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

// --- ConfigMap tests ---

func TestOpenClawConfigMapController(t *testing.T) {

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create ConfigMap for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawConfigMapName,
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
					Name:      ClawConfigMapName,
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
	})

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip ConfigMap creation for non-matching names", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
				if err := deleteAndWait(&openclawv1alpha1.Claw{}, client.ObjectKey{Name: resourceName, Namespace: namespace}); err != nil {
					t.Fatalf("cleanup failed: %v", err)
				}
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			time.Sleep(2 * time.Second)

			configMap := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawConfigMapName,
				Namespace: namespace,
			}, configMap)
			require.Error(t, err, "ConfigMap should not have been created for non-instance OpenClaw")
		})
	})
}

// --- PVC tests ---

func TestOpenClawPersistentVolumeClaimController(t *testing.T) {

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create PVC for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawPVCName,
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
					Name:      ClawPVCName,
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

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip PVC creation for non-matching names", func(t *testing.T) {
			instance := &openclawv1alpha1.Claw{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawInstanceName, Namespace: namespace}, instance)
			if err == nil {
				_ = k8sClient.Delete(ctx, instance)
			}

			pvc := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}, pvc)
			if err == nil {
				pvc.Finalizers = []string{}
				_ = k8sClient.Update(ctx, pvc)
				_ = k8sClient.Delete(ctx, pvc)

				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}, pvc)
					return err != nil
				}, "PVC should be deleted before test")
			}

			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
				if err := deleteAndWait(&openclawv1alpha1.Claw{}, client.ObjectKey{Name: resourceName, Namespace: namespace}); err != nil {
					t.Fatalf("cleanup failed: %v", err)
				}
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			pvc = &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}, pvc)
				return apierrors.IsNotFound(err)
			}, "PVC should not have been created for non-instance OpenClaw")
		})
	})
}

// --- Deployment tests ---

func TestOpenClawDeploymentController(t *testing.T) {

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create Deployment for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
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
					Name:      ClawDeploymentName,
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")

			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawDeploymentName,
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
					Name:      ClawIngressNetworkPolicyName,
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

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip Deployment creation for non-matching names", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
				if err := deleteAndWait(&openclawv1alpha1.Claw{}, client.ObjectKey{Name: resourceName, Namespace: namespace}); err != nil {
					t.Fatalf("cleanup failed: %v", err)
				}
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			time.Sleep(2 * time.Second)

			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawDeploymentName,
				Namespace: namespace,
			}, deployment)
			require.Error(t, err, "Deployment should not have been created for non-instance OpenClaw")
		})
	})
}

// --- Gateway Secret tests ---

func TestOpenClawGatewaySecretController(t *testing.T) {

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create gateway Secret when OpenClaw instance is reconciled", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
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
					Name:      ClawGatewaySecretName,
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
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, "initial token should be created")
			initialToken := string(secret.Data[GatewayTokenKeyName])

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			secret = &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
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
					Name:      ClawGatewaySecretName,
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
					Name:      ClawGatewaySecretName,
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
					Name:      ClawGatewaySecretName,
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

			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.Credentials = testCredentials()
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			gatewaySecret := createTestGatewaySecret(t, ClawGatewaySecretName, namespace)
			require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway Secret")
			assert.Empty(t, gatewaySecret.OwnerReferences, "gateway Secret should not have owner references initially")

			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
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
					Name:      ClawGatewaySecretName,
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
			configMap.SetName(ClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example-openclaw.apps.cluster.com"

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			require.NoError(t, err, "injectRouteHostIntoConfigMap failed")

			openclawJSON, found, err := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			require.NoError(t, err, "failed to get openclaw.json")
			assert.True(t, found, "openclaw.json not found in ConfigMap data")
			assert.Contains(t, openclawJSON, routeHost)
			assert.NotContains(t, openclawJSON, "OPENCLAW_ROUTE_HOST")
		})

		t.Run("should replace all occurrences of OPENCLAW_ROUTE_HOST placeholder", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(ClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST","OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example.com"

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			require.NoError(t, err, "injectRouteHostIntoConfigMap failed")

			openclawJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			hostCount := strings.Count(openclawJSON, routeHost)
			assert.Equal(t, 2, hostCount, "expected 2 occurrences of routeHost")
			assert.NotContains(t, openclawJSON, "OPENCLAW_ROUTE_HOST")
		})

		t.Run("should use localhost fallback when routeHost is empty", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(ClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := ""

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			require.NoError(t, err, "injectRouteHostIntoConfigMap failed")

			openclawJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			assert.Contains(t, openclawJSON, "http://localhost:18789")
			assert.NotContains(t, openclawJSON, "OPENCLAW_ROUTE_HOST")
		})
	})

	t.Run("When reconciling with Route CRD not registered", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create ConfigMap with localhost fallback when Route CRD not available", func(t *testing.T) {
			instance := &openclawv1alpha1.Claw{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance); err == nil {
				_ = k8sClient.Delete(ctx, instance)
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
					return err != nil
				}, "OpenClaw should be deleted")
			}

			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance = &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace

			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
				{
					Name: "gemini",
					Type: openclawv1alpha1.CredentialTypeAPIKey,
					SecretRef: &openclawv1alpha1.SecretRef{
						Name: apiKeySecret,
						Key:  apiKeySecretKey,
					},
					Domain: ".googleapis.com",
					APIKey: &openclawv1alpha1.APIKeyConfig{
						Header: "x-goog-api-key",
					},
				},
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

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
					Name:      ClawConfigMapName,
					Namespace: namespace,
				}, configMap)
				if err != nil {
					return false
				}
				openclawJSON, ok := configMap.Data["openclaw.json"]
				if !ok {
					return false
				}
				return strings.Contains(openclawJSON, "http://localhost:18789")
			}, "ConfigMap should contain localhost fallback")
		})
	})

	t.Run("Proxy deployment configuration", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should build kustomized objects with proxy deployment", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)

			reconciler := createClawReconciler()

			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			objects, err := reconciler.buildKustomizedObjects()
			require.NoError(t, err, "buildKustomizedObjects failed")

			var proxyDeployment *unstructured.Unstructured
			for _, obj := range objects {
				if obj.GetKind() == DeploymentKind && obj.GetName() == ClawProxyDeploymentName {
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
}
