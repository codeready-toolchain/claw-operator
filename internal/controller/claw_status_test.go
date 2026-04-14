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

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Status condition tests ---

func TestOpenClawStatusConditions(t *testing.T) {
	t.Run("When reconciling an Claw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should set GatewayTokenSecretRef in status after reconciliation", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance))
			assert.Equal(t, ClawGatewaySecretName, updatedInstance.Status.GatewayTokenSecretRef)
		})

		t.Run("should set Ready condition to False after initial resource creation", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &clawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
				return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == clawv1alpha1.ConditionReasonProvisioning
			}, "Ready condition should be False with Provisioning reason")
		})

		t.Run("should keep Ready condition False when only openclaw Deployment is ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
			require.NotNil(t, condition, "Ready condition should not be nil")
			assert.Equal(t, metav1.ConditionFalse, condition.Status, "Ready condition status")
			assert.Equal(t, clawv1alpha1.ConditionReasonProvisioning, condition.Reason, "Ready condition reason")
		})

		t.Run("should keep Ready condition False when only openclaw-proxy Deployment is ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
			require.NotNil(t, condition, "Ready condition should not be nil")
			assert.Equal(t, metav1.ConditionFalse, condition.Status, "Ready condition status")
			assert.Equal(t, clawv1alpha1.ConditionReasonProvisioning, condition.Reason, "Ready condition reason")
		})

		t.Run("should set Ready condition to True when both Deployments are ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
			require.NotNil(t, condition, "Ready condition should not be nil")
			assert.Equal(t, metav1.ConditionTrue, condition.Status, "Ready condition status")
			assert.Equal(t, clawv1alpha1.ConditionReasonReady, condition.Reason, "Ready condition reason")
		})

		t.Run("should update LastTransitionTime only on status change", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			var initialTransitionTime metav1.Time
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &clawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
				if condition != nil {
					initialTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, "initial Ready condition should be set")

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

			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			var secondTransitionTime metav1.Time
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &clawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
				if condition != nil && condition.Status == metav1.ConditionTrue {
					secondTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, "Ready condition should transition to True")

			assert.False(t, secondTransitionTime.Time.Before(initialTransitionTime.Time), "LastTransitionTime should not go backwards")
		})

		t.Run("should preserve LastTransitionTime when status unchanged", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			var initialTransitionTime metav1.Time
			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &clawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
				if condition != nil && condition.Status == metav1.ConditionFalse {
					initialTransitionTime = condition.LastTransitionTime
					return true
				}
				return false
			}, "initial Ready condition should be False")

			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
			condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
			require.NotNil(t, condition, "Ready condition should not be nil")
			assert.Equal(t, metav1.ConditionFalse, condition.Status, "Ready condition status")
			assert.Equal(t, initialTransitionTime, condition.LastTransitionTime, "LastTransitionTime should not have changed")
		})

		t.Run("should handle missing Deployments gracefully", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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
			require.NoError(t, err, "reconcile should not error even if deployments don't exist")

			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &clawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
				return condition != nil && condition.Status == metav1.ConditionFalse
			}, "Ready condition should be set to False when deployments are missing")
		})

		t.Run("should set ObservedGeneration correctly in conditions", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

			instance.Spec.Credentials = testCredentials()
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

			waitFor(t, timeout, interval, func() bool {
				updatedInstance := &clawv1alpha1.Claw{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance)
				if err != nil {
					return false
				}
				condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
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

				instance := &clawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.Credentials = testCredentials()
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

				updatedInstance := &clawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should keep status.url empty when only openclaw deployment is ready", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				instance := &clawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.Credentials = testCredentials()
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

				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				updatedInstance := &clawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should keep status.url empty when only proxy deployment is ready", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				instance := &clawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.Credentials = testCredentials()
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

				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				updatedInstance := &clawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should clear status.url when deployments transition from ready to not ready", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				instance := &clawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.Credentials = testCredentials()
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

				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

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

				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				updatedInstance := &clawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should not set status.url when Route does not exist (vanilla Kubernetes)", func(t *testing.T) {
				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				instance := &clawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.Credentials = testCredentials()
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

				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				updatedInstance := &clawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")
				assert.Empty(t, updatedInstance.Status.URL, "expected empty status.url")
			})

			t.Run("should set status.url with token fragment when deployments are ready and Route exists", func(t *testing.T) {
				t.Skip("This test requires Route CRD to be installed - should be run in e2e tests with OpenShift cluster. Installing CRD dynamically in envtest interferes with other tests that expect it not to be present.")

				t.Cleanup(func() {
					deleteAndWaitAllResources(t, namespace)
				})

				instance := &clawv1alpha1.Claw{}
				instance.Name = resourceName
				instance.Namespace = namespace
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret), "failed to create secret")

				instance.Spec.Credentials = testCredentials()
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
				err = k8sClient.Create(ctx, routeCRD)
				if err != nil && !apierrors.IsAlreadyExists(err) {
					require.NoError(t, err, "failed to create Route CRD")
				}

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

				route := &unstructured.Unstructured{}
				route.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "route.openshift.io",
					Version: "v1",
					Kind:    "Route",
				})
				route.SetName("openclaw")
				route.SetNamespace(namespace)

				routeHost := "openclaw-default.apps.example.com"

				require.NoError(t, k8sClient.Create(ctx, route), "failed to create Route")

				waitFor(t, timeout, interval, func() bool {
					return k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw", Namespace: namespace}, route) == nil
				}, "Route should be created")

				route.Object["status"] = map[string]interface{}{
					"ingress": []interface{}{
						map[string]interface{}{
							"host": routeHost,
						},
					},
				}

				require.NoError(t, k8sClient.Status().Update(ctx, route), "failed to update Route status")

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

				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Name:      resourceName,
						Namespace: namespace,
					},
				})
				require.NoError(t, err, "reconcile failed")

				updatedInstance := &clawv1alpha1.Claw{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated instance")

				assert.NotEmpty(t, updatedInstance.Status.URL, "expected non-empty status.url")
				expectedPrefix := "https://" + routeHost
				assert.True(t, strings.HasPrefix(updatedInstance.Status.URL, expectedPrefix), "status.url should have prefix %s", expectedPrefix)
				assert.Contains(t, updatedInstance.Status.URL, "#token=")

				urlParts := strings.Split(updatedInstance.Status.URL, "#token=")
				require.Len(t, urlParts, 2, "URL should have exactly one #token= fragment")
				assert.Equal(t, "https://"+routeHost, urlParts[0], "URL host part")
				assert.NotEmpty(t, urlParts[1], "token should not be empty")

				gatewaySecret := &corev1.Secret{}
				require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: ClawGatewaySecretName, Namespace: namespace}, gatewaySecret), "failed to get gateway secret")
				expectedToken := string(gatewaySecret.Data[GatewayTokenKeyName])
				assert.NotEmpty(t, expectedToken, "expected non-empty gateway token")

				assert.Equal(t, expectedToken, urlParts[1], "URL token")

				_ = k8sClient.Delete(ctx, route)

				crdToDelete := &apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{
						Name: "routes.route.openshift.io",
					},
				}
				_ = k8sClient.Delete(ctx, crdToDelete)

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

// --- URL status field tests ---

func TestOpenClawURLStatusField(t *testing.T) {

	t.Run("When reconciling an Claw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should populate URL field when both deployments are ready and Route exists", func(t *testing.T) {
			t.Skip("Route CRD not available in envtest - requires e2e test with OpenShift cluster")
		})

		t.Run("should leave URL field empty when deployments are not ready", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

			instance.Spec.Credentials = testCredentials()
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

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated Claw instance")
			assert.Empty(t, updatedInstance.Status.URL, "expected empty URL")
		})

		t.Run("should leave URL field empty when Route does not exist", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			instance := &clawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

			instance.Spec.Credentials = testCredentials()
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

			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}, deployment)
				return err == nil
			}, "openclaw deployment to be created")

			deployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, deployment), "failed to update openclaw deployment status")

			proxyDeployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}, proxyDeployment)
				return err == nil
			}, "openclaw-proxy deployment to be created")

			proxyDeployment.Status.Conditions = []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			}
			require.NoError(t, k8sClient.Status().Update(ctx, proxyDeployment), "failed to update openclaw-proxy deployment status")

			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			updatedInstance := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updatedInstance), "failed to get updated Claw instance")
			assert.Empty(t, updatedInstance.Status.URL, "expected empty URL")
		})

		t.Run("should include https:// scheme in URL format", func(t *testing.T) {
			t.Skip("Route CRD not available in envtest - requires e2e test with OpenShift cluster")
		})
	})
}

func TestGatewayTokenRetrieval(t *testing.T) {
	const namespace = "default"
	ctx := context.Background()

	setupGatewaySecretTest := func(t *testing.T) {
		t.Helper()
		gatewaySecret := &corev1.Secret{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawGatewaySecretName, Namespace: namespace}, gatewaySecret); err == nil {
			_ = k8sClient.Delete(ctx, gatewaySecret)
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawGatewaySecretName, Namespace: namespace}, gatewaySecret)
				return err != nil
			}, "gateway secret to be deleted")
		}
	}

	t.Run("should retrieve and decode gateway token from openclaw-gateway-token", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		gatewaySecret := &corev1.Secret{}
		gatewaySecret.Name = ClawGatewaySecretName
		gatewaySecret.Namespace = namespace
		testToken := "test-gateway-token-123456"
		gatewaySecret.Data = map[string][]byte{
			GatewayTokenKeyName: []byte(testToken),
		}
		require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway secret")

		reconciler := &ClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		assert.Equal(t, testToken, token, "expected token to match")
	})

	t.Run("should return empty string when gateway secret does not exist", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		reconciler := &ClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		assert.Empty(t, token, "expected empty string")
	})

	t.Run("should return empty string when token key is missing from secret", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		gatewaySecret := &corev1.Secret{}
		gatewaySecret.Name = ClawGatewaySecretName
		gatewaySecret.Namespace = namespace
		gatewaySecret.Data = map[string][]byte{
			"other-key": []byte("other-value"),
		}
		require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway secret")

		reconciler := &ClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		assert.Empty(t, token, "expected empty string")
	})

	t.Run("should return empty string when token value is empty", func(t *testing.T) {
		setupGatewaySecretTest(t)
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		gatewaySecret := &corev1.Secret{}
		gatewaySecret.Name = ClawGatewaySecretName
		gatewaySecret.Namespace = namespace
		gatewaySecret.Data = map[string][]byte{
			GatewayTokenKeyName: []byte(""),
		}
		require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway secret")

		reconciler := &ClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}
		token := reconciler.getGatewayToken(ctx, namespace)

		assert.Empty(t, token, "expected empty string")
	})
}

func TestURLConstructionWithTokenFragment(t *testing.T) {
	t.Run("URL construction scenarios", func(t *testing.T) {
		tests := []struct {
			name     string
			routeURL string
			token    string
			expected string
		}{
			{
				name:     "should append token fragment when both route and token are provided",
				routeURL: "https://openclaw-route.apps.example.com",
				token:    "abc123def456",
				expected: "https://openclaw-route.apps.example.com#token=abc123def456",
			},
			{
				name:     "should return route URL without fragment when token is empty",
				routeURL: "https://openclaw-route.apps.example.com",
				token:    "",
				expected: "https://openclaw-route.apps.example.com",
			},
			{
				name:     "should return empty string when route URL is empty",
				routeURL: "",
				token:    "abc123def456",
				expected: "",
			},
			{
				name:     "should return empty string when both route and token are empty",
				routeURL: "",
				token:    "",
				expected: "",
			},
			{
				name:     "should percent-encode special characters in token",
				routeURL: "https://openclaw-route.apps.example.com",
				token:    "token+with=special&chars#fragment",
				expected: "https://openclaw-route.apps.example.com#token=token%2Bwith%3Dspecial%26chars%23fragment",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := buildClawURL(tt.routeURL, tt.token)
				assert.Equal(t, tt.expected, result, "URL construction result")
			})
		}
	})

	t.Run("should follow format https://<route-host>#token=<gateway-token>", func(t *testing.T) {
		routeURL := "https://openclaw-default.apps.cluster.example.com"
		token := "64chartoken1234567890abcdef64chartoken1234567890abcdef123456"

		result := buildClawURL(routeURL, token)

		expected := "https://openclaw-default.apps.cluster.example.com#token=64chartoken1234567890abcdef64chartoken1234567890abcdef123456"
		assert.Equal(t, expected, result, "URL construction result")
		assert.True(t, strings.HasPrefix(result, "https://"), "expected result to start with https://")
		assert.Contains(t, result, "#token=")
	})
}
