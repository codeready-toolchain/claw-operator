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

// APIKeyInjector injects a secret value into a custom header.
type APIKeyInjector struct {
	header         string
	valuePrefix    string
	envVar         string
	defaultHeaders map[string]string
}

func NewAPIKeyInjector(route *Route) (*APIKeyInjector, error) {
	if route.Header == "" {
		return nil, fmt.Errorf("api_key injector requires header")
	}
	if route.EnvVar == "" {
		return nil, fmt.Errorf("api_key injector requires envVar")
	}
	return &APIKeyInjector{
		header:         route.Header,
		valuePrefix:    route.ValuePrefix,
		envVar:         route.EnvVar,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (a *APIKeyInjector) Inject(req *http.Request) error {
	value := os.Getenv(a.envVar)
	if value == "" {
		return fmt.Errorf("credential env var %s is empty", a.envVar)
	}
	req.Header.Set(a.header, a.valuePrefix+value)
	for k, v := range a.defaultHeaders {
		req.Header.Set(k, v)
	}
	return nil
}
