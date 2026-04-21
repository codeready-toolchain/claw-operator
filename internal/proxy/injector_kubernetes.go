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
	"net/url"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesInjector injects Bearer tokens from a kubeconfig file, mapping
// each cluster's hostname:port to its corresponding user token.
type KubernetesInjector struct {
	tokens         map[string]string // normalized "hostname:port" -> bearer token
	defaultHeaders map[string]string
}

// NewKubernetesInjector creates a KubernetesInjector from a route with a kubeconfigPath.
// It parses the kubeconfig once and builds a hostname:port -> token lookup map.
func NewKubernetesInjector(route *Route) (*KubernetesInjector, error) {
	if route.KubeconfigPath == "" {
		return nil, fmt.Errorf("kubeconfigPath is required for kubernetes injector")
	}

	cfg, err := clientcmd.LoadFromFile(route.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", route.KubeconfigPath, err)
	}

	tokens := make(map[string]string)

	// Build cluster name -> server URL map
	clusterServers := make(map[string]string)
	for name, cluster := range cfg.Clusters {
		clusterServers[name] = cluster.Server
	}

	// Walk contexts to map server -> token
	for _, ctx := range cfg.Contexts {
		serverURL, ok := clusterServers[ctx.Cluster]
		if !ok {
			continue
		}
		authInfo, ok := cfg.AuthInfos[ctx.AuthInfo]
		if !ok {
			continue
		}
		key := normalizeHost(serverURL)
		if key == "" {
			continue
		}
		tokens[key] = authInfo.Token
	}

	return &KubernetesInjector{
		tokens:         tokens,
		defaultHeaders: route.DefaultHeaders,
	}, nil
}

// Inject sets the Authorization header with the appropriate Bearer token
// for the request's target hostname:port.
func (k *KubernetesInjector) Inject(req *http.Request) error {
	host := strings.ToLower(req.URL.Host)
	if !strings.Contains(host, ":") {
		host += ":443"
	}

	token, ok := k.tokens[host]
	if !ok {
		return fmt.Errorf("no token for host %s", host)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	for key, val := range k.defaultHeaders {
		req.Header.Set(key, val)
	}

	return nil
}

// normalizeHost extracts "hostname:port" from a server URL, defaulting port to "443".
func normalizeHost(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return ""
	}
	hostname := u.Hostname()
	if hostname == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(hostname) + ":" + port
}
