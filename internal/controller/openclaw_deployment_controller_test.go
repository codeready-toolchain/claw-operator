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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClawDeployment Controller", func() {
	const (
		namespace = "default"
	)

	Context("When reconciling without ConfigMap", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)
		})

		It("should NOT create Deployment when ConfigMap doesn't exist", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawDeploymentReconciler{
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

			By("Verifying Deployment was NOT created")
			deployment := &appsv1.Deployment{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err != nil
			}, time.Second*2, interval).Should(BeTrue())
		})
	})

	Context("When reconciling with ConfigMap", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClaw{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)

			// Cleanup configmap
			configMap := &corev1.ConfigMap{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawConfigMapName, Namespace: namespace}, configMap)
			_ = k8sClient.Delete(ctx, configMap)

			// Cleanup deployment
			deployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw", Namespace: namespace}, deployment)
			_ = k8sClient.Delete(ctx, deployment)
		})

		It("should create Deployment when ConfigMap exists", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Creating the ConfigMap")
			configMap := &corev1.ConfigMap{}
			configMap.Name = OpenClawConfigMapName
			configMap.Namespace = namespace
			configMap.Data = map[string]string{"test": "data"}
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawDeploymentReconciler{
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

			By("Checking if Deployment was created")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})

		It("should set correct owner reference on Deployment", func() {
			By("Creating a new OpenClaw named 'instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Creating the ConfigMap")
			configMap := &corev1.ConfigMap{}
			configMap.Name = OpenClawConfigMapName
			configMap.Namespace = namespace
			configMap.Data = map[string]string{"test": "data"}
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawDeploymentReconciler{
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

			By("Checking Deployment has correct owner reference")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				if len(deployment.OwnerReferences) == 0 {
					return false
				}
				ownerRef := deployment.OwnerReferences[0]
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

			// Cleanup configmap if it was created
			configMap := &corev1.ConfigMap{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawConfigMapName, Namespace: namespace}, configMap)
			_ = k8sClient.Delete(ctx, configMap)
		})

		It("should skip Deployment creation for non-matching names", func() {
			By("Creating a new OpenClaw with name 'other-instance'")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Creating the ConfigMap")
			configMap := &corev1.ConfigMap{}
			configMap.Name = OpenClawConfigMapName
			configMap.Namespace = namespace
			configMap.Data = map[string]string{"test": "data"}
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawDeploymentReconciler{
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

			By("Verifying Deployment was NOT created")
			deployment := &appsv1.Deployment{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err != nil
			}, time.Second*2, interval).Should(BeTrue())
		})
	})
})
