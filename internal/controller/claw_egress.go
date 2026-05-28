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

package controller

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// egressTarget represents a parsed egress destination derived from an MCP URL.
type egressTarget struct {
	Port      int
	Namespace string // empty = same namespace as the Claw instance
	External  bool
}

// classifyServiceURL parses a URL and classifies it as in-cluster (same or
// cross namespace) or external based on Kubernetes DNS naming conventions.
//
// Classification rules:
//
//	no dots                          → in-cluster, same namespace
//	ends .svc.cluster.local          → in-cluster, namespace = 2nd label
//	ends .svc                        → in-cluster, namespace = 2nd label
//	else (2+ labels, IP, etc.)       → external
//
// Two-label hostnames like "svc.namespace" are treated as external because
// NO_PROXY only bypasses .svc and .svc.cluster.local suffixes. Traffic to
// bare two-part names flows through the proxy, so a gateway egress rule
// would have no effect. Users should use the .svc suffix for cross-namespace
// in-cluster services (e.g., "mcp-server.shared-tools.svc:9001").
func classifyServiceURL(rawURL, instanceNamespace string) (egressTarget, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return egressTarget{}, fmt.Errorf("failed to parse URL %q: %w", rawURL, err)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return egressTarget{}, fmt.Errorf("URL %q has no hostname", rawURL)
	}

	port, err := resolvePort(parsed)
	if err != nil {
		return egressTarget{}, err
	}

	if net.ParseIP(hostname) != nil {
		return egressTarget{Port: port, External: true}, nil
	}

	parts := strings.Split(hostname, ".")

	var namespace string
	isInCluster := false

	switch {
	case len(parts) == 1:
		isInCluster = true
	case strings.HasSuffix(hostname, ".svc.cluster.local"):
		isInCluster = true
		namespace = parts[1]
	case strings.HasSuffix(hostname, ".svc"):
		isInCluster = true
		namespace = parts[1]
	}

	if !isInCluster {
		return egressTarget{Port: port, External: true}, nil
	}

	if namespace == instanceNamespace {
		namespace = ""
	}

	return egressTarget{Port: port, Namespace: namespace}, nil
}

// resolvePort extracts the port from a parsed URL, defaulting to 80 for HTTP
// and 443 for HTTPS.
func resolvePort(u *url.URL) (int, error) {
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return 0, fmt.Errorf("invalid port %q in URL %q: %w", p, u.String(), err)
		}
		return port, nil
	}
	switch u.Scheme {
	case "https":
		return 443, nil
	default:
		return 80, nil
	}
}

// classifyMcpEgressTargets iterates spec.mcpServers and returns deduplicated
// egress targets. Stdio servers (no URL) and unparseable URLs are skipped.
func classifyMcpEgressTargets(instance *clawv1alpha1.Claw) []egressTarget {
	log := ctrl.Log.WithName("egress")
	seen := map[string]bool{}
	var targets []egressTarget

	for name, mcp := range instance.Spec.McpServers {
		if mcp.URL == "" {
			continue
		}
		target, err := classifyServiceURL(mcp.URL, instance.Namespace)
		if err != nil {
			log.Info("Skipping MCP server with unparseable URL",
				"server", name, "url", mcp.URL, "error", err)
			continue
		}

		key := dedupKey(target)
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, target)
	}

	slices.SortFunc(targets, func(a, b egressTarget) int {
		if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		if a.External != b.External {
			if a.External {
				return 1
			}
			return -1
		}
		return 0
	})

	return targets
}

func dedupKey(t egressTarget) string {
	if t.External {
		return fmt.Sprintf("external:%d", t.Port)
	}
	return fmt.Sprintf("incluster:%s:%d", t.Namespace, t.Port)
}

// injectMcpGatewayEgressRules appends egress rules to {instance}-egress for
// in-cluster MCP targets. Same-namespace targets get podSelector:{} + port;
// cross-namespace targets get namespaceSelector with kubernetes.io/metadata.name + port.
func injectMcpGatewayEgressRules(
	objects []*unstructured.Unstructured,
	targets []egressTarget,
	instanceName string,
) error {
	inCluster := filterInCluster(targets)
	if len(inCluster) == 0 {
		return nil
	}

	npName := getEgressNetworkPolicyName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}

		egress, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
		if err != nil {
			return fmt.Errorf("failed to get egress rules from NetworkPolicy: %w", err)
		}
		if !found {
			egress = []any{}
		}

		for _, t := range inCluster {
			egress = append(egress, buildInClusterEgressRule(t))
		}

		if err := unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress"); err != nil {
			return fmt.Errorf("failed to set egress rules on NetworkPolicy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

func buildInClusterEgressRule(t egressTarget) map[string]any {
	ports := []any{
		map[string]any{
			"port":     int64(t.Port),
			"protocol": "TCP",
		},
	}

	var to []any
	if t.Namespace == "" {
		to = []any{
			map[string]any{
				"podSelector": map[string]any{},
			},
		}
	} else {
		to = []any{
			map[string]any{
				"namespaceSelector": map[string]any{
					"matchLabels": map[string]any{
						"kubernetes.io/metadata.name": t.Namespace,
					},
				},
			},
		}
	}

	return map[string]any{
		"to":    to,
		"ports": ports,
	}
}

// injectMcpProxyEgressPorts adds non-443 external MCP ports to the first egress
// rule in {instance}-proxy-egress, following the injectKubePortsIntoNetworkPolicy
// pattern.
func injectMcpProxyEgressPorts(
	objects []*unstructured.Unstructured,
	targets []egressTarget,
	instanceName string,
) error {
	uniquePorts := collectExternalNon443Ports(targets)
	if len(uniquePorts) == 0 {
		return nil
	}

	npName := getProxyEgressNetworkPolicyName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}

		egress, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
		if err != nil {
			return fmt.Errorf("failed to get egress rules from proxy NetworkPolicy: %w", err)
		}
		if !found || len(egress) == 0 {
			return fmt.Errorf("egress rules not found in proxy NetworkPolicy")
		}

		httpsRule, ok := egress[0].(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected egress rule type in proxy NetworkPolicy")
		}

		ports, _, _ := unstructured.NestedSlice(httpsRule, "ports")

		existingPorts := map[int64]bool{}
		for _, p := range ports {
			if pm, ok := p.(map[string]any); ok {
				if v, exists := pm["port"]; exists {
					if portNum, ok := v.(int64); ok {
						existingPorts[portNum] = true
					}
				}
			}
		}

		for _, port := range uniquePorts {
			if !existingPorts[int64(port)] {
				ports = append(ports, map[string]any{
					"port":     int64(port),
					"protocol": "TCP",
				})
			}
		}

		if err := unstructured.SetNestedSlice(httpsRule, ports, "ports"); err != nil {
			return fmt.Errorf("failed to set ports on proxy egress rule: %w", err)
		}
		egress[0] = httpsRule
		if err := unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress"); err != nil {
			return fmt.Errorf("failed to set egress rules on proxy NetworkPolicy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

func collectExternalNon443Ports(targets []egressTarget) []int {
	seen := map[int]bool{}
	var ports []int
	for _, t := range targets {
		if t.External && t.Port != 443 && !seen[t.Port] {
			seen[t.Port] = true
			ports = append(ports, t.Port)
		}
	}
	slices.Sort(ports)
	return ports
}

func filterInCluster(targets []egressTarget) []egressTarget {
	var result []egressTarget
	for _, t := range targets {
		if !t.External {
			result = append(result, t)
		}
	}
	return result
}

// injectAllowedEgress appends the user's spec.networkPolicy.allowedEgress rules
// to {instance}-egress. Rules are converted from typed NetworkPolicyEgressRule to
// unstructured maps via JSON round-trip.
func injectAllowedEgress(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.NetworkPolicy == nil || len(instance.Spec.NetworkPolicy.AllowedEgress) == 0 {
		return nil
	}

	npName := getEgressNetworkPolicyName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}

		egress, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
		if err != nil {
			return fmt.Errorf("failed to get egress rules from NetworkPolicy: %w", err)
		}
		if !found {
			egress = []any{}
		}

		for _, rule := range instance.Spec.NetworkPolicy.AllowedEgress {
			ruleJSON, err := json.Marshal(rule)
			if err != nil {
				return fmt.Errorf("failed to marshal allowedEgress rule: %w", err)
			}
			var ruleMap map[string]any
			if err := json.Unmarshal(ruleJSON, &ruleMap); err != nil {
				return fmt.Errorf("failed to unmarshal allowedEgress rule: %w", err)
			}
			egress = append(egress, ruleMap)
		}

		if err := unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress"); err != nil {
			return fmt.Errorf("failed to set egress rules on NetworkPolicy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}
