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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func TestConfigureProxyForCredentials(t *testing.T) {
	buildObjects := func(t *testing.T) []*unstructured.Unstructured {
		t.Helper()
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)
		return objects
	}

	findProxyContainer := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == getProxyDeploymentName(testInstanceName) {
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
			if obj.GetKind() == DeploymentKind && obj.GetName() == getProxyDeploymentName(testInstanceName) {
				vols, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
				return vols
			}
		}
		return nil
	}

	t.Run("should add GCP volume and mount for gcp credential", func(t *testing.T) {
		objects := buildObjects(t)
		creds := []clawv1alpha1.CredentialSpec{
			{
				Name:      "vertex",
				Type:      clawv1alpha1.CredentialTypeGCP,
				SecretRef: &clawv1alpha1.SecretRef{Name: "gcp-sa", Key: "sa.json"},
				Domain:    ".googleapis.com",
				GCP:       &clawv1alpha1.GCPConfig{Project: "p", Location: "us-central1"},
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, toResolved(creds)))

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
		creds := []clawv1alpha1.CredentialSpec{
			{
				Name:   "no-ref",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, toResolved(creds)))

		container := findProxyContainer(t, objects)
		envVars, _, _ := unstructured.NestedSlice(container, "env")
		for _, e := range envVars {
			env := e.(map[string]any)
			assert.NotEqual(t, "CRED_NO_REF", env["name"], "should not add env var for credential without secretRef")
		}
	})

	t.Run("should handle multiple credential types together", func(t *testing.T) {
		objects := buildObjects(t)
		creds := []clawv1alpha1.CredentialSpec{
			{
				Name:      "gemini",
				Type:      clawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &clawv1alpha1.SecretRef{Name: "s1", Key: "k1"},
				Domain:    ".googleapis.com",
				APIKey:    &clawv1alpha1.APIKeyConfig{Header: "x-goog-api-key"},
			},
			{
				Name:      "openai",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{Name: "s2", Key: "k2"},
				Domain:    "api.openai.com",
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, toResolved(creds)))

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

func TestStampSecretVersionAnnotation(t *testing.T) {
	ctx := context.Background()

	t.Run("should stamp Secret ResourceVersion on proxy pod template", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))

		annotations := deployment.Spec.Template.Annotations
		require.NotNil(t, annotations, "pod template annotations should exist")
		geminiSecretVersionKey := clawv1alpha1.AnnotationPrefixSecretVersion + "gemini" + clawv1alpha1.AnnotationSuffixSecretVersion
		rv, ok := annotations[geminiSecretVersionKey]
		assert.True(t, ok, "gemini-secret-version annotation should exist")
		assert.NotEmpty(t, rv, "ResourceVersion should not be empty")
	})

	t.Run("should update annotation when Secret data changes", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))
		geminiSecretVersionKey := clawv1alpha1.AnnotationPrefixSecretVersion + "gemini" + clawv1alpha1.AnnotationSuffixSecretVersion
		originalRV := deployment.Spec.Template.Annotations[geminiSecretVersionKey]
		require.NotEmpty(t, originalRV)

		// Update the Secret data
		secret := &corev1.Secret{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      aiModelSecret,
			Namespace: namespace,
		}, secret))
		secret.Data[aiModelSecretKey] = []byte("rotated-api-key")
		require.NoError(t, k8sClient.Update(ctx, secret))

		// Reconcile again
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))
		newRV := deployment.Spec.Template.Annotations[geminiSecretVersionKey]
		assert.NotEqual(t, originalRV, newRV,
			"annotation should change after Secret data update (old=%s, new=%s)", originalRV, newRV)
	})

	t.Run("should skip credentials without secretRef", func(t *testing.T) {
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

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))

		annotations := deployment.Spec.Template.Annotations
		for key := range annotations {
			assert.False(t, strings.HasSuffix(key, clawv1alpha1.AnnotationSuffixSecretVersion),
				"should not have secret-version annotations for none-type credentials, found %s", key)
		}
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
		secret.Name = ClawGatewaySecretName
		secret.Namespace = namespace

		requests := reconciler.findClawsReferencingSecret(ctx, secret)
		assert.Empty(t, requests)
	})
}

// --- Provider defaults tests ---

func TestResolveProviderDefaults(t *testing.T) {
	tests := []struct {
		name       string
		cred       clawv1alpha1.CredentialSpec
		wantDomain string
		wantHeader string
		wantErr    string
	}{
		{
			name: "google apiKey fills domain and header",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
			},
			wantDomain: "generativelanguage.googleapis.com",
			wantHeader: "x-goog-api-key",
		},
		{
			name: "anthropic apiKey fills domain and header",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "anthropic",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "anthropic",
			},
			wantDomain: "api.anthropic.com",
			wantHeader: "x-api-key",
		},
		{
			name: "google gcp fills domain",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				GCP:      &clawv1alpha1.GCPConfig{Project: "p", Location: "us-central1"},
			},
			wantDomain: ".googleapis.com",
		},
		{
			name: "anthropic gcp fills domain",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				GCP:      &clawv1alpha1.GCPConfig{Project: "p", Location: "us-east5"},
			},
			wantDomain: ".googleapis.com",
		},
		{
			name: "explicit domain preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "custom-proxy.internal",
			},
			wantDomain: "custom-proxy.internal",
			wantHeader: "x-goog-api-key",
		},
		{
			name: "explicit apiKey preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-custom-key"},
			},
			wantDomain: "generativelanguage.googleapis.com",
			wantHeader: "x-custom-key",
		},
		{
			name: "unknown provider with domain succeeds",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				Domain:   "api.custom-llm.com",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
			wantDomain: "api.custom-llm.com",
			wantHeader: "x-api-key",
		},
		{
			name: "unknown provider without domain errors",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				APIKey:   &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
			wantErr: "domain is required",
		},
		{
			name: "unknown provider without apiKey errors",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				Domain:   "api.custom-llm.com",
			},
			wantErr: "apiKey config is required",
		},
		{
			name: "no provider with domain and apiKey succeeds",
			cred: clawv1alpha1.CredentialSpec{
				Name:   "legacy",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &clawv1alpha1.APIKeyConfig{Header: "x-token"},
			},
			wantDomain: "api.example.com",
			wantHeader: "x-token",
		},
		{
			name: "bearer type with no domain errors",
			cred: clawv1alpha1.CredentialSpec{
				Name:     "custom",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "custom-llm",
			},
			wantErr: "domain is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred := tt.cred
			err := resolveProviderDefaults(&cred)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDomain, cred.Domain)
			if tt.wantHeader != "" {
				require.NotNil(t, cred.APIKey)
				assert.Equal(t, tt.wantHeader, cred.APIKey.Header)
			}
		})
	}
}
