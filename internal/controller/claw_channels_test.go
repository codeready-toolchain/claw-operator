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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestResolveChannelDefaults(t *testing.T) {
	tests := []struct {
		name          string
		cred          clawv1alpha1.CredentialSpec
		wantType      clawv1alpha1.CredentialType
		wantDomain    string
		wantPathToken string
		wantAPIKey    string
		wantErr       string
	}{
		{
			name:          "telegram infers pathToken type and domain",
			cred:          clawv1alpha1.CredentialSpec{Name: "tg", Channel: "telegram"},
			wantType:      clawv1alpha1.CredentialTypePathToken,
			wantDomain:    "api.telegram.org",
			wantPathToken: "/bot",
		},
		{
			name:       "discord infers apiKey type and domain",
			cred:       clawv1alpha1.CredentialSpec{Name: "dc", Channel: "discord"},
			wantType:   clawv1alpha1.CredentialTypeAPIKey,
			wantDomain: "discord.com",
			wantAPIKey: "Authorization",
		},
		{
			name:       "slack infers bearer type and domain",
			cred:       clawv1alpha1.CredentialSpec{Name: "sl", Channel: "slack"},
			wantType:   clawv1alpha1.CredentialTypeBearer,
			wantDomain: "slack.com",
		},
		{
			name:     "whatsapp infers none type with no domain",
			cred:     clawv1alpha1.CredentialSpec{Name: "wa", Channel: "whatsapp"},
			wantType: clawv1alpha1.CredentialTypeNone,
		},
		{
			name: "explicit type is preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:    "tg",
				Channel: "telegram",
				Type:    clawv1alpha1.CredentialTypeBearer,
			},
			wantType:   clawv1alpha1.CredentialTypeBearer,
			wantDomain: "api.telegram.org",
		},
		{
			name: "explicit domain is preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:    "tg",
				Channel: "telegram",
				Domain:  "telegram.internal.corp.com",
			},
			wantType:      clawv1alpha1.CredentialTypePathToken,
			wantDomain:    "telegram.internal.corp.com",
			wantPathToken: "/bot",
		},
		{
			name: "explicit pathToken is preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:      "tg",
				Channel:   "telegram",
				PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/custom"},
			},
			wantType:      clawv1alpha1.CredentialTypePathToken,
			wantDomain:    "api.telegram.org",
			wantPathToken: "/custom",
		},
		{
			name: "explicit apiKey is preserved",
			cred: clawv1alpha1.CredentialSpec{
				Name:    "dc",
				Channel: "discord",
				APIKey:  &clawv1alpha1.APIKeyConfig{Header: "X-Custom"},
			},
			wantType:   clawv1alpha1.CredentialTypeAPIKey,
			wantDomain: "discord.com",
			wantAPIKey: "X-Custom",
		},
		{
			name:    "unknown channel returns error",
			cred:    clawv1alpha1.CredentialSpec{Name: "irc", Channel: "irc"},
			wantErr: "unknown channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cred := tt.cred
			err := resolveChannelDefaults(&cred)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, cred.Type)
			assert.Equal(t, tt.wantDomain, cred.Domain)
			if tt.wantPathToken != "" {
				require.NotNil(t, cred.PathToken)
				assert.Equal(t, tt.wantPathToken, cred.PathToken.Prefix)
			}
			if tt.wantAPIKey != "" {
				require.NotNil(t, cred.APIKey)
				assert.Equal(t, tt.wantAPIKey, cred.APIKey.Header)
			}
		})
	}
}

func TestGenerateCompanionRoutes(t *testing.T) {
	tests := []struct {
		name          string
		cred          clawv1alpha1.CredentialSpec
		wantDomains   []string
		wantInjectors []string
	}{
		{
			name:          "discord generates two companion routes",
			cred:          clawv1alpha1.CredentialSpec{Channel: "discord"},
			wantDomains:   []string{"gateway.discord.gg", "cdn.discordapp.com"},
			wantInjectors: []string{"none", "none"},
		},
		{
			name:          "slack generates one companion route",
			cred:          clawv1alpha1.CredentialSpec{Channel: "slack"},
			wantDomains:   []string{".slack.com"},
			wantInjectors: []string{"none"},
		},
		{
			name:          "whatsapp generates five companion routes",
			cred:          clawv1alpha1.CredentialSpec{Channel: "whatsapp"},
			wantDomains:   []string{".whatsapp.com", ".whatsapp.net", ".facebook.com", ".facebook.net", ".fbcdn.net"},
			wantInjectors: []string{"none", "none", "none", "none", "none"},
		},
		{
			name: "telegram generates no companion routes",
			cred: clawv1alpha1.CredentialSpec{Channel: "telegram"},
		},
		{
			name: "no channel generates no companion routes",
			cred: clawv1alpha1.CredentialSpec{Type: clawv1alpha1.CredentialTypeBearer, Domain: "api.example.com"},
		},
		{
			name: "unknown channel generates no companion routes",
			cred: clawv1alpha1.CredentialSpec{Channel: "irc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := generateCompanionRoutes(tt.cred)
			if len(tt.wantDomains) == 0 {
				assert.Empty(t, routes)
				return
			}
			require.Len(t, routes, len(tt.wantDomains))
			for i, r := range routes {
				assert.Equal(t, tt.wantDomains[i], r.Domain)
				assert.Equal(t, tt.wantInjectors[i], r.Injector)
			}
		})
	}
}

func TestBuildChannelConfig(t *testing.T) {
	t.Run("telegram builds base config with enabled and botToken", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{Name: "tg", Channel: "telegram"}
		config, err := buildChannelConfig(cred)
		require.NoError(t, err)
		assert.Equal(t, true, config["enabled"])
		assert.Equal(t, "placeholder", config["botToken"])
	})

	t.Run("discord builds base config with enabled and botToken", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{Name: "dc", Channel: "discord"}
		config, err := buildChannelConfig(cred)
		require.NoError(t, err)
		assert.Equal(t, true, config["enabled"])
		assert.Equal(t, "placeholder", config["botToken"])
	})

	t.Run("slack builds base config with both tokens", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{Name: "sl", Channel: "slack"}
		config, err := buildChannelConfig(cred)
		require.NoError(t, err)
		assert.Equal(t, true, config["enabled"])
		assert.Equal(t, "xoxb-placeholder", config["botToken"])
		assert.Equal(t, "xapp-placeholder", config["appToken"])
	})

	t.Run("whatsapp builds minimal config with enabled only", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{Name: "wa", Channel: "whatsapp"}
		config, err := buildChannelConfig(cred)
		require.NoError(t, err)
		assert.Equal(t, true, config["enabled"])
		assert.Len(t, config, 1)
	})

	t.Run("channelConfig is deep-merged into base config", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "tg",
			Channel: "telegram",
			ChannelConfig: &runtime.RawExtension{
				Raw: []byte(`{"dmPolicy":"allowlist","allowFrom":[12345]}`),
			},
		}
		config, err := buildChannelConfig(cred)
		require.NoError(t, err)
		assert.Equal(t, true, config["enabled"])
		assert.Equal(t, "placeholder", config["botToken"])
		assert.Equal(t, "allowlist", config["dmPolicy"])
		allowFrom, ok := config["allowFrom"].([]any)
		require.True(t, ok)
		assert.Len(t, allowFrom, 1)
	})

	t.Run("protected key enabled is rejected", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "tg",
			Channel: "telegram",
			ChannelConfig: &runtime.RawExtension{
				Raw: []byte(`{"enabled":false}`),
			},
		}
		_, err := buildChannelConfig(cred)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operator-managed")
		assert.Contains(t, err.Error(), "enabled")
	})

	t.Run("protected key botToken is rejected", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "tg",
			Channel: "telegram",
			ChannelConfig: &runtime.RawExtension{
				Raw: []byte(`{"botToken":"my-real-token"}`),
			},
		}
		_, err := buildChannelConfig(cred)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "botToken")
	})

	t.Run("unknown channel returns error", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{Name: "irc", Channel: "irc"}
		_, err := buildChannelConfig(cred)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown channel")
	})
}

func TestDeepMergeMap(t *testing.T) {
	t.Run("scalars are overwritten", func(t *testing.T) {
		dst := map[string]any{"a": 1, "b": 2}
		src := map[string]any{"b": 3}
		result := deepMergeMap(dst, src)
		assert.Equal(t, 1, result["a"])
		assert.Equal(t, 3, result["b"])
	})

	t.Run("arrays are replaced wholesale", func(t *testing.T) {
		dst := map[string]any{"items": []any{1, 2, 3}}
		src := map[string]any{"items": []any{4, 5}}
		result := deepMergeMap(dst, src)
		assert.Equal(t, []any{4, 5}, result["items"])
	})

	t.Run("objects are deep-merged recursively", func(t *testing.T) {
		dst := map[string]any{
			"nested": map[string]any{"a": 1, "b": 2},
		}
		src := map[string]any{
			"nested": map[string]any{"b": 3, "c": 4},
		}
		result := deepMergeMap(dst, src)
		nested := result["nested"].(map[string]any)
		assert.Equal(t, 1, nested["a"])
		assert.Equal(t, 3, nested["b"])
		assert.Equal(t, 4, nested["c"])
	})

	t.Run("new keys from src are added", func(t *testing.T) {
		dst := map[string]any{"a": 1}
		src := map[string]any{"b": 2}
		result := deepMergeMap(dst, src)
		assert.Equal(t, 1, result["a"])
		assert.Equal(t, 2, result["b"])
	})

	t.Run("original maps are not mutated", func(t *testing.T) {
		dst := map[string]any{"a": 1}
		src := map[string]any{"b": 2}
		_ = deepMergeMap(dst, src)
		assert.Len(t, dst, 1)
		assert.Len(t, src, 1)
	})
}

func TestInjectChannelsIntoConfigMap(t *testing.T) {
	t.Run("injects single channel into operator.json", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials([]clawv1alpha1.CredentialSpec{
			{Name: "tg", Channel: "telegram", Type: clawv1alpha1.CredentialTypePathToken,
				Domain: "api.telegram.org", PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"}},
		})
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectChannelsIntoConfigMap(objects, instance))

		operatorJSON := extractOperatorJSON(t, objects, instance.Name)
		channels, ok := operatorJSON["channels"].(map[string]any)
		require.True(t, ok, "channels should be present in operator.json")
		tg, ok := channels["telegram"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, tg["enabled"])
		assert.Equal(t, "placeholder", tg["botToken"])

		plugins, ok := operatorJSON["plugins"].(map[string]any)
		require.True(t, ok)
		entries, ok := plugins["entries"].(map[string]any)
		require.True(t, ok)
		tgPlugin, ok := entries["telegram"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, tgPlugin["enabled"])
	})

	t.Run("injects multiple channels into operator.json", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials([]clawv1alpha1.CredentialSpec{
			{Name: "tg", Channel: "telegram", Type: clawv1alpha1.CredentialTypePathToken,
				Domain: "api.telegram.org", PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"}},
			{Name: "wa", Channel: "whatsapp", Type: clawv1alpha1.CredentialTypeNone},
		})
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectChannelsIntoConfigMap(objects, instance))

		operatorJSON := extractOperatorJSON(t, objects, instance.Name)
		channels := operatorJSON["channels"].(map[string]any)
		assert.Contains(t, channels, "telegram")
		assert.Contains(t, channels, "whatsapp")
	})

	t.Run("skips injection when no channels are present", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectChannelsIntoConfigMap(objects, instance))

		operatorJSON := extractOperatorJSON(t, objects, instance.Name)
		_, hasChannels := operatorJSON["channels"]
		assert.False(t, hasChannels, "channels should not be injected when no channel credentials exist")
	})

	t.Run("channelConfig is merged into channel config block", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials([]clawv1alpha1.CredentialSpec{
			{
				Name:      "tg",
				Channel:   "telegram",
				Type:      clawv1alpha1.CredentialTypePathToken,
				Domain:    "api.telegram.org",
				PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"},
				ChannelConfig: &runtime.RawExtension{
					Raw: []byte(`{"dmPolicy":"allowlist","allowFrom":[12345]}`),
				},
			},
		})
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectChannelsIntoConfigMap(objects, instance))

		operatorJSON := extractOperatorJSON(t, objects, instance.Name)
		channels := operatorJSON["channels"].(map[string]any)
		tg := channels["telegram"].(map[string]any)
		assert.Equal(t, true, tg["enabled"])
		assert.Equal(t, "placeholder", tg["botToken"])
		assert.Equal(t, "allowlist", tg["dmPolicy"])
	})

	t.Run("protected key in channelConfig returns error", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials([]clawv1alpha1.CredentialSpec{
			{
				Name:      "tg",
				Channel:   "telegram",
				Type:      clawv1alpha1.CredentialTypePathToken,
				Domain:    "api.telegram.org",
				PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"},
				ChannelConfig: &runtime.RawExtension{
					Raw: []byte(`{"enabled":false}`),
				},
			},
		})
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		err = injectChannelsIntoConfigMap(objects, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operator-managed")
	})
}

func TestGenerateProxyConfigWithChannelCompanions(t *testing.T) {
	t.Run("discord channel generates primary route plus companion routes", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "dc",
			Channel: "discord",
			Type:    clawv1alpha1.CredentialTypeAPIKey,
			Domain:  "discord.com",
			APIKey:  &clawv1alpha1.APIKeyConfig{Header: "Authorization", ValuePrefix: "Bot "},
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "discord-secret", Key: "token"},
			},
		}

		data, err := generateProxyConfig(toResolved([]clawv1alpha1.CredentialSpec{cred}))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		primaryRoute := findRouteByDomain(t, cfg.Routes, "discord.com")
		assert.Equal(t, "api_key", primaryRoute.Injector)

		gatewayRoute := findRouteByDomain(t, cfg.Routes, "gateway.discord.gg")
		assert.Equal(t, "none", gatewayRoute.Injector)

		cdnRoute := findRouteByDomain(t, cfg.Routes, "cdn.discordapp.com")
		assert.Equal(t, "none", cdnRoute.Injector)
	})

	t.Run("whatsapp channel generates only companion routes (no primary domain)", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "wa",
			Channel: "whatsapp",
			Type:    clawv1alpha1.CredentialTypeNone,
		}

		data, err := generateProxyConfig(toResolved([]clawv1alpha1.CredentialSpec{cred}))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		for _, route := range cfg.Routes {
			assert.NotEmpty(t, route.Domain, "no route should have an empty domain")
		}

		whatsappCom := findRouteByDomain(t, cfg.Routes, ".whatsapp.com")
		assert.Equal(t, "none", whatsappCom.Injector)

		whatsappNet := findRouteByDomain(t, cfg.Routes, ".whatsapp.net")
		assert.Equal(t, "none", whatsappNet.Injector)
	})

	t.Run("telegram channel generates no companion routes", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:      "tg",
			Channel:   "telegram",
			Type:      clawv1alpha1.CredentialTypePathToken,
			Domain:    "api.telegram.org",
			PathToken: &clawv1alpha1.PathTokenConfig{Prefix: "/bot"},
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "tg-secret", Key: "token"},
			},
		}

		data, err := generateProxyConfig(toResolved([]clawv1alpha1.CredentialSpec{cred}))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		primaryRoute := findRouteByDomain(t, cfg.Routes, "api.telegram.org")
		assert.Equal(t, "path_token", primaryRoute.Injector)

		expectedCount := 1 + len(builtinPassthroughDomains)
		assert.Len(t, cfg.Routes, expectedCount, "should only have primary route + builtins")
	})
}

func TestChannelSecretRefHelpers(t *testing.T) {
	t.Run("primarySecret returns first entry", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "my-secret", Key: "token"},
			},
		}
		ref := primarySecret(cred)
		require.NotNil(t, ref)
		assert.Equal(t, "my-secret", ref.Name)
		assert.Equal(t, "token", ref.Key)
	})

	t.Run("primarySecret returns nil for empty slice", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{}
		ref := primarySecret(cred)
		assert.Nil(t, ref)
	})

	t.Run("secretForRole returns matching entry", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "slack-secret", Key: "bot-token", Role: "botToken"},
				{Name: "slack-secret", Key: "app-token", Role: "appToken"},
			},
		}
		bot := secretForRole(cred, "botToken")
		require.NotNil(t, bot)
		assert.Equal(t, "bot-token", bot.Key)

		app := secretForRole(cred, "appToken")
		require.NotNil(t, app)
		assert.Equal(t, "app-token", app.Key)
	})

	t.Run("secretForRole returns nil when no match", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "s", Key: "k", Role: "botToken"},
			},
		}
		ref := secretForRole(cred, "appToken")
		assert.Nil(t, ref)
	})

	t.Run("referencesSecret matches any entry", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "secret-a", Key: "k"},
				{Name: "secret-b", Key: "k"},
			},
		}
		assert.True(t, referencesSecret(cred, "secret-a"))
		assert.True(t, referencesSecret(cred, "secret-b"))
		assert.False(t, referencesSecret(cred, "secret-c"))
	})

	t.Run("referencesSecret returns false for empty slice", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{}
		assert.False(t, referencesSecret(cred, "any-secret"))
	})

	t.Run("proxySecretForCredential picks botToken for slack channel", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "slack",
			Channel: "slack",
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "slack-secret", Key: "app-token", Role: "appToken"},
				{Name: "slack-secret", Key: "bot-token", Role: "botToken"},
			},
		}
		ref := proxySecretForCredential(cred)
		require.NotNil(t, ref)
		assert.Equal(t, "bot-token", ref.Key)
		assert.Equal(t, "botToken", ref.Role)
	})

	t.Run("proxySecretForCredential falls back to first entry for single-secret channel", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name:    "tg",
			Channel: "telegram",
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "tg-secret", Key: "token"},
			},
		}
		ref := proxySecretForCredential(cred)
		require.NotNil(t, ref)
		assert.Equal(t, "token", ref.Key)
	})

	t.Run("proxySecretForCredential falls back to first entry for non-channel credential", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name: "gemini",
			SecretRef: []clawv1alpha1.SecretRefEntry{
				{Name: "gemini-secret", Key: "api-key"},
			},
		}
		ref := proxySecretForCredential(cred)
		require.NotNil(t, ref)
		assert.Equal(t, "api-key", ref.Key)
	})
}

// --- Integration tests: channel credentials through envtest reconciler ---

func TestChannelCredentialReconciliation(t *testing.T) {
	ctx := context.Background()

	t.Run("should reconcile telegram channel credential end-to-end", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: "tg-secret", Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		secret := createTestAPIKeySecret("tg-secret", namespace, "token", "test-bot-token")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:    "telegram",
				Channel: "telegram",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "tg-secret", Key: "token"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "gateway ConfigMap should be created")

		var operatorConfig map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &operatorConfig))
		channels, ok := operatorConfig["channels"].(map[string]any)
		require.True(t, ok, "channels block should be present in operator.json")
		tg, ok := channels["telegram"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, tg["enabled"])
		assert.Equal(t, "placeholder", tg["botToken"])

		proxyCM := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, proxyCM) == nil
		}, "proxy config ConfigMap should be created")

		var proxyCfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(proxyCM.Data["proxy-config.json"]), &proxyCfg))
		route := findRouteByDomain(t, proxyCfg.Routes, "api.telegram.org")
		assert.Equal(t, "path_token", route.Injector)
		assert.Equal(t, "/bot", route.PathPrefix)
	})

	t.Run("should reconcile discord channel with companion routes in proxy config", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: "dc-secret", Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		secret := createTestAPIKeySecret("dc-secret", namespace, "token", "test-discord-token")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:    "discord",
				Channel: "discord",
				SecretRef: []clawv1alpha1.SecretRefEntry{
					{Name: "dc-secret", Key: "token"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		proxyCM := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, proxyCM) == nil
		}, "proxy config ConfigMap should be created")

		var proxyCfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(proxyCM.Data["proxy-config.json"]), &proxyCfg))

		primaryRoute := findRouteByDomain(t, proxyCfg.Routes, "discord.com")
		assert.Equal(t, "api_key", primaryRoute.Injector)
		assert.Equal(t, "Authorization", primaryRoute.Header)
		assert.Equal(t, "Bot ", primaryRoute.ValuePrefix)

		gatewayRoute := findRouteByDomain(t, proxyCfg.Routes, "gateway.discord.gg")
		assert.Equal(t, "none", gatewayRoute.Injector)

		cdnRoute := findRouteByDomain(t, proxyCfg.Routes, "cdn.discordapp.com")
		assert.Equal(t, "none", cdnRoute.Injector)
	})

	t.Run("should set CredentialsResolved=False for unknown channel", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:    "irc",
				Channel: "irc",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown channel")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updated))

		var credFound bool
		for _, c := range updated.Status.Conditions {
			if c.Type == clawv1alpha1.ConditionTypeCredentialsResolved {
				credFound = true
				assert.Equal(t, "False", string(c.Status))
				assert.Equal(t, clawv1alpha1.ConditionReasonValidationFailed, c.Reason)
				assert.Contains(t, c.Message, "unknown channel")
			}
		}
		assert.True(t, credFound, "CredentialsResolved=False should be set for unknown channel")
	})

	t.Run("should reconcile whatsapp channel with no secret required", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:    "wa",
				Channel: "whatsapp",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		proxyCM := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, proxyCM) == nil
		}, "proxy config ConfigMap should be created")

		var proxyCfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(proxyCM.Data["proxy-config.json"]), &proxyCfg))

		whatsappCom := findRouteByDomain(t, proxyCfg.Routes, ".whatsapp.com")
		assert.Equal(t, "none", whatsappCom.Injector)

		whatsappNet := findRouteByDomain(t, proxyCfg.Routes, ".whatsapp.net")
		assert.Equal(t, "none", whatsappNet.Injector)
	})
}

// extractOperatorJSON is a test helper that parses operator.json from the gateway ConfigMap.
func extractOperatorJSON(t *testing.T, objects []*unstructured.Unstructured, instanceName string) map[string]any {
	t.Helper()
	configMapName := getConfigMapName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != ConfigMapKind || obj.GetName() != configMapName {
			continue
		}
		operatorJSON, found, err := unstructured.NestedString(obj.Object, "data", "operator.json")
		require.NoError(t, err)
		require.True(t, found, "operator.json should be present in ConfigMap")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))
		return config
	}
	t.Fatalf("ConfigMap %q not found in objects", configMapName)
	return nil
}
