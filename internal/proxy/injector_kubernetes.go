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

// KubernetesInjector reads a projected SA token file on each request and injects
// Authorization: Bearer <token>. The file is re-read per request to handle token rotation.
type KubernetesInjector struct {
	tokenPath      string
	defaultHeaders map[string]string
}

func NewKubernetesInjector(route *Route) (*KubernetesInjector, error) {
	if route.SATokenPath == "" {
		return nil, fmt.Errorf("kubernetes injector requires saTokenPath")
	}
	return &KubernetesInjector{
		tokenPath:      route.SATokenPath,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

func (k *KubernetesInjector) Inject(req *http.Request) error {
	data, err := os.ReadFile(k.tokenPath)
	if err != nil {
		return fmt.Errorf("read SA token from %s: %w", k.tokenPath, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return fmt.Errorf("SA token file %s is empty", k.tokenPath)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range k.defaultHeaders {
		req.Header.Set(k, v)
	}
	return nil
}
