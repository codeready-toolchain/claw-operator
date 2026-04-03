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

var _ = Describe("OpenClawPersistentVolumeClaim Controller", func() {
	const (
		namespace = "default"
		apiKey    = "test-api-key"
	)

	Context("When reconciling an OpenClaw named 'instance'", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)

			// Cleanup PVC
			pvc := &corev1.PersistentVolumeClaim{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawPVCName, Namespace: namespace}, pvc)
			_ = k8sClient.Delete(ctx, pvc)
		})

		It("should create PVC for OpenClaw named 'instance'", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = apiKey
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

			By("Checking if PVC was created")
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawPVCName,
					Namespace: namespace,
				}, pvc)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})

		It("should set correct owner reference on PVC", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = apiKey
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

			By("Checking PVC has correct owner reference")
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawPVCName,
					Namespace: namespace,
				}, pvc)
				if err != nil {
					return false
				}
				if len(pvc.OwnerReferences) == 0 {
					return false
				}
				ownerRef := pvc.OwnerReferences[0]
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

		BeforeEach(func() {
			// Cleanup any instance named "instance" from previous tests
			instance := &openclawv1alpha1.OpenClaw{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawInstanceName, Namespace: namespace}, instance)
			if err == nil {
				_ = k8sClient.Delete(ctx, instance)
			}

			// Force delete PVC by removing finalizers
			pvc := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawPVCName, Namespace: namespace}, pvc)
			if err == nil {
				pvc.Finalizers = []string{}
				_ = k8sClient.Update(ctx, pvc)
				_ = k8sClient.Delete(ctx, pvc)

				// Wait for PVC to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawPVCName, Namespace: namespace}, pvc)
					return err != nil
				}, timeout, interval).Should(BeTrue())
			}
		})

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)
		})

		It("should skip PVC creation for non-matching names", func() {
			By("Creating a new OpenClaw with name 'other-instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.APIKey = apiKey
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

			By("Verifying PVC was NOT created")
			pvc := &corev1.PersistentVolumeClaim{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawPVCName,
					Namespace: namespace,
				}, pvc)
				return err != nil
			}, time.Second*2, interval).Should(BeTrue())
		})
	})
})
