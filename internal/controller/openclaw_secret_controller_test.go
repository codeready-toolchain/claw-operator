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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClawSecret Controller", func() {
	const (
		namespace  = "default"
		testAPIKey = "test-api-key-12345"
	)

	Context("When reconciling an OpenClaw named 'instance'", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)

			// Cleanup secret
			secret := &corev1.Secret{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawProxySecretName, Namespace: namespace}, secret)
			_ = k8sClient.Delete(ctx, secret)
		})

		It("should create Secret when it doesn't exist", func() {
			By("Creating a new OpenClaw named 'instance' with APIKey")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = testAPIKey
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

			By("Checking if Secret was created")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Secret has correct GEMINI_API_KEY data")
			Expect(secret.Data).To(HaveKey(GeminiAPIKeyName))
			Expect(string(secret.Data[GeminiAPIKeyName])).To(Equal(testAPIKey))
		})

		It("should update Secret when it already exists", func() {
			By("Creating a pre-existing Secret with old data")
			oldSecret := &corev1.Secret{}
			oldSecret.Name = OpenClawProxySecretName
			oldSecret.Namespace = namespace
			oldSecret.Data = map[string][]byte{
				GeminiAPIKeyName: []byte("old-api-key"),
			}
			Expect(k8sClient.Create(ctx, oldSecret)).Should(Succeed())

			By("Creating a new OpenClaw named 'instance' with new APIKey")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = testAPIKey
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

			By("Checking if Secret was updated with new APIKey")
			secret := &corev1.Secret{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return ""
				}
				return string(secret.Data[GeminiAPIKeyName])
			}, timeout, interval).Should(Equal(testAPIKey))
		})

		It("should update Secret when APIKey changes in CR", func() {
			By("Creating a new OpenClaw named 'instance' with initial APIKey")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = "initial-key"
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Reconciling with initial APIKey")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Secret has initial APIKey")
			secret := &corev1.Secret{}
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return ""
				}
				return string(secret.Data[GeminiAPIKeyName])
			}, timeout, interval).Should(Equal("initial-key"))

			By("Updating OpenClaw CR with new APIKey")
			err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			Expect(err).NotTo(HaveOccurred())
			instance.Spec.APIKey = "updated-key"
			Expect(k8sClient.Update(ctx, instance)).Should(Succeed())

			By("Reconciling with updated APIKey")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Secret has updated APIKey")
			Eventually(func() string {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return ""
				}
				return string(secret.Data[GeminiAPIKeyName])
			}, timeout, interval).Should(Equal("updated-key"))
		})

		It("should recreate Secret if deleted manually", func() {
			By("Creating a new OpenClaw named 'instance' with APIKey")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = testAPIKey
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

			By("Verifying Secret exists")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Manually deleting the Secret")
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())

			By("Reconciling again to recreate Secret")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Secret is recreated")
			newSecret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
					Namespace: namespace,
				}, newSecret)
				return err == nil
			}, timeout, interval).Should(BeTrue())
			Expect(string(newSecret.Data[GeminiAPIKeyName])).To(Equal(testAPIKey))
		})

		It("should set correct owner reference on Secret", func() {
			By("Creating a new OpenClaw named 'instance' with APIKey")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = testAPIKey
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

			By("Checking Secret has correct owner reference")
			secret := &corev1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawProxySecretName,
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
	})
})
