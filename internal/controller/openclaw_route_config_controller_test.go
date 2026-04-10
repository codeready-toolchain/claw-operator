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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawRouteConfiguration(t *testing.T) {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret"
		apiKeySecretKey = "api-key"
	)

	t.Run("ConfigMap injection logic", func(t *testing.T) {
		t.Run("should replace OPENCLAW_ROUTE_HOST placeholder with Route host", func(t *testing.T) {
			// Create a mock ConfigMap object with placeholder
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(OpenClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example-openclaw.apps.cluster.com"

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Inject Route host
			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			require.NoError(t, err, "injectRouteHostIntoConfigMap failed")

			// Verify replacement
			openclawJSON, found, err := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			require.NoError(t, err, "failed to get openclaw.json")
			assert.True(t, found, "openclaw.json not found in ConfigMap data")
			assert.Contains(t, openclawJSON, routeHost)
			assert.NotContains(t, openclawJSON, "OPENCLAW_ROUTE_HOST")
		})

		t.Run("should replace all occurrences of OPENCLAW_ROUTE_HOST placeholder", func(t *testing.T) {
			// Create a mock ConfigMap object with multiple placeholders
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(OpenClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST","OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example.com"

			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			require.NoError(t, err, "injectRouteHostIntoConfigMap failed")

			openclawJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			// Count occurrences of Route host
			hostCount := strings.Count(openclawJSON, routeHost)
			assert.Equal(t, 2, hostCount, "expected 2 occurrences of routeHost")
			// Ensure no placeholder remains
			assert.NotContains(t, openclawJSON, "OPENCLAW_ROUTE_HOST")
		})

		t.Run("should use localhost fallback when routeHost is empty", func(t *testing.T) {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(OpenClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "" // Empty = vanilla Kubernetes

			reconciler := &OpenClawResourceReconciler{
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
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		t.Run("should create ConfigMap with localhost fallback when Route CRD not available", func(t *testing.T) {
			// Cleanup before test starts (in case previous test didn't clean up)
			instance := &openclawv1alpha1.OpenClaw{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance); err == nil {
				_ = k8sClient.Delete(ctx, instance)
				// Wait for deletion to complete
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
					return err != nil
				}, "OpenClaw should be deleted")
			}

			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Create a new OpenClaw instance
			instance = &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace

			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconcile the created resource (Route CRD not available in envtest)
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Check if ConfigMap contains localhost fallback
			configMap := &corev1.ConfigMap{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawConfigMapName,
					Namespace: namespace,
				}, configMap)
				if err != nil {
					return false
				}
				openclawJSON, ok := configMap.Data["openclaw.json"]
				if !ok {
					return false
				}
				// Should contain localhost fallback since Route CRD is not registered
				return strings.Contains(openclawJSON, "http://localhost:18789")
			}, "ConfigMap should contain localhost fallback")
		})
	})

	t.Run("Proxy deployment configuration", func(t *testing.T) {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		t.Run("should still configure proxy deployment and stamp secret version", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Create a new OpenClaw instance
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace

			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconcile the created resource
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Verify buildKustomizedObjects still calls configureProxyDeployment and stampSecretVersionAnnotation
			objects, err := reconciler.buildKustomizedObjects(ctx, instance)
			require.NoError(t, err, "buildKustomizedObjects failed")

			// Find proxy deployment
			var proxyDeployment *unstructured.Unstructured
			for _, obj := range objects {
				if obj.GetKind() == DeploymentKind && obj.GetName() == OpenClawProxyDeploymentName {
					proxyDeployment = obj
					break
				}
			}
			require.NotNil(t, proxyDeployment, "proxy deployment not found in kustomized objects")

			// Verify Secret reference is configured
			containers, found, err := unstructured.NestedSlice(proxyDeployment.Object, "spec", "template", "spec", "containers")
			require.NoError(t, err, "failed to get containers")
			assert.True(t, found, "containers not found in proxy deployment")
			assert.NotEmpty(t, containers, "expected at least one container in proxy deployment")

			// Verify Secret version annotation is stamped
			annotations, found, err := unstructured.NestedStringMap(proxyDeployment.Object, "spec", "template", "metadata", "annotations")
			require.NoError(t, err, "failed to get annotations")
			assert.True(t, found, "annotations not found in proxy deployment pod template")
			assert.Contains(t, annotations, "openclaw.sandbox.redhat.com/gemini-secret-version")
		})
	})
}
