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
)

// Injector injects credentials into an HTTP request.
type Injector interface {
	Inject(req *http.Request) error
}

// authHeaders are stripped from every request before injection (defense in depth).
var authHeaders = []string{
	"Authorization",
	"X-Api-Key",
	"X-Goog-Api-Key",
	"Proxy-Authorization",
}

// StripAuthHeaders removes all known auth headers from the request.
func StripAuthHeaders(req *http.Request) {
	for _, h := range authHeaders {
		req.Header.Del(h)
	}
}

// NewInjector creates an Injector for the given route configuration.
func NewInjector(route *Route) (Injector, error) {
	switch route.Injector {
	case "api_key":
		return NewAPIKeyInjector(route)
	case "bearer":
		return NewBearerInjector(route)
	case "gcp":
		return NewGCPInjector(route)
	case "kubernetes":
		return NewKubernetesInjector(route)
	case "none":
		return NewNoneInjector(route)
	case "path_token":
		return NewPathTokenInjector(route)
	case "oauth2":
		return NewOAuth2Injector(route)
	default:
		return nil, fmt.Errorf("unknown injector type: %s", route.Injector)
	}
}
