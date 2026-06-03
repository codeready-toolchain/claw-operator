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

const codexAuthSecretName = "codex-auth"

func createCodexAuthSecret(t *testing.T, ctx context.Context, raw string) {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: codexAuthSecretName, Namespace: namespace},
		Data: map[string][]byte{
			"auth.json": []byte(raw),
		},
	}
	_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: codexAuthSecretName, Namespace: namespace})
	require.NoError(t, k8sClient.Create(ctx, secret))
}

func testClawWithCodexOAuth(profileID string) *clawv1alpha1.Claw {
	return &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		Spec: clawv1alpha1.ClawSpec{
			CodexOAuth: &clawv1alpha1.CodexOAuthSpec{
				SecretRef: clawv1alpha1.SecretRefEntry{Name: codexAuthSecretName, Key: "auth.json"},
				ProfileID: profileID,
				Model:     "gpt-5.5",
				Models:    []string{"openai/gpt-5.4-mini"},
			},
		},
	}
}

func TestCodexOAuthConfig(t *testing.T) {
	ctx := context.Background()
	validAuthJSON := `{"auth_mode":"chatgpt","tokens":{"access_token":"codex-access-token","refresh_token":"codex-refresh-token","account_id":"acct_123"}}`

	t.Run("reconcile injects codex oauth config and mounts auth secret", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: codexAuthSecretName, Namespace: namespace})
		})
		createCodexAuthSecret(t, ctx, validAuthJSON)

		instance := testClawWithCodexOAuth("openai-codex:default")
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: getConfigMapName(testInstanceName), Namespace: namespace}, cm))
		assert.NotContains(t, cm.Data["operator.json"], "codex-refresh-token")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		plugins := config["plugins"].(map[string]any)
		assert.ElementsMatch(t, []any{"openai", "codex"}, plugins["allow"].([]any))
		entries := plugins["entries"].(map[string]any)
		assert.True(t, entries["openai"].(map[string]any)["enabled"].(bool))
		assert.True(t, entries["codex"].(map[string]any)["enabled"].(bool))

		auth := config["auth"].(map[string]any)
		profiles := auth["profiles"].(map[string]any)
		assert.Equal(t, map[string]any{"provider": "openai", "mode": "oauth"}, profiles["openai:chatgpt-default"])
		order := auth["order"].(map[string]any)
		assert.Equal(t, []any{"openai:chatgpt-default"}, order["openai"])

		defaults := config["agents"].(map[string]any)["defaults"].(map[string]any)
		model := defaults["model"].(map[string]any)
		assert.Equal(t, "codex/gpt-5.5", model["primary"])
		models := defaults["models"].(map[string]any)
		assert.Equal(t, "codex", models["codex/gpt-5.5"].(map[string]any)["agentRuntime"].(map[string]any)["id"])
		assert.Equal(t, "codex", models["codex/gpt-5.4-mini"].(map[string]any)["agentRuntime"].(map[string]any)["id"])

		deploy := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: getClawDeploymentName(testInstanceName), Namespace: namespace}, deploy))
		var foundEnv, foundMount, foundVolume bool
		for _, ic := range deploy.Spec.Template.Spec.InitContainers {
			if ic.Name != ClawInitConfigContainerName {
				continue
			}
			for _, env := range ic.Env {
				if env.Name == codexOAuthProfileEnvVar && env.Value == "openai:chatgpt-default" {
					foundEnv = true
				}
			}
			for _, mount := range ic.VolumeMounts {
				if mount.Name == codexOAuthMountName && mount.MountPath == codexOAuthMountPath && mount.ReadOnly {
					foundMount = true
				}
			}
		}
		for _, volume := range deploy.Spec.Template.Spec.Volumes {
			if volume.Name == codexOAuthMountName && volume.Secret != nil {
				foundVolume = true
				assert.Equal(t, codexAuthSecretName, volume.Secret.SecretName)
				require.Len(t, volume.Secret.Items, 1)
				assert.Equal(t, "auth.json", volume.Secret.Items[0].Key)
				assert.Equal(t, "auth.json", volume.Secret.Items[0].Path)
			}
		}
		assert.True(t, foundEnv, "init-config should receive normalized profile id")
		assert.True(t, foundMount, "init-config should mount Codex OAuth secret")
		assert.True(t, foundVolume, "deployment should define Codex OAuth secret volume")
	})

	t.Run("missing secret fails validation", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: codexAuthSecretName, Namespace: namespace})
		})

		instance := testClawWithCodexOAuth("")
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `codexOAuth: Secret "codex-auth" not found`)
	})

	t.Run("invalid auth json fails validation", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: codexAuthSecretName, Namespace: namespace})
		})
		createCodexAuthSecret(t, ctx, `{"auth_mode":"api-key","tokens":{}}`)

		instance := testClawWithCodexOAuth("")
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `auth_mode "chatgpt"`)
	})
}

func TestCodexOAuthReferencesSecret(t *testing.T) {
	instance := clawv1alpha1.Claw{
		Spec: clawv1alpha1.ClawSpec{
			CodexOAuth: &clawv1alpha1.CodexOAuthSpec{
				SecretRef: clawv1alpha1.SecretRefEntry{Name: codexAuthSecretName, Key: "auth.json"},
			},
		},
	}

	assert.True(t, clawReferencesSecret(instance, codexAuthSecretName))
	assert.False(t, clawReferencesSecret(instance, "other-secret"))
}
