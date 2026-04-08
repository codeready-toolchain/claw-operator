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
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClaw Secret Reference Tests", func() {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret-ref"
		apiKeySecretKey = "api-key"
	)

	Context("When reconciling OpenClaw with Secret references", func() {
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
		})

		It("should set SecretNotFound status when referenced Secret does not exist", func() {
			By("Creating OpenClaw without creating the referenced Secret")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: apiKeySecret},
				Key:                  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Reconciling the OpenClaw instance")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).To(HaveOccurred())

			By("Checking status condition shows SecretNotFound")
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
				if err != nil {
					return ""
				}
				condition := meta.FindStatusCondition(instance.Status.Conditions, "Available")
				if condition == nil {
					return ""
				}
				return condition.Reason
			}, timeout, interval).Should(Equal("SecretNotFound"))
		})

		It("should set SecretKeyNotFound status when Secret exists but key is missing", func() {
			By("Creating Secret without the expected key")
			// Create Secret directly (don't use helper which deletes existing Secrets)
			wrongSecret := &corev1.Secret{}
			wrongSecret.Name = apiKeySecret
			wrongSecret.Namespace = namespace
			wrongSecret.Type = corev1.SecretTypeOpaque
			wrongSecret.Data = map[string][]byte{
				"wrong-key": []byte(apiKey),
			}
			// Delete any existing Secret first
			existing := &corev1.Secret{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: apiKeySecret, Namespace: namespace}, existing)
			_ = k8sClient.Delete(ctx, existing)
			Expect(k8sClient.Create(ctx, wrongSecret)).Should(Succeed())

			By("Creating OpenClaw with reference to missing key")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: apiKeySecret},
				Key:                  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Reconciling the OpenClaw instance")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).To(HaveOccurred())

			By("Checking status condition shows SecretKeyNotFound")
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
				if err != nil {
					return ""
				}
				condition := meta.FindStatusCondition(instance.Status.Conditions, "Available")
				if condition == nil {
					return ""
				}
				return condition.Reason
			}, timeout, interval).Should(Equal("SecretKeyNotFound"))
		})

		It("should configure proxy deployment to reference the user's Secret", func() {
			By("Creating the referenced Secret")
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating OpenClaw instance")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: apiKeySecret},
				Key:                  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Reconciling the OpenClaw instance")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying proxy deployment references the user's Secret")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				// Find the proxy container and check GEMINI_API_KEY env var
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == OpenClawProxyDeploymentContainerName {
						for _, env := range container.Env {
							if env.Name == OpenClawProxyDeploymentGeminiAPiKeyEnvKey && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
								return env.ValueFrom.SecretKeyRef.Name == apiKeySecret &&
									env.ValueFrom.SecretKeyRef.Key == apiKeySecretKey
							}
						}
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should maintain Secret reference when user updates Secret value", func() {
			By("Creating the referenced Secret")
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, "initial-key")
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating OpenClaw instance")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: apiKeySecret},
				Key:                  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Initial reconciliation")
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying proxy deployment references the Secret")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == OpenClawProxyDeploymentContainerName {
						for _, env := range container.Env {
							if env.Name == OpenClawProxyDeploymentGeminiAPiKeyEnvKey && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
								return env.ValueFrom.SecretKeyRef.Name == apiKeySecret
							}
						}
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Updating the referenced Secret value")
			err = k8sClient.Get(ctx, client.ObjectKey{Name: apiKeySecret, Namespace: namespace}, secret)
			Expect(err).NotTo(HaveOccurred())
			secret.Data[apiKeySecretKey] = []byte("updated-key")
			Expect(k8sClient.Update(ctx, secret)).Should(Succeed())

			By("Verifying deployment still references the same Secret (Kubernetes auto-propagates the new value)")
			// The deployment env var should still point to the same Secret reference
			// Kubernetes will automatically propagate the updated value to the pod
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      "openclaw-proxy",
				Namespace: namespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			found := false
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == OpenClawProxyDeploymentContainerName {
					for _, env := range container.Env {
						if env.Name == OpenClawProxyDeploymentGeminiAPiKeyEnvKey && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
							Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal(apiKeySecret))
							Expect(env.ValueFrom.SecretKeyRef.Key).To(Equal(apiKeySecretKey))
							found = true
							break
						}
					}
				}
			}
			Expect(found).To(BeTrue(), "GEMINI_API_KEY env var should be configured in proxy deployment")
		})
	})
})
