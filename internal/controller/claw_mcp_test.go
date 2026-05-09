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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func makeMcpConfigMap(jsonContent string) []*unstructured.Unstructured {
	cm := &unstructured.Unstructured{}
	cm.SetKind(ConfigMapKind)
	cm.SetName(getConfigMapName(testInstanceName))
	cm.Object["data"] = map[string]any{
		"operator.json": jsonContent,
	}
	return []*unstructured.Unstructured{cm}
}

func getMcpConfig(t *testing.T, objects []*unstructured.Unstructured) map[string]any {
	t.Helper()
	raw, _, err := unstructured.NestedString(objects[0].Object, "data", "operator.json")
	require.NoError(t, err)
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &config))
	return config
}

func testClawWithMcpServers(servers map[string]clawv1alpha1.McpServerSpec) *clawv1alpha1.Claw {
	return &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		Spec:       clawv1alpha1.ClawSpec{McpServers: servers},
	}
}

func TestInjectMcpServersIntoConfigMap(t *testing.T) {
	t.Run("should inject HTTP MCP server with url and transport", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"context7": {
				URL:       "https://mcp.context7.com/mcp",
				Transport: "streamable-http",
			},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		mcp := config["mcp"].(map[string]any)
		servers := mcp["servers"].(map[string]any)
		server := servers["context7"].(map[string]any)

		assert.Equal(t, "https://mcp.context7.com/mcp", server["url"])
		assert.Equal(t, "streamable-http", server["transport"])
		assert.NotContains(t, server, "command")
		assert.NotContains(t, server, "args")
		assert.NotContains(t, server, "env")
	})

	t.Run("should inject stdio MCP server with command, args, and env", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"github": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-github"},
				Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder"},
			},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		mcp := config["mcp"].(map[string]any)
		servers := mcp["servers"].(map[string]any)
		server := servers["github"].(map[string]any)

		assert.Equal(t, "npx", server["command"])
		args := server["args"].([]any)
		assert.Equal(t, []any{"-y", "@modelcontextprotocol/server-github"}, args)
		env := server["env"].(map[string]any)
		assert.Equal(t, "placeholder", env["GITHUB_PERSONAL_ACCESS_TOKEN"])
		assert.NotContains(t, server, "url")
		assert.NotContains(t, server, "transport")
	})

	t.Run("should inject mixed HTTP and stdio servers", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"context7": {
				URL:       "https://mcp.context7.com/mcp",
				Transport: "streamable-http",
			},
			"github": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-github"},
				Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder"},
			},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		mcp := config["mcp"].(map[string]any)
		servers := mcp["servers"].(map[string]any)

		require.Contains(t, servers, "context7")
		require.Contains(t, servers, "github")

		ctx7 := servers["context7"].(map[string]any)
		assert.Equal(t, "https://mcp.context7.com/mcp", ctx7["url"])

		gh := servers["github"].(map[string]any)
		assert.Equal(t, "npx", gh["command"])
	})

	t.Run("should skip injection when mcpServers is empty", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(nil)

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		assert.NotContains(t, config, "mcp")
	})

	t.Run("should omit args when empty", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"simple": {Command: "my-server"},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["simple"].(map[string]any)
		assert.Equal(t, "my-server", server["command"])
		assert.NotContains(t, server, "args")
		assert.NotContains(t, server, "env")
	})

	t.Run("should omit env when empty", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"tool": {
				Command: "tool-server",
				Args:    []string{"--verbose"},
			},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["tool"].(map[string]any)
		assert.Contains(t, server, "args")
		assert.NotContains(t, server, "env")
	})

	t.Run("should omit transport when empty for HTTP server", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"remote": {URL: "https://api.example.com/mcp"},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["remote"].(map[string]any)
		assert.Equal(t, "https://api.example.com/mcp", server["url"])
		assert.NotContains(t, server, "transport")
	})

	t.Run("should include env but omit args for stdio server with env only", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"db": {
				Command: "node",
				Env:     map[string]string{"DB_HOST": "postgres.internal"},
			},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["db"].(map[string]any)
		assert.Equal(t, "node", server["command"])
		assert.NotContains(t, server, "args")
		assert.Contains(t, server, "env")
		assert.Equal(t, "postgres.internal", server["env"].(map[string]any)["DB_HOST"])
	})

	t.Run("should return error when ConfigMap not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"test": {URL: "https://example.com/mcp"},
		})

		err := injectMcpServersIntoConfigMap(objects, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})

	t.Run("should preserve existing config keys", func(t *testing.T) {
		objects := makeMcpConfigMap(`{"gateway":{"port":18789},"models":{"providers":{}}}`)
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"test": {URL: "https://example.com/mcp"},
		})

		require.NoError(t, injectMcpServersIntoConfigMap(objects, instance))

		config := getMcpConfig(t, objects)
		assert.Contains(t, config, "gateway")
		assert.Contains(t, config, "models")
		assert.Contains(t, config, "mcp")
	})
}

func TestMcpServersIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should inject MCP servers into ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"context7": {
						URL:       "https://mcp.context7.com/mcp",
						Transport: "streamable-http",
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		operatorJSON, ok := cm.Data["operator.json"]
		require.True(t, ok, "operator.json should exist")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

		mcp, ok := config["mcp"].(map[string]any)
		require.True(t, ok, "mcp section should exist")
		servers, ok := mcp["servers"].(map[string]any)
		require.True(t, ok, "mcp.servers section should exist")
		require.Contains(t, servers, "context7")

		ctx7 := servers["context7"].(map[string]any)
		assert.Equal(t, "https://mcp.context7.com/mcp", ctx7["url"])
		assert.Equal(t, "streamable-http", ctx7["transport"])
	})

	t.Run("should set McpServersConfigured condition when mcpServers present", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"test": {URL: "https://example.com/mcp"},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))

		condition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		require.NotNil(t, condition, "McpServersConfigured condition should be set")
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonConfigured, condition.Reason)
	})

	t.Run("should not set McpServersConfigured condition when mcpServers empty", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))

		condition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		assert.Nil(t, condition, "McpServersConfigured condition should not be set when no MCP servers")
	})

	t.Run("should inject stdio MCP server into ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"github": {
						Command: "npx",
						Args:    []string{"-y", "@modelcontextprotocol/server-github"},
						Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder"},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		operatorJSON, ok := cm.Data["operator.json"]
		require.True(t, ok, "operator.json should exist")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

		mcp, ok := config["mcp"].(map[string]any)
		require.True(t, ok, "mcp section should exist")
		servers, ok := mcp["servers"].(map[string]any)
		require.True(t, ok, "mcp.servers section should exist")
		require.Contains(t, servers, "github")

		gh := servers["github"].(map[string]any)
		assert.Equal(t, "npx", gh["command"])
		args := gh["args"].([]any)
		assert.Equal(t, []any{"-y", "@modelcontextprotocol/server-github"}, args)
		env := gh["env"].(map[string]any)
		assert.Equal(t, "placeholder", env["GITHUB_PERSONAL_ACCESS_TOKEN"])
	})
}

func TestMcpServerCELValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("should reject MCP server with neither command nor url", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"empty": {},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "CEL should reject MCP server with neither command nor url")
		assert.Contains(t, err.Error(), "either command (stdio) or url (HTTP) must be set")
	})

	t.Run("should reject MCP server with both command and url", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"both": {
						Command: "npx",
						URL:     "https://example.com/mcp",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "CEL should reject MCP server with both command and url")
		assert.Contains(t, err.Error(), "command and url are mutually exclusive")
	})

	t.Run("should accept valid stdio MCP server", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"github": {
						Command: "npx",
						Args:    []string{"-y", "@modelcontextprotocol/server-github"},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.NoError(t, err, "valid stdio MCP server should be accepted")
	})

	t.Run("should accept valid HTTP MCP server", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"context7": {
						URL:       "https://mcp.context7.com/mcp",
						Transport: "streamable-http",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.NoError(t, err, "valid HTTP MCP server should be accepted")
	})
}
