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

package proxy

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func writeTestKubeconfig(t *testing.T, clusters map[string]string, users map[string]string, contexts map[string][2]string) string {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	for name, server := range clusters {
		cfg.Clusters[name] = &clientcmdapi.Cluster{Server: server}
	}
	for name, token := range users {
		cfg.AuthInfos[name] = &clientcmdapi.AuthInfo{Token: token}
	}
	for name, pair := range contexts {
		cfg.Contexts[name] = &clientcmdapi.Context{Cluster: pair[0], AuthInfo: pair[1]}
	}
	data, err := clientcmd.Write(*cfg)
	require.NoError(t, err)

	f, err := os.CreateTemp("", "kubeconfig-*.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(f.Name()) })

	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestKubernetesInjector(t *testing.T) {
	t.Run("should inject bearer token for matching host", func(t *testing.T) {
		path := writeTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com:6443"},
			map[string]string{"admin": "my-secret-token"},
			map[string][2]string{"ctx": {"prod", "admin"}},
		)

		inj, err := NewKubernetesInjector(&Route{
			Domain:         "api.example.com:6443",
			Injector:       "kubernetes",
			KubeconfigPath: path,
		})
		require.NoError(t, err)

		req := &http.Request{
			URL:    &url.URL{Host: "api.example.com:6443"},
			Header: http.Header{},
		}
		require.NoError(t, inj.Inject(req))

		assert.Equal(t, "Bearer my-secret-token", req.Header.Get("Authorization"))
	})

	t.Run("should return error for unmatched host", func(t *testing.T) {
		path := writeTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"ctx": {"prod", "admin"}},
		)

		inj, err := NewKubernetesInjector(&Route{
			Domain:         "api.example.com:6443",
			Injector:       "kubernetes",
			KubeconfigPath: path,
		})
		require.NoError(t, err)

		req := &http.Request{
			URL:    &url.URL{Host: "other.example.com:6443"},
			Header: http.Header{},
		}
		err = inj.Inject(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no token for host")
	})

	t.Run("should normalize port default to 443", func(t *testing.T) {
		path := writeTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"ctx": {"prod", "admin"}},
		)

		inj, err := NewKubernetesInjector(&Route{
			Domain:         "api.example.com:443",
			Injector:       "kubernetes",
			KubeconfigPath: path,
		})
		require.NoError(t, err)

		req := &http.Request{
			URL:    &url.URL{Host: "api.example.com"},
			Header: http.Header{},
		}
		require.NoError(t, inj.Inject(req))
		assert.Equal(t, "Bearer my-token", req.Header.Get("Authorization"))
	})

	t.Run("should set default headers", func(t *testing.T) {
		path := writeTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"ctx": {"prod", "admin"}},
		)

		inj, err := NewKubernetesInjector(&Route{
			Domain:         "api.example.com:6443",
			Injector:       "kubernetes",
			KubeconfigPath: path,
			DefaultHeaders: map[string]string{"X-Custom": "value"},
		})
		require.NoError(t, err)

		req := &http.Request{
			URL:    &url.URL{Host: "api.example.com:6443"},
			Header: http.Header{},
		}
		require.NoError(t, inj.Inject(req))
		assert.Equal(t, "value", req.Header.Get("X-Custom"))
	})

	t.Run("should error when kubeconfigPath is empty", func(t *testing.T) {
		_, err := NewKubernetesInjector(&Route{
			Domain:   "api.example.com:6443",
			Injector: "kubernetes",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kubeconfigPath is required")
	})

	t.Run("should error when kubeconfig file does not exist", func(t *testing.T) {
		_, err := NewKubernetesInjector(&Route{
			Domain:         "api.example.com:6443",
			Injector:       "kubernetes",
			KubeconfigPath: "/nonexistent/path/kubeconfig",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load kubeconfig")
	})

	t.Run("should handle multi-cluster kubeconfig", func(t *testing.T) {
		path := writeTestKubeconfig(t,
			map[string]string{
				"prod":    "https://api.prod.example.com:6443",
				"staging": "https://api.staging.example.com:8443",
			},
			map[string]string{
				"prod-admin":    "prod-token",
				"staging-admin": "staging-token",
			},
			map[string][2]string{
				"prod-ctx":    {"prod", "prod-admin"},
				"staging-ctx": {"staging", "staging-admin"},
			},
		)

		inj, err := NewKubernetesInjector(&Route{
			Domain:         "api.prod.example.com:6443",
			Injector:       "kubernetes",
			KubeconfigPath: path,
		})
		require.NoError(t, err)

		prodReq := &http.Request{
			URL:    &url.URL{Host: "api.prod.example.com:6443"},
			Header: http.Header{},
		}
		require.NoError(t, inj.Inject(prodReq))
		assert.Equal(t, "Bearer prod-token", prodReq.Header.Get("Authorization"))

		stagingReq := &http.Request{
			URL:    &url.URL{Host: "api.staging.example.com:8443"},
			Header: http.Header{},
		}
		require.NoError(t, inj.Inject(stagingReq))
		assert.Equal(t, "Bearer staging-token", stagingReq.Header.Get("Authorization"))
	})
}

func TestKubernetesInjectorIPv6(t *testing.T) {
	t.Run("should inject token for IPv6 bracketed host", func(t *testing.T) {
		path := writeTestKubeconfig(t,
			map[string]string{"ipv6": "https://[::1]:6443"},
			map[string]string{"admin": "ipv6-token"},
			map[string][2]string{"ctx": {"ipv6", "admin"}},
		)

		inj, err := NewKubernetesInjector(&Route{
			Domain:         "[::1]:6443",
			Injector:       "kubernetes",
			KubeconfigPath: path,
		})
		require.NoError(t, err)

		req := &http.Request{
			URL:    &url.URL{Host: "[::1]:6443"},
			Header: http.Header{},
		}
		require.NoError(t, inj.Inject(req))
		assert.Equal(t, "Bearer ipv6-token", req.Header.Get("Authorization"))
	})

	t.Run("should reject empty token at startup", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["prod"] = &clientcmdapi.Cluster{Server: "https://api.example.com:6443"}
		cfg.AuthInfos["empty"] = &clientcmdapi.AuthInfo{Token: ""}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "prod", AuthInfo: "empty"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		f, err := os.CreateTemp("", "kubeconfig-*.yaml")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Remove(f.Name()) })
		_, err = f.Write(data)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		_, err = NewKubernetesInjector(&Route{
			Domain:         "api.example.com:6443",
			Injector:       "kubernetes",
			KubeconfigPath: f.Name(),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no token configured")
	})
}

func TestNormalizeRequestHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"api.example.com:6443", "api.example.com:6443"},
		{"api.example.com", "api.example.com:443"},
		{"[::1]:6443", "::1:6443"},
		{"[2001:db8::1]:8443", "2001:db8::1:8443"},
		{"API.EXAMPLE.COM:8443", "api.example.com:8443"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeRequestHost(tt.input))
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://api.example.com:6443", "api.example.com:6443"},
		{"https://api.example.com", "api.example.com:443"},
		{"https://API.EXAMPLE.COM:8443", "api.example.com:8443"},
		{"invalid-url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeHost(tt.input))
		})
	}
}
