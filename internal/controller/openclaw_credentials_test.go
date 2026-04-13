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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

// --- Credential validation tests ---

func TestOpenClawCredentialValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("should succeed with valid apiKey credential", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)
	})

	t.Run("should succeed with zero credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)
	})

	t.Run("should fail when Secret does not exist", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name:      "bad",
				Type:      openclawv1alpha1.CredentialTypeBearer,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "no-such-secret", Key: "key"},
				Domain:    "api.example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: ClawInstanceName, Namespace: namespace},
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

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name:      "test",
				Type:      openclawv1alpha1.CredentialTypeBearer,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "wrong-key-secret", Key: "api-key"},
				Domain:    "api.example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: ClawInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key \"api-key\" not found")
	})

	t.Run("should succeed with none credential type (no secretRef required)", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name:   "passthrough",
				Type:   openclawv1alpha1.CredentialTypeNone,
				Domain: "example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)
	})

	t.Run("should reject creation when secretRef is nil for apiKey type via CEL validation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name:   "no-ref",
				Type:   openclawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &openclawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject apiKey without secretRef")
		assert.Contains(t, err.Error(), "secretRef is required")
	})

	t.Run("should reject creation when apiKey config is nil via CEL validation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name:      "no-config",
				Type:      openclawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "some-secret", Key: "key"},
				Domain:    "api.example.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject apiKey without apiKey config")
		assert.Contains(t, err.Error(), "apiKey config is required")
	})

	t.Run("should set CredentialsResolved=False when validation fails", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name:      "bad",
				Type:      openclawv1alpha1.CredentialTypeBearer,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "missing", Key: "k"},
				Domain:    "api.example.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, _ = reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: ClawInstanceName, Namespace: namespace},
		})

		updated := &openclawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: ClawInstanceName, Namespace: namespace}, updated))

		var found bool
		for _, c := range updated.Status.Conditions {
			if c.Type == openclawv1alpha1.ConditionTypeCredentialsResolved {
				found = true
				assert.Equal(t, "False", string(c.Status))
				assert.Equal(t, openclawv1alpha1.ConditionReasonValidationFailed, c.Reason)
				break
			}
		}
		assert.True(t, found, "CredentialsResolved=False condition should be set on validation failure")
	})

	t.Run("should set CredentialsResolved condition", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		updatedInstance := &openclawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: ClawInstanceName, Namespace: namespace}, updatedInstance))

		var found bool
		for _, c := range updatedInstance.Status.Conditions {
			if c.Type == openclawv1alpha1.ConditionTypeCredentialsResolved {
				found = true
				assert.Equal(t, "True", string(c.Status))
				assert.Equal(t, openclawv1alpha1.ConditionReasonResolved, c.Reason)
				break
			}
		}
		assert.True(t, found, "CredentialsResolved condition should be set")
	})

	t.Run("should set ProxyConfigured condition", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		updatedInstance := &openclawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: ClawInstanceName, Namespace: namespace}, updatedInstance))

		var found bool
		for _, c := range updatedInstance.Status.Conditions {
			if c.Type == openclawv1alpha1.ConditionTypeProxyConfigured {
				found = true
				assert.Equal(t, "True", string(c.Status))
				assert.Equal(t, openclawv1alpha1.ConditionReasonConfigured, c.Reason)
				break
			}
		}
		assert.True(t, found, "ProxyConfigured condition should be set")
	})
}

// --- Secret reference and proxy deployment wiring tests ---

func TestOpenClawCredentialSecretReference(t *testing.T) {
	t.Run("When reconciling OpenClaw with credential references", func(t *testing.T) {
		const resourceName = ClawInstanceName
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
					Name:      ClawProxyDeploymentName,
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
					Name:      ClawProxyDeploymentName,
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				annotations := deployment.Spec.Template.Annotations
				if annotations == nil {
					return false
				}
				_, exists := annotations["openclaw.sandbox.redhat.com/proxy-config-hash"]
				return exists
			}, "pod template should have proxy-config-hash annotation")
		})
	})

	t.Run("should fail validation when Secret does not exist", func(t *testing.T) {
		ctx := context.Background()
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		instance := &openclawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []openclawv1alpha1.CredentialSpec{
			{
				Name: "missing-cred",
				Type: openclawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &openclawv1alpha1.SecretRef{
					Name: "nonexistent-secret",
					Key:  "api-key",
				},
				Domain: "api.example.com",
				APIKey: &openclawv1alpha1.APIKeyConfig{
					Header: "x-api-key",
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create OpenClaw instance")

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      ClawInstanceName,
				Namespace: namespace,
			},
		})
		require.Error(t, err, "expected error when Secret does not exist")
		assert.Contains(t, err.Error(), "credential validation failed")
		assert.Contains(t, err.Error(), "nonexistent-secret")
	})
}

func TestConfigureProxyForCredentials(t *testing.T) {
	buildObjects := func(t *testing.T) []*unstructured.Unstructured {
		t.Helper()
		reconciler := createClawReconciler()
		objects, err := reconciler.buildKustomizedObjects()
		require.NoError(t, err)
		return objects
	}

	findProxyContainer := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == ClawProxyDeploymentName {
				containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
				require.NotEmpty(t, containers)
				c, ok := containers[0].(map[string]any)
				require.True(t, ok)
				return c
			}
		}
		t.Fatal("proxy deployment not found")
		return nil
	}

	findVolumes := func(t *testing.T, objects []*unstructured.Unstructured) []any {
		t.Helper()
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == ClawProxyDeploymentName {
				vols, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
				return vols
			}
		}
		return nil
	}

	t.Run("should add GCP volume and mount for gcp credential", func(t *testing.T) {
		objects := buildObjects(t)
		creds := []openclawv1alpha1.CredentialSpec{
			{
				Name:      "vertex",
				Type:      openclawv1alpha1.CredentialTypeGCP,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "gcp-sa", Key: "sa.json"},
				Domain:    ".googleapis.com",
				GCP:       &openclawv1alpha1.GCPConfig{Project: "p", Location: "us-central1"},
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, creds))

		container := findProxyContainer(t, objects)
		mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")

		var foundMount bool
		for _, m := range mounts {
			mount := m.(map[string]any)
			if mount["name"] == "cred-vertex" {
				assert.Equal(t, "/etc/proxy/credentials/vertex", mount["mountPath"])
				assert.Equal(t, true, mount["readOnly"])
				foundMount = true
			}
		}
		assert.True(t, foundMount, "GCP credential volume mount should be present")

		volumes := findVolumes(t, objects)
		var foundVol bool
		for _, v := range volumes {
			vol := v.(map[string]any)
			if vol["name"] == "cred-vertex" {
				foundVol = true
				secret, ok := vol["secret"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcp-sa", secret["secretName"])
			}
		}
		assert.True(t, foundVol, "GCP credential volume should be present")
	})

	t.Run("should skip credentials with nil secretRef for apiKey type", func(t *testing.T) {
		objects := buildObjects(t)
		creds := []openclawv1alpha1.CredentialSpec{
			{
				Name:   "no-ref",
				Type:   openclawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &openclawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, creds))

		container := findProxyContainer(t, objects)
		envVars, _, _ := unstructured.NestedSlice(container, "env")
		for _, e := range envVars {
			env := e.(map[string]any)
			assert.NotEqual(t, "CRED_NO_REF", env["name"], "should not add env var for credential without secretRef")
		}
	})

	t.Run("should handle multiple credential types together", func(t *testing.T) {
		objects := buildObjects(t)
		creds := []openclawv1alpha1.CredentialSpec{
			{
				Name:      "gemini",
				Type:      openclawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "s1", Key: "k1"},
				Domain:    ".googleapis.com",
				APIKey:    &openclawv1alpha1.APIKeyConfig{Header: "x-goog-api-key"},
			},
			{
				Name:      "openai",
				Type:      openclawv1alpha1.CredentialTypeBearer,
				SecretRef: &openclawv1alpha1.SecretRef{Name: "s2", Key: "k2"},
				Domain:    "api.openai.com",
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, creds))

		container := findProxyContainer(t, objects)
		envVars, _, _ := unstructured.NestedSlice(container, "env")

		envNames := make(map[string]bool)
		for _, e := range envVars {
			env := e.(map[string]any)
			envNames[env["name"].(string)] = true
		}
		assert.True(t, envNames["CRED_GEMINI"], "should have CRED_GEMINI")
		assert.True(t, envNames["CRED_OPENAI"], "should have CRED_OPENAI")
	})
}

func TestFindClawsReferencingSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("should map referenced secret to Claw reconcile request", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()

		secret := &corev1.Secret{}
		secret.Name = aiModelSecret
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		require.Len(t, requests, 1)
		assert.Equal(t, ClawInstanceName, requests[0].Name)
		assert.Equal(t, namespace, requests[0].Namespace)
	})

	t.Run("should return empty for unreferenced secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()

		secret := &corev1.Secret{}
		secret.Name = "unrelated-secret"
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		assert.Empty(t, requests)
	})

	t.Run("should skip gateway secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()

		secret := &corev1.Secret{}
		secret.Name = ClawGatewaySecretName
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		assert.Empty(t, requests)
	})
}
