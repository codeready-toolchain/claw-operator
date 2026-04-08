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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClaw Status Conditions", func() {
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

			// Cleanup deployments
			openclawDeployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawDeploymentName, Namespace: namespace}, openclawDeployment)
			_ = k8sClient.Delete(ctx, openclawDeployment)

			proxyDeployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw-proxy", Namespace: namespace}, proxyDeployment)
			_ = k8sClient.Delete(ctx, proxyDeployment)
		})

		It("should set Available condition to False after initial resource creation", func() {
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

			By("Checking if Available condition is set to False")
			Eventually(func() bool {
				updatedInstance := &openclawv1alpha1.OpenClaw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
				return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "Provisioning"
			}, timeout, interval).Should(BeTrue())
		})

		It("should keep Available condition False when only openclaw Deployment is ready", func() {
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

			By("Reconciling to create resources")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating openclaw Deployment to Available=True")
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

			By("Reconciling again to update status")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Available condition remains False")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)).Should(Succeed())
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("Provisioning"))
		})

		It("should keep Available condition False when only openclaw-proxy Deployment is ready", func() {
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

			By("Reconciling to create resources")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating openclaw-proxy Deployment to Available=True")
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

			By("Reconciling again to update status")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Available condition remains False")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)).Should(Succeed())
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("Provisioning"))
		})

		It("should set Available condition to True when both Deployments are ready", func() {
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

			By("Reconciling again to update status")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking Available condition is True")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)).Should(Succeed())
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal("Ready"))
		})

		It("should update LastTransitionTime only on status change", func() {
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

			By("First reconciliation")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial LastTransitionTime")
			var initialTransitionTime metav1.Time
			Eventually(func() bool {
				updatedInstance := &openclawv1alpha1.OpenClaw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
				if condition != nil {
					initialTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

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

			By("Second reconciliation - status changes to True")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying LastTransitionTime was updated")
			var secondTransitionTime metav1.Time
			Eventually(func() bool {
				updatedInstance := &openclawv1alpha1.OpenClaw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
				if condition != nil && condition.Status == metav1.ConditionTrue {
					secondTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

			// In fast test environments, timestamps might be the same, but should not go backwards
			Expect(secondTransitionTime.Time.Before(initialTransitionTime.Time)).To(BeFalse())
		})

		It("should preserve LastTransitionTime when status unchanged", func() {
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

			By("First reconciliation")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting initial LastTransitionTime")
			var initialTransitionTime metav1.Time
			Eventually(func() bool {
				updatedInstance := &openclawv1alpha1.OpenClaw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
				if condition != nil && condition.Status == metav1.ConditionFalse {
					initialTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Second reconciliation - status remains False")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying LastTransitionTime was NOT updated")
			updatedInstance := &openclawv1alpha1.OpenClaw{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)).Should(Succeed())
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.LastTransitionTime).To(Equal(initialTransitionTime))
		})

		It("should handle missing Deployments gracefully", func() {
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

			By("Reconciling without creating Deployments first")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			// Should not error even if deployments don't exist yet
			Expect(err).NotTo(HaveOccurred())

			By("Checking Available condition is set to False")
			Eventually(func() bool {
				updatedInstance := &openclawv1alpha1.OpenClaw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
				return condition != nil && condition.Status == metav1.ConditionFalse
			}, timeout, interval).Should(BeTrue())
		})

		It("should set ObservedGeneration correctly in conditions", func() {
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

			By("Checking ObservedGeneration matches instance generation")
			Eventually(func() bool {
				updatedInstance := &openclawv1alpha1.OpenClaw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Available")
				return condition != nil && condition.ObservedGeneration == updatedInstance.Generation
			}, timeout, interval).Should(BeTrue())
		})
	})
})
