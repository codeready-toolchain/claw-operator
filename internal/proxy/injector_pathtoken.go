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
	"fmt"
	"net/http"
	"os"
)

// PathTokenInjector prepends a token into the URL path.
// For example, Telegram bots use /bot<token>/method — the prefix is "/bot"
// and the token is read from an env var, yielding /bot<token>/original/path.
type PathTokenInjector struct {
	envVar         string
	pathPrefix     string
	defaultHeaders map[string]string
}

func NewPathTokenInjector(route *Route) (*PathTokenInjector, error) {
	if route.EnvVar == "" {
		return nil, fmt.Errorf("path_token injector requires envVar")
	}
	if route.PathPrefix == "" {
		return nil, fmt.Errorf("path_token injector requires pathPrefix")
	}
	return &PathTokenInjector{
		envVar:         route.EnvVar,
		pathPrefix:     route.PathPrefix,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (p *PathTokenInjector) Inject(req *http.Request) error {
	token := os.Getenv(p.envVar)
	if token == "" {
		return fmt.Errorf("credential env var %s is empty", p.envVar)
	}
	req.URL.Path = p.pathPrefix + token + req.URL.Path
	for k, v := range p.defaultHeaders {
		req.Header.Set(k, v)
	}
	return nil
}
