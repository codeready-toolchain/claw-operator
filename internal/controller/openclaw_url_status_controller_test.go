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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClaw URL Status Field", func() {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret"
		apiKeySecretKey = "api-key"
	)

	Context("When reconciling an OpenClaw named 'instance'", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)

			// Cleanup API key Secret
			apiSecret := &corev1.Secret{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: apiKeySecret, Namespace: namespace}, apiSecret)
			_ = k8sClient.Delete(ctx, apiSecret)

			// Cleanup deployments
			openclawDeployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawDeploymentName, Namespace: namespace}, openclawDeployment)
			_ = k8sClient.Delete(ctx, openclawDeployment)

			proxyDeployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw-proxy", Namespace: namespace}, proxyDeployment)
			_ = k8sClient.Delete(ctx, proxyDeployment)
		})

		It("should populate URL field when both deployments are ready and Route exists", func() {
			Skip("Route CRD not available in envtest - requires e2e test with OpenShift cluster")
		})

		It("should leave URL field empty when deployments are not ready", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Reconciling to create resources")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking URL field is empty when deployments not ready")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)).Should(Succeed())
			Expect(updatedInstance.Status.URL).To(BeEmpty())
		})

		It("should leave URL field empty when Route does not exist", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Reconciling to create resources")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating both Deployments to Available=True")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawDeploymentName, Namespace: namespace}, deployment)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, deployment)).Should(Succeed())

			proxyDeployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw-proxy", Namespace: namespace}, proxyDeployment)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, proxyDeployment)).Should(Succeed())

			By("Reconciling again - Route does not exist")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking URL field is empty when Route not found")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)).Should(Succeed())
			Expect(updatedInstance.Status.URL).To(BeEmpty())
		})

		It("should include https:// scheme in URL format", func() {
			Skip("Route CRD not available in envtest - requires e2e test with OpenShift cluster")
		})
	})

	Context("When testing gateway token retrieval", func() {
		ctx := context.Background()

		BeforeEach(func() {
			// Ensure cleanup of any existing gateway secret before each test
			gatewaySecret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace}, gatewaySecret); err == nil {
				_ = k8sClient.Delete(ctx, gatewaySecret)
				// Wait for deletion to complete
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace}, gatewaySecret)
					return err != nil
				}, timeout, interval).Should(BeTrue())
			}
		})

		AfterEach(func() {
			// Cleanup gateway secret
			gatewaySecret := &corev1.Secret{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace}, gatewaySecret)
			_ = k8sClient.Delete(ctx, gatewaySecret)
		})

		It("should retrieve and decode gateway token from openclaw-secrets", func() {
			By("Creating gateway secret with token")
			gatewaySecret := &corev1.Secret{}
			gatewaySecret.Name = OpenClawGatewaySecretName
			gatewaySecret.Namespace = namespace
			testToken := "test-gateway-token-123456"
			gatewaySecret.Data = map[string][]byte{
				GatewayTokenKeyName: []byte(testToken),
			}
			Expect(k8sClient.Create(ctx, gatewaySecret)).Should(Succeed())

			By("Calling getGatewayToken method")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			token := reconciler.getGatewayToken(ctx, namespace)

			By("Verifying token is correctly retrieved")
			Expect(token).To(Equal(testToken))
		})

		It("should return empty string when gateway secret does not exist", func() {
			By("Calling getGatewayToken without creating secret")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			token := reconciler.getGatewayToken(ctx, namespace)

			By("Verifying empty string is returned")
			Expect(token).To(BeEmpty())
		})

		It("should return empty string when token key is missing from secret", func() {
			By("Creating gateway secret without token key")
			gatewaySecret := &corev1.Secret{}
			gatewaySecret.Name = OpenClawGatewaySecretName
			gatewaySecret.Namespace = namespace
			gatewaySecret.Data = map[string][]byte{
				"other-key": []byte("other-value"),
			}
			Expect(k8sClient.Create(ctx, gatewaySecret)).Should(Succeed())

			By("Calling getGatewayToken method")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			token := reconciler.getGatewayToken(ctx, namespace)

			By("Verifying empty string is returned")
			Expect(token).To(BeEmpty())
		})

		It("should return empty string when token value is empty", func() {
			By("Creating gateway secret with empty token")
			gatewaySecret := &corev1.Secret{}
			gatewaySecret.Name = OpenClawGatewaySecretName
			gatewaySecret.Namespace = namespace
			gatewaySecret.Data = map[string][]byte{
				GatewayTokenKeyName: []byte(""),
			}
			Expect(k8sClient.Create(ctx, gatewaySecret)).Should(Succeed())

			By("Calling getGatewayToken method")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			token := reconciler.getGatewayToken(ctx, namespace)

			By("Verifying empty string is returned")
			Expect(token).To(BeEmpty())
		})
	})

	Context("When testing URL construction with token fragment", func() {
		type urlTestCase struct {
			name     string
			routeURL string
			token    string
			expected string
		}

		DescribeTable("URL construction scenarios",
			func(tc urlTestCase) {
				result := buildOpenClawURL(tc.routeURL, tc.token)
				Expect(result).To(Equal(tc.expected))
			},
			Entry("should append token fragment when both route and token are provided",
				urlTestCase{
					name:     "with route and token",
					routeURL: "https://openclaw-route.apps.example.com",
					token:    "abc123def456",
					expected: "https://openclaw-route.apps.example.com#token=abc123def456",
				}),
			Entry("should return route URL without fragment when token is empty",
				urlTestCase{
					name:     "with route but no token",
					routeURL: "https://openclaw-route.apps.example.com",
					token:    "",
					expected: "https://openclaw-route.apps.example.com",
				}),
			Entry("should return empty string when route URL is empty",
				urlTestCase{
					name:     "no route URL",
					routeURL: "",
					token:    "abc123def456",
					expected: "",
				}),
			Entry("should return empty string when both route and token are empty",
				urlTestCase{
					name:     "no route or token",
					routeURL: "",
					token:    "",
					expected: "",
				}),
			Entry("should percent-encode special characters in token",
				urlTestCase{
					name:     "token with special characters",
					routeURL: "https://openclaw-route.apps.example.com",
					token:    "token+with=special&chars#fragment",
					expected: "https://openclaw-route.apps.example.com#token=token%2Bwith%3Dspecial%26chars%23fragment",
				}),
		)

		It("should follow format https://<route-host>#token=<gateway-token>", func() {
			By("Constructing URL with typical OpenShift route and hex token")
			routeURL := "https://openclaw-default.apps.cluster.example.com"
			token := "64chartoken1234567890abcdef64chartoken1234567890abcdef123456"

			result := buildOpenClawURL(routeURL, token)

			Expect(result).To(Equal("https://openclaw-default.apps.cluster.example.com#token=64chartoken1234567890abcdef64chartoken1234567890abcdef123456"))
			Expect(result).To(HavePrefix("https://"))
			Expect(result).To(ContainSubstring("#token="))
		})
	})
})
