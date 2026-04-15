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

// --- Proxy CA tests ---

func TestClawProxyCA(t *testing.T) {
	ctx := context.Background()

	t.Run("should create proxy CA Secret on first reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawProxyCACertSecretName,
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")

		assert.Contains(t, secret.Data, "ca.crt")
		assert.Contains(t, secret.Data, "ca.key")
	})

	t.Run("should create valid X.509 CA certificate", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawProxyCACertSecretName,
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
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawProxyCACertSecretName,
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")
		initialCert := string(secret.Data["ca.crt"])

		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		secret2 := &corev1.Secret{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      ClawProxyCACertSecretName,
			Namespace: namespace,
		}, secret2))
		assert.Equal(t, initialCert, string(secret2.Data["ca.crt"]), "CA cert should not change")
	})

	t.Run("should set owner reference on proxy CA Secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		secret := &corev1.Secret{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawProxyCACertSecretName,
				Namespace: namespace,
			}, secret) == nil
		}, "proxy CA Secret should be created")

		require.NotEmpty(t, secret.OwnerReferences, "CA Secret should have owner references")
		assert.Equal(t, ClawResourceKind, secret.OwnerReferences[0].Kind)
		assert.Equal(t, ClawInstanceName, secret.OwnerReferences[0].Name)
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "generativelanguage.googleapis.com", cfg.Routes[0].Domain)
		assert.Equal(t, "api_key", cfg.Routes[0].Injector)
		assert.Equal(t, "CRED_GEMINI", cfg.Routes[0].EnvVar)
		assert.Equal(t, "x-goog-api-key", cfg.Routes[0].Header)
		assert.Equal(t, "/gemini", cfg.Routes[0].PathPrefix)
		assert.Equal(t, "https://generativelanguage.googleapis.com", cfg.Routes[0].Upstream)
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Empty(t, cfg.Routes[0].PathPrefix, "should not have gateway path prefix")
		assert.Empty(t, cfg.Routes[0].Upstream, "should not have gateway upstream")
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "bearer", cfg.Routes[0].Injector)
		assert.Equal(t, "CRED_OPENAI", cfg.Routes[0].EnvVar)
		assert.Equal(t, "org-123", cfg.Routes[0].DefaultHeaders["OpenAI-Organization"])
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "gcp", cfg.Routes[0].Injector)
		assert.Equal(t, "/etc/proxy/credentials/vertex/sa-key.json", cfg.Routes[0].SAFilePath)
		assert.Equal(t, "my-project", cfg.Routes[0].GCPProject)
		assert.Equal(t, "/vertex", cfg.Routes[0].PathPrefix)
		assert.Equal(t, "https://us-central1-aiplatform.googleapis.com", cfg.Routes[0].Upstream)
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 2)
		assert.Equal(t, "api.example.com", cfg.Routes[0].Domain, "exact match should come first")
		assert.Equal(t, ".example.com", cfg.Routes[1].Domain, "suffix match should come second")
	})

	t.Run("should generate config with none route", func(t *testing.T) {
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:   "passthrough",
				Type:   clawv1alpha1.CredentialTypeNone,
				Domain: "internal.example.com",
			},
		}

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "none", cfg.Routes[0].Injector)
		assert.Equal(t, "internal.example.com", cfg.Routes[0].Domain)
		assert.Empty(t, cfg.Routes[0].EnvVar, "none should not have envVar")
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "path_token", cfg.Routes[0].Injector)
		assert.Equal(t, "CRED_TELEGRAM", cfg.Routes[0].EnvVar)
		assert.Equal(t, "/bot", cfg.Routes[0].PathPrefix)
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "oauth2", cfg.Routes[0].Injector)
		assert.Equal(t, "CRED_MYSERVICE", cfg.Routes[0].EnvVar)
		assert.Equal(t, "my-client-id", cfg.Routes[0].ClientID)
		assert.Equal(t, "https://auth.myservice.com/token", cfg.Routes[0].TokenURL)
		assert.Equal(t, []string{"read", "write"}, cfg.Routes[0].Scopes)
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 2)
	})

	t.Run("should handle empty credentials", func(t *testing.T) {
		data, err := generateProxyConfig(nil)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		assert.Nil(t, cfg.Routes)
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "/bot", cfg.Routes[0].PathPrefix, "pathToken prefix should be preserved")
		assert.Empty(t, cfg.Routes[0].Upstream, "pathToken should not get gateway upstream even with provider set")
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "/claude", cfg.Routes[0].PathPrefix)
		assert.Equal(t, "https://api.anthropic.com", cfg.Routes[0].Upstream)
		assert.Equal(t, "bearer", cfg.Routes[0].Injector)
		assert.Equal(t, "2023-06-01", cfg.Routes[0].DefaultHeaders["anthropic-version"])
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

		data, err := generateProxyConfig(credentials)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "/myservice", cfg.Routes[0].PathPrefix)
		assert.Equal(t, "https://api.myservice.com", cfg.Routes[0].Upstream)
		assert.Equal(t, "oauth2", cfg.Routes[0].Injector)
	})
}

func TestConfigureProxyImage(t *testing.T) {
	buildObjects := func(t *testing.T) []*unstructured.Unstructured {
		t.Helper()
		reconciler := createClawReconciler()
		objects, err := reconciler.buildKustomizedObjects()
		require.NoError(t, err)
		return objects
	}

	getProxyImage := func(t *testing.T, objects []*unstructured.Unstructured) string {
		t.Helper()
		for _, obj := range objects {
			if obj.GetKind() != DeploymentKind || obj.GetName() != ClawProxyDeploymentName {
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
		objects := buildObjects(t)
		require.NoError(t, configureProxyImage(objects, "quay.io/myuser/claw-proxy:v1"))

		assert.Equal(t, "quay.io/myuser/claw-proxy:v1", getProxyImage(t, objects))
	})

	t.Run("should preserve default image when empty", func(t *testing.T) {
		objects := buildObjects(t)
		require.NoError(t, configureProxyImage(objects, ""))

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
			name: "anthropic uses domain directly",
			cred: clawv1alpha1.CredentialSpec{
				Provider: "anthropic",
				Type:     clawv1alpha1.CredentialTypeBearer,
				Domain:   "api.anthropic.com",
			},
			wantUpstream: "https://api.anthropic.com",
			wantBasePath: "",
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
		cm.SetName(ClawConfigMapName)
		cm.Object["data"] = map[string]any{
			"openclaw.json": jsonContent,
		}
		return []*unstructured.Unstructured{cm}
	}

	baseJSON := `{"models":{"providers":{}},"gateway":{"port":18789}}`

	getProviders := func(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
		t.Helper()
		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "openclaw.json")
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

		require.NoError(t, injectProvidersIntoConfigMap(objects, credentials))

		providers := getProviders(t, objects)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "http://claw-proxy:8080/gemini/v1beta", google["baseUrl"])
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

		require.NoError(t, injectProvidersIntoConfigMap(objects, credentials))

		providers := getProviders(t, objects)
		assert.Contains(t, providers, "google")
		assert.Contains(t, providers, "anthropic")
		anthropic := providers["anthropic"].(map[string]any)
		assert.Equal(t, "http://claw-proxy:8080/claude", anthropic["baseUrl"])
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

		require.NoError(t, injectProvidersIntoConfigMap(objects, credentials))

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

		require.NoError(t, injectProvidersIntoConfigMap(objects, credentials))

		providers := getProviders(t, objects)
		require.Contains(t, providers, "google")
		google := providers["google"].(map[string]any)
		assert.Equal(t, "http://claw-proxy:8080/vertex/v1/projects/my-proj/locations/europe-west1/publishers/google", google["baseUrl"])
	})

	t.Run("should preserve other config sections", func(t *testing.T) {
		objects := makeConfigMap(baseJSON)
		require.NoError(t, injectProvidersIntoConfigMap(objects, nil))

		raw, _, err := unstructured.NestedString(objects[0].Object, "data", "openclaw.json")
		require.NoError(t, err)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &config))
		gateway := config["gateway"].(map[string]any)
		assert.Equal(t, float64(18789), gateway["port"])
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
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawProxyConfigMapName,
				Namespace: namespace,
			}, cm) == nil
		}, "proxy config ConfigMap should be created")

		data, ok := cm.Data["proxy-config.json"]
		assert.True(t, ok, "proxy-config.json should exist in ConfigMap")

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(data), &cfg))
		require.Len(t, cfg.Routes, 1, "should have one route for the test credential")
		assert.Equal(t, ".googleapis.com", cfg.Routes[0].Domain)
	})

	t.Run("should include gateway fields in proxy config when credential has provider", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawProxyConfigMapName,
				Namespace: namespace,
			}, cm) == nil
		}, "proxy config ConfigMap should be created")

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(cm.Data["proxy-config.json"]), &cfg))
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "/gemini", cfg.Routes[0].PathPrefix, "should have gateway path prefix")
		assert.Equal(t, "https://generativelanguage.googleapis.com", cfg.Routes[0].Upstream, "should have gateway upstream")
	})
}

func TestOpenClawDynamicProviders(t *testing.T) {
	ctx := context.Background()

	t.Run("should inject dynamic providers into ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawConfigMapName,
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		openclawJSON, ok := cm.Data["openclaw.json"]
		require.True(t, ok, "openclaw.json should exist")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(openclawJSON), &config))

		models, ok := config["models"].(map[string]any)
		require.True(t, ok, "models section should exist")
		providers, ok := models["providers"].(map[string]any)
		require.True(t, ok, "providers section should exist")
		require.Contains(t, providers, "google", "google provider should be injected")

		google, ok := providers["google"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "http://claw-proxy:8080/gemini/v1beta", google["baseUrl"])
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", google["apiKey"])
	})

	t.Run("should have empty providers when no credentials have provider set", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{}
		instance.Name = ClawInstanceName
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
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawConfigMapName,
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["openclaw.json"]), &config))

		models := config["models"].(map[string]any)
		providers := models["providers"].(map[string]any)
		assert.Empty(t, providers, "providers should be empty when no credentials have provider set")
	})

	t.Run("should have empty providers and filtered model defaults for MITM-only credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstanceMITMOnly(t, ctx, ClawInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, ClawInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawConfigMapName,
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["openclaw.json"]), &config))

		models := config["models"].(map[string]any)
		providers := models["providers"].(map[string]any)
		assert.Empty(t, providers, "providers should be empty for MITM-only credentials")

		agents, _ := config["agents"].(map[string]any)
		defaults, _ := agents["defaults"].(map[string]any)
		modelAliases, _ := defaults["models"].(map[string]any)
		assert.Empty(t, modelAliases, "model aliases should be empty when no providers are configured")
	})
}
