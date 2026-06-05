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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- deepMerge unit tests ---

func TestDeepMerge(t *testing.T) {
	t.Run("empty override returns base unchanged", func(t *testing.T) {
		base := map[string]any{"a": 1, "b": "hello"}
		result := deepMerge(base, map[string]any{})
		assert.Equal(t, 1, result["a"])
		assert.Equal(t, "hello", result["b"])
	})

	t.Run("empty base returns override", func(t *testing.T) {
		override := map[string]any{"x": 42}
		result := deepMerge(map[string]any{}, override)
		assert.Equal(t, 42, result["x"])
	})

	t.Run("override wins on scalar collision", func(t *testing.T) {
		base := map[string]any{"key": "old"}
		override := map[string]any{"key": "new"}
		result := deepMerge(base, override)
		assert.Equal(t, "new", result["key"])
	})

	t.Run("disjoint keys are combined", func(t *testing.T) {
		base := map[string]any{"a": 1}
		override := map[string]any{"b": 2}
		result := deepMerge(base, override)
		assert.Equal(t, 1, result["a"])
		assert.Equal(t, 2, result["b"])
	})

	t.Run("nested maps are merged recursively", func(t *testing.T) {
		base := map[string]any{
			"gateway": map[string]any{
				"port": 18789,
				"auth": map[string]any{"mode": "token"},
			},
		}
		override := map[string]any{
			"gateway": map[string]any{
				"auth": map[string]any{"mode": "password"},
				"cors": map[string]any{"origin": "*"},
			},
		}
		result := deepMerge(base, override)
		gw := result["gateway"].(map[string]any)
		assert.Equal(t, 18789, gw["port"], "base-only key preserved")
		assert.Equal(t, "password", gw["auth"].(map[string]any)["mode"], "override wins")
		assert.Equal(t, "*", gw["cors"].(map[string]any)["origin"], "new key added")
	})

	t.Run("slices are replaced not merged", func(t *testing.T) {
		base := map[string]any{"origins": []any{"a", "b"}}
		override := map[string]any{"origins": []any{"x"}}
		result := deepMerge(base, override)
		assert.Equal(t, []any{"x"}, result["origins"])
	})

	t.Run("override map replaces base scalar", func(t *testing.T) {
		base := map[string]any{"key": "scalar"}
		override := map[string]any{"key": map[string]any{"nested": true}}
		result := deepMerge(base, override)
		assert.Equal(t, map[string]any{"nested": true}, result["key"])
	})

	t.Run("override scalar replaces base map", func(t *testing.T) {
		base := map[string]any{"key": map[string]any{"nested": true}}
		override := map[string]any{"key": "scalar"}
		result := deepMerge(base, override)
		assert.Equal(t, "scalar", result["key"])
	})

	t.Run("neither input is mutated", func(t *testing.T) {
		base := map[string]any{"a": 1}
		override := map[string]any{"b": 2}
		_ = deepMerge(base, override)
		assert.Len(t, base, 1, "base should not be mutated")
		assert.Len(t, override, 1, "override should not be mutated")
	})

	t.Run("three-level deep merge", func(t *testing.T) {
		base := map[string]any{
			"l1": map[string]any{
				"l2": map[string]any{
					"base_key": "preserved",
				},
			},
		}
		override := map[string]any{
			"l1": map[string]any{
				"l2": map[string]any{
					"override_key": "added",
				},
			},
		}
		result := deepMerge(base, override)
		l2 := result["l1"].(map[string]any)["l2"].(map[string]any)
		assert.Equal(t, "preserved", l2["base_key"])
		assert.Equal(t, "added", l2["override_key"])
	})
}

// --- parseUserRawConfig unit tests ---

func TestParseUserRawConfig(t *testing.T) {
	t.Run("returns empty map when config is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		result, err := parseUserRawConfig(instance)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("returns empty map when raw is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Config: &clawv1alpha1.ConfigSpec{},
			},
		}
		result, err := parseUserRawConfig(instance)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("returns empty map when raw bytes are empty", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{Raw: []byte{}},
					},
				},
			},
		}
		result, err := parseUserRawConfig(instance)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("parses valid JSON", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{"diagnostics":{"otel":{"enabled":true}}}`),
						},
					},
				},
			},
		}
		result, err := parseUserRawConfig(instance)
		require.NoError(t, err)
		diag := result["diagnostics"].(map[string]any)
		otel := diag["otel"].(map[string]any)
		assert.Equal(t, true, otel["enabled"])
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{Raw: []byte(`{invalid}`)},
					},
				},
			},
		}
		_, err := parseUserRawConfig(instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse spec.config.raw")
	})
}

// --- enforceInfrastructureKeys unit tests ---

func TestEnforceInfrastructureKeys(t *testing.T) {
	t.Run("sets infrastructure values on empty config", func(t *testing.T) {
		config := map[string]any{}
		enforceInfrastructureKeys(config)

		gw := config["gateway"].(map[string]any)
		assert.Equal(t, "local", gw["mode"])
		assert.Equal(t, "lan", gw["bind"])
		assert.Equal(t, float64(18789), gw["port"])
		assert.Equal(t, true, gw["controlUi"].(map[string]any)["enabled"])
	})

	t.Run("overrides user-set infrastructure values", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{
				"mode":      "remote",
				"bind":      "0.0.0.0",
				"port":      float64(9999),
				"controlUi": map[string]any{"enabled": false, "theme": "dark"},
			},
		}
		enforceInfrastructureKeys(config)

		gw := config["gateway"].(map[string]any)
		assert.Equal(t, "local", gw["mode"])
		assert.Equal(t, "lan", gw["bind"])
		assert.Equal(t, float64(18789), gw["port"])
		controlUI := gw["controlUi"].(map[string]any)
		assert.Equal(t, true, controlUI["enabled"])
		assert.Equal(t, "dark", controlUI["theme"], "non-infra keys preserved")
	})
}

// --- enforceTrustedProxies unit tests ---

func TestEnforceTrustedProxies(t *testing.T) {
	t.Run("adds RFC1918 ranges to empty config", func(t *testing.T) {
		config := map[string]any{}
		enforceTrustedProxies(config)

		proxies := getStringSlice(config, "gateway", "trustedProxies")
		assert.Contains(t, proxies, "10.0.0.0/8")
		assert.Contains(t, proxies, "172.16.0.0/12")
		assert.Len(t, proxies, 2)
	})

	t.Run("appends to user-provided ranges without duplicating", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{
				"trustedProxies": []any{"192.168.0.0/16", "10.0.0.0/8"},
			},
		}
		enforceTrustedProxies(config)

		proxies := getStringSlice(config, "gateway", "trustedProxies")
		assert.Contains(t, proxies, "192.168.0.0/16")
		assert.Contains(t, proxies, "10.0.0.0/8")
		assert.Contains(t, proxies, "172.16.0.0/12")
		assert.Len(t, proxies, 3, "10.0.0.0/8 should not be duplicated")
	})

	t.Run("deduplicates when all ranges already present", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{
				"trustedProxies": []any{"10.0.0.0/8", "172.16.0.0/12"},
			},
		}
		enforceTrustedProxies(config)

		proxies := getStringSlice(config, "gateway", "trustedProxies")
		assert.Len(t, proxies, 2)
	})
}

// --- disableUpdateCheck unit tests ---

func TestDisableUpdateCheck(t *testing.T) {
	t.Run("sets checkOnStart false on empty config", func(t *testing.T) {
		config := map[string]any{}
		disableUpdateCheck(config)

		update := config["update"].(map[string]any)
		assert.Equal(t, false, update["checkOnStart"])
	})

	t.Run("overrides user-set checkOnStart true", func(t *testing.T) {
		config := map[string]any{
			"update": map[string]any{
				"checkOnStart": true,
				"channel":      "stable",
			},
		}
		disableUpdateCheck(config)

		update := config["update"].(map[string]any)
		assert.Equal(t, false, update["checkOnStart"])
		assert.Equal(t, "stable", update["channel"], "non-target keys preserved")
	})
}

func TestSkipDefaultPersonality(t *testing.T) {
	t.Run("sets skipOptionalBootstrapFiles on empty config", func(t *testing.T) {
		config := map[string]any{}
		skipDefaultPersonality(config)

		agents := config["agents"].(map[string]any)
		defaults := agents["defaults"].(map[string]any)
		skip := defaults["skipOptionalBootstrapFiles"].([]any)
		assert.ElementsMatch(t, []any{"SOUL.md", "IDENTITY.md"}, skip)
	})

	t.Run("overrides user-set skipOptionalBootstrapFiles", func(t *testing.T) {
		config := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"skipOptionalBootstrapFiles": []any{"USER.md"},
					"model":                      map[string]any{"primary": "test"},
				},
			},
		}
		skipDefaultPersonality(config)

		agents := config["agents"].(map[string]any)
		defaults := agents["defaults"].(map[string]any)
		skip := defaults["skipOptionalBootstrapFiles"].([]any)
		assert.ElementsMatch(t, []any{"SOUL.md", "IDENTITY.md"}, skip)
		assert.Equal(t, map[string]any{"primary": "test"}, defaults["model"], "non-target keys preserved")
	})
}

// --- spec.config.raw full pipeline integration tests ---

func TestSpecConfigRawIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("user-only keys pass through to operator.json", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"diagnostics": {"otel": {"enabled": true}},
								"session": {"timeout": 3600}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		diag := config["diagnostics"].(map[string]any)
		assert.Equal(t, true, diag["otel"].(map[string]any)["enabled"],
			"user-only diagnostics key should pass through")
		session := config["session"].(map[string]any)
		assert.Equal(t, float64(3600), session["timeout"],
			"user-only session key should pass through")
	})

	t.Run("always-win keys override user config", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"gateway": {
									"mode": "remote",
									"bind": "0.0.0.0",
									"port": 9999,
									"auth": {"mode": "password"},
									"controlUi": {"enabled": false}
								}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		gw := config["gateway"].(map[string]any)
		assert.Equal(t, "local", gw["mode"], "gateway.mode is always-win")
		assert.Equal(t, "lan", gw["bind"], "gateway.bind is always-win")
		assert.Equal(t, float64(18789), gw["port"], "gateway.port is always-win")
		assert.Equal(t, true, gw["controlUi"].(map[string]any)["enabled"],
			"controlUi.enabled is always-win")
		assert.Equal(t, "token", gw["auth"].(map[string]any)["mode"],
			"auth.mode is always-win (spec.auth is nil)")
	})

	t.Run("CORS origins append user entries plus route host", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"gateway": {
									"controlUi": {
										"allowedOrigins": ["https://custom.example.com"]
									}
								}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		origins := getStringSlice(config, "gateway", "controlUi", "allowedOrigins")
		assert.Contains(t, origins, "https://custom.example.com",
			"user-provided origin should be present")
		assert.Contains(t, origins, "http://localhost:18789",
			"operator-injected localhost fallback should be appended")
	})

	t.Run("model catalog merges with user models and user primary wins", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"agents": {
									"defaults": {
										"model": {"primary": "openrouter/qwen3-14b"},
										"models": {
											"openrouter/qwen3-14b": {"alias": "Qwen 3 14B"},
											"google/gemini-3.5-flash": {"alias": "My Flash"}
										}
									}
								}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		defaults := config["agents"].(map[string]any)["defaults"].(map[string]any)
		models := defaults["models"].(map[string]any)

		assert.Contains(t, models, "openrouter/qwen3-14b",
			"user-added model should be present")
		flash := models["google/gemini-3.5-flash"].(map[string]any)
		assert.Equal(t, "My Flash", flash["alias"],
			"user alias should win over catalog alias")
		assert.Contains(t, models, "google/gemini-3.1-pro-preview",
			"catalog model not set by user should be added")

		primary := defaults["model"].(map[string]any)["primary"]
		assert.Equal(t, "openrouter/qwen3-14b", primary,
			"user-set primary should win over catalog default")
	})

	t.Run("trusted proxies append user entries plus RFC1918", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"gateway": {
									"trustedProxies": ["192.168.0.0/16"]
								}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		proxies := getStringSlice(config, "gateway", "trustedProxies")
		assert.Contains(t, proxies, "192.168.0.0/16", "user CIDR preserved")
		assert.Contains(t, proxies, "10.0.0.0/8", "RFC1918 range appended")
		assert.Contains(t, proxies, "172.16.0.0/12", "RFC1918 range appended")
	})

	t.Run("nil spec.config produces identical behavior to before", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)

		gw := config["gateway"].(map[string]any)
		assert.Equal(t, "local", gw["mode"])
		assert.Equal(t, "lan", gw["bind"])
		assert.Equal(t, float64(18789), gw["port"])
		assert.Equal(t, "token", gw["auth"].(map[string]any)["mode"])
		assert.Equal(t, true, gw["controlUi"].(map[string]any)["enabled"])

		proxies := getStringSlice(config, "gateway", "trustedProxies")
		assert.Contains(t, proxies, "10.0.0.0/8")
		assert.Contains(t, proxies, "172.16.0.0/12")

		update := config["update"].(map[string]any)
		assert.Equal(t, false, update["checkOnStart"],
			"update.checkOnStart should be disabled by default")

		models := config["models"].(map[string]any)
		assert.NotNil(t, models["providers"])
	})

	t.Run("update check is disabled even if user enables it", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"update": {"checkOnStart": true, "channel": "beta"}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		update := config["update"].(map[string]any)
		assert.Equal(t, false, update["checkOnStart"],
			"update.checkOnStart is always-win false")
		assert.Equal(t, "beta", update["channel"],
			"non-enforced update keys are preserved")
	})

	t.Run("user plugin entries for non-declared keys are preserved", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Config: &clawv1alpha1.ConfigSpec{
					Raw: &clawv1alpha1.RawConfig{
						RawExtension: runtime.RawExtension{
							Raw: []byte(`{
								"plugins": {
									"entries": {
										"my-custom-plugin": {"enabled": true}
									}
								}
							}`),
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		config := getReconciled(t, ctx)
		entries := config["plugins"].(map[string]any)["entries"].(map[string]any)
		plugin := entries["my-custom-plugin"].(map[string]any)
		assert.Equal(t, true, plugin["enabled"],
			"user-managed plugin entry should be preserved")
	})
}

// getReconciled reads operator.json from the reconciled ConfigMap.
func getReconciled(t *testing.T, ctx context.Context) map[string]any {
	t.Helper()
	cm := &corev1.ConfigMap{}
	waitFor(t, timeout, interval, func() bool {
		return k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, cm) == nil
	}, "ConfigMap should be created")

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))
	return config
}
