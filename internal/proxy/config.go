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
	"fmt"
	"os"
	"strings"
)

// Route is a single route entry in the proxy config JSON.
type Route struct {
	Domain         string            `json:"domain"`
	Injector       string            `json:"injector"`
	Header         string            `json:"header,omitempty"`
	ValuePrefix    string            `json:"valuePrefix,omitempty"`
	EnvVar         string            `json:"envVar,omitempty"`
	SAFilePath     string            `json:"saFilePath,omitempty"`
	GCPProject     string            `json:"gcpProject,omitempty"`
	GCPLocation    string            `json:"gcpLocation,omitempty"`
	PathPrefix     string            `json:"pathPrefix,omitempty"`
	Upstream       string            `json:"upstream,omitempty"`
	ClientID       string            `json:"clientID,omitempty"`
	TokenURL       string            `json:"tokenURL,omitempty"`
	Scopes         []string          `json:"scopes,omitempty"`
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`
	KubeconfigPath string            `json:"kubeconfigPath,omitempty"`
	CACert         string            `json:"caCert,omitempty"`
}

// Config is the top-level proxy configuration.
type Config struct {
	Routes []Route `json:"routes"`
}

// LoadConfig reads and parses the proxy config JSON file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// MatchRoute finds the first route matching the given host.
// Exact matches are checked first (they appear first in the config by convention),
// then suffix matches (domains starting with ".").
// Port-qualified domains (e.g., "api.example.com:6443") match against the full
// hostname:port from the request. Bare domains use hostname-only matching.
func (c *Config) MatchRoute(host string) *Route {
	hostLower := strings.ToLower(host)

	// Extract hostname without port for bare domain matching
	hostname := hostLower
	if idx := strings.LastIndex(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	for i := range c.Routes {
		domain := strings.ToLower(c.Routes[i].Domain)
		if strings.HasPrefix(domain, ".") {
			// Suffix match always uses hostname only
			if strings.HasSuffix(hostname, domain) || hostname == domain[1:] {
				return &c.Routes[i]
			}
		} else if strings.Contains(domain, ":") {
			// Port-qualified domain: match against full host:port
			if hostLower == domain {
				return &c.Routes[i]
			}
		} else {
			// Bare domain: match hostname only
			if hostname == domain {
				return &c.Routes[i]
			}
		}
	}
	return nil
}

// MatchRouteByPath finds the first gateway route whose PathPrefix matches the request path.
// Returns the matched route and the path with the prefix stripped.
func (c *Config) MatchRouteByPath(path string) (*Route, string) {
	for i := range c.Routes {
		prefix := c.Routes[i].PathPrefix
		if prefix == "" || c.Routes[i].Upstream == "" {
			continue
		}
		if strings.HasPrefix(path, prefix+"/") {
			return &c.Routes[i], strings.TrimPrefix(path, prefix)
		}
		if path == prefix {
			return &c.Routes[i], "/"
		}
	}
	return nil, ""
}
