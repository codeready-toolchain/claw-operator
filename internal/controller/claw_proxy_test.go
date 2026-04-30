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
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func findRouteByDomain(t *testing.T, routes []proxyRoute, domain string) proxyRoute {
	t.Helper()
	for _, r := range routes {
		if r.Domain == domain {
			return r
		}
	}
	t.Fatalf("route with domain %q not found in %d routes", domain, len(routes))
	return proxyRoute{}
}

// --- Proxy CA tests ---

func TestClawProxyCA(t *testing.T) {
	ctx := context.Background()

	t.Run("should create proxy CA Secret on first reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyCAConfigMapName(testInstanceName),
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")

		assert.Contains(t, secret.Data, "ca.crt")
		assert.Contains(t, secret.Data, "ca.key")
	})

	t.Run("should create valid X.509 CA certificate", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyCAConfigMapName(testInstanceName),
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")

		block, _ := pem.Decode(secret.Data["ca.crt"])
		require.NotNil(t, block, "ca.crt should be valid PEM")

		cert, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err, "ca.crt should be valid X.509")
		assert.True(t, cert.IsCA, "certificate should be a CA")
		assert.Equal(t, "Claw Proxy CA", cert.Subject.CommonName)
	})

	t.Run("should not regenerate CA on subsequent reconciliations", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyCAConfigMapName(testInstanceName),
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")
		initialCert := string(secret.Data["ca.crt"])

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		secret2 := &corev1.Secret{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyCAConfigMapName(testInstanceName),
			Namespace: namespace,
		}, secret2))
		assert.Equal(t, initialCert, string(secret2.Data["ca.crt"]), "CA cert should not change")
	})

	t.Run("should set owner reference on proxy CA Secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyCAConfigMapName(testInstanceName),
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")

		require.NotEmpty(t, secret.OwnerReferences, "CA Secret should have owner references")
		assert.Equal(t, ClawResourceKind, secret.OwnerReferences[0].Kind)
		assert.Equal(t, testInstanceName, secret.OwnerReferences[0].Name)
	})
}

func TestGenerateCACertificate(t *testing.T) {
	certPEM, keyPEM, err := generateCACertificate()
	require.NoError(t, err)
	require.NotEmpty(t, certPEM)
	require.NotEmpty(t, keyPEM)

	certBlock, _ := pem.Decode(certPEM)
	require.NotNil(t, certBlock)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	assert.True(t, cert.IsCA)
	assert.Equal(t, 0, cert.MaxPathLen)
	assert.True(t, cert.MaxPathLenZero)

	keyBlock, _ := pem.Decode(keyPEM)
	require.NotNil(t, keyBlock)
	assert.Equal(t, "EC PRIVATE KEY", keyBlock.Type)
}

// --- Proxy config tests ---

func TestGenerateProxyConfig(t *testing.T) {
	t.Run("should generate config with apiKey route and gateway when provider set", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "secret",
					Key:  "key",
				},
				Domain: "generativelanguage.googleapis.com",
				APIKey: &clawv1alpha1.APIKeyConfig{
					Header: "x-goog-api-key",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "generativelanguage.googleapis.com")
		assert.Equal(t, "api_key", route.Injector)
		assert.Equal(t, "CRED_GEMINI", route.EnvVar)
		assert.Equal(t, "x-goog-api-key", route.Header)
		assert.Equal(t, "/gemini", route.PathPrefix)
		assert.Equal(t, "https://generativelanguage.googleapis.com", route.Upstream)
	})

	t.Run("should not set gateway fields when provider is empty", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "telegram",
				Type: clawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "secret",
					Key:  "key",
				},
				Domain: "api.telegram.org",
				APIKey: &clawv1alpha1.APIKeyConfig{
					Header: "x-api-key",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.telegram.org")
		assert.Empty(t, route.PathPrefix, "should not have gateway path prefix")
		assert.Empty(t, route.Upstream, "should not have gateway upstream")
	})

	t.Run("should generate config with bearer route", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "openai",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "secret",
					Key:  "key",
				},
				Domain: "api.openai.com",
				DefaultHeaders: map[string]string{
					"OpenAI-Organization": "org-123",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.openai.com")
		assert.Equal(t, "bearer", route.Injector)
		assert.Equal(t, "CRED_OPENAI", route.EnvVar)
		assert.Equal(t, "org-123", route.DefaultHeaders["OpenAI-Organization"])
	})

	t.Run("should generate config with GCP route and Vertex AI gateway", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "gcp-secret",
					Key:  "sa.json",
				},
				Domain: ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-central1",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, ".googleapis.com")
		assert.Equal(t, "gcp", route.Injector)
		assert.Equal(t, "/etc/proxy/credentials/vertex/sa-key.json", route.SAFilePath)
		assert.Equal(t, "my-project", route.GCPProject)
		assert.Equal(t, "/vertex", route.PathPrefix)
		assert.Equal(t, "https://us-central1-aiplatform.googleapis.com", route.Upstream)
	})

	t.Run("should order exact matches before suffix matches", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "suffix",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "s", Key: "k",
				},
				Domain: ".example.com",
			},
			{
				Name: "exact",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "s", Key: "k",
				},
				Domain: "api.example.com",
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		expectedDomains := []string{
			"api.example.com",
			"openrouter.ai",
			"raw.githubusercontent.com",
			"registry.npmjs.org",
			".example.com",
		}
		require.Len(t, cfg.Routes, len(expectedDomains))
		assert.Equal(t, expectedDomains[0], cfg.Routes[0].Domain, "exact match should come first")
		assert.Equal(t, expectedDomains[1], cfg.Routes[1].Domain, "builtin exact should come before suffix")
		assert.Equal(t, expectedDomains[2], cfg.Routes[2].Domain, "builtin exact should come before suffix")
		assert.Equal(t, expectedDomains[3], cfg.Routes[3].Domain, "builtin exact should come before suffix")
		assert.Equal(t, expectedDomains[4], cfg.Routes[4].Domain, "suffix match should come last")
	})

	t.Run("should generate config with none route", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:   "passthrough",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: "internal.example.com",
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "internal.example.com")
		assert.Equal(t, "none", route.Injector)
		assert.Empty(t, route.EnvVar, "none should not have envVar")
	})

	t.Run("should generate config with pathToken route", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "telegram",
				Type: clawv1alpha1.CredentialTypePathToken,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "telegram-secret",
					Key:  "token",
				},
				Domain: "api.telegram.org",
				PathToken: &clawv1alpha1.PathTokenConfig{
					Prefix: "/bot",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.telegram.org")
		assert.Equal(t, "path_token", route.Injector)
		assert.Equal(t, "CRED_TELEGRAM", route.EnvVar)
		assert.Equal(t, "/bot", route.PathPrefix)
	})

	t.Run("should generate config with oauth2 route", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "myservice",
				Type: clawv1alpha1.CredentialTypeOAuth2,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "oauth-secret",
					Key:  "client-secret",
				},
				Domain: "api.myservice.com",
				OAuth2: &clawv1alpha1.OAuth2Config{
					ClientID: "my-client-id",
					TokenURL: "https://auth.myservice.com/token",
					Scopes:   []string{"read", "write"},
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.myservice.com")
		assert.Equal(t, "oauth2", route.Injector)
		assert.Equal(t, "CRED_MYSERVICE", route.EnvVar)
		assert.Equal(t, "my-client-id", route.ClientID)
		assert.Equal(t, "https://auth.myservice.com/token", route.TokenURL)
		assert.Equal(t, []string{"read", "write"}, route.Scopes)
	})

	t.Run("should include all credential types together", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:   "passthrough",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: "internal.example.com",
			},
			{
				Name: "keep-me",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "s", Key: "k",
				},
				Domain: "api.example.com",
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		expectedCount := 2 + len(builtinPassthroughDomains)
		require.Len(t, cfg.Routes, expectedCount, "2 credential routes + builtin passthrough")
	})

	t.Run("should preserve pathToken prefix and skip gateway routing when provider is set", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "telegram",
				Type:     clawv1alpha1.CredentialTypePathToken,
				Provider: "custom",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "telegram-secret",
					Key:  "token",
				},
				Domain: "api.telegram.org",
				PathToken: &clawv1alpha1.PathTokenConfig{
					Prefix: "/bot",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.telegram.org")
		assert.Equal(t, "/bot", route.PathPrefix, "pathToken prefix should be preserved")
		assert.Empty(t, route.Upstream, "pathToken should not get gateway upstream even with provider set")
	})

	t.Run("should set gateway fields for bearer credential when provider is set", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "claude",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "anthropic",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "anthropic-secret",
					Key:  "api-key",
				},
				Domain: "api.anthropic.com",
				DefaultHeaders: map[string]string{
					"anthropic-version": "2023-06-01",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.anthropic.com")
		assert.Equal(t, "/claude", route.PathPrefix)
		assert.Equal(t, "https://api.anthropic.com", route.Upstream)
		assert.Equal(t, "bearer", route.Injector)
		assert.Equal(t, "2023-06-01", route.DefaultHeaders["anthropic-version"])
	})

	t.Run("should set gateway fields for oauth2 credential when provider is set", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "myservice",
				Type:     clawv1alpha1.CredentialTypeOAuth2,
				Provider: "myservice",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "oauth-secret",
					Key:  "client-secret",
				},
				Domain: "api.myservice.com",
				OAuth2: &clawv1alpha1.OAuth2Config{
					ClientID: "my-client-id",
					TokenURL: "https://auth.myservice.com/token",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.myservice.com")
		assert.Equal(t, "/myservice", route.PathPrefix)
		assert.Equal(t, "https://api.myservice.com", route.Upstream)
		assert.Equal(t, "oauth2", route.Injector)
	})
}

func TestBuiltinPassthroughDomains(t *testing.T) {
	t.Run("should include openrouter.ai as none route with no credentials", func(t *testing.T) {
		data, err := generateProxyConfig(nil) //nolint:staticcheck
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, len(builtinPassthroughDomains))
		route := findRouteByDomain(t, cfg.Routes, "openrouter.ai")
		assert.Equal(t, "none", route.Injector)
	})

	t.Run("should include raw.githubusercontent.com with path restriction", func(t *testing.T) {
		data, err := generateProxyConfig(nil) //nolint:staticcheck
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "raw.githubusercontent.com")
		assert.Equal(t, "none", route.Injector)
		assert.Equal(t, []string{"/BerriAI/litellm/", "/WhiskeySockets/Baileys/"}, route.AllowedPaths)
	})

	t.Run("should include registry.npmjs.org as none route with no credentials", func(t *testing.T) {
		data, err := generateProxyConfig(nil) //nolint:staticcheck
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, len(builtinPassthroughDomains))
		route := findRouteByDomain(t, cfg.Routes, "registry.npmjs.org")
		assert.Equal(t, "none", route.Injector)
	})

	t.Run("should have no path restriction on unrestricted builtins", func(t *testing.T) {
		data, err := generateProxyConfig(nil) //nolint:staticcheck
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "openrouter.ai")
		assert.Empty(t, route.AllowedPaths)
		route = findRouteByDomain(t, cfg.Routes, "registry.npmjs.org")
		assert.Empty(t, route.AllowedPaths)
	})

	t.Run("should propagate AllowedPaths from user credential", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:         "github",
				Type:         clawv1alpha1.CredentialTypeNone,
				Domain:       "api.github.com",
				AllowedPaths: []string{"/repos/myorg/"},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "api.github.com")
		assert.Equal(t, []string{"/repos/myorg/"}, route.AllowedPaths)
	})

	t.Run("should skip builtin route when user credential covers the domain", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "openrouter",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "or-secret",
					Key:  "api-key",
				},
				Domain:   "openrouter.ai",
				Provider: "openrouter",
			},
			{
				Name:   "npm",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: "registry.npmjs.org",
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		counts := make(map[string]int)
		for _, r := range cfg.Routes {
			counts[r.Domain]++
		}
		assert.Equal(t, 1, counts["openrouter.ai"], "should not duplicate openrouter.ai when user already has it")
		assert.Equal(t, 1, counts["registry.npmjs.org"], "should not duplicate registry.npmjs.org when user already has it")

		route := findRouteByDomain(t, cfg.Routes, "openrouter.ai")
		assert.Equal(t, "bearer", route.Injector, "user credential should take precedence")
	})
}

func TestConfigureProxyImage(t *testing.T) {
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

	getProxyImage := func(t *testing.T, objects []*unstructured.Unstructured) string {
		t.Helper()
		for _, obj := range objects {
			if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(testInstanceName) {
				continue
			}
			containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			for _, c := range containers {
				cm := c.(map[string]any)
				if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawProxyContainerName {
					img, _, _ := unstructured.NestedString(cm, "image")
					return img
				}
			}
		}
		t.Fatal("proxy container not found")
		return ""
	}

	t.Run("should override proxy image when set", func(t *testing.T) {
		instance, objects := buildObjects(t)
		require.NoError(t, configureProxyImage(objects, instance, "quay.io/myuser/claw-proxy:v1"))

		assert.Equal(t, "quay.io/myuser/claw-proxy:v1", getProxyImage(t, objects))
	})

	t.Run("should preserve default image when empty", func(t *testing.T) {
		instance, objects := buildObjects(t)
		require.NoError(t, configureProxyImage(objects, instance, ""))

		assert.Equal(t, "claw-proxy:latest", getProxyImage(t, objects))
	})
}

func TestResolveProviderInfo(t *testing.T) {
	tests := []struct {
		name         string
		cred         clawv1alpha1.CredentialSpec
		wantUpstream string
		wantBasePath string
	}{
		{
			name: "google apiKey uses Gemini REST API",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "google",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Domain:   "generativelanguage.googleapis.com",
			},
			wantUpstream: "https://generativelanguage.googleapis.com",
			wantBasePath: "/v1beta",
		},
		{
			name: "google gcp uses Vertex AI",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "google",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-central1",
				},
			},
			wantUpstream: "https://us-central1-aiplatform.googleapis.com",
			wantBasePath: "/v1/projects/my-project/locations/us-central1/publishers/google",
		},
		{
			name: "anthropic bearer uses domain directly",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "anthropic",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   "api.anthropic.com",
			},
			wantUpstream: "https://api.anthropic.com",
			wantBasePath: "",
		},
		{
			name: "anthropic gcp uses Vertex AI with anthropic publisher",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "anthropic",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
			wantUpstream: "https://us-east5-aiplatform.googleapis.com",
			wantBasePath: "/v1/projects/my-project/locations/us-east5/publishers/anthropic",
		},
		{
			name: "unknown provider with exact domain",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "custom-llm",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   "api.custom-llm.com",
			},
			wantUpstream: "https://api.custom-llm.com",
			wantBasePath: "",
		},
		{
			name: "unknown provider with suffix domain strips dot",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "custom",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   ".custom.ai",
			},
			wantUpstream: "https://custom.ai",
			wantBasePath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := resolveProviderInfo(tt.cred)
			assert.Equal(t, tt.wantUpstream, info.Upstream)
			assert.Equal(t, tt.wantBasePath, info.BasePath)
		})
	}
}

func TestInjectProvidersIntoConfigMap(t *testing.T) {
	makeConfigMap := func(jsonContent string) []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{
			"operator.json": jsonContent,
		}
		return []*unstructured.Unstructured{cm}
	}

	baseJSON := `{"models":{"providers":{}},"gateway":{"port":18789}}`

	getProviders := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		models := config["models"].(map[string]any)
		return models["providers"].(map[string]any)
	}

	t.Run("should inject google provider with correct baseUrl", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta", google["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", google["apiKey"])
	})

	t.Run("should inject multiple providers", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
			{
				Name:     "claude",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Provider: "anthropic",
				Domain:   "api.anthropic.com",
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		assert.Contains(t, providers, "google")
		assert.Contains(t, providers, "anthropic")
		anthropic := providers["anthropic"].(map[string]any)
		assert.Equal(t, "https://api.anthropic.com", anthropic["baseUrl"])
	})

	t.Run("should leave providers empty when no provider is set", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:   "telegram",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.telegram.org",
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		assert.Empty(t, providers)
	})

	t.Run("should use Vertex AI upstream for google gcp credential", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-proj",
					Location: "europe-west1",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "https://europe-west1-aiplatform.googleapis.com/v1/projects/my-proj/locations/europe-west1/publishers/google", google["baseUrl"])
	})

	t.Run("should preserve other config sections", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(nil)))

		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		gateway := config["gateway"].(map[string]any)
		assert.Equal(t, float64(18789), gateway["port"])
	})

	t.Run("should skip pathToken credentials even with provider set", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "telegram",
				Type:     clawv1alpha1.CredentialTypePathToken,
				Provider: "telegram",
				Domain:   "api.telegram.org",
				PathToken: &clawv1alpha1.PathTokenConfig{
					Prefix: "/bot",
				},
			},
		}

		require.NoError(t, injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials)))

		providers := getProviders(t, objects)
		assert.Empty(t, providers, "pathToken credentials should not generate provider entries")
	})

	t.Run("should reject duplicate providers", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		credentials := []clawv1alpha1.CredentialSpec{
			{Name: "gemini-1", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
			{Name: "gemini-2", Type: clawv1alpha1.CredentialTypeAPIKey, Provider: "google", Domain: "generativelanguage.googleapis.com"},
		}

		err := injectProvidersIntoConfigMap(objects, testClawWithCredentials(credentials))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate provider")
		assert.Contains(t, err.Error(), "google")
	})

}

func TestCredEnvVarName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gemini", "CRED_GEMINI"},
		{"vertex-ai", "CRED_VERTEX_AI"},
		{"OpenAI", "CRED_OPENAI"},
		{"my-custom-key", "CRED_MY_CUSTOM_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, credEnvVarName(tt.input))
		})
	}
}

func TestOpenClawProxyConfigMap(t *testing.T) {
	ctx := context.Background()

	t.Run("should create proxy config ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "proxy config ConfigMap should be created")

		data, ok := cm.Data["proxy-config.json"]
		assert.True(t, ok, "proxy-config.json should exist in ConfigMap")

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(data), &cfg))
		route := findRouteByDomain(t, cfg.Routes, ".googleapis.com")
		assert.Equal(t, "api_key", route.Injector)
	})

	t.Run("should include path-restricted raw.githubusercontent.com builtin after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "proxy config ConfigMap should be created")

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(cm.Data["proxy-config.json"]), &cfg))
		route := findRouteByDomain(t, cfg.Routes, "raw.githubusercontent.com")
		assert.Equal(t, "none", route.Injector)
		assert.Equal(t, []string{"/BerriAI/litellm/", "/WhiskeySockets/Baileys/"}, route.AllowedPaths)
	})

	t.Run("should include gateway fields in proxy config when credential has provider", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "proxy config ConfigMap should be created")

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(cm.Data["proxy-config.json"]), &cfg))
		route := findRouteByDomain(t, cfg.Routes, ".googleapis.com")
		assert.Equal(t, "/gemini", route.PathPrefix, "should have gateway path prefix")
		assert.Equal(t, "https://generativelanguage.googleapis.com", route.Upstream, "should have gateway upstream")
	})
}

func TestOpenClawDynamicProviders(t *testing.T) {
	ctx := context.Background()

	t.Run("should inject dynamic providers into ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		operatorJSON, ok := cm.Data["operator.json"]
		require.True(t, ok, "operator.json should exist")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

		models, ok := config["models"].(map[string]any)
		require.True(t, ok, "models section should exist")
		providers, ok := models["providers"].(map[string]any)
		require.True(t, ok, "providers section should exist")
		require.Contains(t, providers, "google", "google provider should be injected")

		google, ok := providers["google"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta", google["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", google["apiKey"])
	})

	t.Run("should have empty providers when no credentials have provider set", func(t *testing.T) {
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

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		models := config["models"].(map[string]any)
		providers := models["providers"].(map[string]any)
		assert.Empty(t, providers, "providers should be empty when no credentials have provider set")
	})

	t.Run("should have empty providers for MITM-only credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstanceMITMOnly(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		models := config["models"].(map[string]any)
		providers := models["providers"].(map[string]any)
		assert.Empty(t, providers, "providers should be empty for MITM-only credentials")
	})
}
