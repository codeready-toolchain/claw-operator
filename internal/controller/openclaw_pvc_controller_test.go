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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawPersistentVolumeClaimController(t *testing.T) {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret"
		apiKeySecretKey = "api-key"
	)

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		t.Run("should create PVC for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
				deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: apiKeySecret, Namespace: namespace})
				deleteAndWait(ctx, &corev1.PersistentVolumeClaim{}, client.ObjectKey{Name: OpenClawPVCName, Namespace: namespace})
			})

			// Create a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
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

			// Check if PVC was created
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawPVCName,
					Namespace: namespace,
				}, pvc)
				return err == nil
			}, "PVC should be created")
		})

		t.Run("should set correct owner reference on PVC", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
				deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: apiKeySecret, Namespace: namespace})
				deleteAndWait(ctx, &corev1.PersistentVolumeClaim{}, client.ObjectKey{Name: OpenClawPVCName, Namespace: namespace})
			})

			// Create a new OpenClaw named 'instance'
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
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

			// Check PVC has correct owner reference
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
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
			}, "PVC should have correct owner reference")
		})
	})

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip PVC creation for non-matching names", func(t *testing.T) {
			// Setup: cleanup any instance named "instance" from previous tests
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
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: OpenClawPVCName, Namespace: namespace}, pvc)
					return err != nil
				}, "PVC should be deleted before test")
			}

			t.Cleanup(func() {
				deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
			})

			// Create a new OpenClaw with name 'other-instance'
			instance = &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			require.NoError(t, k8sClient.Create(ctx, secret), "failed to create Secret")

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Reconcile the created resource
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			if err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}

			// Verify PVC was NOT created
			// Sleep to give reconciler time to (incorrectly) create resources
			time.Sleep(2 * time.Second)

			pvc = &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, client.ObjectKey{
				Name:      OpenClawPVCName,
				Namespace: namespace,
			}, pvc)
			require.Error(t, err, "PVC should not have been created for non-instance OpenClaw")
		})
	})
}
