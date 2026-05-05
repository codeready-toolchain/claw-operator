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
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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
			"clawhub.ai",
			"codeload.github.com",
			"github.com",
			"openrouter.ai",
			"raw.githubusercontent.com",
			"registry.npmjs.org",
			".example.com",
		}
		require.Len(t, cfg.Routes, len(expectedDomains))
		for i, want := range expectedDomains {
			assert.Equal(t, want, cfg.Routes[i].Domain, "route %d should be %s", i, want)
		}
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

	t.Run("should generate config with Discord apiKey credential", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "discord",
				Type: clawv1alpha1.CredentialTypeAPIKey,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "discord-bot-secret",
					Key:  "token",
				},
				Domain: "discord.com",
				APIKey: &clawv1alpha1.APIKeyConfig{
					Header:      "Authorization",
					ValuePrefix: "Bot ",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "discord.com")
		assert.Equal(t, "api_key", route.Injector)
		assert.Equal(t, "Authorization", route.Header)
		assert.Equal(t, "Bot ", route.ValuePrefix)
		assert.Equal(t, "CRED_DISCORD", route.EnvVar)
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

	t.Run("should generate Slack dual-credential routes with correct ordering", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name: "slack-app",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "slack-secret",
					Key:  "app-token",
				},
				Domain:       "slack.com",
				AllowedPaths: []string{"/api/apps.connections.open"},
			},
			{
				Name: "slack-bot",
				Type: clawv1alpha1.CredentialTypeBearer,
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "slack-secret",
					Key:  "bot-token",
				},
				Domain: "slack.com",
			},
			{
				Name:   "slack-ws",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: ".slack.com",
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		var slackRoutes []proxyRoute
		for _, r := range cfg.Routes {
			if r.Domain == "slack.com" {
				slackRoutes = append(slackRoutes, r)
			}
		}
		require.Len(t, slackRoutes, 2, "should emit both slack.com routes")

		assert.Equal(t, []string{"/api/apps.connections.open"}, slackRoutes[0].AllowedPaths,
			"allowedPaths route should come first")
		assert.Equal(t, "CRED_SLACK_APP", slackRoutes[0].EnvVar)

		assert.Empty(t, slackRoutes[1].AllowedPaths, "catch-all route should have no allowedPaths")
		assert.Equal(t, "CRED_SLACK_BOT", slackRoutes[1].EnvVar)

		suffixRoute := findRouteByDomain(t, cfg.Routes, ".slack.com")
		assert.Equal(t, "none", suffixRoute.Injector)

		// Verify overall ordering: all exact routes before suffix routes
		lastExact := -1
		firstSuffix := len(cfg.Routes)
		for i, r := range cfg.Routes {
			if strings.HasPrefix(r.Domain, ".") {
				if i < firstSuffix {
					firstSuffix = i
				}
			} else {
				lastExact = i
			}
		}
		assert.Less(t, lastExact, firstSuffix, "all exact routes should precede suffix routes")
	})
}

func TestBuiltinPassthroughDomains(t *testing.T) {
	t.Run("should include clawhub.ai as none route with no credentials", func(t *testing.T) {
		data, err := generateProxyConfig(nil) //nolint:staticcheck
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, "clawhub.ai")
		assert.Equal(t, "none", route.Injector)
		assert.Empty(t, route.AllowedPaths)
	})

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

// --- Proxy config Vertex SDK tests ---

func TestGenerateProxyConfigVertexSDK(t *testing.T) {
	t.Run("should not create gateway route for GCP anthropic credential", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "vertex-sa",
					Key:  "sa.json",
				},
				Domain: ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		route := findRouteByDomain(t, cfg.Routes, ".googleapis.com")
		assert.Equal(t, "gcp", route.Injector)
		assert.Empty(t, route.PathPrefix, "Vertex SDK provider should not have gateway path prefix")
		assert.Empty(t, route.Upstream, "Vertex SDK provider should not have gateway upstream")
	})

	t.Run("should create gateway route for GCP google but not GCP anthropic", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini-vertex",
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
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				SecretRef: &clawv1alpha1.SecretRef{
					Name: "vertex-sa",
					Key:  "sa.json",
				},
				Domain: ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
		}

		data, err := generateProxyConfig(toResolved(credentials))
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		var googleRoute, anthropicRoute *proxyRoute
		for i := range cfg.Routes {
			if cfg.Routes[i].SAFilePath == "/etc/proxy/credentials/gemini-vertex/sa-key.json" {
				googleRoute = &cfg.Routes[i]
			}
			if cfg.Routes[i].SAFilePath == "/etc/proxy/credentials/anthropic-vertex/sa-key.json" {
				anthropicRoute = &cfg.Routes[i]
			}
		}

		require.NotNil(t, googleRoute, "google GCP route should exist")
		assert.Equal(t, "/gemini-vertex", googleRoute.PathPrefix, "google GCP should have gateway prefix")
		assert.NotEmpty(t, googleRoute.Upstream, "google GCP should have gateway upstream")

		require.NotNil(t, anthropicRoute, "anthropic GCP route should exist")
		assert.Empty(t, anthropicRoute.PathPrefix, "anthropic GCP should not have gateway prefix")
		assert.Empty(t, anthropicRoute.Upstream, "anthropic GCP should not have gateway upstream")
	})
}

// --- Proxy credential wiring tests ---

func TestConfigureProxyForCredentials(t *testing.T) {
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
		instance, objects := buildObjects(t)
		creds := []clawv1alpha1.CredentialSpec{
			{
				Name:      "vertex",
				Type:      clawv1alpha1.CredentialTypeGCP,
				SecretRef: &clawv1alpha1.SecretRef{Name: "gcp-sa", Key: "sa.json"},
				Domain:    ".googleapis.com",
				GCP:       &clawv1alpha1.GCPConfig{Project: "p", Location: "us-central1"},
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, instance, toResolved(creds)))

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
		instance, objects := buildObjects(t)
		creds := []clawv1alpha1.CredentialSpec{
			{
				Name:   "no-ref",
				Type:   clawv1alpha1.CredentialTypeAPIKey,
				Domain: "api.example.com",
				APIKey: &clawv1alpha1.APIKeyConfig{Header: "x-api-key"},
			},
		}
		require.NoError(t, configureProxyForCredentials(objects, instance, toResolved(creds)))

		container := findProxyContainer(t, objects)
		envVars, _, _ := unstructured.NestedSlice(container, "env")
		for _, e := range envVars {
			env := e.(map[string]any)
			assert.NotEqual(t, "CRED_NO_REF", env["name"], "should not add env var for credential without secretRef")
		}
	})

	t.Run("should handle multiple credential types together", func(t *testing.T) {
		instance, objects := buildObjects(t)
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
		require.NoError(t, configureProxyForCredentials(objects, instance, toResolved(creds)))

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

	t.Run("should add kubernetes kubeconfig volume mount", func(t *testing.T) {
		instance, objects := buildObjects(t)
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
			},
		}

		require.NoError(t, configureProxyForCredentials(objects, instance, creds))

		for _, obj := range objects {
			if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(testInstanceName) {
				continue
			}
			volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
			var foundVol bool
			for _, v := range volumes {
				vol := v.(map[string]any)
				if vol["name"] == testKubeCredVolume {
					foundVol = true
					secret := vol["secret"].(map[string]any)
					assert.Equal(t, testKubeSecretName, secret["secretName"])
					items := secret["items"].([]any)
					item := items[0].(map[string]any)
					assert.Equal(t, "config", item["key"])
					assert.Equal(t, "kubeconfig", item["path"])
				}
			}
			assert.True(t, foundVol, "kubernetes credential volume should be present")
		}
	})
}

// --- Secret version annotation tests ---

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

		secret := &corev1.Secret{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      aiModelSecret,
			Namespace: namespace,
		}, secret))
		secret.Data[aiModelSecretKey] = []byte("rotated-api-key")
		require.NoError(t, k8sClient.Update(ctx, secret))

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

// --- Proxy config kubernetes route tests ---

func TestGenerateProxyConfigKubernetes(t *testing.T) {
	t.Run("should generate routes per cluster from kubeconfig", func(t *testing.T) {
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Name: "prod", Hostname: "api.prod.example.com", Port: "6443"},
						{Name: "staging", Hostname: "api.staging.example.com", Port: "443"},
					},
				},
			},
		}

		data, err := generateProxyConfig(creds)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		prodRoute := findRouteByDomain(t, cfg.Routes, "api.prod.example.com:6443")
		assert.Equal(t, "kubernetes", prodRoute.Injector)
		assert.Equal(t, "/etc/proxy/credentials/k8s/kubeconfig", prodRoute.KubeconfigPath)

		stagingRoute := findRouteByDomain(t, cfg.Routes, "api.staging.example.com:443")
		assert.Equal(t, "kubernetes", stagingRoute.Injector)
	})

	t.Run("should skip kubernetes credential with nil kubeconfig", func(t *testing.T) {
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "k8s",
					Type: clawv1alpha1.CredentialTypeKubernetes,
				},
				KubeConfig: nil,
			},
		}

		data, err := generateProxyConfig(creds)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		assert.Len(t, cfg.Routes, len(builtinPassthroughDomains))
	})

	t.Run("should include caCert when cluster has CAData", func(t *testing.T) {
		caData := []byte("-----BEGIN CERTIFICATE-----\nMIIBfake\n-----END CERTIFICATE-----\n")
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Name: "prod", Hostname: "api.prod.example.com", Port: "6443", CAData: caData},
						{Name: "staging", Hostname: "api.staging.example.com", Port: "443"},
					},
				},
			},
		}

		data, err := generateProxyConfig(creds)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		prodRoute := findRouteByDomain(t, cfg.Routes, "api.prod.example.com:6443")
		assert.NotEmpty(t, prodRoute.CACert, "route with CAData should have caCert set")

		decoded, err := base64.StdEncoding.DecodeString(prodRoute.CACert)
		require.NoError(t, err)
		assert.Equal(t, caData, decoded)

		stagingRoute := findRouteByDomain(t, cfg.Routes, "api.staging.example.com:443")
		assert.Empty(t, stagingRoute.CACert, "route without CAData should have empty caCert")
	})
}
