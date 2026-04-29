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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	testInstanceLabel = "claw.sandbox.redhat.com/instance"
)

// TestInjectInstanceLabels_Deployment verifies that instance labels are correctly
// injected into Deployment spec.selector.matchLabels and spec.template.metadata.labels
func TestInjectInstanceLabels_Deployment(t *testing.T) {
	t.Run("should inject instance label into Deployment selector and pod template labels", func(t *testing.T) {
		deployment := &unstructured.Unstructured{}
		deployment.SetKind(DeploymentKind)
		deployment.SetName("claw")
		deployment.Object["spec"] = map[string]any{
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{
						"app.kubernetes.io/name": "claw",
					},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "gateway"},
					},
				},
			},
		}

		objects := []*unstructured.Unstructured{deployment}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify top-level metadata labels
		labels := objects[0].GetLabels()
		assert.Equal(t, testInstanceName, labels[testInstanceLabel], "top-level metadata should have instance label")

		// Verify spec.selector.matchLabels
		selector, found, err := unstructured.NestedMap(objects[0].Object, "spec", "selector", "matchLabels")
		require.NoError(t, err)
		require.True(t, found, "selector.matchLabels should exist")
		assert.Equal(t, testInstanceName, selector[testInstanceLabel], "selector.matchLabels should have instance label")
		assert.Equal(t, "claw", selector["app.kubernetes.io/name"], "original labels should be preserved")

		// Verify spec.template.metadata.labels
		templateLabels, found, err := unstructured.NestedMap(objects[0].Object, "spec", "template", "metadata", "labels")
		require.NoError(t, err)
		require.True(t, found, "template.metadata.labels should exist")
		assert.Equal(t, testInstanceName, templateLabels[testInstanceLabel], "template labels should have instance label")
		assert.Equal(t, "claw", templateLabels["app.kubernetes.io/name"], "original template labels should be preserved")
	})

	t.Run("should handle proxy Deployment", func(t *testing.T) {
		deployment := &unstructured.Unstructured{}
		deployment.SetKind(DeploymentKind)
		deployment.SetName("claw-proxy")
		deployment.Object["spec"] = map[string]any{
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw-proxy",
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{
						"app.kubernetes.io/name": "claw-proxy",
					},
				},
			},
		}

		objects := []*unstructured.Unstructured{deployment}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		selector, _, _ := unstructured.NestedMap(objects[0].Object, "spec", "selector", "matchLabels")
		assert.Equal(t, testInstanceName, selector[testInstanceLabel])

		templateLabels, _, _ := unstructured.NestedMap(objects[0].Object, "spec", "template", "metadata", "labels")
		assert.Equal(t, testInstanceName, templateLabels[testInstanceLabel])
	})
}

// TestInjectInstanceLabels_Service verifies that instance labels are correctly
// injected into Service spec.selector
func TestInjectInstanceLabels_Service(t *testing.T) {
	t.Run("should inject instance label into Service selector", func(t *testing.T) {
		service := &unstructured.Unstructured{}
		service.SetKind("Service")
		service.SetName("claw")
		service.Object["spec"] = map[string]any{
			"selector": map[string]any{
				"app.kubernetes.io/name": "claw",
			},
		}

		objects := []*unstructured.Unstructured{service}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify top-level metadata labels
		labels := objects[0].GetLabels()
		assert.Equal(t, testInstanceName, labels[testInstanceLabel], "top-level metadata should have instance label")

		// Verify spec.selector
		selector, found, err := unstructured.NestedMap(objects[0].Object, "spec", "selector")
		require.NoError(t, err)
		require.True(t, found, "selector should exist")
		assert.Equal(t, testInstanceName, selector[testInstanceLabel], "selector should have instance label")
		assert.Equal(t, "claw", selector["app.kubernetes.io/name"], "original selector labels should be preserved")
	})

	t.Run("should handle proxy Service", func(t *testing.T) {
		service := &unstructured.Unstructured{}
		service.SetKind("Service")
		service.SetName("claw-proxy")
		service.Object["spec"] = map[string]any{
			"selector": map[string]any{
				"app.kubernetes.io/name": "claw-proxy",
			},
		}

		objects := []*unstructured.Unstructured{service}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		selector, _, _ := unstructured.NestedMap(objects[0].Object, "spec", "selector")
		assert.Equal(t, testInstanceName, selector[testInstanceLabel])
		assert.Equal(t, "claw-proxy", selector["app.kubernetes.io/name"])
	})
}

// TestInjectInstanceLabels_NetworkPolicy verifies that instance labels are correctly
// injected into NetworkPolicy spec.podSelector and peer podSelectors in egress/ingress rules
func TestInjectInstanceLabels_NetworkPolicy(t *testing.T) {
	t.Run("should inject instance label into NetworkPolicy podSelector", func(t *testing.T) {
		netpol := &unstructured.Unstructured{}
		netpol.SetKind(NetworkPolicyKind)
		netpol.SetName("claw-egress")
		netpol.Object["spec"] = map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"policyTypes": []any{"Egress"},
		}

		objects := []*unstructured.Unstructured{netpol}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify top-level metadata labels
		labels := objects[0].GetLabels()
		assert.Equal(t, testInstanceName, labels[testInstanceLabel], "top-level metadata should have instance label")

		// Verify spec.podSelector.matchLabels
		podSelector, found, err := unstructured.NestedMap(objects[0].Object, "spec", "podSelector", "matchLabels")
		require.NoError(t, err)
		require.True(t, found, "podSelector.matchLabels should exist")
		assert.Equal(t, testInstanceName, podSelector[testInstanceLabel], "podSelector should have instance label")
		assert.Equal(t, "claw", podSelector["app.kubernetes.io/name"], "original podSelector labels should be preserved")
	})

	t.Run("should inject instance label into NetworkPolicy egress peer podSelectors", func(t *testing.T) {
		netpol := &unstructured.Unstructured{}
		netpol.SetKind(NetworkPolicyKind)
		netpol.SetName("claw-egress")
		netpol.Object["spec"] = map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"egress": []any{
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app.kubernetes.io/name": "claw-proxy",
								},
							},
						},
					},
				},
			},
			"policyTypes": []any{"Egress"},
		}

		objects := []*unstructured.Unstructured{netpol}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify egress[0].to[0].podSelector.matchLabels
		egress, found, err := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		require.NoError(t, err)
		require.True(t, found, "egress should exist")
		require.Len(t, egress, 1, "should have one egress rule")

		egressRule := egress[0].(map[string]any)
		to := egressRule["to"].([]any)
		require.Len(t, to, 1, "should have one egress peer")

		peer := to[0].(map[string]any)
		podSelector, found, err := unstructured.NestedMap(peer, "podSelector", "matchLabels")
		require.NoError(t, err)
		require.True(t, found, "egress peer podSelector should exist")
		assert.Equal(t, testInstanceName, podSelector[testInstanceLabel], "egress peer podSelector should have instance label")
		assert.Equal(t, "claw-proxy", podSelector["app.kubernetes.io/name"], "original egress peer labels should be preserved")
	})

	t.Run("should inject instance label into NetworkPolicy ingress peer podSelectors", func(t *testing.T) {
		netpol := &unstructured.Unstructured{}
		netpol.SetKind(NetworkPolicyKind)
		netpol.SetName("claw-ingress")
		netpol.Object["spec"] = map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"ingress": []any{
				map[string]any{
					"from": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app": "router",
								},
							},
						},
					},
				},
			},
			"policyTypes": []any{"Ingress"},
		}

		objects := []*unstructured.Unstructured{netpol}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify ingress[0].from[0].podSelector.matchLabels
		ingress, found, err := unstructured.NestedSlice(objects[0].Object, "spec", "ingress")
		require.NoError(t, err)
		require.True(t, found, "ingress should exist")
		require.Len(t, ingress, 1, "should have one ingress rule")

		ingressRule := ingress[0].(map[string]any)
		from := ingressRule["from"].([]any)
		require.Len(t, from, 1, "should have one ingress peer")

		peer := from[0].(map[string]any)
		podSelector, found, err := unstructured.NestedMap(peer, "podSelector", "matchLabels")
		require.NoError(t, err)
		require.True(t, found, "ingress peer podSelector should exist")
		assert.Equal(t, testInstanceName, podSelector[testInstanceLabel], "ingress peer podSelector should have instance label")
		assert.Equal(t, "router", podSelector["app"], "original ingress peer labels should be preserved")
	})

	t.Run("should handle NetworkPolicy with multiple egress rules and peers", func(t *testing.T) {
		netpol := &unstructured.Unstructured{}
		netpol.SetKind(NetworkPolicyKind)
		netpol.SetName("claw-proxy-egress")
		netpol.Object["spec"] = map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw-proxy",
				},
			},
			"egress": []any{
				// Rule 1: Allow to kube-dns
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"k8s-app": "kube-dns",
								},
							},
						},
					},
				},
				// Rule 2: Allow to internet (no podSelector)
				map[string]any{
					"to": []any{
						map[string]any{
							"ipBlock": map[string]any{
								"cidr": "0.0.0.0/0",
							},
						},
					},
				},
				// Rule 3: Allow to multiple pods
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app": "backend-1",
								},
							},
						},
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app": "backend-2",
								},
							},
						},
					},
				},
			},
			"policyTypes": []any{"Egress"},
		}

		objects := []*unstructured.Unstructured{netpol}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		require.Len(t, egress, 3, "should have three egress rules")

		// Rule 1: kube-dns
		rule1 := egress[0].(map[string]any)
		to1 := rule1["to"].([]any)
		peer1 := to1[0].(map[string]any)
		podSelector1, found1, _ := unstructured.NestedMap(peer1, "podSelector", "matchLabels")
		require.True(t, found1)
		assert.Equal(t, testInstanceName, podSelector1[testInstanceLabel])
		assert.Equal(t, "kube-dns", podSelector1["k8s-app"])

		// Rule 2: ipBlock (no podSelector, should be unchanged)
		rule2 := egress[1].(map[string]any)
		to2 := rule2["to"].([]any)
		peer2 := to2[0].(map[string]any)
		_, found2, _ := unstructured.NestedMap(peer2, "podSelector")
		assert.False(t, found2, "ipBlock peer should not have podSelector")

		// Rule 3: multiple pods
		rule3 := egress[2].(map[string]any)
		to3 := rule3["to"].([]any)
		require.Len(t, to3, 2, "rule 3 should have two peers")

		peer3a := to3[0].(map[string]any)
		podSelector3a, found3a, _ := unstructured.NestedMap(peer3a, "podSelector", "matchLabels")
		require.True(t, found3a)
		assert.Equal(t, testInstanceName, podSelector3a[testInstanceLabel])
		assert.Equal(t, "backend-1", podSelector3a["app"])

		peer3b := to3[1].(map[string]any)
		podSelector3b, found3b, _ := unstructured.NestedMap(peer3b, "podSelector", "matchLabels")
		require.True(t, found3b)
		assert.Equal(t, testInstanceName, podSelector3b[testInstanceLabel])
		assert.Equal(t, "backend-2", podSelector3b["app"])
	})

	t.Run("should handle NetworkPolicy with both ingress and egress", func(t *testing.T) {
		netpol := &unstructured.Unstructured{}
		netpol.SetKind(NetworkPolicyKind)
		netpol.SetName("claw-full")
		netpol.Object["spec"] = map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"ingress": []any{
				map[string]any{
					"from": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app": "frontend",
								},
							},
						},
					},
				},
			},
			"egress": []any{
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app": "database",
								},
							},
						},
					},
				},
			},
			"policyTypes": []any{"Ingress", "Egress"},
		}

		objects := []*unstructured.Unstructured{netpol}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify ingress peer
		ingress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "ingress")
		ingressRule := ingress[0].(map[string]any)
		from := ingressRule["from"].([]any)
		ingressPeer := from[0].(map[string]any)
		ingressPodSelector, _, _ := unstructured.NestedMap(ingressPeer, "podSelector", "matchLabels")
		assert.Equal(t, testInstanceName, ingressPodSelector[testInstanceLabel])
		assert.Equal(t, "frontend", ingressPodSelector["app"])

		// Verify egress peer
		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		egressRule := egress[0].(map[string]any)
		to := egressRule["to"].([]any)
		egressPeer := to[0].(map[string]any)
		egressPodSelector, _, _ := unstructured.NestedMap(egressPeer, "podSelector", "matchLabels")
		assert.Equal(t, testInstanceName, egressPodSelector[testInstanceLabel])
		assert.Equal(t, "database", egressPodSelector["app"])
	})
}

// TestInjectInstanceLabels_MultipleResources verifies that instance labels
// are correctly applied across different resource types in a single call
func TestInjectInstanceLabels_MultipleResources(t *testing.T) {
	t.Run("should inject instance label into all resource types", func(t *testing.T) {
		deployment := &unstructured.Unstructured{}
		deployment.SetKind(DeploymentKind)
		deployment.SetName("claw")
		deployment.Object["spec"] = map[string]any{
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{
						"app.kubernetes.io/name": "claw",
					},
				},
			},
		}

		service := &unstructured.Unstructured{}
		service.SetKind("Service")
		service.SetName("claw")
		service.Object["spec"] = map[string]any{
			"selector": map[string]any{
				"app.kubernetes.io/name": "claw",
			},
		}

		netpol := &unstructured.Unstructured{}
		netpol.SetKind(NetworkPolicyKind)
		netpol.SetName("claw-egress")
		netpol.Object["spec"] = map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]any{
					"app.kubernetes.io/name": "claw",
				},
			},
			"egress": []any{
				map[string]any{
					"to": []any{
						map[string]any{
							"podSelector": map[string]any{
								"matchLabels": map[string]any{
									"app.kubernetes.io/name": "claw-proxy",
								},
							},
						},
					},
				},
			},
		}

		objects := []*unstructured.Unstructured{deployment, service, netpol}
		err := injectInstanceLabels(objects, testInstanceName)
		require.NoError(t, err)

		// Verify all resources have top-level instance labels
		for _, obj := range objects {
			labels := obj.GetLabels()
			assert.Equal(t, testInstanceName, labels[testInstanceLabel],
				"resource %s should have instance label", obj.GetKind())
		}

		// Verify Deployment selectors
		depSelector, _, _ := unstructured.NestedMap(objects[0].Object, "spec", "selector", "matchLabels")
		assert.Equal(t, testInstanceName, depSelector[testInstanceLabel])

		// Verify Service selector
		svcSelector, _, _ := unstructured.NestedMap(objects[1].Object, "spec", "selector")
		assert.Equal(t, testInstanceName, svcSelector[testInstanceLabel])

		// Verify NetworkPolicy podSelector and egress peer
		npPodSelector, _, _ := unstructured.NestedMap(objects[2].Object, "spec", "podSelector", "matchLabels")
		assert.Equal(t, testInstanceName, npPodSelector[testInstanceLabel])

		egress, _, _ := unstructured.NestedSlice(objects[2].Object, "spec", "egress")
		egressRule := egress[0].(map[string]any)
		to := egressRule["to"].([]any)
		peer := to[0].(map[string]any)
		peerSelector, _, _ := unstructured.NestedMap(peer, "podSelector", "matchLabels")
		assert.Equal(t, testInstanceName, peerSelector[testInstanceLabel])
	})
}

// TestInjectInstanceLabels_DifferentInstances verifies that different instance names
// result in different instance labels, enabling multi-instance isolation
func TestInjectInstanceLabels_DifferentInstances(t *testing.T) {
	t.Run("should support different instance names for isolation", func(t *testing.T) {
		makeDeployment := func(name string) *unstructured.Unstructured {
			dep := &unstructured.Unstructured{}
			dep.SetKind(DeploymentKind)
			dep.SetName(name)
			dep.Object["spec"] = map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{
						"app.kubernetes.io/name": "claw",
					},
				},
				"template": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"app.kubernetes.io/name": "claw",
						},
					},
				},
			}
			return dep
		}

		// Instance 1
		instance1Objects := []*unstructured.Unstructured{makeDeployment("claw-1")}
		err := injectInstanceLabels(instance1Objects, "instance-1")
		require.NoError(t, err)

		// Instance 2
		instance2Objects := []*unstructured.Unstructured{makeDeployment("claw-2")}
		err = injectInstanceLabels(instance2Objects, "instance-2")
		require.NoError(t, err)

		// Verify instance 1 has "instance-1" label
		selector1, _, _ := unstructured.NestedMap(instance1Objects[0].Object, "spec", "selector", "matchLabels")
		assert.Equal(t, "instance-1", selector1[testInstanceLabel])

		// Verify instance 2 has "instance-2" label
		selector2, _, _ := unstructured.NestedMap(instance2Objects[0].Object, "spec", "selector", "matchLabels")
		assert.Equal(t, "instance-2", selector2[testInstanceLabel])

		// Verify they're different (ensuring isolation)
		assert.NotEqual(t, selector1[testInstanceLabel], selector2[testInstanceLabel],
			"different instances should have different instance labels")
	})
}
