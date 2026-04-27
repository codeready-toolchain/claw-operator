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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	t.Run("should load valid config", func(t *testing.T) {
		f, err := os.CreateTemp("", "proxy-config-*.json")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Remove(f.Name()) })

		_, err = f.WriteString(`{"routes":[{"domain":"api.example.com","injector":"bearer","envVar":"CRED_EXAMPLE"}]}`)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		cfg, err := LoadConfig(f.Name())
		require.NoError(t, err)
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, "api.example.com", cfg.Routes[0].Domain)
		assert.Equal(t, "bearer", cfg.Routes[0].Injector)
	})

	t.Run("should deserialize allowedPaths", func(t *testing.T) {
		f, err := os.CreateTemp("", "proxy-config-*.json")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Remove(f.Name()) })

		_, err = f.WriteString(`{"routes":[{"domain":"raw.githubusercontent.com","injector":"none","allowedPaths":["/BerriAI/litellm/"]}]}`)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		cfg, err := LoadConfig(f.Name())
		require.NoError(t, err)
		require.Len(t, cfg.Routes, 1)
		assert.Equal(t, []string{"/BerriAI/litellm/"}, cfg.Routes[0].AllowedPaths)
	})

	t.Run("should return error for missing file", func(t *testing.T) {
		_, err := LoadConfig("/nonexistent/path.json")
		require.Error(t, err)
	})

	t.Run("should return error for invalid JSON", func(t *testing.T) {
		f, err := os.CreateTemp("", "proxy-config-*.json")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Remove(f.Name()) })

		_, err = f.WriteString(`{invalid}`)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		_, err = LoadConfig(f.Name())
		require.Error(t, err)
	})
}

func TestMatchRoute(t *testing.T) {
	cfg := &Config{
		Routes: []Route{
			{Domain: "api.example.com", Injector: "bearer"},
			{Domain: ".googleapis.com", Injector: "api_key"},
			{Domain: "openrouter.ai", Injector: "bearer"},
			{Domain: "registry.npmjs.org", Injector: "none"},
		},
	}

	tests := []struct {
		name    string
		host    string
		wantNil bool
		wantDom string
	}{
		{name: "exact match", host: "api.example.com", wantDom: "api.example.com"},
		{name: "exact match with port", host: "api.example.com:443", wantDom: "api.example.com"},
		{name: "npm registry exact match", host: "registry.npmjs.org", wantDom: "registry.npmjs.org"},
		{name: "npm registry exact match with port", host: "registry.npmjs.org:443", wantDom: "registry.npmjs.org"},
		{name: "suffix match", host: "generativelanguage.googleapis.com", wantDom: ".googleapis.com"},
		{name: "suffix match bare domain", host: "googleapis.com", wantDom: ".googleapis.com"},
		{name: "no match", host: "unknown.example.org", wantNil: true},
		{name: "case insensitive", host: "API.EXAMPLE.COM", wantDom: "api.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := cfg.MatchRoute(tt.host)
			if tt.wantNil {
				assert.Nil(t, route)
			} else {
				require.NotNil(t, route)
				assert.Equal(t, tt.wantDom, route.Domain)
			}
		})
	}
}

func TestPathAllowed(t *testing.T) {
	tests := []struct {
		name         string
		allowedPaths []string
		path         string
		want         bool
	}{
		{name: "empty list allows all", allowedPaths: nil, path: "/anything", want: true},
		{name: "matching prefix allows", allowedPaths: []string{"/BerriAI/litellm/"}, path: "/BerriAI/litellm/main/model_prices.json", want: true},
		{name: "exact prefix match allows", allowedPaths: []string{"/BerriAI/litellm/"}, path: "/BerriAI/litellm/", want: true},
		{name: "non-matching prefix rejects", allowedPaths: []string{"/BerriAI/litellm/"}, path: "/evil-repo/malware/payload", want: false},
		{name: "multiple prefixes any match allows", allowedPaths: []string{"/foo/", "/bar/"}, path: "/bar/baz", want: true},
		{name: "multiple prefixes none match rejects", allowedPaths: []string{"/foo/", "/bar/"}, path: "/qux/baz", want: false},
		{name: "partial prefix does not match", allowedPaths: []string{"/BerriAI/litellm/"}, path: "/BerriAI/litellm-fork/main/file", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := &Route{AllowedPaths: tt.allowedPaths}
			assert.Equal(t, tt.want, route.PathAllowed(tt.path))
		})
	}
}

func TestMatchRoutePortAware(t *testing.T) {
	cfg := &Config{
		Routes: []Route{
			{Domain: "api.example.com:6443", Injector: "kubernetes"},
			{Domain: "api.example.com:8443", Injector: "kubernetes"},
			{Domain: "api.example.com", Injector: "bearer"},
			{Domain: ".googleapis.com", Injector: "api_key"},
			{Domain: "[::1]:6443", Injector: "kubernetes"},
			{Domain: "[2001:db8::1]:8443", Injector: "kubernetes"},
		},
	}

	tests := []struct {
		name    string
		host    string
		wantNil bool
		wantDom string
	}{
		{name: "port-qualified match on 6443", host: "api.example.com:6443", wantDom: "api.example.com:6443"},
		{name: "port-qualified match on 8443", host: "api.example.com:8443", wantDom: "api.example.com:8443"},
		{name: "bare domain match with standard port", host: "api.example.com:443", wantDom: "api.example.com"},
		{name: "bare domain match without port", host: "api.example.com", wantDom: "api.example.com"},
		{name: "suffix match still works with port-qualified routes", host: "storage.googleapis.com:443", wantDom: ".googleapis.com"},
		{name: "no match on wrong port", host: "api.example.com:9999", wantDom: "api.example.com"},
		{name: "IPv6 loopback with port", host: "[::1]:6443", wantDom: "[::1]:6443"},
		{name: "IPv6 full address with port", host: "[2001:db8::1]:8443", wantDom: "[2001:db8::1]:8443"},
		{name: "IPv6 no match on wrong port", host: "[::1]:9999", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := cfg.MatchRoute(tt.host)
			if tt.wantNil {
				assert.Nil(t, route)
			} else {
				require.NotNil(t, route)
				assert.Equal(t, tt.wantDom, route.Domain)
			}
		})
	}
}
