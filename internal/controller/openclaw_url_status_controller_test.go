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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawURLStatusField(t *testing.T) {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret"
		apiKeySecretKey = "api-key"
	)

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		t.Run("should populate URL field when both deployments are ready and Route exists", func(t *testing.T) {
			t.Skip("Route CRD not available in envtest - requires e2e test with OpenShift cluster")
		})

		t.Run("should leave URL field empty when deployments are not ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
				deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: apiKeySecret, Namespace: namespace})
				deleteAndWait(ctx, &appsv1.Deployment{}, client.ObjectKey{Name: OpenClawDeploymentName, Namespace: namespace})
				deleteAndWait(ctx, &appsv1.Deployment{}, client.ObjectKey{Name: OpenClawProxyDeploymentName, Namespace: namespace})
			})

			t.Log("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			t.Log("Reconciling to create resources")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			t.Log("Checking URL field is empty when deployments not ready")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated OpenClaw instance")
			assert.Empty(t, updatedInstance.Status.URL, "expected empty URL")
		})

		t.Run("should leave URL field empty when Route does not exist", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
				deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: apiKeySecret, Namespace: namespace})
				deleteAndWait(ctx, &appsv1.Deployment{}, client.ObjectKey{Name: OpenClawDeploymentName, Namespace: namespace})
				deleteAndWait(ctx, &appsv1.Deployment{}, client.ObjectKey{Name: OpenClawProxyDeploymentName, Namespace: namespace})
			})

			t.Log("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			t.Log("Reconciling to create resources")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			t.Log("Updating both Deployments to Available=True")
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawDeploymentName, Namespace: namespace}, deployment)
				return err == nil
			}, "openclaw deployment to be created")

			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update openclaw deployment status")

			proxyDeployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
				return err == nil
			}, "openclaw-proxy deployment to be created")

			proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update openclaw-proxy deployment status")

			t.Log("Reconciling again - Route does not exist")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			if err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}

			t.Log("Checking URL field is empty when Route not found")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated OpenClaw instance")
			assert.Empty(t, updatedInstance.Status.URL, "expected empty URL")
		})

		t.Run("should include https:// scheme in URL format", func(t *testing.T) {
			t.Skip("Route CRD not available in envtest - requires e2e test with OpenShift cluster")
		})
	})
}

func TestGatewayTokenRetrieval(t *testing.T) {
	const namespace = "default"
	ctx := context.Background()

	setupGatewaySecretTest := func(t *testing.T) {
		t.Helper()
		// Ensure cleanup of any existing gateway secret before each test
		gatewaySecret := &corev1.Secret{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace}, gatewaySecret); err == nil {
			_ = k8sClient.Delete(ctx, gatewaySecret)
			// Wait for deletion to complete
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace}, gatewaySecret)
				return err != nil
			}, "gateway secret to be deleted")
		}
	}

	t.Run("should retrieve and decode gateway token from openclaw-secrets", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace})
		})

		t.Log("Creating gateway secret with token")
		gatewaySecret := &corev1.Secret{}
		gatewaySecret.Name = OpenClawGatewaySecretName
		gatewaySecret.Namespace = namespace
		testToken := "test-gateway-token-123456"
		gatewaySecret.Data = map[string][]byte{
			GatewayTokenKeyName: []byte(testToken),
		}
		require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway secret")

		t.Log("Calling getGatewayToken method")
		reconciler := &OpenClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		t.Log("Verifying token is correctly retrieved")
		assert.Equal(t, testToken, token, "expected token to match")
	})

	t.Run("should return empty string when gateway secret does not exist", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace})
		})

		t.Log("Calling getGatewayToken without creating secret")
		reconciler := &OpenClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		t.Log("Verifying empty string is returned")
		assert.Empty(t, token, "expected empty string")
	})

	t.Run("should return empty string when token key is missing from secret", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace})
		})

		t.Log("Creating gateway secret without token key")
		gatewaySecret := &corev1.Secret{}
		gatewaySecret.Name = OpenClawGatewaySecretName
		gatewaySecret.Namespace = namespace
		gatewaySecret.Data = map[string][]byte{
			"other-key": []byte("other-value"),
		}
		require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway secret")

		t.Log("Calling getGatewayToken method")
		reconciler := &OpenClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		t.Log("Verifying empty string is returned")
		assert.Empty(t, token, "expected empty string")
	})

	t.Run("should return empty string when token value is empty", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace})
		})

		t.Log("Creating gateway secret with empty token")
		gatewaySecret := &corev1.Secret{}
		gatewaySecret.Name = OpenClawGatewaySecretName
		gatewaySecret.Namespace = namespace
		gatewaySecret.Data = map[string][]byte{
			GatewayTokenKeyName: []byte(""),
		}
		require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway secret")

		t.Log("Calling getGatewayToken method")
		reconciler := &OpenClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		t.Log("Verifying empty string is returned")
		assert.Empty(t, token, "expected empty string")
	})
}

func TestURLConstructionWithTokenFragment(t *testing.T) {
	t.Run("URL construction scenarios", func(t *testing.T) {
		tests := []struct {
			name     string
			routeURL string
			token    string
			expected string
		}{
			{
				name:     "should append token fragment when both route and token are provided",
				routeURL: "https://openclaw-route.apps.example.com",
				token:    "abc123def456",
				expected: "https://openclaw-route.apps.example.com#token=abc123def456",
			},
			{
				name:     "should return route URL without fragment when token is empty",
				routeURL: "https://openclaw-route.apps.example.com",
				token:    "",
				expected: "https://openclaw-route.apps.example.com",
			},
			{
				name:     "should return empty string when route URL is empty",
				routeURL: "",
				token:    "abc123def456",
				expected: "",
			},
			{
				name:     "should return empty string when both route and token are empty",
				routeURL: "",
				token:    "",
				expected: "",
			},
			{
				name:     "should percent-encode special characters in token",
				routeURL: "https://openclaw-route.apps.example.com",
				token:    "token+with=special&chars#fragment",
				expected: "https://openclaw-route.apps.example.com#token=token%2Bwith%3Dspecial%26chars%23fragment",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := buildOpenClawURL(tt.routeURL, tt.token)
				assert.Equal(t, tt.expected, result, "URL construction result")
			})
		}
	})

	t.Run("should follow format https://<route-host>#token=<gateway-token>", func(t *testing.T) {
		t.Log("Constructing URL with typical OpenShift route and hex token")
		routeURL := "https://openclaw-default.apps.cluster.example.com"
		token := "64chartoken1234567890abcdef64chartoken1234567890abcdef123456"

		result := buildOpenClawURL(routeURL, token)

		expected := "https://openclaw-default.apps.cluster.example.com#token=64chartoken1234567890abcdef64chartoken1234567890abcdef123456"
		assert.Equal(t, expected, result, "URL construction result")
		assert.True(t, strings.HasPrefix(result, "https://"), "expected result to start with https://")
		assert.Contains(t, result, "#token=")
	})
}
