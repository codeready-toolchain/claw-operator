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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClawGatewaySecret Controller", func() {
	const (
		namespace       = "default"
		testAPIKey      = "test-api-key-12345"
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

			// Cleanup gateway secret
			secret := &corev1.Secret{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawGatewaySecretName, Namespace: namespace}, secret)
			_ = k8sClient.Delete(ctx, secret)
		})

		It("should create gateway Secret when OpenClaw instance is reconciled", func() {
			By("Creating a new OpenClaw named 'instance' with APIKey")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())

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

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if gateway Secret was created")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Secret has OPENCLAW_GATEWAY_TOKEN data entry")
			Expect(secret.Data).To(HaveKey(GatewayTokenKeyName))
		})

		It("should create token with exactly 64 hex characters", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())

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

			By("Reconciling to create Secret")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying token format and length")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				token, exists := secret.Data[GatewayTokenKeyName]
				if !exists {
					return false
				}
				// Token should be exactly 64 hex characters
				hexPattern := regexp.MustCompile("^[0-9a-f]{64}$")
				return hexPattern.Match(token)
			}, timeout, interval).Should(BeTrue())
		})

		It("should not regenerate token when secret already exists", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())

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

			By("Reconciling to create Secret with initial token")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting the initial token value")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, timeout, interval).Should(BeTrue())
			initialToken := string(secret.Data[GatewayTokenKeyName])

			By("Reconciling again")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying token was not regenerated")
			secret = &corev1.Secret{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return ""
				}
				return string(secret.Data[GatewayTokenKeyName])
			}, timeout, interval).Should(Equal(initialToken))
		})

		It("should generate unique tokens for different reconciliations when secret is deleted", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())

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

			By("Reconciling to create Secret with first token")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting the first token value")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, timeout, interval).Should(BeTrue())
			firstToken := string(secret.Data[GatewayTokenKeyName])

			By("Deleting the Secret")
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())

			By("Reconciling again to generate a new token")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying a new unique token was generated")
			newSecret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, newSecret)
				if err != nil {
					return false
				}
				secondToken := string(newSecret.Data[GatewayTokenKeyName])
				// Tokens should be different
				return len(secondToken) > 0 && secondToken != firstToken
			}, timeout, interval).Should(BeTrue())
		})

		It("should set correct owner reference on gateway Secret during initial creation", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())
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

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking gateway Secret has correct owner reference")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == OpenClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, timeout, interval).Should(BeTrue())
		})

		It("should set correct owner reference on gateway Secret when it already existed", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())
			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())
			// Create gateway secret
			gatewaySecret := createTestGatewaySecret(OpenClawGatewaySecretName, namespace)
			Expect(k8sClient.Create(ctx, gatewaySecret)).Should(Succeed())
			Expect(gatewaySecret.OwnerReferences).To(BeEmpty())
			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking gateway Secret has correct owner reference")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == OpenClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, timeout, interval).Should(BeTrue())
		})

		It("should have owner reference that enables garbage collection", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, testAPIKey)
			Expect(k8sClient.Create(ctx, apiSecret)).Should(Succeed())

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

			By("Reconciling to create gateway Secret")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying gateway Secret has owner reference for garbage collection")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				// Verify owner reference has BlockOwnerDeletion set
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == OpenClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, timeout, interval).Should(BeTrue())
		})
	})
})
