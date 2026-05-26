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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func testClawWithMcpServers(servers map[string]clawv1alpha1.McpServerSpec) *clawv1alpha1.Claw {
	return &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		Spec:       clawv1alpha1.ClawSpec{McpServers: servers},
	}
}

func TestInjectMcpServers(t *testing.T) {
	t.Run("should inject HTTP MCP server with url and transport", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"context7": {
				URL:       "https://mcp.context7.com/mcp",
				Transport: clawv1alpha1.McpTransportStreamableHTTP,
			},
		})

		injectMcpServers(config, instance)

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
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"github": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-github"},
				Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder"},
			},
		})

		injectMcpServers(config, instance)

		mcp := config["mcp"].(map[string]any)
		servers := mcp["servers"].(map[string]any)
		server := servers["github"].(map[string]any)

		assert.Equal(t, "npx", server["command"])
		args := server["args"].([]string)
		assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, args)
		env := server["env"].(map[string]string)
		assert.Equal(t, "placeholder", env["GITHUB_PERSONAL_ACCESS_TOKEN"])
		assert.NotContains(t, server, "url")
		assert.NotContains(t, server, "transport")
	})

	t.Run("should inject mixed HTTP and stdio servers", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"context7": {
				URL:       "https://mcp.context7.com/mcp",
				Transport: clawv1alpha1.McpTransportStreamableHTTP,
			},
			"github": {
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-github"},
				Env:     map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder"},
			},
		})

		injectMcpServers(config, instance)

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
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(nil)

		injectMcpServers(config, instance)

		assert.NotContains(t, config, "mcp")
	})

	t.Run("should omit args when empty", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"simple": {Command: "my-server"},
		})

		injectMcpServers(config, instance)

		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["simple"].(map[string]any)
		assert.Equal(t, "my-server", server["command"])
		assert.NotContains(t, server, "args")
		assert.NotContains(t, server, "env")
	})

	t.Run("should omit env when empty", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"tool": {
				Command: "tool-server",
				Args:    []string{"--verbose"},
			},
		})

		injectMcpServers(config, instance)

		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["tool"].(map[string]any)
		assert.Contains(t, server, "args")
		assert.NotContains(t, server, "env")
	})

	t.Run("should omit transport when empty for HTTP server", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"remote": {URL: "https://api.example.com/mcp"},
		})

		injectMcpServers(config, instance)

		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["remote"].(map[string]any)
		assert.Equal(t, "https://api.example.com/mcp", server["url"])
		assert.NotContains(t, server, "transport")
	})

	t.Run("should include env but omit args for stdio server with env only", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"db": {
				Command: "node",
				Env:     map[string]string{"DB_HOST": "postgres.internal"},
			},
		})

		injectMcpServers(config, instance)

		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["db"].(map[string]any)
		assert.Equal(t, "node", server["command"])
		assert.NotContains(t, server, "args")
		assert.Contains(t, server, "env")
		assert.Equal(t, "postgres.internal", server["env"].(map[string]string)["DB_HOST"])
	})

	t.Run("should only set mcp.servers key", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{"port": 18789},
			"models":  map[string]any{"providers": map[string]any{}},
		}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"test": {URL: "https://example.com/mcp"},
		})

		injectMcpServers(config, instance)

		assert.Contains(t, config, "gateway")
		assert.Contains(t, config, "models")
		mcp := config["mcp"].(map[string]any)
		assert.Len(t, mcp, 1)
		assert.Contains(t, mcp, "servers")
	})
}

func TestBuildMcpServerConfig(t *testing.T) {
	t.Run("should include envFrom names as placeholder env vars", func(t *testing.T) {
		spec := clawv1alpha1.McpServerSpec{
			Command: "node",
			Args:    []string{"db-mcp-server.js"},
			Env:     map[string]string{"DB_HOST": "postgres.internal"},
			EnvFrom: []clawv1alpha1.McpEnvFromSecret{
				{
					Name:      "DB_PASSWORD",
					SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-creds", Key: "password"},
				},
			},
		}

		config := buildMcpServerConfig(spec)

		assert.Equal(t, "node", config["command"])
		env := config["env"].(map[string]string)
		assert.Equal(t, "postgres.internal", env["DB_HOST"])
		assert.Equal(t, "DB_PASSWORD", env["DB_PASSWORD"])
	})

	t.Run("should include only envFrom placeholders when no plain env", func(t *testing.T) {
		spec := clawv1alpha1.McpServerSpec{
			Command: "my-tool",
			EnvFrom: []clawv1alpha1.McpEnvFromSecret{
				{
					Name:      "API_KEY",
					SecretRef: clawv1alpha1.SecretRefEntry{Name: "secret", Key: "key"},
				},
			},
		}

		config := buildMcpServerConfig(spec)

		env := config["env"].(map[string]string)
		assert.Equal(t, "API_KEY", env["API_KEY"])
		assert.Len(t, env, 1)
	})

	t.Run("should omit env when neither env nor envFrom set", func(t *testing.T) {
		spec := clawv1alpha1.McpServerSpec{
			Command: "simple-server",
		}

		config := buildMcpServerConfig(spec)

		assert.NotContains(t, config, "env")
	})

	t.Run("should not include envFrom for HTTP servers", func(t *testing.T) {
		spec := clawv1alpha1.McpServerSpec{
			URL:       "https://example.com/mcp",
			Transport: clawv1alpha1.McpTransportStreamableHTTP,
		}

		config := buildMcpServerConfig(spec)

		assert.NotContains(t, config, "env")
		assert.Equal(t, "https://example.com/mcp", config["url"])
	})

	t.Run("should merge multiple envFrom entries with plain env", func(t *testing.T) {
		spec := clawv1alpha1.McpServerSpec{
			Command: "multi-secret",
			Env:     map[string]string{"HOST": "localhost"},
			EnvFrom: []clawv1alpha1.McpEnvFromSecret{
				{
					Name:      "USER",
					SecretRef: clawv1alpha1.SecretRefEntry{Name: "s1", Key: "user"},
				},
				{
					Name:      "PASS",
					SecretRef: clawv1alpha1.SecretRefEntry{Name: "s2", Key: "pass"},
				},
			},
		}

		config := buildMcpServerConfig(spec)

		env := config["env"].(map[string]string)
		assert.Equal(t, "localhost", env["HOST"])
		assert.Equal(t, "USER", env["USER"])
		assert.Equal(t, "PASS", env["PASS"])
		assert.Len(t, env, 3)
	})
}

func TestInjectMcpServersWithEnvFrom(t *testing.T) {
	t.Run("should inject stdio server with envFrom placeholders", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMcpServers(map[string]clawv1alpha1.McpServerSpec{
			"custom-db": {
				Command: "node",
				Args:    []string{"db-mcp-server.js"},
				Env:     map[string]string{"DB_HOST": "postgres.internal"},
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_PASSWORD",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-creds", Key: "password"},
					},
				},
			},
		})

		injectMcpServers(config, instance)

		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["custom-db"].(map[string]any)

		assert.Equal(t, "node", server["command"])
		env := server["env"].(map[string]string)
		assert.Equal(t, "postgres.internal", env["DB_HOST"])
		assert.Equal(t, "DB_PASSWORD", env["DB_PASSWORD"])
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
						Transport: clawv1alpha1.McpTransportStreamableHTTP,
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

	t.Run("should remove McpServersConfigured condition when mcpServers removed", func(t *testing.T) {
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
		require.NotNil(t, condition, "condition should be set after first reconcile")

		updated.Spec.McpServers = nil
		require.NoError(t, k8sClient.Update(ctx, updated))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		after := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, after))
		condition = meta.FindStatusCondition(after.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		assert.Nil(t, condition, "McpServersConfigured condition should be removed after mcpServers cleared")
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

	t.Run("should inject envFrom MCP server and mount env vars on gateway", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		aiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, aiSecret))

		dbSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db-credentials", Namespace: namespace},
			Data:       map[string][]byte{"password": []byte("s3cret")},
		}
		require.NoError(t, k8sClient.Create(ctx, dbSecret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"custom-db": {
						Command: "node",
						Args:    []string{"db-mcp-server.js"},
						Env:     map[string]string{"DB_HOST": "postgres.internal"},
						EnvFrom: []clawv1alpha1.McpEnvFromSecret{
							{
								Name:      "DB_PASSWORD",
								SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-credentials", Key: "password"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify ConfigMap has envFrom placeholder
		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))
		server := config["mcp"].(map[string]any)["servers"].(map[string]any)["custom-db"].(map[string]any)
		env := server["env"].(map[string]any)
		assert.Equal(t, "postgres.internal", env["DB_HOST"])
		assert.Equal(t, "DB_PASSWORD", env["DB_PASSWORD"])

		// Verify gateway deployment has secretKeyRef env var
		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		var foundSecretEnv bool
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name != ClawGatewayContainerName {
				continue
			}
			for _, e := range c.Env {
				if e.Name == "DB_PASSWORD" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					assert.Equal(t, "db-credentials", e.ValueFrom.SecretKeyRef.Name)
					assert.Equal(t, "password", e.ValueFrom.SecretKeyRef.Key)
					foundSecretEnv = true
				}
			}
		}
		assert.True(t, foundSecretEnv, "gateway should have DB_PASSWORD env from secretKeyRef")

		// Verify condition is True
		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))
		condition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
	})

	t.Run("should set McpServersConfigured=False when envFrom secret is missing", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		aiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, aiSecret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"custom-db": {
						Command: "node",
						EnvFrom: []clawv1alpha1.McpEnvFromSecret{
							{
								Name:      "DB_PASSWORD",
								SecretRef: clawv1alpha1.SecretRefEntry{Name: "nonexistent-secret", Key: "password"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err, "reconcile should fail when envFrom secret is missing")
		assert.Contains(t, err.Error(), "nonexistent-secret")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))

		condition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		require.NotNil(t, condition, "McpServersConfigured should be set")
		assert.Equal(t, metav1.ConditionFalse, condition.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonValidationFailed, condition.Reason)
		assert.Contains(t, condition.Message, "nonexistent-secret")

		readyCondition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCondition)
		assert.Equal(t, metav1.ConditionFalse, readyCondition.Status)
	})

	t.Run("should set McpServersConfigured=False when envFrom secret key is missing", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		aiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, aiSecret))

		wrongKeySecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db-creds-wrong", Namespace: namespace},
			Data:       map[string][]byte{"wrong-key": []byte("value")},
		}
		require.NoError(t, k8sClient.Create(ctx, wrongKeySecret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"custom-db": {
						Command: "node",
						EnvFrom: []clawv1alpha1.McpEnvFromSecret{
							{
								Name:      "DB_PASSWORD",
								SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-creds-wrong", Key: "password"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err, "reconcile should fail when envFrom secret key is missing")
		assert.Contains(t, err.Error(), "password")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))

		condition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		require.NotNil(t, condition, "McpServersConfigured should be set")
		assert.Equal(t, metav1.ConditionFalse, condition.Status)
		assert.Contains(t, condition.Message, "password")
		assert.Contains(t, condition.Message, "not found in Secret")
	})

	t.Run("should stamp MCP secret version annotation on gateway deployment", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		aiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, aiSecret))

		dbSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db-stamp-test", Namespace: namespace},
			Data:       map[string][]byte{"password": []byte("s3cret")},
		}
		require.NoError(t, k8sClient.Create(ctx, dbSecret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"db-tool": {
						Command: "node",
						EnvFrom: []clawv1alpha1.McpEnvFromSecret{
							{
								Name:      "DB_PASS",
								SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-stamp-test", Key: "password"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		annotationKey := clawv1alpha1.AnnotationPrefixMcpSecretVersion + mcpAnnotationKey("db-tool", "DB_PASS") + clawv1alpha1.AnnotationSuffixMcpSecretVersion
		rv, exists := deployment.Spec.Template.Annotations[annotationKey]
		assert.True(t, exists, "MCP secret version annotation should be present")
		assert.NotEmpty(t, rv, "annotation value should be the secret ResourceVersion")
	})

	t.Run("should succeed when MCP servers have no envFrom entries", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		aiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, aiSecret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"context7": {URL: "https://mcp.context7.com/mcp"},
					"github": {
						Command: "npx",
						Env:     map[string]string{"TOKEN": "placeholder"},
					},
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
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionTrue, condition.Status)
	})

	t.Run("should recover from validation failure after creating missing secret", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		aiSecret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, aiSecret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"db-tool": {
						Command: "node",
						EnvFrom: []clawv1alpha1.McpEnvFromSecret{
							{
								Name:      "DB_PASS",
								SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-recovery-test", Key: "password"},
							},
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()

		// First reconcile fails — secret doesn't exist
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))
		condition := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionFalse, condition.Status)

		// Create the missing secret
		dbSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db-recovery-test", Namespace: namespace},
			Data:       map[string][]byte{"password": []byte("s3cret")},
		}
		require.NoError(t, k8sClient.Create(ctx, dbSecret))

		// Second reconcile succeeds
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, updated))
		condition = meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeMcpServersConfigured)
		require.NotNil(t, condition)
		assert.Equal(t, metav1.ConditionTrue, condition.Status,
			"condition should transition from False to True after secret is created")
	})

	t.Run("should accept valid MCP server with envFrom via CEL", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"db-tool": {
						Command: "node",
						EnvFrom: []clawv1alpha1.McpEnvFromSecret{
							{
								Name:      "DB_PASSWORD",
								SecretRef: clawv1alpha1.SecretRefEntry{Name: "my-secret", Key: "pass"},
							},
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.NoError(t, err, "valid stdio MCP server with envFrom should be accepted by CEL")
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
						Transport: clawv1alpha1.McpTransportStreamableHTTP,
					},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.NoError(t, err, "valid HTTP MCP server should be accepted")
	})

	t.Run("should reject stdio MCP server with transport set", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				McpServers: map[string]clawv1alpha1.McpServerSpec{
					"bad": {
						Command:   "npx",
						Transport: clawv1alpha1.McpTransportSSE,
					},
				},
			},
		}
		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "CEL should reject stdio MCP server with transport set")
		assert.Contains(t, err.Error(), "transport is only allowed for HTTP MCP servers (url)")
	})
}
