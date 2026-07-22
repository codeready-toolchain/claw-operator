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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Image pull policy tests ---

func TestConfigureImagePullPolicy(t *testing.T) {
	makeObjects := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName("test-deploy")
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":            "gateway",
							"imagePullPolicy": "IfNotPresent",
						},
						map[string]any{
							"name":            "sidecar",
							"imagePullPolicy": "IfNotPresent",
						},
					},
					"initContainers": []any{
						map[string]any{
							"name":            "init",
							"imagePullPolicy": "IfNotPresent",
						},
					},
				},
			},
		}
		svc := &unstructured.Unstructured{}
		svc.SetKind(ServiceKind)
		svc.SetName("test-svc")
		return []*unstructured.Unstructured{dep, svc}
	}

	t.Run("empty policy preserves defaults", func(t *testing.T) {
		objects := makeObjects()
		require.NoError(t, configureImagePullPolicy(objects, ""))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		for _, c := range containers {
			cm := c.(map[string]any)
			assert.Equal(t, "IfNotPresent", cm["imagePullPolicy"])
		}
	})

	t.Run("overrides all containers and initContainers", func(t *testing.T) {
		objects := makeObjects()
		require.NoError(t, configureImagePullPolicy(objects, "Always"))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		for _, c := range containers {
			cm := c.(map[string]any)
			assert.Equal(t, "Always", cm["imagePullPolicy"])
		}

		initContainers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "initContainers")
		for _, c := range initContainers {
			cm := c.(map[string]any)
			assert.Equal(t, "Always", cm["imagePullPolicy"])
		}
	})

	t.Run("skips non-deployment objects", func(t *testing.T) {
		svc := &unstructured.Unstructured{}
		svc.SetKind(ServiceKind)
		svc.SetName("test-svc")
		objects := []*unstructured.Unstructured{svc}
		require.NoError(t, configureImagePullPolicy(objects, "Always"))
	})

	t.Run("handles deployment with no initContainers", func(t *testing.T) {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName("no-init")
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "main", "imagePullPolicy": "Never"},
					},
				},
			},
		}
		require.NoError(t, configureImagePullPolicy([]*unstructured.Unstructured{dep}, "Always"))

		containers, _, _ := unstructured.NestedSlice(dep.Object, "spec", "template", "spec", "containers")
		cm := containers[0].(map[string]any)
		assert.Equal(t, "Always", cm["imagePullPolicy"])
	})
}

// --- Anthropic Vertex AI deployment configuration tests ---

func TestConfigureClawDeploymentForAnthropicVertex(t *testing.T) {
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

		require.NoError(t, configureClawDeploymentForAnthropicVertexSDK(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		var adcEnv, projectEnv, nodeOptsEnv map[string]any
		for _, e := range envVars {
			env := e.(map[string]any)
			switch env["name"] {
			case "GOOGLE_APPLICATION_CREDENTIALS":
				adcEnv = env
			case "ANTHROPIC_VERTEX_PROJECT_ID":
				projectEnv = env
			case envNodeOptions:
				nodeOptsEnv = env
			case envGoogleCloudLocation, "CLOUD_ML_REGION":
				t.Errorf("unexpected env %q on Anthropic Vertex path; region comes from provider baseUrl", env["name"])
			}
		}

		require.NotNil(t, adcEnv, "GOOGLE_APPLICATION_CREDENTIALS should be set")
		assert.Equal(t, "/etc/vertex-adc/adc.json", adcEnv["value"])

		require.NotNil(t, projectEnv, "ANTHROPIC_VERTEX_PROJECT_ID should be set")
		assert.Equal(t, "my-project", projectEnv["value"])

		require.NotNil(t, nodeOptsEnv, "NODE_OPTIONS should preload GoogleAuth stub")
		assert.Equal(t, googleAuthProxyStubRequire, nodeOptsEnv["value"])

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

		require.NoError(t, configureClawDeploymentForAnthropicVertexSDK(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Len(t, envVars, 1, "should only have original HOME env var")
	})

	t.Run("mixed providers still configure Vertex ADC path only", func(t *testing.T) {
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
			{
				Name:     "openai",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "openai",
				Domain:   "api.openai.com",
			},
		}

		require.NoError(t, configureClawDeploymentForAnthropicVertexSDK(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		var adcEnv, nodeOptsEnv, openaiKeyEnv map[string]any
		for _, e := range envVars {
			env := e.(map[string]any)
			switch env["name"] {
			case "GOOGLE_APPLICATION_CREDENTIALS":
				adcEnv = env
			case envNodeOptions:
				nodeOptsEnv = env
			case "OPENAI_API_KEY":
				openaiKeyEnv = env
			}
		}

		require.NotNil(t, adcEnv, "Vertex ADC env should be present alongside openai")
		require.NotNil(t, nodeOptsEnv, "GoogleAuth stub preload should be present alongside openai")
		assert.Equal(t, googleAuthProxyStubRequire, nodeOptsEnv["value"])
		assert.Nil(t, openaiKeyEnv,
			"Anthropic Vertex helper must not inject openai env; openai stays on the credential/proxy path")
	})
}

// --- GCP Vertex deployment configuration tests ---

func TestConfigureClawDeploymentForGCPVertex(t *testing.T) {
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
						},
					},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should set GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION", func(t *testing.T) {
		objects := makeDeployment()
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-gcp-project",
					Location: "us-central1",
				},
			},
		}

		require.NoError(t, configureClawDeploymentForGCPVertex(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		var projectEnv, locationEnv map[string]any
		for _, e := range envVars {
			env := e.(map[string]any)
			switch env["name"] {
			case "GOOGLE_CLOUD_PROJECT":
				projectEnv = env
			case envGoogleCloudLocation:
				locationEnv = env
			}
		}

		require.NotNil(t, projectEnv, "GOOGLE_CLOUD_PROJECT should be set")
		assert.Equal(t, "my-gcp-project", projectEnv["value"])

		require.NotNil(t, locationEnv, "GOOGLE_CLOUD_LOCATION should be set")
		assert.Equal(t, "us-central1", locationEnv["value"])
	})

	t.Run("should use global location", func(t *testing.T) {
		objects := makeDeployment()
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeGCP,
				Provider: "google",
				Domain:   ".googleapis.com",
				GCP: &clawv1alpha1.GCPConfig{
					Project:  "my-project",
					Location: "global",
				},
			},
		}

		require.NoError(t, configureClawDeploymentForGCPVertex(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)

		var locationEnv map[string]any
		for _, e := range envVars {
			env := e.(map[string]any)
			if env["name"] == envGoogleCloudLocation {
				locationEnv = env
			}
		}

		require.NotNil(t, locationEnv)
		assert.Equal(t, "global", locationEnv["value"])
	})

	t.Run("should be no-op for google apiKey credential", func(t *testing.T) {
		objects := makeDeployment()
		credentials := []clawv1alpha1.CredentialSpec{
			{
				Name:     "gemini",
				Type:     clawv1alpha1.CredentialTypeAPIKey,
				Provider: "google",
				Domain:   "generativelanguage.googleapis.com",
			},
		}

		require.NoError(t, configureClawDeploymentForGCPVertex(objects, toResolved(credentials), testInstanceName))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Len(t, envVars, 1, "should only have original HOME env var")
	})

	t.Run("should be no-op for non-google Vertex credential", func(t *testing.T) {
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

		require.NoError(t, configureClawDeploymentForGCPVertex(objects, toResolved(credentials), testInstanceName))

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

// --- CreateOrUpdate deployment apply tests ---

func TestApplyDeployment(t *testing.T) {
	ctx := context.Background()

	makeUnstructuredDeployment := func(name, ns, image string) *unstructured.Unstructured {
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

	t.Run("should create deployment on first apply", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired := makeUnstructuredDeployment("cou-create", namespace, DefaultOpenClawImage)

		changed, err := reconciler.applyDeployment(ctx, desired)
		require.NoError(t, err)
		assert.True(t, changed, "first apply should report changed")

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-create", Namespace: namespace}, deployment))
		assert.Equal(t, DefaultOpenClawImage, deployment.Spec.Template.Spec.Containers[0].Image)
	})

	t.Run("should not update on identical second apply", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired1 := makeUnstructuredDeployment("cou-idempotent", namespace, DefaultOpenClawImage)

		changed, err := reconciler.applyDeployment(ctx, desired1)
		require.NoError(t, err)
		assert.True(t, changed)

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-idempotent", Namespace: namespace}, deployment))
		gen1 := deployment.Generation

		desired2 := makeUnstructuredDeployment("cou-idempotent", namespace, DefaultOpenClawImage)
		changed, err = reconciler.applyDeployment(ctx, desired2)
		require.NoError(t, err)
		assert.False(t, changed, "identical second apply should report unchanged")

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-idempotent", Namespace: namespace}, deployment))
		assert.Equal(t, gen1, deployment.Generation, "generation should not increment on idempotent apply")
	})

	t.Run("should update when image changes", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired1 := makeUnstructuredDeployment("cou-image-change", namespace, "ghcr.io/openclaw/openclaw:v1")

		_, err := reconciler.applyDeployment(ctx, desired1)
		require.NoError(t, err)

		desired2 := makeUnstructuredDeployment("cou-image-change", namespace, "ghcr.io/openclaw/openclaw:v2")
		changed, err := reconciler.applyDeployment(ctx, desired2)
		require.NoError(t, err)
		assert.True(t, changed, "image change should be detected")

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-image-change", Namespace: namespace}, deployment))
		assert.Equal(t, "ghcr.io/openclaw/openclaw:v2", deployment.Spec.Template.Spec.Containers[0].Image)
	})

	t.Run("should update when replicas change", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired1 := makeUnstructuredDeployment("cou-replicas", namespace, DefaultOpenClawImage)

		_, err := reconciler.applyDeployment(ctx, desired1)
		require.NoError(t, err)

		desired2 := makeUnstructuredDeployment("cou-replicas", namespace, DefaultOpenClawImage)
		require.NoError(t, unstructured.SetNestedField(desired2.Object, int64(0), "spec", "replicas"))

		changed, err := reconciler.applyDeployment(ctx, desired2)
		require.NoError(t, err)
		assert.True(t, changed, "replicas change should be detected")
	})

	t.Run("should preserve annotations from other controllers", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired1 := makeUnstructuredDeployment("cou-annot", namespace, DefaultOpenClawImage)

		_, err := reconciler.applyDeployment(ctx, desired1)
		require.NoError(t, err)

		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-annot", Namespace: namespace}, deployment))
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}
		deployment.Annotations["kubectl.kubernetes.io/last-applied-configuration"] = "{}"
		require.NoError(t, k8sClient.Update(ctx, deployment))

		desired2 := makeUnstructuredDeployment("cou-annot", namespace, DefaultOpenClawImage)
		_, err = reconciler.applyDeployment(ctx, desired2)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-annot", Namespace: namespace}, deployment))
		assert.Equal(t, "{}",
			deployment.Annotations["kubectl.kubernetes.io/last-applied-configuration"],
			"annotations from other controllers should be preserved")
	})

	t.Run("should preserve labels from other controllers", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		reconciler := createClawReconciler()
		desired1 := makeUnstructuredDeployment("cou-labels", namespace, DefaultOpenClawImage)

		_, err := reconciler.applyDeployment(ctx, desired1)
		require.NoError(t, err)

		// Simulate another controller adding a label
		deployment := &appsv1.Deployment{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-labels", Namespace: namespace}, deployment))
		if deployment.Labels == nil {
			deployment.Labels = make(map[string]string)
		}
		deployment.Labels["other-controller"] = "managed"
		require.NoError(t, k8sClient.Update(ctx, deployment))

		// Re-apply — operator's labels should merge, not clobber
		desired2 := makeUnstructuredDeployment("cou-labels", namespace, DefaultOpenClawImage)
		desired2.SetLabels(map[string]string{"app": "claw"})
		_, err = reconciler.applyDeployment(ctx, desired2)
		require.NoError(t, err)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "cou-labels", Namespace: namespace}, deployment))
		assert.Equal(t, "managed", deployment.Labels["other-controller"],
			"labels from other controllers should be preserved")
		assert.Equal(t, "claw", deployment.Labels["app"])
	})
}

// --- CreateOrUpdate integration test (full reconcile loop) ---

func TestDeploymentCreateOrUpdateIntegration(t *testing.T) {
	const resourceName = testInstanceName
	ctx := context.Background()

	t.Run("should not increment gateway generation on idempotent reconcile", func(t *testing.T) {
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

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getClawDeploymentName(testInstanceName),
			Namespace: namespace,
		}, deployment))

		assert.Equal(t, gen1, deployment.Generation,
			"generation should not increment on idempotent reconcile")
	})

	t.Run("should not increment proxy generation on idempotent reconcile", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		proxyDeploy := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyDeploymentName(testInstanceName),
				Namespace: namespace,
			}, proxyDeploy) == nil
		}, "proxy Deployment should be created")

		proxyGen := proxyDeploy.Generation

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name:      getProxyDeploymentName(testInstanceName),
			Namespace: namespace,
		}, proxyDeploy))

		assert.Equal(t, proxyGen, proxyDeploy.Generation,
			"proxy generation should not increment on idempotent reconcile")
	})

	t.Run("should set owner references on deployments", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, resourceName, namespace)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		for _, name := range []string{
			getClawDeploymentName(testInstanceName),
			getProxyDeploymentName(testInstanceName),
		} {
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment) == nil
			}, name+" should be created")

			require.NotEmpty(t, deployment.OwnerReferences,
				"%s should have owner references", name)
			assert.Equal(t, ClawResourceKind, deployment.OwnerReferences[0].Kind,
				"%s owner should be a Claw", name)
		}
	})
}

// --- NO_PROXY tests ---

const envNoProxy = "NO_PROXY"

// findEnvValue returns a pointer to the value of the first unstructured env var matching name, or nil.
func findEnvValue(envVars []any, name string) *string {
	for _, e := range envVars {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if em["name"] == name {
			v, _ := em["value"].(string)
			return &v
		}
	}
	return nil
}

// findContainer returns the container with the given name from a Deployment, or nil.
func findContainer(dep *appsv1.Deployment, name string) *corev1.Container {
	for i := range dep.Spec.Template.Spec.Containers {
		if dep.Spec.Template.Spec.Containers[i].Name == name {
			return &dep.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

// findContainerEnv returns the env var with the given name from a container, or nil.
func findContainerEnv(c *corev1.Container, name string) *corev1.EnvVar {
	for i := range c.Env {
		if c.Env[i].Name == name {
			return &c.Env[i]
		}
	}
	return nil
}

func TestConfigureGatewayNoProxy(t *testing.T) {
	makeGatewayDeploy := func() []*unstructured.Unstructured {
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
								map[string]any{"name": "NO_PROXY", "value": "localhost,127.0.0.1," + testInstanceName + "-proxy"},
								map[string]any{"name": envNoProxyLower, "value": "localhost,127.0.0.1," + testInstanceName + "-proxy"},
							},
						},
					},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should not modify NO_PROXY when bypass is off (default)", func(t *testing.T) {
		objects := makeGatewayDeploy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}

		require.NoError(t, configureGatewayNoProxy(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		envVars := containers[0].(map[string]any)["env"].([]any)
		val := findEnvValue(envVars, envNoProxy)
		require.NotNil(t, val, "NO_PROXY env var should exist")
		assert.NotContains(t, *val, ".svc")
	})

	t.Run("should append .svc suffixes when bypass is on", func(t *testing.T) {
		objects := makeGatewayDeploy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Network: &clawv1alpha1.NetworkSpec{InClusterBypass: ptr.To(true)},
			},
		}

		require.NoError(t, configureGatewayNoProxy(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		envVars := containers[0].(map[string]any)["env"].([]any)

		noProxyVal := findEnvValue(envVars, envNoProxy)
		require.NotNil(t, noProxyVal, "NO_PROXY env var should exist")
		assert.Contains(t, *noProxyVal, ".svc,.svc.cluster.local")

		noProxyLowerVal := findEnvValue(envVars, envNoProxyLower)
		require.NotNil(t, noProxyLowerVal, "no_proxy env var should exist")
		assert.Contains(t, *noProxyLowerVal, ".svc,.svc.cluster.local")
	})

	t.Run("should not modify NO_PROXY when bypass is explicitly false", func(t *testing.T) {
		objects := makeGatewayDeploy()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Network: &clawv1alpha1.NetworkSpec{InClusterBypass: ptr.To(false)},
			},
		}

		require.NoError(t, configureGatewayNoProxy(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		envVars := containers[0].(map[string]any)["env"].([]any)
		val := findEnvValue(envVars, envNoProxy)
		require.NotNil(t, val, "NO_PROXY env var should exist")
		assert.NotContains(t, *val, ".svc")
	})
}

func TestGatewayNoProxyIntegration(t *testing.T) {
	t.Run("gateway should not have .svc in NO_PROXY when bypass is off", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
			}, deployment) == nil
		}, "gateway deployment should be created")

		gatewayContainer := findContainer(deployment, ClawGatewayContainerName)
		require.NotNil(t, gatewayContainer, "gateway container should exist")

		noProxyEnv := findContainerEnv(gatewayContainer, envNoProxy)
		require.NotNil(t, noProxyEnv, "NO_PROXY env var should exist on gateway")
		assert.NotContains(t, noProxyEnv.Value, ".svc",
			"NO_PROXY should not contain .svc when bypass is off")

		noProxyLowerEnv := findContainerEnv(gatewayContainer, envNoProxyLower)
		require.NotNil(t, noProxyLowerEnv, "no_proxy env var should exist on gateway")
		assert.NotContains(t, noProxyLowerEnv.Value, ".svc",
			"no_proxy should not contain .svc when bypass is off")
	})

	t.Run("gateway should have .svc in NO_PROXY when bypass is on", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
				Network:     &clawv1alpha1.NetworkSpec{InClusterBypass: ptr.To(true)},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
			}, deployment) == nil
		}, "gateway deployment should be created")

		gatewayContainer := findContainer(deployment, ClawGatewayContainerName)
		require.NotNil(t, gatewayContainer, "gateway container should exist")

		noProxyEnv := findContainerEnv(gatewayContainer, envNoProxy)
		require.NotNil(t, noProxyEnv, "NO_PROXY env var should exist on gateway")
		assert.Contains(t, noProxyEnv.Value, ".svc,.svc.cluster.local",
			"NO_PROXY should contain .svc suffixes when bypass is on")

		noProxyLowerEnv := findContainerEnv(gatewayContainer, envNoProxyLower)
		require.NotNil(t, noProxyLowerEnv, "no_proxy env var should exist on gateway")
		assert.Contains(t, noProxyLowerEnv.Value, ".svc,.svc.cluster.local",
			"no_proxy should contain .svc suffixes when bypass is on")
	})
}

// --- Vertex ADC ConfigMap apply tests ---

func TestApplyVertexADCConfigMap(t *testing.T) {
	t.Run("creates ADC ConfigMap when Vertex SDK credentials are present", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "vertex-cred",
						Type:     clawv1alpha1.CredentialTypeGCP,
						Provider: "anthropic",
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: aiModelSecret, Key: aiModelSecretKey},
						},
						Domain: ".googleapis.com",
						GCP: &clawv1alpha1.GCPConfig{
							Project:  "test-project",
							Location: "us-central1",
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		creds := toResolved(instance.Spec.Credentials)

		require.NoError(t, reconciler.applyVertexADCConfigMap(ctx, instance, creds))

		cm := &corev1.ConfigMap{}
		cmName := getVertexADCConfigMapName(testInstanceName)
		err := k8sClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm)
		require.NoError(t, err, "Vertex ADC ConfigMap should be created")

		assert.Contains(t, cm.Data["adc.json"], "authorized_user")
		assert.Contains(t, cm.Data["adc.json"], "proxy-managed-token")
		assert.Contains(t, cm.Data["google-auth-proxy-stub.js"], "refreshTokenNoCache")
		assert.Contains(t, cm.Data["google-auth-proxy-stub.js"], "claw.vertexProxyADC")
		assert.Contains(t, cm.Data["google-auth-proxy-stub.js"], "isVertexProxyADCClient")
		assert.Contains(t, cm.Data["google-auth-proxy-stub.js"], vertexProxyVendedToken)
		assert.NotContains(t, cm.Data["google-auth-proxy-stub.js"], "UserRefreshClient.prototype",
			"stub must not mutate UserRefreshClient.prototype process-wide")

		require.Len(t, cm.OwnerReferences, 1, "should have owner reference")
		assert.Equal(t, instance.Name, cm.OwnerReferences[0].Name)
	})

	t.Run("no-ops when no Vertex SDK credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		creds := toResolved(instance.Spec.Credentials)

		require.NoError(t, reconciler.applyVertexADCConfigMap(ctx, instance, creds))

		cm := &corev1.ConfigMap{}
		cmName := getVertexADCConfigMapName(testInstanceName)
		err := k8sClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm)
		assert.True(t, apierrors.IsNotFound(err), "Vertex ADC ConfigMap should not exist for non-Vertex credentials")
	})

	t.Run("creates ADC ConfigMap for mixed Vertex + openai credentials", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "vertex-cred",
						Type:     clawv1alpha1.CredentialTypeGCP,
						Provider: "anthropic",
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: aiModelSecret, Key: aiModelSecretKey},
						},
						Domain: ".googleapis.com",
						GCP: &clawv1alpha1.GCPConfig{
							Project:  "test-project",
							Location: "us-central1",
						},
					},
					{
						Name:     "openai",
						Type:     clawv1alpha1.CredentialTypeBearer,
						Provider: "openai",
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: aiModelSecret, Key: aiModelSecretKey},
						},
						Domain: "api.openai.com",
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		require.NoError(t, reconciler.applyVertexADCConfigMap(ctx, instance, toResolved(instance.Spec.Credentials)))

		cm := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, client.ObjectKey{
			Name: getVertexADCConfigMapName(testInstanceName), Namespace: namespace,
		}, cm)
		require.NoError(t, err, "Vertex ADC ConfigMap should exist when Vertex is present among mixed providers")
		assert.Contains(t, cm.Data["google-auth-proxy-stub.js"], "isVertexProxyADCClient")
	})

	t.Run("cleans up orphaned ConfigMap when creds change to non-Vertex", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: []clawv1alpha1.CredentialSpec{
					{
						Name:     "vertex-cred",
						Type:     clawv1alpha1.CredentialTypeGCP,
						Provider: "anthropic",
						SecretRef: []clawv1alpha1.SecretRefEntry{
							{Name: aiModelSecret, Key: aiModelSecretKey},
						},
						Domain: ".googleapis.com",
						GCP: &clawv1alpha1.GCPConfig{
							Project:  "test-project",
							Location: "us-central1",
						},
					},
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		vertexCreds := toResolved(instance.Spec.Credentials)

		require.NoError(t, reconciler.applyVertexADCConfigMap(ctx, instance, vertexCreds))

		cmName := getVertexADCConfigMapName(testInstanceName)
		cm := &corev1.ConfigMap{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm),
			"ADC ConfigMap should exist after first apply")

		nonVertexCreds := toResolved(testCredentials())
		require.NoError(t, reconciler.applyVertexADCConfigMap(ctx, instance, nonVertexCreds))

		err := k8sClient.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm)
		assert.True(t, apierrors.IsNotFound(err), "ADC ConfigMap should be deleted when switching to non-Vertex creds")
	})
}

// --- enableServiceLinks tests ---

func TestEnableServiceLinks(t *testing.T) {
	t.Run("all deployment manifests should have enableServiceLinks false", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		ctx := context.Background()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials: testCredentials(),
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		for _, name := range []string{
			getClawDeploymentName(testInstanceName),
			getProxyDeploymentName(testInstanceName),
		} {
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment) == nil
			}, name+" should be created")

			require.NotNil(t, deployment.Spec.Template.Spec.EnableServiceLinks,
				"%s should have enableServiceLinks set", name)
			assert.False(t, *deployment.Spec.Template.Spec.EnableServiceLinks,
				"%s should have enableServiceLinks=false", name)
		}
	})
}

// --- Service account tests ---

const testServiceAccountName = "my-sa"

func TestConfigureClawDeploymentServiceAccount(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"automountServiceAccountToken": false,
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

	t.Run("should set serviceAccountName and enable token mounting", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.ServiceAccountName = "my-cloud-sa"

		require.NoError(t, configureClawDeploymentServiceAccount(objects, instance))

		saName, found, err := unstructured.NestedString(
			objects[0].Object, "spec", "template", "spec", "serviceAccountName")
		require.NoError(t, err)
		assert.True(t, found, "serviceAccountName should be set")
		assert.Equal(t, "my-cloud-sa", saName)

		autoMount, found, err := unstructured.NestedBool(
			objects[0].Object, "spec", "template", "spec", "automountServiceAccountToken")
		require.NoError(t, err)
		assert.True(t, found, "automountServiceAccountToken should be set")
		assert.True(t, autoMount, "automountServiceAccountToken should be true")
	})

	t.Run("should be no-op when serviceAccountName is empty", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName

		require.NoError(t, configureClawDeploymentServiceAccount(objects, instance))

		_, found, _ := unstructured.NestedString(
			objects[0].Object, "spec", "template", "spec", "serviceAccountName")
		assert.False(t, found, "serviceAccountName should not be set")

		autoMount, _, _ := unstructured.NestedBool(
			objects[0].Object, "spec", "template", "spec", "automountServiceAccountToken")
		assert.False(t, autoMount, "automountServiceAccountToken should remain false")
	})

	t.Run("should return error when deployment is missing", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.ServiceAccountName = testServiceAccountName

		err := configureClawDeploymentServiceAccount(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gateway deployment")
		assert.Contains(t, err.Error(), "not found in manifests")
	})

	t.Run("should only modify gateway deployment, not proxy", func(t *testing.T) {
		gatewayDep := &unstructured.Unstructured{}
		gatewayDep.SetKind(DeploymentKind)
		gatewayDep.SetName(getClawDeploymentName(testInstanceName))
		gatewayDep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"automountServiceAccountToken": false,
					"containers": []any{
						map[string]any{"name": ClawGatewayContainerName},
					},
				},
			},
		}

		proxyDep := &unstructured.Unstructured{}
		proxyDep.SetKind(DeploymentKind)
		proxyDep.SetName(getProxyDeploymentName(testInstanceName))
		proxyDep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"automountServiceAccountToken": false,
					"containers": []any{
						map[string]any{"name": "proxy"},
					},
				},
			},
		}

		objects := []*unstructured.Unstructured{gatewayDep, proxyDep}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Spec.ServiceAccountName = testServiceAccountName

		require.NoError(t, configureClawDeploymentServiceAccount(objects, instance))

		gatewaySA, found, err := unstructured.NestedString(
			gatewayDep.Object, "spec", "template", "spec", "serviceAccountName")
		require.NoError(t, err)
		assert.True(t, found, "gateway should have serviceAccountName set")
		assert.Equal(t, testServiceAccountName, gatewaySA)

		gatewayAutoMount, found, err := unstructured.NestedBool(
			gatewayDep.Object, "spec", "template", "spec", "automountServiceAccountToken")
		require.NoError(t, err)
		assert.True(t, found, "gateway should have automountServiceAccountToken set")
		assert.True(t, gatewayAutoMount, "gateway automountServiceAccountToken should be true")

		proxySA, found, _ := unstructured.NestedString(
			proxyDep.Object, "spec", "template", "spec", "serviceAccountName")
		assert.False(t, found, "proxy should not have serviceAccountName set")
		assert.Empty(t, proxySA)

		proxyAutoMount, _, _ := unstructured.NestedBool(
			proxyDep.Object, "spec", "template", "spec", "automountServiceAccountToken")
		assert.False(t, proxyAutoMount, "proxy automountServiceAccountToken should remain false")
	})
}

func TestServiceAccountIntegration(t *testing.T) {
	t.Run("should set serviceAccountName on gateway pod when specified", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials:        testCredentials(),
				ServiceAccountName: "workload-identity-sa",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
			}, deployment) == nil
		}, "gateway deployment should be created")

		assert.Equal(t, "workload-identity-sa", deployment.Spec.Template.Spec.ServiceAccountName,
			"gateway pod should use the custom SA")
		require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken,
			"automountServiceAccountToken should be set")
		assert.True(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken,
			"automountServiceAccountToken should be true when SA is specified")
	})

	t.Run("should not set serviceAccountName when omitted", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
			}, deployment) == nil
		}, "gateway deployment should be created")

		// The API server defaults serviceAccountName to "default" — assert.Empty would fail.
		assert.NotEqual(t, "workload-identity-sa", deployment.Spec.Template.Spec.ServiceAccountName,
			"gateway pod should not have a custom SA when omitted")
		require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken,
			"automountServiceAccountToken should be set")
		assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken,
			"automountServiceAccountToken should be false when SA is not specified")
	})

	t.Run("proxy pod should not be affected by serviceAccountName", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials:        testCredentials(),
				ServiceAccountName: "workload-identity-sa",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		proxyDeploy := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: getProxyDeploymentName(testInstanceName), Namespace: namespace,
			}, proxyDeploy) == nil
		}, "proxy deployment should be created")

		assert.NotEqual(t, "workload-identity-sa", proxyDeploy.Spec.Template.Spec.ServiceAccountName,
			"proxy pod should not have the gateway's custom SA")
		require.NotNil(t, proxyDeploy.Spec.Template.Spec.AutomountServiceAccountToken,
			"proxy automountServiceAccountToken should be set")
		assert.False(t, *proxyDeploy.Spec.Template.Spec.AutomountServiceAccountToken,
			"proxy automountServiceAccountToken should remain false")
	})

	t.Run("should update deployment when serviceAccountName changes", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Credentials:        testCredentials(),
				ServiceAccountName: "first-sa",
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
			}, deployment) == nil
		}, "gateway deployment should be created")

		assert.Equal(t, "first-sa", deployment.Spec.Template.Spec.ServiceAccountName)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		instance.Spec.ServiceAccountName = "second-sa"
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
		}, deployment))
		assert.Equal(t, "second-sa", deployment.Spec.Template.Spec.ServiceAccountName,
			"gateway should use updated SA after reconcile")

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		instance.Spec.ServiceAccountName = ""
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: getClawDeploymentName(testInstanceName), Namespace: namespace,
		}, deployment))
		assert.NotEqual(t, "second-sa", deployment.Spec.Template.Spec.ServiceAccountName,
			"gateway should revert SA after clearing the field")
		require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
		assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken,
			"automountServiceAccountToken should revert to false")
	})
}

// --- Image field tests ---

func TestOpenClawImage(t *testing.T) {
	t.Run("should default image when omitted", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		assert.Equal(t, DefaultOpenClawImage, instance.Spec.Image)
	})

	t.Run("should propagate custom image to all gateway containers", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Image = DefaultOpenClawImage
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

		gatewayContainerNames := []string{"init-volume", ClawInitConfigContainerName, ClawGatewayContainerName}
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			for _, name := range gatewayContainerNames {
				if ic.Name == name {
					assert.Equal(t, DefaultOpenClawImage, ic.Image,
						"init container %s should use custom image", name)
				}
			}
		}
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == ClawGatewayContainerName {
				assert.Equal(t, DefaultOpenClawImage, c.Image, "gateway container should use custom image")
			}
		}
	})

	t.Run("should set status.image to custom image", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Image = DefaultOpenClawImage
		instance.Spec.Credentials = testCredentials()
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		assert.Equal(t, DefaultOpenClawImage, instance.Status.Image)
	})

	t.Run("should set status.image to default when spec.image is omitted", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: testInstanceName, Namespace: namespace,
		}, instance))
		assert.Equal(t, DefaultOpenClawImage, instance.Status.Image)
	})
}

// --- Gateway resource override tests ---

func TestConfigureGatewayResources(t *testing.T) {
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
							"resources": map[string]any{
								"requests": map[string]any{"memory": "768Mi", "cpu": "100m"},
								"limits":   map[string]any{"memory": "4Gi", "cpu": "2"},
							},
						},
					},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	gatewayResources := func(objects []*unstructured.Unstructured) map[string]any {
		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		res, _, _ := unstructured.NestedMap(containers[0].(map[string]any), "resources")
		return res
	}

	t.Run("no override leaves manifest defaults untouched", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{ObjectMeta: metav1.ObjectMeta{Name: testInstanceName}}
		require.NoError(t, configureGatewayResources(objects, instance))
		res := gatewayResources(objects)
		assert.Equal(t, "4Gi", res["limits"].(map[string]any)["memory"])
		assert.Equal(t, "768Mi", res["requests"].(map[string]any)["memory"])
	})

	t.Run("memory limit override merges per-key and keeps defaults", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Resources: &clawv1alpha1.GatewayResourcesSpec{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
				},
			},
		}
		require.NoError(t, configureGatewayResources(objects, instance))
		res := gatewayResources(objects)
		limits := res["limits"].(map[string]any)
		requests := res["requests"].(map[string]any)
		assert.Equal(t, "8Gi", limits["memory"], "memory limit raised")
		assert.Equal(t, "2", limits["cpu"], "cpu limit keeps its default")
		assert.Equal(t, "768Mi", requests["memory"], "requests untouched")
		assert.Equal(t, "100m", requests["cpu"], "requests untouched")
	})

	t.Run("requests and limits can both be overridden", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Resources: &clawv1alpha1.GatewayResourcesSpec{
					Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
				},
			},
		}
		require.NoError(t, configureGatewayResources(objects, instance))
		res := gatewayResources(objects)
		assert.Equal(t, "2Gi", res["requests"].(map[string]any)["memory"])
		assert.Equal(t, "8Gi", res["limits"].(map[string]any)["memory"])
	})

	t.Run("override with no matching deployment errors", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
			Spec: clawv1alpha1.ClawSpec{
				Resources: &clawv1alpha1.GatewayResourcesSpec{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
				},
			},
		}
		err := configureGatewayResources([]*unstructured.Unstructured{}, instance)
		require.Error(t, err, "a requested override that lands nowhere must not report success")
		assert.Contains(t, err.Error(), "not found in manifests")
	})
}
