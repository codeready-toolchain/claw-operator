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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawSecretReference(t *testing.T) {

	t.Run("When reconciling OpenClaw with Secret references", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should configure proxy deployment to reference the user's Secret", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// given
			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			// when
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			t.Log("Verifying proxy deployment references the user's Secret")
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				// Find the proxy container and check GEMINI_API_KEY env var
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == ClawProxyDeploymentContainerName {
						for _, env := range container.Env {
							if env.Name == ClawProxyDeploymentGeminiAPiKeyEnvKey && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
								return env.ValueFrom.SecretKeyRef.Name == aiModelSecret &&
									env.ValueFrom.SecretKeyRef.Key == aiModelSecretKey
							}
						}
					}
				}
				return false
			}, "proxy deployment to reference user's Secret")
		})

		t.Run("should stamp Secret ResourceVersion annotation on pod template", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			t.Log("Creating the referenced Secret")
			secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, secret), "Failed to create Secret")

			t.Log("Creating OpenClaw instance")
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "Failed to create OpenClaw instance")

			t.Log("Reconciling the OpenClaw instance")
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			t.Log("Verifying pod template has Secret version annotation")
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				annotations := deployment.Spec.Template.Annotations
				if annotations == nil {
					return false
				}
				version, exists := annotations["openclaw.sandbox.redhat.com/gemini-secret-version"]
				return exists && version == secret.ResourceVersion
			}, "pod template to have Secret version annotation")

			t.Log("Updating the Secret data")
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: aiModelSecret, Namespace: namespace}, secret), "Failed to get Secret")
			originalVersion := secret.ResourceVersion
			secret.Data[aiModelSecretKey] = []byte("updated-api-key")
			require.NoError(t, k8sClient.Update(ctx, secret), "Failed to update Secret")

			t.Log("Fetching updated Secret to get new ResourceVersion")
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: aiModelSecret, Namespace: namespace}, secret), "Failed to get updated Secret")
			assert.NotEqual(t, originalVersion, secret.ResourceVersion, "Secret ResourceVersion should change")

			t.Log("Reconciling again after Secret update")
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			t.Log("Verifying pod template annotation updated with new Secret version")
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw-proxy",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				annotations := deployment.Spec.Template.Annotations
				if annotations == nil {
					return false
				}
				version, exists := annotations["openclaw.sandbox.redhat.com/gemini-secret-version"]
				return exists && version == secret.ResourceVersion && version != originalVersion
			}, "pod template annotation to update with new Secret version")
		})
	})

	t.Run("should fail to configure proxy deployment if the Secret does not exist", func(t *testing.T) {
		ctx := context.Background()
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		t.Log("Creating OpenClaw instance")
		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
			Name: aiModelSecret,
			Key:  aiModelSecretKey,
		}
		require.NoError(t, k8sClient.Create(ctx, instance), "Failed to create OpenClaw instance")

		t.Log("Reconciling the OpenClaw instance")
		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      ClawInstanceName,
				Namespace: namespace,
			},
		})
		require.Error(t, err, "expected error when Secret does not exist")
		assert.Contains(t, err.Error(), "failed to stamp Secret version annotation: failed to get Secret test-gemini-secret for version stamping")
	})
}
