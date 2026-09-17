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
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestParseCodexAuthJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
		wantID  string
	}{
		{
			name:   "valid auth.json",
			json:   `{"auth_mode":"chatgpt","tokens":{"access_token":"eyJ...","refresh_token":"v1.abc","account_id":"acct_123"}}`,
			wantID: "acct_123",
		},
		{
			name:   "valid without access_token",
			json:   `{"auth_mode":"chatgpt","tokens":{"refresh_token":"v1.abc","account_id":"acct_456"}}`,
			wantID: "acct_456",
		},
		{
			name:    "wrong auth_mode",
			json:    `{"auth_mode":"api_key","tokens":{"refresh_token":"v1.abc","account_id":"acct_123"}}`,
			wantErr: `auth_mode must be "chatgpt"`,
		},
		{
			name:    "missing auth_mode",
			json:    `{"tokens":{"refresh_token":"v1.abc","account_id":"acct_123"}}`,
			wantErr: `auth_mode must be "chatgpt"`,
		},
		{
			name:    "missing refresh_token",
			json:    `{"auth_mode":"chatgpt","tokens":{"account_id":"acct_123"}}`,
			wantErr: "refresh_token is required",
		},
		{
			name:    "missing account_id",
			json:    `{"auth_mode":"chatgpt","tokens":{"refresh_token":"v1.abc"}}`,
			wantErr: "account_id is required",
		},
		{
			name:    "invalid JSON",
			json:    `not json`,
			wantErr: "parse codex auth.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCodexAuthJSON([]byte(tt.json))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, result.AccountID)
		})
	}
}

func TestNewCodexOAuthInjector(t *testing.T) {
	t.Run("requires codexAuthFilePath", func(t *testing.T) {
		_, err := NewCodexOAuthInjector(&Route{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codexAuthFilePath")
	})

	t.Run("accepts valid route", func(t *testing.T) {
		inj, err := NewCodexOAuthInjector(&Route{
			CodexAuthFilePath: "/tmp/auth.json",
		})
		require.NoError(t, err)
		require.NotNil(t, inj)
	})
}

func TestNewInjectorCodexOAuth(t *testing.T) {
	inj, err := NewInjector(&Route{
		Injector:          injectorCodexOAuth,
		CodexAuthFilePath: "/tmp/auth.json",
	})
	require.NoError(t, err)
	require.NotNil(t, inj)
	_, ok := inj.(*CodexOAuthInjector)
	assert.True(t, ok)
}

func TestCodexOAuthInjectorHappyPath(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-codex-token","token_type":"Bearer","expires_in":86400}`))
	}))
	defer tokenServer.Close()

	authJSON := `{"auth_mode":"chatgpt","tokens":{"refresh_token":"v1.refresh","account_id":"acct_test123"}}`
	authFile := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(authFile, []byte(authJSON), 0600))

	inj := &CodexOAuthInjector{
		authFilePath:   authFile,
		defaultHeaders: map[string]string{"x-custom": "value"},
	}
	// Override init to use test token server
	inj.once.Do(func() {
		data, err := os.ReadFile(inj.authFilePath)
		if err != nil {
			inj.initErr = err
			return
		}
		auth, err := ParseCodexAuthJSON(data)
		if err != nil {
			inj.initErr = err
			return
		}
		inj.accountID = auth.AccountID

		cfg := &testCodexConfig{tokenURL: tokenServer.URL + "/token"}
		inj.tokenSource = cfg.tokenSource(auth.RefreshToken)
	})

	req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	err := inj.Inject(req)
	require.NoError(t, err)

	assert.Equal(t, "Bearer test-codex-token", req.Header.Get("Authorization"))
	assert.Equal(t, "acct_test123", req.Header.Get("chatgpt-account-id"))
	assert.Equal(t, "openclaw", req.Header.Get("originator"))
	assert.Equal(t, "responses=experimental", req.Header.Get("OpenAI-Beta"))
	assert.Equal(t, "value", req.Header.Get("x-custom"))
}

func TestCodexOAuthInjectorSkipsAuthorizationDefaultHeader(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"real-token","token_type":"Bearer","expires_in":86400}`))
	}))
	defer tokenServer.Close()

	authJSON := `{"auth_mode":"chatgpt","tokens":{"refresh_token":"v1.refresh","account_id":"acct_skip"}}`
	authFile := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(authFile, []byte(authJSON), 0600))

	inj := &CodexOAuthInjector{
		authFilePath:   authFile,
		defaultHeaders: map[string]string{"Authorization": "should-be-ignored", "x-extra": "kept"},
	}
	inj.once.Do(func() {
		data, _ := os.ReadFile(inj.authFilePath)
		auth, _ := ParseCodexAuthJSON(data)
		inj.accountID = auth.AccountID
		cfg := &testCodexConfig{tokenURL: tokenServer.URL + "/token"}
		inj.tokenSource = cfg.tokenSource(auth.RefreshToken)
	})

	req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, inj.Inject(req))

	assert.Equal(t, "Bearer real-token", req.Header.Get("Authorization"))
	assert.Equal(t, "kept", req.Header.Get("x-extra"))
}

func TestCodexOAuthInjectorMissingFile(t *testing.T) {
	inj, err := NewCodexOAuthInjector(&Route{
		CodexAuthFilePath: "/nonexistent/auth.json",
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://chatgpt.com/api", nil)
	err = inj.Inject(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read codex auth file")
}

func TestCodexOAuthInjectorInvalidJSON(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(authFile, []byte("not json"), 0600))

	inj, err := NewCodexOAuthInjector(&Route{
		CodexAuthFilePath: authFile,
	})
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://chatgpt.com/api", nil)
	err = inj.Inject(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse codex auth.json")
}

func TestStripAuthHeadersIncludesCodexHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("Chatgpt-Account-Id", "acct_123")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")

	StripAuthHeaders(req)

	assert.Empty(t, req.Header.Get("Chatgpt-Account-Id"))
	assert.Empty(t, req.Header.Get("OpenAI-Beta"))
	assert.Empty(t, req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

// testCodexConfig is a helper that creates an oauth2.TokenSource using a test token URL.
type testCodexConfig struct {
	tokenURL string
}

func (c *testCodexConfig) tokenSource(refreshToken string) *codexTokenSource {
	return &codexTokenSource{
		tokenURL:     c.tokenURL,
		refreshToken: refreshToken,
	}
}

// codexTokenSource fetches tokens from a test server for unit tests.
type codexTokenSource struct {
	tokenURL     string
	refreshToken string
}

func (s *codexTokenSource) Token() (*oauth2.Token, error) {
	resp, err := http.Post(s.tokenURL, "application/x-www-form-urlencoded", nil) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, err
	}
	return &oauth2.Token{
		AccessToken: tok.AccessToken,
		TokenType:   tok.TokenType,
	}, nil
}
