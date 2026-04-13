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

import "net/http"

// NoneInjector passes requests through without adding credentials.
// It still applies default headers if configured.
type NoneInjector struct {
	defaultHeaders map[string]string
}

func NewNoneInjector(route *Route) (*NoneInjector, error) {
	return &NoneInjector{
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (n *NoneInjector) Inject(req *http.Request) error {
	for k, v := range n.defaultHeaders {
		req.Header.Set(k, v)
	}
	return nil
}
