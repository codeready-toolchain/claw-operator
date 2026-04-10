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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawDeploymentController(t *testing.T) {

	// NOTE: The unified controller creates all resources atomically via server-side apply,
	// so ConfigMap dependency tests are no longer relevant. All resources are created together.

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		t.Run("should create Deployment for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Create a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconcile the created resource
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Check if Deployment was created
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")
		})

		t.Run("should set correct owner reference on Deployment", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Create a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconcile the created resource
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Check if Deployment was created
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawDeploymentName,
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")

			// Check Deployment has correct owner reference
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawDeploymentName,
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
			}, "Deployment should have correct owner reference")
		})
	})

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip Deployment creation for non-matching names", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
				if err := deleteAndWait(&openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace}); err != nil {
					t.Fatalf("cleanup failed: %v", err)
				}
			})

			// Create a new OpenClaw with name 'other-instance'
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconcile the created resource
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			require.NoError(t, err, "reconcile failed")

			// Verify Deployment was NOT created
			// Sleep to give reconciler time to (incorrectly) create resources
			time.Sleep(2 * time.Second)

			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      OpenClawDeploymentName,
				Namespace: namespace,
			}, deployment)
			require.Error(t, err, "Deployment should not have been created for non-instance OpenClaw")
		})
	})
}
