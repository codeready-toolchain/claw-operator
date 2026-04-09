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

		It("should configure proxy deployment to reference the user's Secret", func() {
			By("Creating the referenced Secret")
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating OpenClaw instance")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
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

		It("should stamp Secret ResourceVersion annotation on pod template", func() {
			By("Creating the referenced Secret")
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating OpenClaw instance")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
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

			By("Verifying pod template has Secret version annotation")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				annotations := deployment.Spec.Template.Annotations
				if annotations == nil {
					return false
				}
				version, exists := annotations["openclaw.sandbox.redhat.com/gemini-secret-version"]
				return exists && version == secret.ResourceVersion
			}, timeout, interval).Should(BeTrue())

			By("Updating the Secret data")
			err = k8sClient.Get(ctx, client.ObjectKey{Name: apiKeySecret, Namespace: namespace}, secret)
			Expect(err).NotTo(HaveOccurred())
			originalVersion := secret.ResourceVersion
			secret.Data[apiKeySecretKey] = []byte("updated-api-key")
			Expect(k8sClient.Update(ctx, secret)).Should(Succeed())

			By("Fetching updated Secret to get new ResourceVersion")
			err = k8sClient.Get(ctx, client.ObjectKey{Name: apiKeySecret, Namespace: namespace}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.ResourceVersion).NotTo(Equal(originalVersion), "Secret ResourceVersion should change")

			By("Reconciling again after Secret update")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod template annotation updated with new Secret version")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				annotations := deployment.Spec.Template.Annotations
				if annotations == nil {
					return false
				}
				version, exists := annotations["openclaw.sandbox.redhat.com/gemini-secret-version"]
				return exists && version == secret.ResourceVersion && version != originalVersion
			}, timeout, interval).Should(BeTrue())
		})
	})

	It("should fail to configure proxy deployment if the Secret does not exist", func() {
		By("Creating OpenClaw instance")
		instance := &openclawv1alpha1.OpenClaw{}
		instance.Name = OpenClawInstanceName
		instance.Namespace = namespace
		instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
			Name: apiKeySecret,
			Key:  apiKeySecretKey,
		}
		Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

		By("Reconciling the OpenClaw instance")
		reconciler := &OpenClawResourceReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      OpenClawInstanceName,
				Namespace: namespace,
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to stamp Secret version annotation: failed to get Secret test-gemini-secret-ref for version stamping"))
	})
})
