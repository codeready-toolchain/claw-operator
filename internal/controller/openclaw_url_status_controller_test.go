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

			instance.Spec.GeminiAPIKey = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: apiKeySecret},
				Key:                  apiKeySecretKey,
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

			instance.Spec.GeminiAPIKey = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: apiKeySecret},
				Key:                  apiKeySecretKey,
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
})
