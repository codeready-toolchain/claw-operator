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
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Credential validation tests ---

func TestOpenClawCredentialValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("should succeed with valid apiKey credential", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
	})

	t.Run("should succeed with zero credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
	})

	t.Run("should fail when Secret does not exist", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "bad",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{Name: "no-such-secret", Key: "key"},
				Domain:    "api.example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential validation failed")
	})

	t.Run("should fail when Secret key is missing", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: "wrong-key-secret", Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		secret := &corev1.Secret{}
		secret.Name = "wrong-key-secret"
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"other-key": []byte("value")}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "test",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{Name: "wrong-key-secret", Key: "api-key"},
				Domain:    "api.example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key \"api-key\" not found")
	})

	t.Run("should succeed with none credential type (no secretRef required)", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:   "passthrough",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: "example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
	})

	t.Run("should reject creation when secretRef is nil for apiKey type via CEL validation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:   "no-ref",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject apiKey without secretRef")
		assert.Contains(t, err.Error(), "secretRef is required")
	})

	t.Run("should reject creation when apiKey config is nil via CEL validation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "no-config",
				Type:      clawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &clawv1alpha1.SecretRef{Name: "some-secret", Key: "key"},
				Domain:    "api.example.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject apiKey without apiKey config")
		assert.Contains(t, err.Error(), "apiKey config is required")
	})

	t.Run("should set CredentialsResolved=False when validation fails", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "bad",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{Name: "missing", Key: "k"},
				Domain:    "api.example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, _ = reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updated))

		var credFound, readyFound bool
		for _, c := range updated.Status.Conditions {
			if c.Type == clawv1alpha1.ConditionTypeCredentialsResolved {
				credFound = true
				assert.Equal(t, "False", string(c.Status))
				assert.Equal(t, clawv1alpha1.ConditionReasonValidationFailed, c.Reason)
			}
			if c.Type == clawv1alpha1.ConditionTypeReady {
				readyFound = true
				assert.Equal(t, "False", string(c.Status))
				assert.Equal(t, clawv1alpha1.ConditionReasonValidationFailed, c.Reason)
				assert.Contains(t, c.Message, "Secret \"missing\" not found")
			}
		}
		assert.True(t, credFound, "CredentialsResolved=False condition should be set on validation failure")
		assert.True(t, readyFound, "Ready=False condition should be set on validation failure")
	})

	t.Run("should set CredentialsResolved condition", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updatedInstance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updatedInstance))

		var found bool
		for _, c := range updatedInstance.Status.Conditions {
			if c.Type == clawv1alpha1.ConditionTypeCredentialsResolved {
				found = true
				assert.Equal(t, "True", string(c.Status))
				assert.Equal(t, clawv1alpha1.ConditionReasonResolved, c.Reason)
				break
			}
		}
		assert.True(t, found, "CredentialsResolved condition should be set")
	})

	t.Run("should set ProxyConfigured condition", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updatedInstance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updatedInstance))

		var found bool
		for _, c := range updatedInstance.Status.Conditions {
			if c.Type == clawv1alpha1.ConditionTypeProxyConfigured {
				found = true
				assert.Equal(t, "True", string(c.Status))
				assert.Equal(t, clawv1alpha1.ConditionReasonConfigured, c.Reason)
				break
			}
		}
		assert.True(t, found, "ProxyConfigured condition should be set")
	})
}

// --- Secret reference and proxy deployment wiring tests ---

func TestOpenClawCredentialSecretReference(t *testing.T) {
	t.Run("When reconciling Claw with credential references", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Run("should configure proxy deployment with credential env vars", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getProxyDeploymentName(testInstanceName),
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "proxy" {
						for _, env := range container.Env {
							if env.Name == "CRED_GEMINI" && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
								return env.ValueFrom.SecretKeyRef.Name == aiModelSecret &&
									env.ValueFrom.SecretKeyRef.Key == aiModelSecretKey
							}
						}
					}
				}
				return false
			}, "proxy deployment should have CRED_GEMINI env var referencing user's Secret")
		})

		t.Run("should stamp proxy config hash annotation on pod template", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      getProxyDeploymentName(testInstanceName),
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				annotations := deployment.Spec.Template.Annotations
				if annotations == nil {
					return false
				}
				_, exists := annotations[clawv1alpha1.AnnotationKeyProxyConfigHash]
				return exists
			}, "pod template should have proxy-config-hash annotation")
		})
	})

}

func TestFindClawsReferencingSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("should map referenced secret to Claw reconcile request", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		secret := &corev1.Secret{}
		secret.Name = aiModelSecret
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		require.Len(t, requests, 1)
		assert.Equal(t, testInstanceName, requests[0].Name)
		assert.Equal(t, namespace, requests[0].Namespace)
	})

	t.Run("should return empty for unreferenced secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		secret := &corev1.Secret{}
		secret.Name = "unrelated-secret"
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		assert.Empty(t, requests)
	})

	t.Run("should skip gateway secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		secret := &corev1.Secret{}
		secret.Name = getGatewaySecretName(testInstanceName)
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		assert.Empty(t, requests)
	})
}
