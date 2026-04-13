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
	"fmt"
	"net/http"
	"os"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// OAuth2Injector performs a client_credentials grant to obtain an access token,
// caches it via a reusable TokenSource, and injects Authorization: Bearer <token>.
// The client secret is read from an environment variable; client ID, token URL,
// and scopes are provided via the route config.
type OAuth2Injector struct {
	envVar         string
	clientID       string
	tokenURL       string
	scopes         []string
	defaultHeaders map[string]string

	once        sync.Once
	tokenSource oauth2.TokenSource
	initErr     error
}

func NewOAuth2Injector(route *Route) (*OAuth2Injector, error) {
	if route.EnvVar == "" {
		return nil, fmt.Errorf("oauth2 injector requires envVar (for client secret)")
	}
	if route.ClientID == "" {
		return nil, fmt.Errorf("oauth2 injector requires clientID")
	}
	if route.TokenURL == "" {
		return nil, fmt.Errorf("oauth2 injector requires tokenURL")
	}
	return &OAuth2Injector{
		envVar:         route.EnvVar,
		clientID:       route.ClientID,
		tokenURL:       route.TokenURL,
		scopes:         route.Scopes,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (o *OAuth2Injector) initTokenSource() {
	clientSecret := os.Getenv(o.envVar)
	if clientSecret == "" {
		o.initErr = fmt.Errorf("credential env var %s is empty", o.envVar)
		return
	}

	cfg := &clientcredentials.Config{
		ClientID:     o.clientID,
		ClientSecret: clientSecret,
		TokenURL:     o.tokenURL,
		Scopes:       o.scopes,
	}
	o.tokenSource = cfg.TokenSource(context.Background())
}

func (o *OAuth2Injector) Inject(req *http.Request) error {
	o.once.Do(o.initTokenSource)
	if o.initErr != nil {
		return o.initErr
	}

	token, err := o.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("oauth2 token exchange failed: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	for k, v := range o.defaultHeaders {
		req.Header.Set(k, v)
	}
	return nil
}
