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
			"operator-gateway.json": operatorJSON,
			"openclaw.json":         `{"gateway":{"$include":"./operator-gateway.json"}}`,
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
		cm.Object["data"] = map[string]any{"operator-gateway.json": "{}"}

		err := stampGatewayConfigHash([]*unstructured.Unstructured{cm}, testInstanceName)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found for config hash stamping")
	})
}
