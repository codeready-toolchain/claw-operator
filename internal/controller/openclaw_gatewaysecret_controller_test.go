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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawGatewaySecretController(t *testing.T) {

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create gateway Secret when OpenClaw instance is reconciled", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Check if gateway Secret was created
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil
			}, "gateway Secret should be created")

			// Verify Secret has OPENCLAW_GATEWAY_TOKEN data entry
			assert.Contains(t, secret.Data, GatewayTokenKeyName)
		})

		t.Run("should create token with exactly 64 hex characters", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Verify token format and length
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				token, exists := secret.Data[GatewayTokenKeyName]
				if !exists {
					return false
				}
				// Token should be exactly 64 hex characters
				hexPattern := regexp.MustCompile("^[0-9a-f]{64}$")
				return hexPattern.Match(token)
			}, "token should be exactly 64 hex characters")
		})

		t.Run("should not regenerate token when secret already exists", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Get the initial token value
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, "initial token should be created")
			initialToken := string(secret.Data[GatewayTokenKeyName])

			// Reconcile again
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Verify token was not regenerated
			secret = &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				currentToken := string(secret.Data[GatewayTokenKeyName])
				return currentToken == initialToken
			}, "token should not be regenerated")
		})

		t.Run("should generate unique tokens for different reconciliations when secret is deleted", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Get the first token value
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				return err == nil && len(secret.Data[GatewayTokenKeyName]) > 0
			}, "first token should be created")
			firstToken := string(secret.Data[GatewayTokenKeyName])

			// Delete the Secret
			require.NoError(t, k8sClient.Delete(ctx, secret), "failed to delete Secret")

			// Reconcile again to generate a new token
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Verify a new unique token was generated
			newSecret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, newSecret)
				if err != nil {
					return false
				}
				secondToken := string(newSecret.Data[GatewayTokenKeyName])
				// Tokens should be different
				return len(secondToken) > 0 && secondToken != firstToken
			}, "new unique token should be generated")
		})

		t.Run("should set correct owner reference on gateway Secret during initial creation", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Check gateway Secret has correct owner reference
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "gateway Secret should have correct owner reference")
		})

		t.Run("should set correct owner reference on gateway Secret when it already existed", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// Create required API key Secret
			apiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
			require.NoError(t, k8sClient.Create(ctx, apiSecret), "failed to create Secret")

			// Create OpenClaw instance manually (not using helper)
			instance := &openclawv1alpha1.Claw{}
			instance.Name = resourceName
			instance.Namespace = namespace
			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			}
			require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw")

			// Create gateway secret
			gatewaySecret := createTestGatewaySecret(t, ClawGatewaySecretName, namespace)
			require.NoError(t, k8sClient.Create(ctx, gatewaySecret), "failed to create gateway Secret")
			assert.Empty(t, gatewaySecret.OwnerReferences, "gateway Secret should not have owner references initially")

			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Check gateway Secret has correct owner reference
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "gateway Secret should have correct owner reference after reconciliation")
		})

		t.Run("should have owner reference that enables garbage collection", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Verify gateway Secret has owner reference for garbage collection
			secret := &corev1.Secret{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawGatewaySecretName,
					Namespace: namespace,
				}, secret)
				if err != nil {
					return false
				}
				if len(secret.OwnerReferences) == 0 {
					return false
				}
				// Verify owner reference has BlockOwnerDeletion set
				ownerRef := secret.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "gateway Secret should have owner reference for garbage collection")
		})
	})
}
