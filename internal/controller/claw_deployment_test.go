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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Vertex AI deployment configuration tests ---

func TestConfigureClawDeploymentForVertex(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": ClawGatewayContainerName,
							"env": []any{
								map[string]any{"name": "HOME", "value": "/home/node"},
							},
							"volumeMounts": []any{},
						},
					},
					"volumes": []any{},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should add vertex env vars and volume mount", func(t *testing.T) {
		objects := makeDeployment()
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "anthropic-vertex",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "anthropic",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "us-east5",
				},
			},
		}

		require.NoError(t, configureClawDeploymentForVertex(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		var adcEnv, projectEnv map[string]any
		for _, e := range envVars {
			env := e.(map[string]any)
			switch env["name"] {
			case "GOOGLE_APPLICATION_CREDENTIALS":
				adcEnv = env
			case "ANTHROPIC_VERTEX_PROJECT_ID":
				projectEnv = env
			}
		}

		require.NotNil(t, adcEnv, "GOOGLE_APPLICATION_CREDENTIALS should be set")
		assert.Equal(t, "/etc/vertex-adc/adc.json", adcEnv["value"])

		require.NotNil(t, projectEnv, "ANTHROPIC_VERTEX_PROJECT_ID should be set")
		assert.Equal(t, "my-project", projectEnv["value"])

		volumeMounts := container["volumeMounts"].([]any)
		require.Len(t, volumeMounts, 1)
		vm := volumeMounts[0].(map[string]any)
		assert.Equal(t, "vertex-adc", vm["name"])
		assert.Equal(t, "/etc/vertex-adc", vm["mountPath"])
		assert.Equal(t, true, vm["readOnly"])

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 1)
		vol := volumes[0].(map[string]any)
		assert.Equal(t, "vertex-adc", vol["name"])
		cmRef := vol["configMap"].(map[string]any)
		assert.Equal(t, getVertexADCConfigMapName(testInstanceName), cmRef["name"])
	})

	t.Run("should be no-op when no vertex credentials exist", func(t *testing.T) {
		objects := makeDeployment()
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
		}

		require.NoError(t, configureClawDeploymentForVertex(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Len(t, envVars, 1, "should only have original HOME env var")
	})
}

// --- Gateway config hash integration tests ---

func TestGatewayConfigHashIntegration(t *testing.T) {
	const resourceName = testInstanceName
	ctx := context.Background()

	t.Run("should stamp gateway config hash annotation on pod template", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment)
			return err == nil
		}, "Deployment should be created")

		hash, exists := deployment.Spec.Template.Annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		assert.True(t, exists, "gateway-config-hash annotation should be present on pod template")
		assert.Regexp(t, `^[0-9a-f]{64}$`, hash, "hash should be a 64-char hex SHA-256")
	})

	t.Run("should produce stable gateway config hash across reconciliations", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")
		hash1, ok1 := deployment.Spec.Template.Annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		require.True(t, ok1, "gateway-config-hash annotation must be present after first reconcile")
		require.NotEmpty(t, hash1, "gateway-config-hash must not be empty after first reconcile")

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      getClawDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment)
		require.NoError(t, err)
		hash2, ok2 := deployment.Spec.Template.Annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		require.True(t, ok2, "gateway-config-hash annotation must be present after second reconcile")
		require.NotEmpty(t, hash2, "gateway-config-hash must not be empty after second reconcile")

		assert.Equal(t, hash1, hash2, "hash should be stable when config hasn't changed")
	})
}

// --- Gateway config hash stamping unit tests ---

func TestStampGatewayConfigHash(t *testing.T) {
	makeObjects := func(operatorJSON string) []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{
			"operator.json": operatorJSON,
			"openclaw.json": `{"agents":{"defaults":{"model":{"primary":"test"}}}}`,
		}

		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": ClawGatewayContainerName},
					},
				},
			},
		}
		return []*unstructured.Unstructured{cm, dep}
	}

	t.Run("should stamp hash annotation on gateway deployment", func(t *testing.T) {
		objects := makeObjects(`{"gateway":{"auth":{"mode":"token","scopes":["operator.admin"]}}}`)
		require.NoError(t, stampGatewayConfigHash(objects, testInstanceName))

		annotations, _, _ := unstructured.NestedStringMap(objects[1].Object, "spec", "template", "metadata", "annotations")
		hash, exists := annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		assert.True(t, exists, "gateway-config-hash annotation should exist")
		assert.Len(t, hash, 64, "hash should be a 64-char hex SHA-256")
	})

	t.Run("should produce different hashes for different config content", func(t *testing.T) {
		objects1 := makeObjects(`{"gateway":{"auth":{"mode":"token"}}}`)
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName))

		objects2 := makeObjects(`{"gateway":{"auth":{"mode":"token","scopes":["operator.admin"]}}}`)
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName))

		ann1, _, _ := unstructured.NestedStringMap(objects1[1].Object, "spec", "template", "metadata", "annotations")
		ann2, _, _ := unstructured.NestedStringMap(objects2[1].Object, "spec", "template", "metadata", "annotations")
		assert.NotEqual(t, ann1[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			ann2[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			"different config should produce different hashes")
	})

	t.Run("should produce identical hashes for identical content", func(t *testing.T) {
		config := `{"gateway":{"port":18789}}`
		objects1 := makeObjects(config)
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName))

		objects2 := makeObjects(config)
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName))

		ann1, _, _ := unstructured.NestedStringMap(objects1[1].Object, "spec", "template", "metadata", "annotations")
		ann2, _, _ := unstructured.NestedStringMap(objects2[1].Object, "spec", "template", "metadata", "annotations")
		assert.Equal(t, ann1[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			ann2[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			"identical config should produce identical hashes")
	})

	t.Run("should return error when ConfigMap is missing", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))

		err := stampGatewayConfigHash([]*unstructured.Unstructured{dep}, testInstanceName)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})

	t.Run("should return error when gateway deployment is missing", func(t *testing.T) {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{"operator.json": "{}"}

		err := stampGatewayConfigHash([]*unstructured.Unstructured{cm}, testInstanceName)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found for config hash stamping")
	})
}

// --- Config mode integration tests ---

func TestConfigModeIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should set CLAW_CONFIG_MODE=overwrite on init-config when spec.configMode is overwrite", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.ConfigMode = clawv1alpha1.ConfigModeOverwrite
		instance.Spec.Credentials = testCredentials()
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

		var configModeValue string
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			if ic.Name == ClawInitConfigContainerName {
				for _, env := range ic.Env {
					if env.Name == ClawConfigModeEnvVar {
						configModeValue = env.Value
					}
				}
			}
		}
		assert.Equal(t, "overwrite", configModeValue,
			"init-config should have CLAW_CONFIG_MODE=overwrite from spec.configMode")
	})

	t.Run("should default CLAW_CONFIG_MODE=merge when spec.configMode is not set", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		var configModeValue string
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			if ic.Name == ClawInitConfigContainerName {
				for _, env := range ic.Env {
					if env.Name == ClawConfigModeEnvVar {
						configModeValue = env.Value
					}
				}
			}
		}
		assert.Equal(t, "merge", configModeValue,
			"init-config should default to CLAW_CONFIG_MODE=merge")
	})
}

// --- Config mode deployment unit tests ---

func TestConfigureClawDeploymentConfigMode(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"initContainers": []any{
						map[string]any{
							"name": ClawInitConfigContainerName,
							"env": []any{
								map[string]any{"name": ClawConfigModeEnvVar, "value": "merge"},
							},
						},
					},
					"containers": []any{
						map[string]any{
							"name": ClawGatewayContainerName,
						},
					},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	modeTests := []struct {
		name     string
		mode     clawv1alpha1.ConfigMode
		expected string
	}{
		{name: "default merge when unset", mode: "", expected: "merge"},
		{name: "overwrite when specified", mode: clawv1alpha1.ConfigModeOverwrite, expected: "overwrite"},
		{name: "merge when explicitly set", mode: clawv1alpha1.ConfigModeMerge, expected: "merge"},
	}
	for _, tt := range modeTests {
		t.Run(tt.name, func(t *testing.T) {
			objects := makeDeployment()
			instance := &clawv1alpha1.Claw{}
			instance.Name = testInstanceName
			instance.Spec.ConfigMode = tt.mode

			require.NoError(t, configureClawDeploymentConfigMode(objects, instance))

			initContainers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "initContainers")
			container := initContainers[0].(map[string]any)
			envVars := container["env"].([]any)

			var modeEnv map[string]any
			for _, e := range envVars {
				env := e.(map[string]any)
				if env["name"] == ClawConfigModeEnvVar {
					modeEnv = env
					break
				}
			}
			require.NotNil(t, modeEnv, "CLAW_CONFIG_MODE should exist")
			assert.Equal(t, tt.expected, modeEnv["value"])
		})
	}

	t.Run("should add env var when not already present", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"initContainers": []any{
						map[string]any{
							"name": ClawInitConfigContainerName,
							"env":  []any{},
						},
					},
					"containers": []any{
						map[string]any{"name": ClawGatewayContainerName},
					},
				},
			},
		}
		objects := []*unstructured.Unstructured{dep}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.ConfigMode = clawv1alpha1.ConfigModeOverwrite

		require.NoError(t, configureClawDeploymentConfigMode(objects, instance))

		initContainers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "initContainers")
		container := initContainers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 1, "env var should have been appended")
		env := envVars[0].(map[string]any)
		assert.Equal(t, ClawConfigModeEnvVar, env["name"])
		assert.Equal(t, "overwrite", env["value"])
	})

	t.Run("should return error when deployment is missing", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName

		err := configureClawDeploymentConfigMode(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "claw deployment not found")
	})

	t.Run("should return error when init-config container is missing", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"initContainers": []any{
						map[string]any{"name": "some-other-container"},
					},
					"containers": []any{
						map[string]any{"name": ClawGatewayContainerName},
					},
				},
			},
		}

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName

		err := configureClawDeploymentConfigMode([]*unstructured.Unstructured{dep}, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), ClawInitConfigContainerName)
	})
}

// --- Kubernetes deployment configuration tests ---

func TestConfigureClawDeploymentForKubernetes(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":         ClawGatewayContainerName,
							"env":          []any{},
							"volumeMounts": []any{},
						},
					},
					"volumes": []any{},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should add KUBECONFIG env, PATH, volumes, and init container", func(t *testing.T) {
		objects := makeDeployment()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: []clawv1alpha1.SecretRefEntry{{Name: "kube-secret", Key: "config"}},
				},
				KubeConfig: &kubeconfigData{},
			},
		}

		require.NoError(t, configureClawDeploymentForKubernetes(objects, creds, DefaultKubectlImage, testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)

		envVars := container["env"].([]any)
		envMap := map[string]string{}
		for _, e := range envVars {
			env := e.(map[string]any)
			envMap[env["name"].(string)] = env["value"].(string)
		}
		assert.Equal(t, "/etc/kube/config", envMap["KUBECONFIG"])
		assert.Contains(t, envMap["PATH"], "/opt/kube-tools")

		volumeMounts := container["volumeMounts"].([]any)
		require.Len(t, volumeMounts, 2)
		vmNames := map[string]string{}
		for _, vm := range volumeMounts {
			m := vm.(map[string]any)
			vmNames[m["name"].(string)] = m["mountPath"].(string)
		}
		assert.Equal(t, "/etc/kube", vmNames["kube-config"])
		assert.Equal(t, "/opt/kube-tools", vmNames["kubectl-bin"])

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 2)
		volNames := map[string]bool{}
		for _, v := range volumes {
			vol := v.(map[string]any)
			volNames[vol["name"].(string)] = true
		}
		assert.True(t, volNames["kube-config"])
		assert.True(t, volNames["kubectl-bin"])

		initContainers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "initContainers")
		require.Len(t, initContainers, 1)
		initC := initContainers[0].(map[string]any)
		assert.Equal(t, "init-kubectl", initC["name"])
		assert.Equal(t, DefaultKubectlImage, initC["image"])
	})

	t.Run("should be no-op when no kubernetes credentials exist", func(t *testing.T) {
		objects := makeDeployment()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:   "gemini",
					Type:   clawv1alpha1.CredentialTypeAPIKey,
					Domain: "generativelanguage.googleapis.com",
				},
			},
		}

		require.NoError(t, configureClawDeploymentForKubernetes(objects, creds, DefaultKubectlImage, testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Empty(t, envVars)
	})
}

// --- MCP gateway env injection tests ---

func TestConfigureGatewayForMcpServers(t *testing.T) {
	makeGatewayDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": ClawGatewayContainerName,
							"env":  []any{},
						},
					},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should add secretKeyRef env vars for envFrom entries", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"custom-db": {
				Command: "node",
				Args:    []string{"db-mcp-server.js"},
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_PASSWORD",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-credentials", Key: "password"},
					},
				},
			},
		}

		require.NoError(t, configureGatewayForMcpServers(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 1)
		env := envVars[0].(map[string]any)
		assert.Equal(t, "DB_PASSWORD", env["name"])
		valueFrom := env["valueFrom"].(map[string]any)
		secretKeyRef := valueFrom["secretKeyRef"].(map[string]any)
		assert.Equal(t, "db-credentials", secretKeyRef["name"])
		assert.Equal(t, "password", secretKeyRef["key"])
	})

	t.Run("should be no-op when no envFrom entries exist", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"context7": {URL: "https://mcp.context7.com/mcp"},
			"github": {
				Command: "npx",
				Env:     map[string]string{"TOKEN": "placeholder"},
			},
		}

		require.NoError(t, configureGatewayForMcpServers(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Empty(t, envVars)
	})

	t.Run("should be no-op when no MCP servers configured", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName

		require.NoError(t, configureGatewayForMcpServers(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Empty(t, envVars)
	})

	t.Run("should handle multiple servers with envFrom", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"db-server": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_PASS",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-secret", Key: "pass"},
					},
				},
			},
			"api-server": {
				Command: "python",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "API_KEY",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "api-secret", Key: "key"},
					},
				},
			},
		}

		require.NoError(t, configureGatewayForMcpServers(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 2)
		envNames := map[string]bool{}
		for _, e := range envVars {
			env := e.(map[string]any)
			envNames[env["name"].(string)] = true
		}
		assert.True(t, envNames["DB_PASS"])
		assert.True(t, envNames["API_KEY"])
	})

	t.Run("should return error when deployment is missing", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"db": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "SECRET",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
					},
				},
			},
		}

		err := configureGatewayForMcpServers(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "claw deployment not found")
	})

	t.Run("should return error when gateway container is missing", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "wrong-container"},
					},
				},
			},
		}

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"db": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "SECRET",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "s", Key: "k"},
					},
				},
			},
		}

		err := configureGatewayForMcpServers([]*unstructured.Unstructured{dep}, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), ClawGatewayContainerName)
	})

	t.Run("should preserve pre-existing env vars on gateway container", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": ClawGatewayContainerName,
							"env": []any{
								map[string]any{"name": "HOME", "value": "/home/node"},
								map[string]any{"name": "NODE_ENV", "value": "production"},
							},
						},
					},
				},
			},
		}

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"db": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_PASS",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-secret", Key: "pass"},
					},
				},
			},
		}

		require.NoError(t, configureGatewayForMcpServers([]*unstructured.Unstructured{dep}, instance))

		containers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 3, "should have 2 pre-existing + 1 new env var")
		assert.Equal(t, "HOME", envVars[0].(map[string]any)["name"])
		assert.Equal(t, "NODE_ENV", envVars[1].(map[string]any)["name"])
		assert.Equal(t, "DB_PASS", envVars[2].(map[string]any)["name"])
	})

	t.Run("should handle multiple envFrom on a single server", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"multi-secret-tool": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_USER",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-secret", Key: "user"},
					},
					{
						Name:      "DB_PASS",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-secret", Key: "pass"},
					},
					{
						Name:      "DB_HOST",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-secret", Key: "host"},
					},
				},
			},
		}

		require.NoError(t, configureGatewayForMcpServers(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 3)
		envNames := map[string]bool{}
		for _, e := range envVars {
			env := e.(map[string]any)
			envNames[env["name"].(string)] = true
			valueFrom := env["valueFrom"].(map[string]any)
			secretKeyRef := valueFrom["secretKeyRef"].(map[string]any)
			assert.Equal(t, "db-secret", secretKeyRef["name"])
		}
		assert.True(t, envNames["DB_USER"])
		assert.True(t, envNames["DB_PASS"])
		assert.True(t, envNames["DB_HOST"])
	})

	t.Run("should deduplicate envFrom entries with the same env var name", func(t *testing.T) {
		objects := makeGatewayDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"aaa-server": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "SHARED_TOKEN",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "secret-a", Key: "token"},
					},
				},
			},
			"bbb-server": {
				Command: "python",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "SHARED_TOKEN",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "secret-b", Key: "token"},
					},
				},
			},
		}

		require.NoError(t, configureGatewayForMcpServers(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 1, "duplicate env var names should be deduped")
		env := envVars[0].(map[string]any)
		assert.Equal(t, "SHARED_TOKEN", env["name"])
		secretKeyRef := env["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]any)
		assert.Equal(t, "secret-b", secretKeyRef["name"], "bbb-server should win (sorted after aaa-server)")
	})

	t.Run("should not duplicate when existing env matches desired secretKeyRef", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": ClawGatewayContainerName,
							"env": []any{
								map[string]any{
									"name": "DB_PASS",
									"valueFrom": map[string]any{
										"secretKeyRef": map[string]any{
											"name": "db-secret",
											"key":  "pass",
										},
									},
								},
							},
						},
					},
				},
			},
		}

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"db": {
				Command: "node",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{
						Name:      "DB_PASS",
						SecretRef: clawv1alpha1.SecretRefEntry{Name: "db-secret", Key: "pass"},
					},
				},
			},
		}

		require.NoError(t, configureGatewayForMcpServers([]*unstructured.Unstructured{dep}, instance))

		containers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		require.Len(t, envVars, 1, "matching existing env should not create a duplicate")
	})

	t.Run("should produce deterministic order across multiple runs", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.McpServers = map[string]clawv1alpha1.McpServerSpec{
			"z-server": {
				Command: "z",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{Name: "Z_VAR", SecretRef: clawv1alpha1.SecretRefEntry{Name: "z-secret", Key: "k"}},
				},
			},
			"a-server": {
				Command: "a",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{Name: "A_VAR", SecretRef: clawv1alpha1.SecretRefEntry{Name: "a-secret", Key: "k"}},
				},
			},
			"m-server": {
				Command: "m",
				EnvFrom: []clawv1alpha1.McpEnvFromSecret{
					{Name: "M_VAR", SecretRef: clawv1alpha1.SecretRefEntry{Name: "m-secret", Key: "k"}},
				},
			},
		}

		orders := make([]string, 0, 10)
		for range 10 {
			objects := makeGatewayDeployment()
			require.NoError(t, configureGatewayForMcpServers(objects, instance))

			containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
			container := containers[0].(map[string]any)
			envVars := container["env"].([]any)

			names := make([]string, 0, len(envVars))
			for _, e := range envVars {
				names = append(names, e.(map[string]any)["name"].(string))
			}
			orders = append(orders, fmt.Sprintf("%v", names))
		}

		for i := 1; i < len(orders); i++ {
			assert.Equal(t, orders[0], orders[i], "env var order should be deterministic")
		}
	})
}

func TestMcpAnnotationKey(t *testing.T) {
	t.Run("should produce valid annotation key segment", func(t *testing.T) {
		key := mcpAnnotationKey("my-server", "MY_VAR")
		assert.Len(t, key, 12, "hash should be 12 hex characters (6 bytes)")
		assert.Regexp(t, `^[0-9a-f]{12}$`, key)
	})

	t.Run("should be deterministic", func(t *testing.T) {
		k1 := mcpAnnotationKey("server", "VAR")
		k2 := mcpAnnotationKey("server", "VAR")
		assert.Equal(t, k1, k2)
	})

	t.Run("should differ for different inputs", func(t *testing.T) {
		k1 := mcpAnnotationKey("server-a", "VAR")
		k2 := mcpAnnotationKey("server-b", "VAR")
		assert.NotEqual(t, k1, k2)
	})

	t.Run("should handle special characters safely", func(t *testing.T) {
		key := mcpAnnotationKey("server/with:special.chars!", "ENV_WITH_UNDERSCORES")
		assert.Regexp(t, `^[0-9a-f]{12}$`, key, "should only contain valid hex chars regardless of input")
	})
}
