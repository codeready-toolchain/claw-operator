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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClawConfigMap Controller", func() {
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
			secret := &corev1.Secret{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: apiKeySecret, Namespace: namespace}, secret)
			_ = k8sClient.Delete(ctx, secret)

			// Cleanup configmap
			configMap := &corev1.ConfigMap{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawConfigMapName, Namespace: namespace}, configMap)
			_ = k8sClient.Delete(ctx, configMap)
		})

		It("should create ConfigMap for OpenClaw named 'instance'", func() {
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

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawConfigMapName,
					Namespace: namespace,
				}, configMap)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})

		It("should set correct owner reference on ConfigMap", func() {
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

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking ConfigMap has correct owner reference")
			configMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawConfigMapName,
					Namespace: namespace,
				}, configMap)
				if err != nil {
					return false
				}
				if len(configMap.OwnerReferences) == 0 {
					return false
				}
				ownerRef := configMap.OwnerReferences[0]
				return ownerRef.Kind == OpenClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When reconciling an OpenClaw with different name", func() {
		const resourceName = "other-instance"
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)
		})

		It("should skip ConfigMap creation for non-matching names", func() {
			By("Creating a new OpenClaw with name 'other-instance'")
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

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap was NOT created")
			configMap := &corev1.ConfigMap{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawConfigMapName,
					Namespace: namespace,
				}, configMap)
				return err != nil
			}, time.Second*2, interval).Should(BeTrue())
		})
	})
})
