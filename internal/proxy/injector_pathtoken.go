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
	"strings"
)

// PathTokenInjector replaces a placeholder token in the URL path with the real
// credential. The client sends requests with a placeholder already embedded
// (e.g., /bot<placeholder>/sendMessage). The injector strips the placeholder
// segment and inserts the real token from an env var.
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

	path := req.URL.Path
	if !strings.HasPrefix(path, p.pathPrefix) {
		return fmt.Errorf("path %q does not start with expected prefix %q", path, p.pathPrefix)
	}

	remainder := path[len(p.pathPrefix):]
	if idx := strings.IndexByte(remainder, '/'); idx >= 0 {
		req.URL.Path = p.pathPrefix + token + remainder[idx:]
	} else {
		req.URL.Path = p.pathPrefix + token
	}

	for k, v := range p.defaultHeaders {
		req.Header.Set(k, v)
	}
	return nil
}
