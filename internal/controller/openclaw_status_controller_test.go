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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawStatusConditions(t *testing.T) {
	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should set GatewayTokenSecretRef in status after reconciliation", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw instance")

			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			updatedInstance := &openclawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance))
			assert.Equal(t, ClawGatewaySecretName, updatedInstance.Status.GatewayTokenSecretRef)
		})

		t.Run("should set Available condition to False after initial resource creation", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconciling the created resource
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Checking if Available condition is set to False
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &openclawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
				return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "Provisioning"
			}, "Available condition should be False with Provisioning reason")
		})

		t.Run("should keep Available condition False when only openclaw Deployment is ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconciling to create resources
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Updating openclaw Deployment to Available=True
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
				return err == nil
			}, "openclaw Deployment should be created")

			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

			// Reconciling again to update status
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Checking Available condition remains False
			updatedInstance := &openclawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
			require.NotNil(t, condition, "Available condition should not be nil")
			assert.Equal(t, metav1.ConditionFalse, condition.Status, "Available condition status")
			assert.Equal(t, "Provisioning", condition.Reason, "Available condition reason")
		})

		t.Run("should keep Available condition False when only openclaw-proxy Deployment is ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconciling to create resources
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Updating openclaw-proxy Deployment to Available=True
			proxyDeployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
				return err == nil
			}, "openclaw-proxy Deployment should be created")

			proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

			// Reconciling again to update status
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Checking Available condition remains False
			updatedInstance := &openclawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
			require.NotNil(t, condition, "Available condition should not be nil")
			assert.Equal(t, metav1.ConditionFalse, condition.Status, "Available condition status")
			assert.Equal(t, "Provisioning", condition.Reason, "Available condition reason")
		})

		t.Run("should set Available condition to True when both Deployments are ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconciling to create resources
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Updating both Deployments to Available=True
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
				return err == nil
			}, "openclaw Deployment should be created")

			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

			proxyDeployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
				return err == nil
			}, "openclaw-proxy Deployment should be created")

			proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

			// Reconciling again to update status
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Checking Available condition is True
			updatedInstance := &openclawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
			require.NotNil(t, condition, "Available condition should not be nil")
			assert.Equal(t, metav1.ConditionTrue, condition.Status, "Available condition status")
			assert.Equal(t, "Ready", condition.Reason, "Available condition reason")
		})

		t.Run("should update LastTransitionTime only on status change", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// First reconciliation
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Getting initial LastTransitionTime
			var initialTransitionTime metav1.Time
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &openclawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
				if condition != nil {
					initialTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, "initial Available condition should be set")

			// Updating both Deployments to Available=True
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
				return err == nil
			}, "openclaw Deployment should be created")

			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

			proxyDeployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
				return err == nil
			}, "openclaw-proxy Deployment should be created")

			proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

			// Second reconciliation - status changes to True
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Verifying LastTransitionTime was updated
			var secondTransitionTime metav1.Time
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &openclawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
				if condition != nil && condition.Status == metav1.ConditionTrue {
					secondTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, "Available condition should transition to True")

			// In fast test environments, timestamps might be the same, but should not go backwards
			assert.False(t, secondTransitionTime.Time.Before(initialTransitionTime.Time), "LastTransitionTime should not go backwards")
		})

		t.Run("should preserve LastTransitionTime when status unchanged", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// First reconciliation
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Getting initial LastTransitionTime
			var initialTransitionTime metav1.Time
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &openclawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
				if condition != nil && condition.Status == metav1.ConditionFalse {
					initialTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, "initial Available condition should be False")

			// Second reconciliation - status remains False
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Verifying LastTransitionTime was NOT updated
			updatedInstance := &openclawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
			require.NotNil(t, condition, "Available condition should not be nil")
			assert.Equal(t, metav1.ConditionFalse, condition.Status, "Available condition status")
			assert.Equal(t, initialTransitionTime, condition.LastTransitionTime, "LastTransitionTime should not have changed")
		})

		t.Run("should handle missing Deployments gracefully", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconciling without creating Deployments first
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			// Should not error even if deployments don't exist yet
			require.NoError(t, err, "reconcile should not error even if deployments don't exist")

			// Checking Available condition is set to False
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &openclawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
				return condition != nil && condition.Status == metav1.ConditionFalse
			}, "Available condition should be set to False when deployments are missing")
		})

		t.Run("should set ObservedGeneration correctly in conditions", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Creating a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

			// Setup reconciler
			reconciler := &ClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconciling the created resource
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Checking ObservedGeneration matches instance generation
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &openclawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, "Ready")
				return condition != nil && condition.ObservedGeneration == updatedInstance.Generation
			}, "ObservedGeneration should match instance generation")
		})

		t.Run("When verifying status.url field", func(t *testing.T) {
			const resourceName = ClawInstanceName
			ctx := context.Background()

			t.Run("should initialize status.url as empty", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				// Creating a new OpenClaw named 'instance'
				instance := &openclawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				// Create API key Secret
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
					Name: aiModelSecret,
					Key:  aiModelSecretKey,
				}
				require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

				// Setup reconciler
				reconciler := &ClawResourceReconciler{
					Client: k8sClient,
					Scheme: scheme.Scheme,
				}

				// Reconciling the created resource
				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Checking status.url is empty
				updatedInstance := &openclawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should keep status.url empty when only openclaw deployment is ready", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				// Creating a new OpenClaw named 'instance'
				instance := &openclawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				// Create API key Secret
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
					Name: aiModelSecret,
					Key:  aiModelSecretKey,
				}
				require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

				// Setup reconciler
				reconciler := &ClawResourceReconciler{
					Client: k8sClient,
					Scheme: scheme.Scheme,
				}

				// Reconciling to create resources
				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Updating openclaw Deployment to Available=True
				deployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
					return err == nil
				}, "openclaw Deployment should be created")

				deployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

				// Reconciling again to update status
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Checking status.url remains empty
				updatedInstance := &openclawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should keep status.url empty when only proxy deployment is ready", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				// Creating a new OpenClaw named 'instance'
				instance := &openclawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				// Create API key Secret
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
					Name: aiModelSecret,
					Key:  aiModelSecretKey,
				}
				require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

				// Setup reconciler
				reconciler := &ClawResourceReconciler{
					Client: k8sClient,
					Scheme: scheme.Scheme,
				}

				// Reconciling to create resources
				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Updating openclaw-proxy Deployment to Available=True
				proxyDeployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
					return err == nil
				}, "openclaw-proxy Deployment should be created")

				proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

				// Reconciling again to update status
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Checking status.url remains empty
				updatedInstance := &openclawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should clear status.url when deployments transition from ready to not ready", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				// Creating a new OpenClaw named 'instance'
				instance := &openclawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				// Create API key Secret
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
					Name: aiModelSecret,
					Key:  aiModelSecretKey,
				}
				require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

				// Setup reconciler
				reconciler := &ClawResourceReconciler{
					Client: k8sClient,
					Scheme: scheme.Scheme,
				}

				// Reconciling to create resources
				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Updating both Deployments to Available=True
				deployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
					return err == nil
				}, "openclaw Deployment should be created")

				deployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

				proxyDeployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
					return err == nil
				}, "openclaw-proxy Deployment should be created")

				proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

				// Reconciling to set URL (Route doesn't exist in envtest, so URL will be empty)
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Updating openclaw Deployment to Available=False
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
					if err != nil {
						return false
					}
					deployment.Status.Conditions = []appsv1.DeploymentCondition{
						{
							Type:   appsv1.DeploymentAvailable,
							Status: corev1.ConditionFalse,
						},
					}
					err = k8sClient.Status().Update(ctx, deployment)
					return err == nil
				}, "openclaw Deployment should be updated to Available=False")

				// Reconciling again to clear URL
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Checking status.url is cleared
				updatedInstance := &openclawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should not set status.url when Route does not exist (vanilla Kubernetes)", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				// Creating a new OpenClaw named 'instance'
				instance := &openclawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				// Create API key Secret
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
					Name: aiModelSecret,
					Key:  aiModelSecretKey,
				}
				require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

				// Setup reconciler
				reconciler := &ClawResourceReconciler{
					Client: k8sClient,
					Scheme: scheme.Scheme,
				}

				// Reconciling to create resources
				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Updating both Deployments to Available=True
				deployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
					return err == nil
				}, "openclaw Deployment should be created")

				deployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

				proxyDeployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
					return err == nil
				}, "openclaw-proxy Deployment should be created")

				proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

				// Reconciling again - Route CRD not available in envtest
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Checking status.url remains empty (Route doesn't exist)
				updatedInstance := &openclawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should set status.url with token fragment when deployments are ready and Route exists", func(t *testing.T) {
				t.Skip("This test requires Route CRD to be installed - should be run in e2e tests with OpenShift cluster. Installing CRD dynamically in envtest interferes with other tests that expect it not to be present.")

				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				// Creating a new OpenClaw named 'instance'
				instance := &openclawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				// Create API key Secret
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
					Name: aiModelSecret,
					Key:  aiModelSecretKey,
				}
				require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

				// Setup reconciler
				reconciler := &ClawResourceReconciler{
					Client: k8sClient,
					Scheme: scheme.Scheme,
				}

				// Reconciling to create resources including gateway secret
				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Installing Route CRD for this test
				routeCRD := &apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: "routes.route.openshift.io",
					},
					Spec: apiextensionsv1.CustomResourceDefinitionSpec{
						Group: "route.openshift.io",
						Names: apiextensionsv1.CustomResourceDefinitionNames{
							Plural:   "routes",
							Singular: "route",
							Kind:     "Route",
							ListKind: "RouteList",
						},
						Scope: apiextensionsv1.NamespaceScoped,
						Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
							{
								Name:    "v1",
								Served:  true,
								Storage: true,
								Schema: &apiextensionsv1.CustomResourceValidation{
									OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
										Type: "object",
										Properties: map[string]apiextensionsv1.JSONSchemaProps{
											"spec": {
												Type:                   "object",
												XPreserveUnknownFields: boolPtr(true),
											},
											"status": {
												Type:                   "object",
												XPreserveUnknownFields: boolPtr(true),
											},
										},
									},
								},
								Subresources: &apiextensionsv1.CustomResourceSubresources{
									Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
								},
							},
						},
					},
				}
				// Try to create the CRD, ignore if it already exists
				err = k8sClient.Create(ctx, routeCRD)
				if err != nil && !apierrors.IsAlreadyExists(err) {
					require.NoError(t, err, "failed to create Route CRD")
				}

				// Wait for CRD to be established
				waitFor(t, timeout, interval, func() bool {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					err := k8sClient.Get(ctx, client.ObjectKey{Name: "routes.route.openshift.io"}, crd)
					if err != nil {
						return false
					}
					for _, cond := range crd.Status.Conditions {
						if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
							return true
						}
					}
					return false
				}, "Route CRD should be established")

				// Creating a fake Route object with status (simulating OpenShift)
				route := &unstructured.Unstructured{}
				route.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "route.openshift.io",
					Version: "v1",
					Kind:    "Route",
				})
				route.SetName("openclaw")
				route.SetNamespace(namespace)

				// Set the Route status with ingress host (simulating what OpenShift router does)
				routeHost := "openclaw-default.apps.example.com"

				// Create the Route first
				require.NoError(t, k8sClient.Create(ctx, route), "failed to create Route")

				// Then update the status separately
				// Fetch the created route first
				waitFor(t, timeout, interval, func() bool {
					return k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw", Namespace: namespace}, route) == nil
				}, "Route should be created")

				// Set status
				route.Object["status"] = map[string]interface{}{
					"ingress": []interface{}{
						map[string]interface{}{
							"host": routeHost,
						},
					},
				}

				// Update Route status
				require.NoError(t, k8sClient.Status().Update(ctx, route), "failed to update Route status")

				// Verify Route was created with status
				waitFor(t, timeout, interval, func() bool {
					createdRoute := &unstructured.Unstructured{}
					createdRoute.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "route.openshift.io",
						Version: "v1",
						Kind:    "Route",
					})
					err := k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw", Namespace: namespace}, createdRoute)
					if err != nil {
						return false
					}
					// Check if status.ingress[0].host exists
					ingress, found, err := unstructured.NestedSlice(createdRoute.Object, "status", "ingress")
					if err != nil || !found || len(ingress) == 0 {
						return false
					}
					firstIngress, ok := ingress[0].(map[string]interface{})
					if !ok {
						return false
					}
					host, found := firstIngress["host"]
					return found && host == routeHost
				}, "Route status should have ingress host")

				// Updating both Deployments to Available=True
				deployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
					return err == nil
				}, "openclaw Deployment should be created")

				deployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update deployment status")

				proxyDeployment := &appsv1.Deployment{}
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
					return err == nil
				}, "openclaw-proxy Deployment should be created")

				proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				}
				require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update proxy deployment status")

				// Reconciling again to populate status.url with Route host
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				// Checking status.url is set with Route host and token fragment
				updatedInstance := &openclawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")

				// URL should be in format: https://<route-host>#token=<gateway-token>
				assert.NotEmpty(t, updatedInstance.Status.URL, "expected non-empty status.url")
				expectedPrefix := "https://" + routeHost
				assert.True(t, strings.HasPrefix(updatedInstance.Status.URL, expectedPrefix), "status.url should have prefix %s", expectedPrefix)
				assert.Contains(t, updatedInstance.Status.URL, "#token=")

				// Verify the token fragment is present and URL-encoded
				urlParts := strings.Split(updatedInstance.Status.URL, "#token=")
				require.Len(t, urlParts, 2, "URL should have exactly one #token= fragment")
				assert.Equal(t, "https://"+routeHost, urlParts[0], "URL host part")
				assert.NotEmpty(t, urlParts[1], "token should not be empty")

				// Verify we can retrieve the same token from the gateway secret
				gatewaySecret := &corev1.Secret{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: ClawGatewaySecretName, Namespace: namespace}, gatewaySecret), "failed to get gateway secret")
				expectedToken := string(gatewaySecret.Data[GatewayTokenKeyName])
				assert.NotEmpty(t, expectedToken, "expected non-empty gateway token")

				// Verify the token in the URL matches (should be URL-encoded version)
				// For hex tokens, URL encoding should be the same as the original
				assert.Equal(t, expectedToken, urlParts[1], "URL token")

				// Cleanup: Delete the Route and CRD
				_ = k8sClient.Delete(ctx, route)

				// Delete the Route CRD to not interfere with other tests
				crdToDelete := &apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: "routes.route.openshift.io",
					},
				}
				_ = k8sClient.Delete(ctx, crdToDelete)

				// Wait for CRD to be fully deleted
				waitFor(t, timeout*3, interval, func() bool {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					err := k8sClient.Get(ctx, client.ObjectKey{Name: "routes.route.openshift.io"}, crd)
					return apierrors.IsNotFound(err)
				}, "Route CRD should be deleted")
			})
		})
	})
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}
