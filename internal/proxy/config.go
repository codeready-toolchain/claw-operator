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

// pathEntryMatches reports whether path matches an AllowedPaths entry.
// Entries ending with "/" use prefix semantics; all others require exact match.
func pathEntryMatches(path, entry string) bool {
	if strings.HasSuffix(entry, "/") {
		return strings.HasPrefix(path, entry)
	}
	return path == entry
}

// PathAllowed reports whether the request path is permitted by this route.
// If AllowedPaths is empty the route is unrestricted. Otherwise the path
// must match at least one entry (exact for bare paths, prefix for "/" entries).
func (r *Route) PathAllowed(path string) bool {
	if len(r.AllowedPaths) == 0 {
		return true
	}
	for _, entry := range r.AllowedPaths {
		if pathEntryMatches(path, entry) {
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

	var portExactMatches, exactMatches, suffixMatches []*Route
	for i := range c.Routes {
		domain := strings.ToLower(c.Routes[i].Domain)
		if !domainMatches(domain, hostLower, hostname) {
			continue
		}
		if strings.HasPrefix(domain, ".") {
			suffixMatches = append(suffixMatches, &c.Routes[i])
		} else if _, _, err := net.SplitHostPort(domain); err == nil {
			portExactMatches = append(portExactMatches, &c.Routes[i])
		} else {
			exactMatches = append(exactMatches, &c.Routes[i])
		}
	}

	matches := portExactMatches
	if len(matches) == 0 {
		matches = exactMatches
	}
	if len(matches) == 0 {
		matches = suffixMatches
	}
	if len(matches) == 0 {
		return nil
	}
	if len(matches) == 1 || path == "" {
		return matches[0]
	}

	// Multiple routes for the same host: pick the route whose AllowedPaths
	// entry is the longest match for the request path. This prevents broad
	// entries (e.g. "/api/") from shadowing more specific ones
	// (e.g. "/api/admin/"). Fall back to the catch-all (no AllowedPaths).
	var catchAll, best *Route
	var bestLen int
	for _, r := range matches {
		if len(r.AllowedPaths) == 0 {
			if catchAll == nil {
				catchAll = r
			}
			continue
		}
		for _, entry := range r.AllowedPaths {
			if pathEntryMatches(path, entry) && len(entry) > bestLen {
				best = r
				bestLen = len(entry)
			}
		}
	}
	if best != nil {
		return best
	}
	return catchAll
}

// NeedsMITMForHost reports whether a matching route requires MITM for the host.
// Exact hostname matches take priority: if any exact match exists, only those
// routes are considered. Suffix/wildcard matches are evaluated only when no
// exact match is found. Used at CONNECT time when the request path is unknown.
func (c *Config) NeedsMITMForHost(host string) bool {
	hostLower := strings.ToLower(host)

	hostname := hostLower
	if h, _, err := net.SplitHostPort(hostLower); err == nil {
		hostname = h
	}

	var hasPortExact, portExactNeedsMITM bool
	var hasExact, exactNeedsMITM, suffixNeedsMITM bool
	for i := range c.Routes {
		domain := strings.ToLower(c.Routes[i].Domain)
		if !domainMatches(domain, hostLower, hostname) {
			continue
		}
		if strings.HasPrefix(domain, ".") {
			if c.Routes[i].NeedsMITM() {
				suffixNeedsMITM = true
			}
		} else if _, _, err := net.SplitHostPort(domain); err == nil {
			hasPortExact = true
			if c.Routes[i].NeedsMITM() {
				portExactNeedsMITM = true
			}
		} else {
			hasExact = true
			if c.Routes[i].NeedsMITM() {
				exactNeedsMITM = true
			}
		}
	}
	if hasPortExact {
		return portExactNeedsMITM
	}
	if hasExact {
		return exactNeedsMITM
	}
	return suffixNeedsMITM
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
