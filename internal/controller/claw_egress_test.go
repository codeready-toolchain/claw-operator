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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- classifyServiceURL tests ---

func TestClassifyServiceURL(t *testing.T) {
	const instanceNS = "my-ns"

	tests := []struct {
		name      string
		rawURL    string
		want      egressTarget
		wantError bool
	}{
		{
			name:   "bare hostname same namespace",
			rawURL: "http://mcp-customer:9001/mcp",
			want:   egressTarget{Port: 9001},
		},
		{
			name:   "two-part cross namespace",
			rawURL: "http://mcp-server.shared-tools:9001/mcp",
			want:   egressTarget{Port: 9001, Namespace: "shared-tools"},
		},
		{
			name:   "FQDN .svc cross namespace",
			rawURL: "http://mcp-server.shared-tools.svc:9001/mcp",
			want:   egressTarget{Port: 9001, Namespace: "shared-tools"},
		},
		{
			name:   "FQDN .svc.cluster.local cross namespace",
			rawURL: "http://mcp-server.shared-tools.svc.cluster.local:9001/mcp",
			want:   egressTarget{Port: 9001, Namespace: "shared-tools"},
		},
		{
			name:   "FQDN pointing to own namespace treated as same namespace",
			rawURL: "http://mcp-server.my-ns.svc.cluster.local:9001/mcp",
			want:   egressTarget{Port: 9001},
		},
		{
			name:   "two-part pointing to own namespace treated as same namespace",
			rawURL: "http://mcp-server.my-ns:9001/mcp",
			want:   egressTarget{Port: 9001},
		},
		{
			name:   "external hostname",
			rawURL: "https://mcp.example.com/mcp",
			want:   egressTarget{Port: 443, External: true},
		},
		{
			name:   "external hostname non-443 port",
			rawURL: "https://mcp.example.com:8443/mcp",
			want:   egressTarget{Port: 8443, External: true},
		},
		{
			name:   "IP address treated as external",
			rawURL: "http://203.0.113.50:9001/mcp",
			want:   egressTarget{Port: 9001, External: true},
		},
		{
			name:   "IPv6 address treated as external",
			rawURL: "http://[::1]:9001/mcp",
			want:   egressTarget{Port: 9001, External: true},
		},
		{
			name:   "HTTP default port 80",
			rawURL: "http://mcp-server/mcp",
			want:   egressTarget{Port: 80},
		},
		{
			name:   "HTTPS default port 443",
			rawURL: "https://mcp.example.com/mcp",
			want:   egressTarget{Port: 443, External: true},
		},
		{
			name:   "external on port 443 no NP change needed",
			rawURL: "https://mcp.example.com:443/mcp",
			want:   egressTarget{Port: 443, External: true},
		},
		{
			name:      "empty URL",
			rawURL:    "",
			wantError: true,
		},
		{
			name:      "malformed URL",
			rawURL:    "://broken",
			wantError: true,
		},
		{
			name:      "no hostname",
			rawURL:    "http:///path",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyServiceURL(tt.rawURL, instanceNS)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- classifyMcpEgressTargets tests ---

func TestClassifyMcpEgressTargets(t *testing.T) {
	t.Run("should classify mixed MCP servers and deduplicate", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "my-ns"},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"same-ns":      {URL: "http://mcp-a:9001/mcp"},
					"same-ns-dup":  {URL: "http://mcp-b:9001/mcp"},
					"cross-ns":     {URL: "http://mcp-server.shared-tools:9001/mcp"},
					"external":     {URL: "https://mcp.example.com:8443/mcp"},
					"external-443": {URL: "https://mcp.other.com/mcp"},
					"stdio":        {Command: "npx", Args: []string{"-y", "mcp-server"}},
				},
			},
		}

		targets := classifyMcpEgressTargets(instance)

		sameNS := filterByNamespace(targets, "")
		crossNS := filterByNamespace(targets, "shared-tools")
		external := filterExternal(targets)

		assert.Len(t, sameNS, 1, "same-namespace targets should be deduplicated by port")
		assert.Equal(t, 9001, sameNS[0].Port)

		assert.Len(t, crossNS, 1)
		assert.Equal(t, 9001, crossNS[0].Port)

		assert.Len(t, external, 2, "should have two external targets (8443 and 443)")
	})

	t.Run("should skip unparseable URLs", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "my-ns"},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"good":   {URL: "http://mcp-a:9001/mcp"},
					"broken": {URL: "://broken"},
				},
			},
		}

		targets := classifyMcpEgressTargets(instance)
		assert.Len(t, targets, 1, "broken URL should be skipped")
	})

	t.Run("should return empty for no MCP servers", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "my-ns"},
		}
		targets := classifyMcpEgressTargets(instance)
		assert.Empty(t, targets)
	})

	t.Run("should return empty for stdio-only MCP servers", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "my-ns"},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"stdio": {Command: "npx"},
				},
			},
		}
		targets := classifyMcpEgressTargets(instance)
		assert.Empty(t, targets)
	})
}

// --- injectMcpGatewayEgressRules tests ---

func TestInjectMcpGatewayEgressRules(t *testing.T) {
	makeGatewayNP := func() []*unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetKind(NetworkPolicyKind)
		np.SetName(getEgressNetworkPolicyName(testInstanceName))
		np.Object["spec"] = map[string]any{
			"egress": []any{
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{"app": "claw-proxy"},
							},
						},
					},
					"ports": []any{
						map[string]any{"port": int64(8080), "protocol": "TCP"},
					},
				},
			},
		}
		return []*unstructured.Unstructured{np}
	}

	t.Run("should append same-namespace egress rule", func(t *testing.T) {
		objects := makeGatewayNP()
		targets := []egressTarget{{Port: 9001}}

		require.NoError(t, injectMcpGatewayEgressRules(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2, "should have original + new rule")

		rule := egress[1].(map[string]any)
		to := rule["to"].([]any)
		toEntry := to[0].(map[string]any)
		_, hasPodSelector := toEntry["podSelector"]
		assert.True(t, hasPodSelector, "same-namespace rule should use podSelector")

		ports := rule["ports"].([]any)
		assert.Equal(t, int64(9001), ports[0].(map[string]any)["port"])
	})

	t.Run("should append cross-namespace egress rule", func(t *testing.T) {
		objects := makeGatewayNP()
		targets := []egressTarget{{Port: 9001, Namespace: "shared-tools"}}

		require.NoError(t, injectMcpGatewayEgressRules(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2)

		rule := egress[1].(map[string]any)
		to := rule["to"].([]any)
		toEntry := to[0].(map[string]any)
		nsSelector := toEntry["namespaceSelector"].(map[string]any)
		matchLabels := nsSelector["matchLabels"].(map[string]any)
		assert.Equal(t, "shared-tools", matchLabels["kubernetes.io/metadata.name"])
	})

	t.Run("should be no-op with no in-cluster targets", func(t *testing.T) {
		objects := makeGatewayNP()
		targets := []egressTarget{{Port: 8443, External: true}}

		require.NoError(t, injectMcpGatewayEgressRules(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 1, "should not modify NP for external-only targets")
	})

	t.Run("should be no-op with empty targets", func(t *testing.T) {
		objects := makeGatewayNP()

		require.NoError(t, injectMcpGatewayEgressRules(objects, nil, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 1)
	})

	t.Run("should return error when NP not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		targets := []egressTarget{{Port: 9001}}

		err := injectMcpGatewayEgressRules(objects, targets, testInstanceName)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// --- injectMcpProxyEgressPorts tests ---

func TestInjectMcpProxyEgressPorts(t *testing.T) {
	makeProxyNP := func() []*unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetKind(NetworkPolicyKind)
		np.SetName(getProxyEgressNetworkPolicyName(testInstanceName))
		np.Object["spec"] = map[string]any{
			"egress": []any{
				map[string]any{
					"ports": []any{
						map[string]any{"port": int64(443), "protocol": "TCP"},
					},
				},
			},
		}
		return []*unstructured.Unstructured{np}
	}

	t.Run("should add non-443 external port", func(t *testing.T) {
		objects := makeProxyNP()
		targets := []egressTarget{{Port: 8443, External: true}}

		require.NoError(t, injectMcpProxyEgressPorts(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 2, "should have original 443 + new 8443")

		var found8443 bool
		for _, p := range ports {
			if p.(map[string]any)["port"] == int64(8443) {
				found8443 = true
			}
		}
		assert.True(t, found8443)
	})

	t.Run("should not duplicate existing 443", func(t *testing.T) {
		objects := makeProxyNP()
		targets := []egressTarget{{Port: 443, External: true}}

		require.NoError(t, injectMcpProxyEgressPorts(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 1, "should not add duplicate 443")
	})

	t.Run("should be no-op with no external targets", func(t *testing.T) {
		objects := makeProxyNP()
		targets := []egressTarget{{Port: 9001}}

		require.NoError(t, injectMcpProxyEgressPorts(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 1)
	})

	t.Run("should be no-op with empty targets", func(t *testing.T) {
		objects := makeProxyNP()

		require.NoError(t, injectMcpProxyEgressPorts(objects, nil, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 1)
	})

	t.Run("should deduplicate multiple external targets with same port", func(t *testing.T) {
		objects := makeProxyNP()
		targets := []egressTarget{
			{Port: 8443, External: true},
			{Port: 8443, External: true},
			{Port: 9443, External: true},
		}

		require.NoError(t, injectMcpProxyEgressPorts(objects, targets, testInstanceName))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 3, "should have 443 + 8443 + 9443")
	})
}

// --- injectAllowedEgress tests ---

func TestInjectAllowedEgress(t *testing.T) {
	makeGatewayNP := func() []*unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetKind(NetworkPolicyKind)
		np.SetName(getEgressNetworkPolicyName(testInstanceName))
		np.Object["spec"] = map[string]any{
			"egress": []any{
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{"app": "claw-proxy"},
							},
						},
					},
					"ports": []any{
						map[string]any{"port": int64(8080), "protocol": "TCP"},
					},
				},
			},
		}
		return []*unstructured.Unstructured{np}
	}

	t.Run("should append user-defined egress rules", func(t *testing.T) {
		objects := makeGatewayNP()
		port3000 := intstr.FromInt32(3000)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				NetworkPolicy: &clawv1alpha1.NetworkPolicySpec{
					AllowedEgress: []netv1.NetworkPolicyEgressRule{
						{
							To: []netv1.NetworkPolicyPeer{
								{
									NamespaceSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"kubernetes.io/metadata.name": "langfuse",
										},
									},
								},
							},
							Ports: []netv1.NetworkPolicyPort{
								{Port: &port3000, Protocol: protocolPtr(corev1.ProtocolTCP)},
							},
						},
					},
				},
			},
		}

		require.NoError(t, injectAllowedEgress(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 2, "should have original + user rule")

		rule := egress[1].(map[string]any)
		to := rule["to"].([]any)
		toEntry := to[0].(map[string]any)
		nsSelector := toEntry["namespaceSelector"].(map[string]any)
		matchLabels := nsSelector["matchLabels"].(map[string]any)
		assert.Equal(t, "langfuse", matchLabels["kubernetes.io/metadata.name"])
	})

	t.Run("should be no-op when networkPolicy is nil", func(t *testing.T) {
		objects := makeGatewayNP()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}

		require.NoError(t, injectAllowedEgress(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 1)
	})

	t.Run("should be no-op when allowedEgress is empty", func(t *testing.T) {
		objects := makeGatewayNP()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				NetworkPolicy: &clawv1alpha1.NetworkPolicySpec{},
			},
		}

		require.NoError(t, injectAllowedEgress(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 1)
	})

	t.Run("should append multiple user rules", func(t *testing.T) {
		objects := makeGatewayNP()
		port3000 := intstr.FromInt32(3000)
		port5432 := intstr.FromInt32(5432)
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				NetworkPolicy: &clawv1alpha1.NetworkPolicySpec{
					AllowedEgress: []netv1.NetworkPolicyEgressRule{
						{
							To: []netv1.NetworkPolicyPeer{
								{
									NamespaceSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"kubernetes.io/metadata.name": "langfuse",
										},
									},
								},
							},
							Ports: []netv1.NetworkPolicyPort{
								{Port: &port3000, Protocol: protocolPtr(corev1.ProtocolTCP)},
							},
						},
						{
							To: []netv1.NetworkPolicyPeer{
								{PodSelector: &metav1.LabelSelector{}},
							},
							Ports: []netv1.NetworkPolicyPort{
								{Port: &port5432, Protocol: protocolPtr(corev1.ProtocolTCP)},
							},
						},
					},
				},
			},
		}

		require.NoError(t, injectAllowedEgress(objects, instance))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		assert.Len(t, egress, 3, "should have original + 2 user rules")
	})
}

// --- helpers ---

func protocolPtr(p corev1.Protocol) *corev1.Protocol {
	return &p
}

func filterByNamespace(targets []egressTarget, ns string) []egressTarget {
	var result []egressTarget
	for _, t := range targets {
		if !t.External && t.Namespace == ns {
			result = append(result, t)
		}
	}
	return result
}

func filterExternal(targets []egressTarget) []egressTarget {
	var result []egressTarget
	for _, t := range targets {
		if t.External {
			result = append(result, t)
		}
	}
	return result
}

// --- Integration tests (envtest) ---

// createClawInstanceWithMcpServers creates a Claw with credentials + MCP servers.
func createClawInstanceWithMcpServers(t *testing.T, ctx context.Context, name, ns string, mcpServers map[string]clawv1alpha1.McpServerSpec, np *clawv1alpha1.NetworkPolicySpec) { //nolint:unparam
	t.Helper()
	secret := createTestAPIKeySecret(aiModelSecret, ns, aiModelSecretKey, aiModelSecretValue)
	require.NoError(t, k8sClient.Create(ctx, secret))

	instance := &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: clawv1alpha1.ClawSpec{
			Credentials:   testCredentials(),
			McpServers:    mcpServers,
			NetworkPolicy: np,
		},
	}
	require.NoError(t, k8sClient.Create(ctx, instance))
}

func TestEgressIntegration(t *testing.T) {
	t.Run("in-cluster MCP adds gateway egress rule", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		createClawInstanceWithMcpServers(t, ctx, testInstanceName, namespace,
			map[string]clawv1alpha1.McpServerSpec{
				"in-cluster": {URL: "http://mcp-customer:9001/mcp"},
			}, nil)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		np := &netv1.NetworkPolicy{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getEgressNetworkPolicyName(testInstanceName),
				Namespace: namespace,
			}, np) == nil
		}, "gateway egress NP should be created")

		foundMcpRule := false
		for _, rule := range np.Spec.Egress {
			for _, port := range rule.Ports {
				if port.Port != nil && port.Port.IntValue() == 9001 {
					foundMcpRule = true
				}
			}
		}
		assert.True(t, foundMcpRule, "gateway egress NP should contain port 9001 rule for in-cluster MCP")
	})

	t.Run("cross-namespace MCP adds namespaceSelector egress rule", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		createClawInstanceWithMcpServers(t, ctx, testInstanceName, namespace,
			map[string]clawv1alpha1.McpServerSpec{
				"cross-ns": {URL: "http://mcp-server.shared-tools:9001/mcp"},
			}, nil)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		np := &netv1.NetworkPolicy{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getEgressNetworkPolicyName(testInstanceName),
				Namespace: namespace,
			}, np) == nil
		}, "gateway egress NP should be created")

		foundCrossNS := false
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil {
					if v, ok := peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]; ok && v == "shared-tools" {
						foundCrossNS = true
					}
				}
			}
		}
		assert.True(t, foundCrossNS, "gateway egress NP should contain namespaceSelector for shared-tools")
	})

	t.Run("external non-443 MCP adds proxy egress port", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		createClawInstanceWithMcpServers(t, ctx, testInstanceName, namespace,
			map[string]clawv1alpha1.McpServerSpec{
				"external": {URL: "https://mcp.example.com:8443/mcp"},
			}, nil)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		np := &netv1.NetworkPolicy{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyEgressNetworkPolicyName(testInstanceName),
				Namespace: namespace,
			}, np) == nil
		}, "proxy egress NP should be created")

		found8443 := false
		for _, rule := range np.Spec.Egress {
			for _, port := range rule.Ports {
				if port.Port != nil && port.Port.IntValue() == 8443 {
					found8443 = true
				}
			}
		}
		assert.True(t, found8443, "proxy egress NP should contain port 8443")
	})

	t.Run("allowedEgress appends user rules to gateway NP", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		port3000 := intstr.FromInt32(3000)
		createClawInstanceWithMcpServers(t, ctx, testInstanceName, namespace, nil,
			&clawv1alpha1.NetworkPolicySpec{
				AllowedEgress: []netv1.NetworkPolicyEgressRule{
					{
						To: []netv1.NetworkPolicyPeer{
							{
								NamespaceSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"kubernetes.io/metadata.name": "langfuse",
									},
								},
							},
						},
						Ports: []netv1.NetworkPolicyPort{
							{Port: &port3000, Protocol: protocolPtr(corev1.ProtocolTCP)},
						},
					},
				},
			})

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		np := &netv1.NetworkPolicy{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getEgressNetworkPolicyName(testInstanceName),
				Namespace: namespace,
			}, np) == nil
		}, "gateway egress NP should be created")

		foundLangfuse := false
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.NamespaceSelector != nil {
					if v, ok := peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]; ok && v == "langfuse" {
						foundLangfuse = true
					}
				}
			}
		}
		assert.True(t, foundLangfuse, "gateway egress NP should contain allowedEgress rule for langfuse")
	})

	t.Run("no MCP servers and no allowedEgress leaves NP unchanged", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		createClawInstance(t, ctx, testInstanceName, namespace)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		np := &netv1.NetworkPolicy{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getEgressNetworkPolicyName(testInstanceName),
				Namespace: namespace,
			}, np) == nil
		}, "gateway egress NP should be created")

		assert.Len(t, np.Spec.Egress, 2, "should only have proxy + DNS rules (no MCP rules added)")
	})
}
