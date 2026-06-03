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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "no-such-secret", Key: "key"}},
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
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "wrong-key-secret", Key: "api-key"}},
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

	t.Run("should reject creation via CEL when secretRef is empty for apiKey type", func(t *testing.T) {
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
		require.Error(t, err, "CEL should reject apiKey credential without secretRef")
		assert.Contains(t, err.Error(), "secretRef is required")
	})

	t.Run("should reject creation via CEL when apiKey config is nil", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "no-config",
				Type:      clawv1alpha1.CredentialTypeAPIKey,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "some-secret", Key: "key"}},
				Domain:    "api.example.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "CEL should reject apiKey credential without apiKey config")
		assert.Contains(t, err.Error(), "apiKey config is required")
	})

	t.Run("should reject creation via CEL when neither type nor channel nor known provider is set", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "bare",
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "s", Key: "k"}},
				Domain:    "api.example.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject credential with neither type, channel, nor known provider")
		assert.Contains(t, err.Error(), "type is required")
	})

	t.Run("should reject creation via CEL when unknown provider is set without type", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "custom",
				Provider:  "custom-llm",
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "s", Key: "k"}},
				Domain:    "api.custom-llm.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject unknown provider without explicit type")
		assert.Contains(t, err.Error(), "type is required")
	})

	t.Run("should reject creation via CEL when both provider and channel are set", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "conflict",
				Channel:  "telegram",
				Provider: "google",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject credential with both provider and channel")
		assert.Contains(t, err.Error(), "provider and channel are mutually exclusive")
	})

	t.Run("should accept creation via CEL when channel is set without type", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:    "tg",
				Channel: "telegram",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.NoError(t, err, "admission should accept credential with channel but no type")
	})

	t.Run("should accept creation via CEL when known provider is set without type", func(t *testing.T) {
		knownProviders := []string{"google", "anthropic", "openai", "xai", "openrouter"}
		for _, provider := range knownProviders {
			t.Run(provider, func(t *testing.T) {
				t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

				secretName := provider + "-secret"
				secret := createTestAPIKeySecret(secretName, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret))

				instance := &clawv1alpha1.Claw{}
				instance.Name = testInstanceName
				instance.Namespace = namespace
				instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
					{
						Name:     provider + "-cred",
						Provider: provider,
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: secretName, Key: aiModelSecretKey},
						},
					},
				}
				err := k8sClient.Create(ctx, instance)
				require.NoError(t, err, "admission should accept known provider %q without explicit type", provider)
			})
		}
	})

	t.Run("should reject creation via CEL when known provider is set without secretRef", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Provider: "google",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should still require secretRef for known providers")
		assert.Contains(t, err.Error(), "secretRef is required")
	})

	t.Run("should admit apiKey type with known apiKey provider without apiKey config", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: aiModelSecret, Key: aiModelSecretKey},
				},
				Domain: "generativelanguage.googleapis.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.NoError(t, err, "admission should accept apiKey type with google provider (has Header defaults)")
	})

	t.Run("should reject apiKey type with known bearer provider without apiKey config", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "openai-wrong-type",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "openai",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "s", Key: "k"},
				},
				Domain: "api.openai.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject apiKey type with openai (no Header defaults)")
		assert.Contains(t, err.Error(), "apiKey config is required")
	})

	t.Run("should reject apiKey type with unknown provider without apiKey config", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "custom-bad",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "custom-llm",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "s", Key: "k"},
				},
				Domain: "custom-llm.example.com",
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "admission should reject apiKey type with unknown provider (no Header defaults)")
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
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "missing", Key: "k"}},
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

	t.Run("should infer type and domain for all known providers", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secrets := map[string]string{
			"google-key":    "google-api-key",
			"anthropic-key": "anthropic-api-key",
			"openai-key":    "openai-api-key",
			"xai-key":       "xai-api-key",
		}
		for name, key := range secrets {
			s := createTestAPIKeySecret(name, namespace, key, "test-value")
			require.NoError(t, k8sClient.Create(ctx, s))
		}

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Provider: "google",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "google-key", Key: "google-api-key"},
				},
			},
			{
				Name:     "anthropic",
				Provider: "anthropic",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "anthropic-key", Key: "anthropic-api-key"},
				},
			},
			{
				Name:     "openai",
				Provider: "openai",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "openai-key", Key: "openai-api-key"},
				},
			},
			{
				Name:     "xai",
				Provider: "xai",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "xai-key", Key: "xai-api-key"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updated))

		var credResolved, proxyConfigured bool
		for _, c := range updated.Status.Conditions {
			if c.Type == clawv1alpha1.ConditionTypeCredentialsResolved && c.Status == "True" {
				credResolved = true
			}
			if c.Type == clawv1alpha1.ConditionTypeProxyConfigured && c.Status == "True" {
				proxyConfigured = true
			}
		}
		assert.True(t, credResolved, "CredentialsResolved should be True")
		assert.True(t, proxyConfigured, "ProxyConfigured should be True")

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))
		providers := providersFromConfig(t, config)

		google, ok := providers["google"].(map[string]any)
		require.True(t, ok, "google provider should exist")
		assert.Contains(t, google["baseUrl"], "generativelanguage.googleapis.com")
		assert.Equal(t, "google-generative-ai", google["api"])

		anthropic, ok := providers["anthropic"].(map[string]any)
		require.True(t, ok, "anthropic provider should exist")
		assert.Contains(t, anthropic["baseUrl"], "api.anthropic.com")
		assert.Equal(t, "anthropic-messages", anthropic["api"])

		openai, ok := providers["openai"].(map[string]any)
		require.True(t, ok, "openai provider should exist")
		assert.Equal(t, "https://api.openai.com/v1", openai["baseUrl"])

		xai, ok := providers["xai"].(map[string]any)
		require.True(t, ok, "xai provider should exist")
		assert.Equal(t, "https://api.x.ai/v1", xai["baseUrl"])
		assert.Equal(t, "openai-responses", xai["api"])
	})
}

// --- Custom provider validation tests ---

func TestCustomProviderValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("should succeed with arbitrary provider string on credential", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:     "custom-llm",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "my-vllm",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: aiModelSecret, Key: aiModelSecretKey},
				},
				Domain: "llm.mycompany.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
	})

	t.Run("should fail when customProvider credentialRef is missing", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "my-cred",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    "llm.mycompany.com",
			},
		}
		instance.Spec.CustomProviders = []clawv1alpha1.CustomProviderSpec{
			{
				Name:          "my-vllm",
				BaseUrl:       "https://llm.mycompany.com/v1",
				CredentialRef: "nonexistent",
				Models:        []clawv1alpha1.CustomModelEntry{{Name: "qwen3-14b"}},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `credentialRef "nonexistent" not found`)
	})

	t.Run("should fail when customProvider name duplicates credential provider", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "gemini",
				Type:      clawv1alpha1.CredentialTypeAPIKey,
				Provider:  "google",
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    ".googleapis.com",
				APIKey:    &clawv1alpha1.APIKeyConfig{Header: "x-goog-api-key"},
			},
		}
		instance.Spec.CustomProviders = []clawv1alpha1.CustomProviderSpec{
			{
				Name:          "google",
				BaseUrl:       "https://my-google-proxy.com/v1",
				CredentialRef: "gemini",
				Models:        []clawv1alpha1.CustomModelEntry{{Name: "custom-model"}},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `name conflicts with provider on credential`)
	})

	t.Run("should reject duplicate customProvider names at admission", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "my-cred",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    "llm.mycompany.com",
			},
		}
		instance.Spec.CustomProviders = []clawv1alpha1.CustomProviderSpec{
			{
				Name:          "my-vllm",
				BaseUrl:       "https://llm.mycompany.com/v1",
				CredentialRef: "my-cred",
				Models:        []clawv1alpha1.CustomModelEntry{{Name: "model-a"}},
			},
			{
				Name:          "my-vllm",
				BaseUrl:       "https://llm2.mycompany.com/v1",
				CredentialRef: "my-cred",
				Models:        []clawv1alpha1.CustomModelEntry{{Name: "model-b"}},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "API server should reject duplicate customProvider names via listType=map")
		assert.Contains(t, err.Error(), "Duplicate value")
	})

	t.Run("should fail when credential names are duplicated", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "my-cred",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    "llm.mycompany.com",
			},
			{
				Name:      "my-cred",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    "llm2.mycompany.com",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `credential "my-cred": duplicate name`)
	})

	t.Run("should succeed with valid customProvider referencing existing credential", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "my-cred",
				Type:      clawv1alpha1.CredentialTypeBearer,
				SecretRef: []clawv1alpha1.SecretRefEntry{{Name: aiModelSecret, Key: aiModelSecretKey}},
				Domain:    "llm.mycompany.com",
			},
		}
		instance.Spec.CustomProviders = []clawv1alpha1.CustomProviderSpec{
			{
				Name:          "my-vllm",
				BaseUrl:       "https://llm.mycompany.com/v1",
				CredentialRef: "my-cred",
				Models:        []clawv1alpha1.CustomModelEntry{{Name: "qwen3-14b", Alias: "Qwen 3 14B"}},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
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

func TestMultiSecretCredentialValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("should validate all secrets in multi-entry SecretRef", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: "slack-secret", Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		secret := &corev1.Secret{}
		secret.Name = "slack-secret"
		secret.Namespace = namespace
		secret.Data = map[string][]byte{
			"bot-token": []byte("xoxb-test"),
			"app-token": []byte("xapp-test"),
		}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:   "slack",
				Type:   clawv1alpha1.CredentialTypeBearer,
				Domain: "slack.com",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "slack-secret", Key: "bot-token", Role: "botToken"},
					{Name: "slack-secret", Key: "app-token", Role: "appToken"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
	})

	t.Run("should fail when one secret in multi-entry SecretRef has wrong key", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: "partial-secret", Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		secret := &corev1.Secret{}
		secret.Name = "partial-secret"
		secret.Namespace = namespace
		secret.Data = map[string][]byte{
			"bot-token": []byte("xoxb-test"),
		}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:   "slack",
				Type:   clawv1alpha1.CredentialTypeBearer,
				Domain: "slack.com",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "partial-secret", Key: "bot-token", Role: "botToken"},
					{Name: "partial-secret", Key: "app-token", Role: "appToken"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key \"app-token\" not found")
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

	t.Run("should map MCP envFrom secret to Claw reconcile request", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		mcpSecretName := "mcp-db-secret"
		mcpSecret := createTestAPIKeySecret(mcpSecretName, namespace, "password", "s3cret")
		require.NoError(t, k8sClient.Create(ctx, mcpSecret))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, mcpSecret) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"db-tool": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_PASSWORD",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: mcpSecretName, Key: "password"},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		requests := reconciler.findClawsReferencingSecret(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: mcpSecretName, Namespace: namespace},
		})
		require.Len(t, requests, 1)
		assert.Equal(t, testInstanceName, requests[0].Name)
	})

	t.Run("should not duplicate request when secret matches both credential and MCP envFrom", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"tool": {
				Command: "cmd",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "TOKEN",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: aiModelSecret, Key: aiModelSecretKey},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconciler := createClawReconciler()
		requests := reconciler.findClawsReferencingSecret(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: aiModelSecret, Namespace: namespace},
		})
		require.Len(t, requests, 1, "should return exactly one request, not duplicate")
	})
}
