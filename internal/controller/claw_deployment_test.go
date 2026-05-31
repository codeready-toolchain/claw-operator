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
		require.NoError(t, stampGatewayConfigHash(objects, testInstanceName, nil))

		annotations, _, _ := unstructured.NestedStringMap(objects[1].Object, "spec", "template", "metadata", "annotations")
		hash, exists := annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash]
		assert.True(t, exists, "gateway-config-hash annotation should exist")
		assert.Len(t, hash, 64, "hash should be a 64-char hex SHA-256")
	})

	t.Run("should produce different hashes for different config content", func(t *testing.T) {
		objects1 := makeObjects(`{"gateway":{"auth":{"mode":"token"}}}`)
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, nil))

		objects2 := makeObjects(`{"gateway":{"auth":{"mode":"token","scopes":["operator.admin"]}}}`)
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, nil))

		ann1, _, _ := unstructured.NestedStringMap(objects1[1].Object, "spec", "template", "metadata", "annotations")
		ann2, _, _ := unstructured.NestedStringMap(objects2[1].Object, "spec", "template", "metadata", "annotations")
		assert.NotEqual(t, ann1[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			ann2[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			"different config should produce different hashes")
	})

	t.Run("should produce identical hashes for identical content", func(t *testing.T) {
		config := `{"gateway":{"port":18789}}`
		objects1 := makeObjects(config)
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, nil))

		objects2 := makeObjects(config)
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, nil))

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

		err := stampGatewayConfigHash([]*unstructured.Unstructured{dep}, testInstanceName, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})

	t.Run("should return error when gateway deployment is missing", func(t *testing.T) {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{"operator.json": "{}"}

		err := stampGatewayConfigHash([]*unstructured.Unstructured{cm}, testInstanceName, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found for config hash stamping")
	})

	t.Run("should produce different hash when workspace keys are added", func(t *testing.T) {
		objects1 := makeObjects(`{"gateway":{"port":18789}}`)
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, nil))

		objects2 := makeObjects(`{"gateway":{"port":18789}}`)
		require.NoError(t, unstructured.SetNestedField(objects2[0].Object, "# Identity", "data", "_ws_IDENTITY.md"))
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, nil))

		ann1, _, _ := unstructured.NestedStringMap(objects1[1].Object, "spec", "template", "metadata", "annotations")
		ann2, _, _ := unstructured.NestedStringMap(objects2[1].Object, "spec", "template", "metadata", "annotations")
		assert.NotEqual(t, ann1[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			ann2[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			"adding workspace keys should change the config hash to trigger rollout")
	})

	t.Run("should produce different hash when skill keys are added", func(t *testing.T) {
		objects1 := makeObjects(`{"gateway":{"port":18789}}`)
		require.NoError(t, stampGatewayConfigHash(objects1, testInstanceName, nil))

		objects2 := makeObjects(`{"gateway":{"port":18789}}`)
		require.NoError(t, unstructured.SetNestedField(objects2[0].Object, "# Skill content", "data", "_skill_compliance"))
		require.NoError(t, stampGatewayConfigHash(objects2, testInstanceName, nil))

		ann1, _, _ := unstructured.NestedStringMap(objects1[1].Object, "spec", "template", "metadata", "annotations")
		ann2, _, _ := unstructured.NestedStringMap(objects2[1].Object, "spec", "template", "metadata", "annotations")
		assert.NotEqual(t, ann1[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			ann2[clawv1alpha1.AnnotationKeyGatewayConfigHash],
			"adding skill keys should change the config hash to trigger rollout")
	})
}

// --- Config mode integration tests ---

func TestConfigModeIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should set CLAW_CONFIG_MODE=overwrite on init-config when spec.config.mergeMode is overwrite", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Config = &clawv1alpha1.ConfigSpec{MergeMode: clawv1alpha1.ConfigModeOverwrite}
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
			"init-config should have CLAW_CONFIG_MODE=overwrite from spec.config.mergeMode")
	})

	t.Run("should default CLAW_CONFIG_MODE=merge when spec.config is not set", func(t *testing.T) {
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
			if tt.mode != "" {
				instance.Spec.Config = &clawv1alpha1.ConfigSpec{MergeMode: tt.mode}
			}

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
		instance.Spec.Config = &clawv1alpha1.ConfigSpec{MergeMode: clawv1alpha1.ConfigModeOverwrite}

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

// --- PVC volume mount safety tests ---

func TestClawHomePVCMountsUseSubPath(t *testing.T) {
	expectedSubPaths := map[string]string{
		"/home/node/.openclaw": "home",
		"/home/node/.local":    "home/.local",
		"/home/node/.cache":    "home/.cache",
		"/home/node/.config":   "home/.config",
	}

	reconciler := createClawReconciler()
	instance := testClawWithCredentials(testCredentials())
	objects, err := reconciler.buildKustomizedObjects(instance)
	require.NoError(t, err)

	gatewayName := getClawDeploymentName(testInstanceName)
	var gatewayDep *unstructured.Unstructured
	for _, obj := range objects {
		if obj.GetKind() == DeploymentKind && obj.GetName() == gatewayName {
			gatewayDep = obj
			break
		}
	}
	require.NotNil(t, gatewayDep, "gateway deployment should exist in rendered manifests")

	seen := make(map[string]bool)
	for _, containerPath := range [][]string{
		{"spec", "template", "spec", "containers"},
		{"spec", "template", "spec", "initContainers"},
	} {
		containers, _, err := unstructured.NestedSlice(gatewayDep.Object, containerPath...)
		require.NoError(t, err)

		for _, c := range containers {
			container := c.(map[string]any)
			name, _, _ := unstructured.NestedString(container, "name")
			if name == "init-volume" {
				continue // init-volume intentionally mounts the raw PVC root
			}
			mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
			for _, m := range mounts {
				mount := m.(map[string]any)
				if mount["name"] != "claw-home" {
					continue
				}
				mountPath, _ := mount["mountPath"].(string)
				subPath, _ := mount["subPath"].(string)
				expected, known := expectedSubPaths[mountPath]
				require.True(t, known,
					"container %q: unexpected claw-home mount at %s", name, mountPath)
				assert.Equal(t, expected, subPath,
					"container %q: claw-home mount at %s must use correct subPath",
					name, mountPath)
				seen[mountPath] = true
			}
		}
	}

	for mountPath := range expectedSubPaths {
		assert.True(t, seen[mountPath],
			"expected claw-home mount at %s not found in any container", mountPath)
	}
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

// --- Desired template hash (Recreate rollout guard) tests ---

func TestStampDesiredTemplateHash(t *testing.T) {
	t.Run("should set annotation on object with no existing annotations", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetName("test")
		stampDesiredTemplateHash(obj, "abc123")
		assert.Equal(t, "abc123", obj.GetAnnotations()[clawv1alpha1.AnnotationKeyDesiredTemplateHash])
	})

	t.Run("should preserve existing annotations", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetName("test")
		obj.SetAnnotations(map[string]string{"existing": "value"})
		stampDesiredTemplateHash(obj, "def456")
		ann := obj.GetAnnotations()
		assert.Equal(t, "def456", ann[clawv1alpha1.AnnotationKeyDesiredTemplateHash])
		assert.Equal(t, "value", ann["existing"])
	})
}

func TestIsRecreateDeploymentUnchanged(t *testing.T) {
	ctx := context.Background()

	makeDeployment := func(name, ns, image string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind(DeploymentKind))
		obj.SetName(name)
		obj.SetNamespace(ns)
		obj.Object["spec"] = map[string]any{
			"replicas": int64(1),
			"selector": map[string]any{
				"matchLabels": map[string]any{"app": "claw"},
			},
			"strategy": map[string]any{"type": "Recreate"},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{"app": "claw"},
					"annotations": map[string]any{
						clawv1alpha1.AnnotationKeyGatewayConfigHash: "somehash",
					},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":  "gateway",
							"image": image,
						},
					},
				},
			},
		}
		return obj
	}

	t.Run("should return false and stamp hash when deployment does not exist", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired := makeDeployment("nonexistent-deploy", namespace, "ghcr.io/openclaw/openclaw:slim")

		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired)
		assert.Error(t, err)
		assert.False(t, unchanged)
		assert.NotEmpty(t, desired.GetAnnotations()[clawv1alpha1.AnnotationKeyDesiredTemplateHash],
			"hash should be stamped on desired even when current doesn't exist")
	})

	t.Run("should return false on first apply then true on identical second reconcile", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		deployName := fmt.Sprintf("hash-test-%s", "idempotent")

		desired1 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired1)
		assert.Error(t, err, "should error because deployment doesn't exist yet")
		assert.False(t, unchanged)

		// Apply the deployment with the stamped hash
		require.NoError(t, k8sClient.Patch(ctx, desired1, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptrTo(true),
		}))

		// Second reconcile with identical template
		desired2 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		unchanged, err = reconciler.isRecreateDeploymentUnchanged(ctx, desired2)
		assert.NoError(t, err)
		assert.True(t, unchanged, "should detect no change on identical template")
	})

	t.Run("should return false when image changes", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		deployName := fmt.Sprintf("hash-test-%s", "image-change")

		desired1 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:v1")
		_, _ = reconciler.isRecreateDeploymentUnchanged(ctx, desired1)
		require.NoError(t, k8sClient.Patch(ctx, desired1, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptrTo(true),
		}))

		desired2 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:v2")
		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired2)
		assert.NoError(t, err)
		assert.False(t, unchanged, "image change should be detected")
		assert.NotEmpty(t, desired2.GetAnnotations()[clawv1alpha1.AnnotationKeyDesiredTemplateHash],
			"new hash should be stamped on desired")
	})

	t.Run("should return false when pod template annotation changes", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		deployName := fmt.Sprintf("hash-test-%s", "ann-change")

		desired1 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		_, _ = reconciler.isRecreateDeploymentUnchanged(ctx, desired1)
		require.NoError(t, k8sClient.Patch(ctx, desired1, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptrTo(true),
		}))

		// Change the gateway-config-hash annotation (simulates config change)
		desired2 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		require.NoError(t, unstructured.SetNestedField(desired2.Object, "newhash",
			"spec", "template", "metadata", "annotations", clawv1alpha1.AnnotationKeyGatewayConfigHash))

		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired2)
		assert.NoError(t, err)
		assert.False(t, unchanged, "annotation change should be detected")
	})

	t.Run("should return false when new container is added", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		deployName := fmt.Sprintf("hash-test-%s", "new-container")

		desired1 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		_, _ = reconciler.isRecreateDeploymentUnchanged(ctx, desired1)
		require.NoError(t, k8sClient.Patch(ctx, desired1, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptrTo(true),
		}))

		// Add a sidecar container
		desired2 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		containers, _, _ := unstructured.NestedSlice(desired2.Object, "spec", "template", "spec", "containers")
		containers = append(containers, map[string]any{
			"name":  "sidecar",
			"image": "otel/collector:latest",
		})
		require.NoError(t, unstructured.SetNestedSlice(desired2.Object, containers, "spec", "template", "spec", "containers"))

		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired2)
		assert.NoError(t, err)
		assert.False(t, unchanged, "adding a container should be detected")
	})

	t.Run("should return false when volume is added", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		deployName := fmt.Sprintf("hash-test-%s", "new-volume")

		desired1 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		_, _ = reconciler.isRecreateDeploymentUnchanged(ctx, desired1)
		require.NoError(t, k8sClient.Patch(ctx, desired1, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptrTo(true),
		}))

		// Add a volume to template spec
		desired2 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		require.NoError(t, unstructured.SetNestedSlice(desired2.Object, []any{
			map[string]any{
				"name":     "new-vol",
				"emptyDir": map[string]any{},
			},
		}, "spec", "template", "spec", "volumes"))

		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired2)
		assert.NoError(t, err)
		assert.False(t, unchanged, "adding a volume should be detected")
	})

	t.Run("should return false when replicas changes (idle/unidle)", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		deployName := fmt.Sprintf("hash-test-%s", "replicas")

		// Apply with replicas=1
		desired1 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		_, _ = reconciler.isRecreateDeploymentUnchanged(ctx, desired1)
		require.NoError(t, k8sClient.Patch(ctx, desired1, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptrTo(true),
		}))

		// Simulate idler scaling to 0 (external actor modifies replicas)
		idled := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		require.NoError(t, unstructured.SetNestedField(idled.Object, int64(0), "spec", "replicas"))
		require.NoError(t, k8sClient.Patch(ctx, idled, client.Apply, &client.PatchOptions{
			FieldManager: "idler",
			Force:        ptrTo(true),
		}))

		// Operator wants replicas=1 again (unidle) — template unchanged, but replicas differs
		desired2 := makeDeployment(deployName, namespace, "ghcr.io/openclaw/openclaw:slim")
		unchanged, err := reconciler.isRecreateDeploymentUnchanged(ctx, desired2)
		assert.NoError(t, err)
		assert.False(t, unchanged, "replicas change should be detected even if template hash matches")
	})

	t.Run("should produce a valid 64-char hex hash", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired := makeDeployment("hash-test-format", namespace, "ghcr.io/openclaw/openclaw:slim")
		_, _ = reconciler.isRecreateDeploymentUnchanged(ctx, desired)

		hash := desired.GetAnnotations()[clawv1alpha1.AnnotationKeyDesiredTemplateHash]
		assert.Regexp(t, `^[0-9a-f]{64}$`, hash, "hash should be a 64-char hex SHA-256")
	})
}

// --- Desired template hash integration test (full reconcile loop) ---

func TestDesiredTemplateHashIntegration(t *testing.T) {
	const resourceName = testInstanceName
	ctx := context.Background()

	t.Run("should stamp desired-template-hash on gateway deployment metadata", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "gateway Deployment should be created")

		hash, exists := deployment.Annotations[clawv1alpha1.AnnotationKeyDesiredTemplateHash]
		assert.True(t, exists, "desired-template-hash should be on Deployment metadata.annotations")
		assert.Regexp(t, `^[0-9a-f]{64}$`, hash, "hash should be a 64-char hex SHA-256")
	})

	t.Run("should not re-apply gateway deployment on idempotent reconcile", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "gateway Deployment should be created")

		gen1 := deployment.Generation

		// Second reconcile — should skip the Recreate deployment
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getClawDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))

		assert.Equal(t, gen1, deployment.Generation,
			"generation should not increment on idempotent reconcile")
	})
}

func ptrTo[T any](v T) *T { return &v }
