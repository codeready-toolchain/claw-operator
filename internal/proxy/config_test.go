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
	"encoding/json"
	"net/http"
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
			route := cfg.MatchRoute(tt.host, "/any")
			if tt.wantNil {
				assert.Nil(t, route)
			} else {
				require.NotNil(t, route)
				assert.Equal(t, tt.wantDom, route.Domain)
			}
		})
	}
}

func TestMatchRouteMultiDomain(t *testing.T) {
	cfg := &Config{
		Routes: []Route{
			{Domain: "slack.com", Injector: "bearer", EnvVar: "CRED_SLACK_APP", AllowedPaths: []string{"/api/apps.connections.open"}},
			{Domain: "slack.com", Injector: "bearer", EnvVar: "CRED_SLACK_BOT"},
			{Domain: ".slack.com", Injector: "none"},
		},
	}

	t.Run("selects specific route when path matches allowedPaths", func(t *testing.T) {
		route := cfg.MatchRoute("slack.com", "/api/apps.connections.open")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_SLACK_APP", route.EnvVar)
	})

	t.Run("falls back to catch-all for non-matching path", func(t *testing.T) {
		route := cfg.MatchRoute("slack.com", "/api/chat.postMessage")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_SLACK_BOT", route.EnvVar)
	})

	t.Run("empty path returns first match", func(t *testing.T) {
		route := cfg.MatchRoute("slack.com", "")
		require.NotNil(t, route)
		assert.Equal(t, "slack.com", route.Domain)
	})

	t.Run("suffix match still works", func(t *testing.T) {
		route := cfg.MatchRoute("wss-primary.slack.com", "/any")
		require.NotNil(t, route)
		assert.Equal(t, ".slack.com", route.Domain)
	})

	t.Run("no match for unknown host", func(t *testing.T) {
		route := cfg.MatchRoute("unknown.com", "/any")
		assert.Nil(t, route)
	})

	t.Run("returns nil when all routes have allowedPaths and none match", func(t *testing.T) {
		noCatchAll := &Config{
			Routes: []Route{
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_A", AllowedPaths: []string{"/v1/"}},
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_B", AllowedPaths: []string{"/v2/"}},
			},
		}
		route := noCatchAll.MatchRoute("api.example.com", "/v3/data")
		assert.Nil(t, route)
	})

	t.Run("selects among multiple allowedPaths routes", func(t *testing.T) {
		multi := &Config{
			Routes: []Route{
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_V1", AllowedPaths: []string{"/v1/"}},
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_V2", AllowedPaths: []string{"/v2/"}},
			},
		}
		route := multi.MatchRoute("api.example.com", "/v2/data")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_V2", route.EnvVar)
	})

	t.Run("selects longest matching prefix among overlapping allowedPaths", func(t *testing.T) {
		overlap := &Config{
			Routes: []Route{
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_BROAD", AllowedPaths: []string{"/api/"}},
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_SPECIFIC", AllowedPaths: []string{"/api/admin/"}},
			},
		}
		route := overlap.MatchRoute("api.example.com", "/api/admin/users")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_SPECIFIC", route.EnvVar)
	})

	t.Run("falls back to broad prefix when specific does not match", func(t *testing.T) {
		overlap := &Config{
			Routes: []Route{
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_BROAD", AllowedPaths: []string{"/api/"}},
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_SPECIFIC", AllowedPaths: []string{"/api/admin/"}},
			},
		}
		route := overlap.MatchRoute("api.example.com", "/api/public/data")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_BROAD", route.EnvVar)
	})

	t.Run("exact host match takes priority over suffix match", func(t *testing.T) {
		cfg := &Config{
			Routes: []Route{
				{Domain: ".example.com", Injector: "bearer", EnvVar: "CRED_SUFFIX"},
				{Domain: "example.com", Injector: "bearer", EnvVar: "CRED_EXACT"},
			},
		}
		route := cfg.MatchRoute("example.com", "/any")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_EXACT", route.EnvVar)
	})

	t.Run("exact path entry does not overmatch", func(t *testing.T) {
		cfg := &Config{
			Routes: []Route{
				{Domain: "slack.com", Injector: "bearer", EnvVar: "CRED_APP", AllowedPaths: []string{"/api/apps.connections.open"}},
				{Domain: "slack.com", Injector: "bearer", EnvVar: "CRED_BOT"},
			},
		}
		route := cfg.MatchRoute("slack.com", "/api/apps.connections.openfoo")
		require.NotNil(t, route)
		assert.Equal(t, "CRED_BOT", route.EnvVar, "should fall through to catch-all, not overmatch the exact entry")
	})
}

func TestNeedsMITMForHost(t *testing.T) {
	cfg := &Config{
		Routes: []Route{
			{Domain: "slack.com", Injector: "bearer", EnvVar: "CRED_APP", AllowedPaths: []string{"/api/apps.connections.open"}},
			{Domain: "slack.com", Injector: "bearer", EnvVar: "CRED_BOT"},
			{Domain: ".slack.com", Injector: "none"},
			{Domain: "passthrough.example.com", Injector: "none"},
		},
	}

	t.Run("true when any route needs MITM", func(t *testing.T) {
		assert.True(t, cfg.NeedsMITMForHost("slack.com"))
	})

	t.Run("false for suffix-only none route", func(t *testing.T) {
		assert.False(t, cfg.NeedsMITMForHost("wss-primary.slack.com"))
	})

	t.Run("false for pure passthrough", func(t *testing.T) {
		assert.False(t, cfg.NeedsMITMForHost("passthrough.example.com"))
	})

	t.Run("false for unknown host", func(t *testing.T) {
		assert.False(t, cfg.NeedsMITMForHost("unknown.com"))
	})

	t.Run("true when mixed none and bearer share a domain", func(t *testing.T) {
		mixed := &Config{
			Routes: []Route{
				{Domain: "api.example.com", Injector: "none"},
				{Domain: "api.example.com", Injector: "bearer", EnvVar: "CRED_X", AllowedPaths: []string{"/secure/"}},
			},
		}
		assert.True(t, mixed.NeedsMITMForHost("api.example.com"))
	})

	t.Run("exact passthrough takes priority over suffix MITM", func(t *testing.T) {
		cfg := &Config{
			Routes: []Route{
				{Domain: "example.com", Injector: "none"},
				{Domain: ".example.com", Injector: "bearer", EnvVar: "CRED_X"},
			},
		}
		assert.False(t, cfg.NeedsMITMForHost("example.com"))
		assert.True(t, cfg.NeedsMITMForHost("api.example.com"))
	})
}

func TestDomainMatches(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		hostLower string
		hostname  string
		want      bool
	}{
		{name: "exact bare match", domain: "api.example.com", hostLower: "api.example.com", hostname: "api.example.com", want: true},
		{name: "exact bare no match", domain: "api.example.com", hostLower: "other.com", hostname: "other.com", want: false},
		{name: "suffix match subdomain", domain: ".example.com", hostLower: "api.example.com", hostname: "api.example.com", want: true},
		{name: "suffix match apex", domain: ".example.com", hostLower: "example.com", hostname: "example.com", want: true},
		{name: "suffix no match", domain: ".example.com", hostLower: "notexample.com", hostname: "notexample.com", want: false},
		{name: "port-qualified match", domain: "api.example.com:6443", hostLower: "api.example.com:6443", hostname: "api.example.com", want: true},
		{name: "port-qualified mismatch", domain: "api.example.com:6443", hostLower: "api.example.com:8443", hostname: "api.example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domainMatches(tt.domain, tt.hostLower, tt.hostname))
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
		{name: "exact entry matches exact path", allowedPaths: []string{"/api/apps.connections.open"}, path: "/api/apps.connections.open", want: true},
		{name: "exact entry rejects longer path", allowedPaths: []string{"/api/apps.connections.open"}, path: "/api/apps.connections.openfoo", want: false},
		{name: "exact entry rejects subpath", allowedPaths: []string{"/api/apps.connections.open"}, path: "/api/apps.connections.open/extra", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := &Route{AllowedPaths: tt.allowedPaths}
			assert.Equal(t, tt.want, route.PathAllowed(tt.path))
		})
	}
}

func TestNeedsMITM(t *testing.T) {
	tests := []struct {
		name  string
		route Route
		want  bool
	}{
		{name: "bearer injector needs MITM", route: Route{Injector: "bearer"}, want: true},
		{name: "api_key injector needs MITM", route: Route{Injector: "api_key"}, want: true},
		{name: "gcp injector needs MITM", route: Route{Injector: "gcp"}, want: true},
		{name: "kubernetes injector needs MITM", route: Route{Injector: "kubernetes"}, want: true},
		{name: "none without restrictions is direct tunnel", route: Route{Injector: "none"}, want: false},
		{name: "none with allowedPaths needs MITM", route: Route{Injector: "none", AllowedPaths: []string{"/foo/"}}, want: true},
		{name: "none with defaultHeaders needs MITM", route: Route{Injector: "none", DefaultHeaders: map[string]string{"X-Custom": "val"}}, want: true},
		{name: "none with both restrictions needs MITM", route: Route{Injector: "none", AllowedPaths: []string{"/foo/"}, DefaultHeaders: map[string]string{"X-Custom": "val"}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.route.NeedsMITM())
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
			route := cfg.MatchRoute(tt.host, "/any")
			if tt.wantNil {
				assert.Nil(t, route)
			} else {
				require.NotNil(t, route)
				assert.Equal(t, tt.wantDom, route.Domain)
			}
		})
	}
}

func TestMatchRoutePortExactPriority(t *testing.T) {
	cfg := &Config{
		Routes: []Route{
			{Domain: "api.example.com", Injector: "none"},
			{Domain: "api.example.com:6443", Injector: "kubernetes"},
		},
	}

	t.Run("port-exact wins over bare-host regardless of config order", func(t *testing.T) {
		route := cfg.MatchRoute("api.example.com:6443", "/any")
		require.NotNil(t, route)
		assert.Equal(t, "api.example.com:6443", route.Domain)
	})

	t.Run("bare-host still works for standard port", func(t *testing.T) {
		route := cfg.MatchRoute("api.example.com:443", "/any")
		require.NotNil(t, route)
		assert.Equal(t, "api.example.com", route.Domain)
	})

	t.Run("bare-host works without port", func(t *testing.T) {
		route := cfg.MatchRoute("api.example.com", "/any")
		require.NotNil(t, route)
		assert.Equal(t, "api.example.com", route.Domain)
	})
}

func TestNeedsMITMForHostPortExactPriority(t *testing.T) {
	cfg := &Config{
		Routes: []Route{
			{Domain: "api.example.com", Injector: "none"},
			{Domain: "api.example.com:6443", Injector: "kubernetes"},
		},
	}

	t.Run("port-exact MITM wins over bare-host passthrough", func(t *testing.T) {
		assert.True(t, cfg.NeedsMITMForHost("api.example.com:6443"))
	})

	t.Run("bare-host passthrough used for standard port", func(t *testing.T) {
		assert.False(t, cfg.NeedsMITMForHost("api.example.com:443"))
	})

	t.Run("bare-host passthrough used without port", func(t *testing.T) {
		assert.False(t, cfg.NeedsMITMForHost("api.example.com"))
	})
}

func TestRouteInjectorFieldNotSerialized(t *testing.T) {
	route := Route{
		Domain:   "example.com",
		Injector: "none",
		injector: injectorFunc(func(_ *http.Request) error { return nil }),
	}

	data, err := json.Marshal(route)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasInjectorField := raw["injector"]
	assert.True(t, hasInjectorField, "JSON 'injector' key should be the string type field")

	var decoded Route
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Nil(t, decoded.injector, "unexported injector field must not survive JSON round-trip")
}

type injectorFunc func(*http.Request) error

func (f injectorFunc) Inject(req *http.Request) error { return f(req) }
