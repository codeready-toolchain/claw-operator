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

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- Vertex AI deployment configuration tests ---

func TestConfigureClawDeploymentForVertex(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(ClawDeploymentName)
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

		require.NoError(t, configureClawDeploymentForVertex(objects, toResolved(credentials)))

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
		assert.Equal(t, ClawVertexADCConfigMapName, cmRef["name"])
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

		require.NoError(t, configureClawDeploymentForVertex(objects, toResolved(credentials)))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Len(t, envVars, 1, "should only have original HOME env var")
	})
}
