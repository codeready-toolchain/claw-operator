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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2/google"
)

func TestDetectCredentialType(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantType google.CredentialsType
		wantErr  string
	}{
		{
			name:     "service account",
			json:     `{"type":"service_account","project_id":"test"}`,
			wantType: google.ServiceAccount,
		},
		{
			name:     "authorized user",
			json:     `{"type":"authorized_user","client_id":"test"}`,
			wantType: google.AuthorizedUser,
		},
		{
			name:    "external account rejected",
			json:    `{"type":"external_account","audience":"test"}`,
			wantErr: "unsupported GCP credential type",
		},
		{
			name:    "empty type rejected",
			json:    `{"type":""}`,
			wantErr: "unsupported GCP credential type",
		},
		{
			name:    "missing type field rejected",
			json:    `{"project_id":"test"}`,
			wantErr: "unsupported GCP credential type",
		},
		{
			name:    "invalid JSON",
			json:    `not json`,
			wantErr: "parse GCP credential file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := detectCredentialType([]byte(tt.json))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, ct)
		})
	}
}

func TestStripAuthHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Api-Key", "key123")
	req.Header.Set("X-Goog-Api-Key", "gkey")
	req.Header.Set("Content-Type", "application/json")

	StripAuthHeaders(req)

	assert.Empty(t, req.Header.Get("Authorization"))
	assert.Empty(t, req.Header.Get("X-Api-Key"))
	assert.Empty(t, req.Header.Get("X-Goog-Api-Key"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestNewInjector(t *testing.T) {
	tests := []struct {
		name    string
		route   Route
		wantErr bool
	}{
		{
			name:  "api_key injector",
			route: Route{Injector: "api_key", Header: "x-api-key", EnvVar: "CRED_TEST"},
		},
		{
			name:  "bearer injector",
			route: Route{Injector: "bearer", EnvVar: "CRED_TEST"},
		},
		{
			name:  "gcp injector",
			route: Route{Injector: "gcp", SAFilePath: "/tmp/sa.json"},
		},
		{
			name:    "unknown injector",
			route:   Route{Injector: "bogus"},
			wantErr: true,
		},
		{
			name:    "api_key without header",
			route:   Route{Injector: "api_key", EnvVar: "CRED_TEST"},
			wantErr: true,
		},
		{
			name:    "bearer without envVar",
			route:   Route{Injector: "bearer"},
			wantErr: true,
		},
		{
			name:  "none injector",
			route: Route{Injector: "none"},
		},
		{
			name:  "path_token injector",
			route: Route{Injector: "path_token", EnvVar: "CRED_TEST", PathPrefix: "/bot"},
		},
		{
			name:    "path_token without envVar",
			route:   Route{Injector: "path_token", PathPrefix: "/bot"},
			wantErr: true,
		},
		{
			name:    "path_token without pathPrefix",
			route:   Route{Injector: "path_token", EnvVar: "CRED_TEST"},
			wantErr: true,
		},
		{
			name:  "oauth2 injector",
			route: Route{Injector: "oauth2", EnvVar: "CRED_TEST", ClientID: "id", TokenURL: "https://example.com/token"},
		},
		{
			name:    "oauth2 without envVar",
			route:   Route{Injector: "oauth2", ClientID: "id", TokenURL: "https://example.com/token"},
			wantErr: true,
		},
		{
			name:    "oauth2 without clientID",
			route:   Route{Injector: "oauth2", EnvVar: "CRED_TEST", TokenURL: "https://example.com/token"},
			wantErr: true,
		},
		{
			name:    "oauth2 without tokenURL",
			route:   Route{Injector: "oauth2", EnvVar: "CRED_TEST", ClientID: "id"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inj, err := NewInjector(&tt.route)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, inj)
			}
		})
	}
}

func TestAPIKeyInjector(t *testing.T) {
	t.Setenv("CRED_GEMINI", "test-api-key")

	inj, err := NewAPIKeyInjector(&Route{
		Header:         "x-goog-api-key",
		EnvVar:         "CRED_GEMINI",
		DefaultHeaders: map[string]string{"x-custom": "value"},
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, inj.Inject(req))

	assert.Equal(t, "test-api-key", req.Header.Get("x-goog-api-key"))
	assert.Equal(t, "value", req.Header.Get("x-custom"))
}

func TestAPIKeyInjectorWithPrefix(t *testing.T) {
	t.Setenv("CRED_DISCORD", "bottoken123")

	inj, err := NewAPIKeyInjector(&Route{
		Header:      "Authorization",
		ValuePrefix: "Bot ",
		EnvVar:      "CRED_DISCORD",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, inj.Inject(req))

	assert.Equal(t, "Bot bottoken123", req.Header.Get("Authorization"))
}

func TestAPIKeyInjectorEmptyEnvVar(t *testing.T) {
	t.Setenv("CRED_EMPTY", "")

	inj, err := NewAPIKeyInjector(&Route{
		Header: "x-api-key",
		EnvVar: "CRED_EMPTY",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	err = inj.Inject(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CRED_EMPTY is empty")
}

func TestBearerInjector(t *testing.T) {
	t.Setenv("CRED_OPENAI", "sk-test-key")

	inj, err := NewBearerInjector(&Route{
		EnvVar:         "CRED_OPENAI",
		DefaultHeaders: map[string]string{"anthropic-version": "2023-06-01"},
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, inj.Inject(req))

	assert.Equal(t, "Bearer sk-test-key", req.Header.Get("Authorization"))
	assert.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
}

func TestNoneInjector(t *testing.T) {
	inj, err := NewNoneInjector(&Route{
		DefaultHeaders: map[string]string{"x-custom": "value"},
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, inj.Inject(req))

	assert.Equal(t, "value", req.Header.Get("x-custom"))
	assert.Empty(t, req.Header.Get("Authorization"), "none injector should not set auth headers")
}

func TestNoneInjectorNoHeaders(t *testing.T) {
	inj, err := NewNoneInjector(&Route{})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("Content-Type", "application/json")
	require.NoError(t, inj.Inject(req))

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestPathTokenInjector(t *testing.T) {
	t.Setenv("CRED_TELEGRAM", "123456:ABC-DEF")

	inj, err := NewPathTokenInjector(&Route{
		EnvVar:         "CRED_TELEGRAM",
		PathPrefix:     "/bot",
		DefaultHeaders: map[string]string{"x-custom": "value"},
	})
	require.NoError(t, err)

	t.Run("replaces placeholder token in path", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/botplaceholder/sendMessage", nil)
		require.NoError(t, inj.Inject(req))

		assert.Equal(t, "/bot123456:ABC-DEF/sendMessage", req.URL.Path)
		assert.Equal(t, "value", req.Header.Get("x-custom"))
	})

	t.Run("bare token without method", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/botplaceholder", nil)
		require.NoError(t, inj.Inject(req))

		assert.Equal(t, "/bot123456:ABC-DEF", req.URL.Path)
	})

	t.Run("multiple path segments after token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/botplaceholder/sendMessage/extra", nil)
		require.NoError(t, inj.Inject(req))

		assert.Equal(t, "/bot123456:ABC-DEF/sendMessage/extra", req.URL.Path)
	})

	t.Run("prefix only with no token segment", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot", nil)
		require.NoError(t, inj.Inject(req))

		assert.Equal(t, "/bot123456:ABC-DEF", req.URL.Path)
	})

	t.Run("realistic token with colons preserved", func(t *testing.T) {
		t.Setenv("CRED_TELEGRAM", "7891011:XYZ_abc-123")

		req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/botdummy/getMe", nil)
		require.NoError(t, inj.Inject(req))

		assert.Equal(t, "/bot7891011:XYZ_abc-123/getMe", req.URL.Path)
	})

	t.Run("error when path does not start with prefix", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/sendMessage", nil)
		err := inj.Inject(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not start with expected prefix")
	})
}

func TestPathTokenInjectorEmptyEnvVar(t *testing.T) {
	t.Setenv("CRED_EMPTY_PATH", "")

	inj, err := NewPathTokenInjector(&Route{
		EnvVar:     "CRED_EMPTY_PATH",
		PathPrefix: "/bot",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/botplaceholder/sendMessage", nil)
	err = inj.Inject(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CRED_EMPTY_PATH is empty")
}

func TestOAuth2InjectorEmptyEnvVar(t *testing.T) {
	t.Setenv("CRED_EMPTY_OAUTH", "")

	inj, err := NewOAuth2Injector(&Route{
		EnvVar:   "CRED_EMPTY_OAUTH",
		ClientID: "id",
		TokenURL: "https://example.com/token",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/data", nil)
	err = inj.Inject(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CRED_EMPTY_OAUTH is empty")
}
