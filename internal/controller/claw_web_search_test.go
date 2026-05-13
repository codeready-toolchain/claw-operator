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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestValidateWebSearchConfig(t *testing.T) {
	ctx := context.Background()

	t.Run("brave with valid secret succeeds", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret("brave-key", namespace, "api-key", "test-brave-key")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "brave",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "brave-key", Key: "api-key"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		err := reconciler.validateWebSearchConfig(ctx, instance)
		require.NoError(t, err)
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("brave with missing secret fails", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "brave",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "nonexistent", Key: "api-key"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		err := reconciler.validateWebSearchConfig(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
	})

	t.Run("brave with wrong key fails", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret("brave-key2", namespace, "api-key", "val")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "brave",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "brave-key2", Key: "wrong-key"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		err := reconciler.validateWebSearchConfig(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `key "wrong-key" not found`)
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
	})

	t.Run("tavily with valid secret succeeds", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret("tavily-key", namespace, "api-key", "test-tavily-key")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "tavily",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "tavily-key", Key: "api-key"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		require.NoError(t, reconciler.validateWebSearchConfig(ctx, instance))
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("duckduckgo needs no secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider: "duckduckgo",
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		require.NoError(t, reconciler.validateWebSearchConfig(ctx, instance))
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("gemini with google credential succeeds", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret("google-api-key", namespace, "api-key", "test-google-key")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{Name: "google", Provider: "google", Type: clawv1alpha1.CredentialTypeAPIKey,
						Domain:    ".googleapis.com",
						SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "google-api-key", Key: "api-key"}}},
				},
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider: "gemini",
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		require.NoError(t, reconciler.validateWebSearchConfig(ctx, instance))
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("gemini without google credential fails", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider: "gemini",
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		err := reconciler.validateWebSearchConfig(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `requires a "google" credential`)
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
	})

	t.Run("unknown provider fails", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret("bogus-key", namespace, "api-key", "val")
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "bogus",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "bogus-key", Key: "api-key"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		err := reconciler.validateWebSearchConfig(ctx, instance)
		require.Error(t, err)
		assert.Equal(t, `unknown web search provider "bogus"`, err.Error())
	})
}

func TestInjectWebSearchIntoConfigMap(t *testing.T) {
	t.Run("brave sets tools.web.search and plugins entry with placeholder", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "brave",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
				},
			},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)

		tools, ok := config["tools"].(map[string]any)
		require.True(t, ok)
		web, ok := tools["web"].(map[string]any)
		require.True(t, ok)
		search, ok := web["search"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, search["enabled"])
		assert.Equal(t, "brave", search["provider"])

		plugins, ok := config["plugins"].(map[string]any)
		require.True(t, ok)
		entries, ok := plugins["entries"].(map[string]any)
		require.True(t, ok)
		braveEntry, ok := entries["brave"].(map[string]any)
		require.True(t, ok)
		braveConfig, ok := braveEntry["config"].(map[string]any)
		require.True(t, ok)
		webSearch, ok := braveConfig["webSearch"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, placeholderAPIKey, webSearch["apiKey"])
	})

	t.Run("tavily with custom config merges into plugin entry", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "tavily",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
					Config:    &runtime.RawExtension{Raw: []byte(`{"maxResults":10}`)},
				},
			},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)
		plugins := config["plugins"].(map[string]any)
		entries := plugins["entries"].(map[string]any)
		tavilyEntry := entries["tavily"].(map[string]any)
		tavilyConfig := tavilyEntry["config"].(map[string]any)
		webSearch := tavilyConfig["webSearch"].(map[string]any)
		assert.Equal(t, placeholderAPIKey, webSearch["apiKey"])
		assert.Equal(t, float64(10), webSearch["maxResults"])
	})

	t.Run("duckduckgo sets search provider with no plugin entry", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider: "duckduckgo",
				},
			},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)
		tools := config["tools"].(map[string]any)
		web := tools["web"].(map[string]any)
		search := web["search"].(map[string]any)
		assert.Equal(t, "duckduckgo", search["provider"])

		// No plugin entry for duckduckgo (key-free)
		_, hasPlugins := config["plugins"]
		assert.False(t, hasPlugins)
	})

	t.Run("gemini sets search provider under google plugin entry", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider: "gemini",
					Config:   &runtime.RawExtension{Raw: []byte(`{"model":"gemini-2.0-flash"}`)},
				},
			},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)
		tools := config["tools"].(map[string]any)
		web := tools["web"].(map[string]any)
		search := web["search"].(map[string]any)
		assert.Equal(t, "gemini", search["provider"])

		plugins := config["plugins"].(map[string]any)
		entries := plugins["entries"].(map[string]any)
		googleEntry := entries["google"].(map[string]any)
		googleConfig := googleEntry["config"].(map[string]any)
		webSearch := googleConfig["webSearch"].(map[string]any)
		assert.Equal(t, "gemini-2.0-flash", webSearch["model"])
		_, hasAPIKey := webSearch["apiKey"]
		assert.False(t, hasAPIKey, "gemini should not have placeholder apiKey")
	})

	t.Run("webFetch sets tools.web.fetch.enabled", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebFetch: &clawv1alpha1.WebFetchSpec{Enabled: true},
			},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)
		tools := config["tools"].(map[string]any)
		web := tools["web"].(map[string]any)
		fetch := web["fetch"].(map[string]any)
		assert.Equal(t, true, fetch["enabled"])
	})

	t.Run("no-op when neither webSearch nor webFetch set", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)
		_, hasTools := config["tools"]
		assert.False(t, hasTools, "tools should not be added when web search/fetch are nil")
	})
}

func TestGenerateProxyConfigWithWebSearch(t *testing.T) {
	t.Run("brave adds api_key route with custom header", func(t *testing.T) {
		ws := &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
		}
		data, err := generateProxyConfig(nil, nil, ws)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		route := findRouteByDomain(t, cfg.Routes, "api.search.brave.com")
		assert.Equal(t, "api_key", route.Injector)
		assert.Equal(t, "X-Subscription-Token", route.Header)
		assert.Equal(t, "CRED_WEBSEARCH", route.EnvVar)
	})

	t.Run("tavily adds bearer route", func(t *testing.T) {
		ws := &clawv1alpha1.WebSearchSpec{
			Provider:  "tavily",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
		}
		data, err := generateProxyConfig(nil, nil, ws)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		route := findRouteByDomain(t, cfg.Routes, "api.tavily.com")
		assert.Equal(t, "bearer", route.Injector)
		assert.Equal(t, "CRED_WEBSEARCH", route.EnvVar)
	})

	t.Run("duckduckgo adds passthrough route", func(t *testing.T) {
		ws := &clawv1alpha1.WebSearchSpec{Provider: "duckduckgo"}
		data, err := generateProxyConfig(nil, nil, ws)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		route := findRouteByDomain(t, cfg.Routes, "html.duckduckgo.com")
		assert.Equal(t, "none", route.Injector)
		assert.Empty(t, route.EnvVar)
	})

	t.Run("gemini adds no new route", func(t *testing.T) {
		ws := &clawv1alpha1.WebSearchSpec{Provider: "gemini"}
		data, err := generateProxyConfig(nil, nil, ws)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		for _, route := range cfg.Routes {
			assert.NotContains(t, route.Domain, "gemini",
				"gemini should not add its own route")
		}
	})

	t.Run("nil webSearch adds no search route", func(t *testing.T) {
		data, err := generateProxyConfig(nil, nil, nil)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		for _, route := range cfg.Routes {
			assert.NotEqual(t, "api.search.brave.com", route.Domain)
			assert.NotEqual(t, "api.tavily.com", route.Domain)
			assert.NotEqual(t, "html.duckduckgo.com", route.Domain)
		}
	})

	t.Run("skips web search route when credential already covers domain", func(t *testing.T) {
		creds := []resolvedCredential{{
			CredentialSpec: clawv1alpha1.CredentialSpec{
				Name:   "brave-cred",
				Domain: "api.search.brave.com",
				Type:   clawv1alpha1.CredentialTypeBearer,
			},
		}}
		ws := &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
		}
		data, err := generateProxyConfig(creds, nil, ws)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		var count int
		for _, route := range cfg.Routes {
			if route.Domain == "api.search.brave.com" {
				count++
			}
		}
		assert.Equal(t, 1, count, "domain should appear exactly once (from credential, not web search)")
	})

	t.Run("skips web search route when suffix credential covers domain", func(t *testing.T) {
		creds := []resolvedCredential{{
			CredentialSpec: clawv1alpha1.CredentialSpec{
				Name:   "brave-wildcard",
				Domain: ".brave.com",
				Type:   clawv1alpha1.CredentialTypeBearer,
			},
		}}
		ws := &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
		}
		data, err := generateProxyConfig(creds, nil, ws)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		for _, route := range cfg.Routes {
			assert.NotEqual(t, "api.search.brave.com", route.Domain,
				"web search route should be skipped when suffix credential covers the domain")
		}
	})
}

func TestConfigureProxyForWebSearch(t *testing.T) {
	buildObjects := func(t *testing.T) (*clawv1alpha1.Claw, []*unstructured.Unstructured) {
		t.Helper()
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)
		return instance, objects
	}

	findProxyEnvVars := func(t *testing.T, objects []*unstructured.Unstructured) []any {
		t.Helper()
		for _, obj := range objects {
			if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(testInstanceName) {
				continue
			}
			containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			for _, c := range containers {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawProxyContainerName {
					envVars, _, _ := unstructured.NestedSlice(cm, "env")
					return envVars
				}
			}
		}
		t.Fatal("proxy container not found")
		return nil
	}

	hasEnvVar := func(envVars []any, name string) bool {
		for _, e := range envVars {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if n, _, _ := unstructured.NestedString(em, "name"); n == name {
				return true
			}
		}
		return false
	}

	t.Run("adds CRED_WEBSEARCH for brave", func(t *testing.T) {
		instance, objects := buildObjects(t)
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "brave-key", Key: "api-key"},
		}

		require.NoError(t, configureProxyForWebSearch(objects, instance))

		envVars := findProxyEnvVars(t, objects)
		assert.True(t, hasEnvVar(envVars, "CRED_WEBSEARCH"), "CRED_WEBSEARCH should be added")
	})

	t.Run("adds CRED_WEBSEARCH for tavily", func(t *testing.T) {
		instance, objects := buildObjects(t)
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider:  "tavily",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "tavily-key", Key: "api-key"},
		}

		require.NoError(t, configureProxyForWebSearch(objects, instance))

		envVars := findProxyEnvVars(t, objects)
		assert.True(t, hasEnvVar(envVars, "CRED_WEBSEARCH"), "CRED_WEBSEARCH should be added")
	})

	t.Run("no env var for duckduckgo", func(t *testing.T) {
		instance, objects := buildObjects(t)
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider: "duckduckgo",
		}

		require.NoError(t, configureProxyForWebSearch(objects, instance))

		envVars := findProxyEnvVars(t, objects)
		assert.False(t, hasEnvVar(envVars, "CRED_WEBSEARCH"), "duckduckgo should not add CRED_WEBSEARCH")
	})

	t.Run("no env var for gemini", func(t *testing.T) {
		instance, objects := buildObjects(t)
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider: "gemini",
		}

		require.NoError(t, configureProxyForWebSearch(objects, instance))

		envVars := findProxyEnvVars(t, objects)
		assert.False(t, hasEnvVar(envVars, "CRED_WEBSEARCH"), "gemini should not add CRED_WEBSEARCH")
	})

	t.Run("no-op when webSearch is nil", func(t *testing.T) {
		instance, objects := buildObjects(t)
		require.NoError(t, configureProxyForWebSearch(objects, instance))

		envVars := findProxyEnvVars(t, objects)
		assert.False(t, hasEnvVar(envVars, "CRED_WEBSEARCH"))
	})
}

func TestStampSecretVersionWithWebSearch(t *testing.T) {
	ctx := context.Background()

	t.Run("stamps web search secret ResourceVersion", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret("ws-secret", namespace, "api-key", "brave-key-val")
		require.NoError(t, k8sClient.Create(ctx, secret))

		// Re-read to get ResourceVersion
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "ws-secret", Namespace: namespace}, secret))

		createClawInstance(t, ctx, testInstanceName, namespace)

		// Update the Claw instance with webSearch
		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "ws-secret", Key: "api-key"},
		}
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))

		annotations := deployment.Spec.Template.Annotations
		require.NotNil(t, annotations)
		wsKey := clawv1alpha1.AnnotationPrefixSecretVersion + webSearchCredPrefix + clawv1alpha1.AnnotationSuffixSecretVersion
		rv, ok := annotations[wsKey]
		assert.True(t, ok, "websearch-secret-version annotation should exist")
		assert.Equal(t, secret.ResourceVersion, rv)
	})
}

func TestWebSearchFullReconcile(t *testing.T) {
	ctx := context.Background()

	t.Run("brave web search produces correct proxy config and operator.json", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		braveSecret := createTestAPIKeySecret("brave-search-secret", namespace, "api-key", "brave-val")
		require.NoError(t, k8sClient.Create(ctx, braveSecret))

		createClawInstance(t, ctx, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "brave-search-secret", Key: "api-key"},
		}
		instance.Spec.WebFetch = &clawv1alpha1.WebFetchSpec{Enabled: true}
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify proxy config JSON contains the brave route
		proxyConfigCM := &corev1.ConfigMap{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyConfigMapName(testInstanceName),
			Namespace: namespace,
		}, proxyConfigCM))
		var proxyCfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(proxyConfigCM.Data["proxy-config.json"]), &proxyCfg))
		braveRoute := findRouteByDomain(t, proxyCfg.Routes, "api.search.brave.com")
		assert.Equal(t, "api_key", braveRoute.Injector)
		assert.Equal(t, "X-Subscription-Token", braveRoute.Header)
		assert.Equal(t, "CRED_WEBSEARCH", braveRoute.EnvVar)

		// Verify proxy deployment has CRED_WEBSEARCH env var
		proxyDeploy := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, proxyDeploy))
		var foundEnv bool
		for _, c := range proxyDeploy.Spec.Template.Spec.Containers {
			if c.Name == ClawProxyContainerName {
				for _, e := range c.Env {
					if e.Name == "CRED_WEBSEARCH" {
						foundEnv = true
						require.NotNil(t, e.ValueFrom)
						require.NotNil(t, e.ValueFrom.SecretKeyRef)
						assert.Equal(t, "brave-search-secret", e.ValueFrom.SecretKeyRef.Name)
						assert.Equal(t, "api-key", e.ValueFrom.SecretKeyRef.Key)
					}
				}
			}
		}
		assert.True(t, foundEnv, "CRED_WEBSEARCH env var should exist on proxy container")

		// Verify operator.json has web search and web fetch config
		gwConfigCM := &corev1.ConfigMap{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getConfigMapName(testInstanceName),
			Namespace: namespace,
		}, gwConfigCM))
		var opJSON map[string]any
		require.NoError(t, json.Unmarshal([]byte(gwConfigCM.Data["operator.json"]), &opJSON))
		tools := opJSON["tools"].(map[string]any)
		web := tools["web"].(map[string]any)
		search := web["search"].(map[string]any)
		assert.Equal(t, true, search["enabled"])
		assert.Equal(t, "brave", search["provider"])
		fetch := web["fetch"].(map[string]any)
		assert.Equal(t, true, fetch["enabled"])

		plugins := opJSON["plugins"].(map[string]any)
		entries := plugins["entries"].(map[string]any)
		braveEntry := entries["brave"].(map[string]any)
		braveConfig := braveEntry["config"].(map[string]any)
		webSearch := braveConfig["webSearch"].(map[string]any)
		assert.Equal(t, placeholderAPIKey, webSearch["apiKey"])

		// Verify WebSearchConfigured condition
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("duckduckgo reconcile requires no secret and sets condition", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{Provider: "duckduckgo"}
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify proxy config has duckduckgo passthrough
		proxyConfigCM := &corev1.ConfigMap{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyConfigMapName(testInstanceName),
			Namespace: namespace,
		}, proxyConfigCM))
		var proxyCfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(proxyConfigCM.Data["proxy-config.json"]), &proxyCfg))
		ddgRoute := findRouteByDomain(t, proxyCfg.Routes, "html.duckduckgo.com")
		assert.Equal(t, "none", ddgRoute.Injector)
		assert.Empty(t, ddgRoute.EnvVar)

		// No CRED_WEBSEARCH on proxy deployment
		proxyDeploy := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, proxyDeploy))
		for _, c := range proxyDeploy.Spec.Template.Spec.Containers {
			if c.Name == ClawProxyContainerName {
				for _, e := range c.Env {
					assert.NotEqual(t, "CRED_WEBSEARCH", e.Name, "duckduckgo should not add CRED_WEBSEARCH")
				}
			}
		}

		// Condition should be True
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("condition removed when webSearch is nil", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		cond := apimeta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		assert.Nil(t, cond, "WebSearchConfigured should not be present when webSearch is nil")
	})
}

func TestInjectWebSearchAndFetchCombined(t *testing.T) {
	t.Run("both webSearch and webFetch set together", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "tavily",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
				},
				WebFetch: &clawv1alpha1.WebFetchSpec{Enabled: true},
			},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, injectWebSearchIntoConfigMap(objects, instance))

		config := extractOperatorJSON(t, objects, instance.Name)
		tools := config["tools"].(map[string]any)
		web := tools["web"].(map[string]any)

		search := web["search"].(map[string]any)
		assert.Equal(t, true, search["enabled"])
		assert.Equal(t, "tavily", search["provider"])

		fetch := web["fetch"].(map[string]any)
		assert.Equal(t, true, fetch["enabled"])

		plugins := config["plugins"].(map[string]any)
		entries := plugins["entries"].(map[string]any)
		tavilyEntry := entries["tavily"].(map[string]any)
		tavilyConfig := tavilyEntry["config"].(map[string]any)
		webSearch := tavilyConfig["webSearch"].(map[string]any)
		assert.Equal(t, placeholderAPIKey, webSearch["apiKey"])
	})
}

func TestConfigureProxyForWebSearchSecretRef(t *testing.T) {
	t.Run("secretKeyRef references correct secret name and key", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.WebSearch = &clawv1alpha1.WebSearchSpec{
			Provider:  "brave",
			SecretRef: &clawv1alpha1.SecretRefEntry{Name: "my-brave-secret", Key: "my-key"},
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		require.NoError(t, configureProxyForWebSearch(objects, instance))

		for _, obj := range objects {
			if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(testInstanceName) {
				continue
			}
			containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			for _, c := range containers {
				cm := c.(map[string]any)
				if name, _, _ := unstructured.NestedString(cm, "name"); name != ClawProxyContainerName {
					continue
				}
				envVars, _, _ := unstructured.NestedSlice(cm, "env")
				for _, e := range envVars {
					em := e.(map[string]any)
					if n, _, _ := unstructured.NestedString(em, "name"); n == "CRED_WEBSEARCH" {
						secretName, _, _ := unstructured.NestedString(em, "valueFrom", "secretKeyRef", "name")
						secretKey, _, _ := unstructured.NestedString(em, "valueFrom", "secretKeyRef", "key")
						assert.Equal(t, "my-brave-secret", secretName)
						assert.Equal(t, "my-key", secretKey)
						return
					}
				}
			}
		}
		t.Fatal("CRED_WEBSEARCH env var not found")
	})
}

func TestWebSearchRouteHelper(t *testing.T) {
	t.Run("nil returns false", func(t *testing.T) {
		_, ok := webSearchRoute(nil)
		assert.False(t, ok)
	})

	t.Run("unknown provider returns false", func(t *testing.T) {
		_, ok := webSearchRoute(&clawv1alpha1.WebSearchSpec{Provider: "unknown"})
		assert.False(t, ok)
	})

	t.Run("gemini returns false", func(t *testing.T) {
		_, ok := webSearchRoute(&clawv1alpha1.WebSearchSpec{Provider: "gemini"})
		assert.False(t, ok)
	})

	t.Run("brave returns correct route", func(t *testing.T) {
		route, ok := webSearchRoute(&clawv1alpha1.WebSearchSpec{Provider: "brave"})
		require.True(t, ok)
		assert.Equal(t, "api.search.brave.com", route.Domain)
		assert.Equal(t, "api_key", route.Injector)
		assert.Equal(t, "X-Subscription-Token", route.Header)
		assert.Equal(t, "CRED_WEBSEARCH", route.EnvVar)
	})

	t.Run("tavily returns correct route", func(t *testing.T) {
		route, ok := webSearchRoute(&clawv1alpha1.WebSearchSpec{Provider: "tavily"})
		require.True(t, ok)
		assert.Equal(t, "api.tavily.com", route.Domain)
		assert.Equal(t, "bearer", route.Injector)
		assert.Equal(t, "CRED_WEBSEARCH", route.EnvVar)
		assert.Empty(t, route.Header)
	})

	t.Run("duckduckgo returns passthrough route", func(t *testing.T) {
		route, ok := webSearchRoute(&clawv1alpha1.WebSearchSpec{Provider: "duckduckgo"})
		require.True(t, ok)
		assert.Equal(t, "html.duckduckgo.com", route.Domain)
		assert.Equal(t, "none", route.Injector)
		assert.Empty(t, route.EnvVar)
	})
}

func TestClawReferencesSecretWebSearch(t *testing.T) {
	t.Run("matches web search secret", func(t *testing.T) {
		instance := clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{
					Provider:  "brave",
					SecretRef: &clawv1alpha1.SecretRefEntry{Name: "brave-search-key", Key: "api-key"},
				},
			},
		}
		assert.True(t, clawReferencesSecret(instance, "brave-search-key"))
		assert.False(t, clawReferencesSecret(instance, "other-secret"))
	})

	t.Run("no match when webSearch is nil", func(t *testing.T) {
		instance := clawv1alpha1.Claw{}
		assert.False(t, clawReferencesSecret(instance, "anything"))
	})

	t.Run("no match when secretRef is nil", func(t *testing.T) {
		instance := clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				WebSearch: &clawv1alpha1.WebSearchSpec{Provider: "duckduckgo"},
			},
		}
		assert.False(t, clawReferencesSecret(instance, "anything"))
	})
}
