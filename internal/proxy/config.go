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
	"net"
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
	AllowedPaths   []string          `json:"allowedPaths,omitempty"`

	injector Injector `json:"-"`
}

// NeedsMITM reports whether the route requires TLS interception (MITM).
// Routes that inject credentials, filter paths, or set default headers need MITM
// to inspect and modify HTTP requests. Pure passthrough routes (injector "none"
// with no path or header requirements) use a direct CONNECT tunnel instead,
// which is required for protocols like WhatsApp's Noise handshake that fail
// under TLS interception.
func (r *Route) NeedsMITM() bool {
	if r.Injector != "none" {
		return true
	}
	return len(r.AllowedPaths) > 0 || len(r.DefaultHeaders) > 0
}

// PathAllowed reports whether the request path is permitted by this route.
// If AllowedPaths is empty the route is unrestricted. Otherwise the path
// must start with at least one of the listed prefixes.
func (r *Route) PathAllowed(path string) bool {
	if len(r.AllowedPaths) == 0 {
		return true
	}
	for _, prefix := range r.AllowedPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
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

// domainMatches reports whether the route's domain matches the given host.
func domainMatches(domain, hostLower, hostname string) bool {
	if strings.HasPrefix(domain, ".") {
		return strings.HasSuffix(hostname, domain) || hostname == domain[1:]
	}
	if _, _, err := net.SplitHostPort(domain); err == nil {
		return hostLower == domain
	}
	return hostname == domain
}

// MatchRoute finds the best route matching the given host and request path.
// When only one route matches the host, it is returned directly. When multiple
// routes share the same domain, the path is used to discriminate: a route whose
// AllowedPaths matches the request path is preferred over a catch-all route
// (one with no AllowedPaths). If path is empty, the first matching route is
// returned (used by CONNECT before the request path is known).
func (c *Config) MatchRoute(host, path string) *Route {
	hostLower := strings.ToLower(host)

	hostname := hostLower
	if h, _, err := net.SplitHostPort(hostLower); err == nil {
		hostname = h
	}

	var matches []*Route
	for i := range c.Routes {
		domain := strings.ToLower(c.Routes[i].Domain)
		if domainMatches(domain, hostLower, hostname) {
			matches = append(matches, &c.Routes[i])
		}
	}

	if len(matches) == 0 {
		return nil
	}
	if len(matches) == 1 || path == "" {
		return matches[0]
	}

	// Multiple routes for the same host: prefer the one whose AllowedPaths
	// matches the request path, fall back to the catch-all (no AllowedPaths).
	var catchAll *Route
	for _, r := range matches {
		if len(r.AllowedPaths) == 0 {
			if catchAll == nil {
				catchAll = r
			}
			continue
		}
		if r.PathAllowed(path) {
			return r
		}
	}
	return catchAll
}

// NeedsMITMForHost reports whether any route matching the host requires MITM.
// Used at CONNECT time when the request path is not yet known.
func (c *Config) NeedsMITMForHost(host string) bool {
	hostLower := strings.ToLower(host)

	hostname := hostLower
	if h, _, err := net.SplitHostPort(hostLower); err == nil {
		hostname = h
	}

	for i := range c.Routes {
		domain := strings.ToLower(c.Routes[i].Domain)
		if domainMatches(domain, hostLower, hostname) && c.Routes[i].NeedsMITM() {
			return true
		}
	}
	return false
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
