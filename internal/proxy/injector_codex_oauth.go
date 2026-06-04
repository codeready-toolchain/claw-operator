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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	codexTokenURL = "https://auth.openai.com/oauth/token"
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// codexAuthFile mirrors the structure of ~/.codex/auth.json.
type codexAuthFile struct {
	AuthMode string `json:"auth_mode"`
	Tokens   struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

// CodexOAuthInjector loads a Codex auth.json file, obtains OAuth2 tokens via
// refresh_token grant, and injects Authorization + Codex-specific headers.
// Tokens are cached and auto-refreshed by the oauth2.TokenSource.
type CodexOAuthInjector struct {
	authFilePath   string
	defaultHeaders map[string]string

	once        sync.Once
	tokenSource oauth2.TokenSource
	accountID   string
	initErr     error
}

func NewCodexOAuthInjector(route *Route) (*CodexOAuthInjector, error) {
	if route.CodexAuthFilePath == "" {
		return nil, fmt.Errorf("codex_oauth injector requires codexAuthFilePath")
	}
	return &CodexOAuthInjector{
		authFilePath:   route.CodexAuthFilePath,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (c *CodexOAuthInjector) init() {
	data, err := os.ReadFile(c.authFilePath)
	if err != nil {
		c.initErr = fmt.Errorf("read codex auth file %s: %w", c.authFilePath, err)
		return
	}

	auth, err := ParseCodexAuthJSON(data)
	if err != nil {
		c.initErr = err
		return
	}

	c.accountID = auth.AccountID

	cfg := &oauth2.Config{
		ClientID: codexClientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: codexTokenURL,
		},
	}

	// AccessToken is intentionally left empty so Token.Valid() returns false
	// and the first call to TokenSource.Token() triggers an immediate refresh.
	// Do NOT set AccessToken without also setting a real Expiry — a zero Expiry
	// with a non-empty AccessToken causes the token to be reused forever.
	initialToken := &oauth2.Token{
		RefreshToken: auth.RefreshToken,
		TokenType:    "Bearer",
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)
	c.tokenSource = cfg.TokenSource(ctx, initialToken)
}

func (c *CodexOAuthInjector) Inject(req *http.Request) error {
	c.once.Do(c.init)
	if c.initErr != nil {
		return c.initErr
	}

	token, err := c.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("codex oauth token refresh failed: %w", err)
	}

	for k, v := range c.defaultHeaders {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("chatgpt-account-id", c.accountID)
	req.Header.Set("originator", "openclaw")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	return nil
}

// CodexAuthData holds the parsed fields from a Codex auth.json file.
type CodexAuthData struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
}

// ParseCodexAuthJSON validates and extracts data from a Codex auth.json file.
func ParseCodexAuthJSON(data []byte) (*CodexAuthData, error) {
	var auth codexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse codex auth.json: %w", err)
	}
	if auth.AuthMode != "chatgpt" {
		return nil, fmt.Errorf("codex auth.json: auth_mode must be \"chatgpt\", got %q", auth.AuthMode)
	}
	if auth.Tokens.RefreshToken == "" {
		return nil, fmt.Errorf("codex auth.json: tokens.refresh_token is required")
	}
	if auth.Tokens.AccountID == "" {
		return nil, fmt.Errorf("codex auth.json: tokens.account_id is required")
	}
	return &CodexAuthData{
		AccessToken:  auth.Tokens.AccessToken,
		RefreshToken: auth.Tokens.RefreshToken,
		AccountID:    auth.Tokens.AccountID,
	}, nil
}
