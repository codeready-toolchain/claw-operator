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
	netv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func makeTestDeploymentForMetrics() []*unstructured.Unstructured {
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
				"volumes": []any{
					map[string]any{
						"name": "config",
						"configMap": map[string]any{
							"name": getConfigMapName(testInstanceName),
						},
					},
				},
			},
		},
	}
	return []*unstructured.Unstructured{dep}
}

func makeTestConfigMapForMetrics() *unstructured.Unstructured {
	cm := &unstructured.Unstructured{}
	cm.SetKind(ConfigMapKind)
	cm.SetName(getConfigMapName(testInstanceName))
	cm.Object["data"] = map[string]any{
		"operator.json": `{"gateway":{}}`,
	}
	return cm
}

func makeTestServiceForMetrics() *unstructured.Unstructured {
	svc := &unstructured.Unstructured{}
	svc.SetKind(ServiceKind)
	svc.SetName(getServiceName(testInstanceName))
	svc.Object["spec"] = map[string]any{
		"ports": []any{
			map[string]any{
				"name":       "gateway",
				"port":       int64(18789),
				"targetPort": int64(18789),
				"protocol":   "TCP",
			},
		},
	}
	return svc
}

func makeTestNetworkPolicyForMetrics() *unstructured.Unstructured {
	np := &unstructured.Unstructured{}
	np.SetKind(NetworkPolicyKind)
	np.SetName(getIngressNetworkPolicyName(testInstanceName))
	np.Object["spec"] = map[string]any{
		"podSelector": map[string]any{
			"matchLabels": map[string]any{"app": "claw"},
		},
		"policyTypes": []any{"Ingress"},
		"ingress": []any{
			map[string]any{
				"from": []any{
					map[string]any{
						"namespaceSelector": map[string]any{
							"matchLabels": map[string]any{
								"policy-group.network.openshift.io/ingress": "",
							},
						},
					},
				},
				"ports": []any{
					map[string]any{"port": int64(18789), "protocol": "TCP"},
				},
			},
		},
	}
	return np
}

func testClawWithMetrics(enabled bool, port *int32) *clawv1alpha1.Claw {
	instance := &clawv1alpha1.Claw{}
	instance.Name = testInstanceName
	instance.Namespace = namespace
	if enabled {
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{
			Enabled: true,
			Port:    port,
		}
	}
	return instance
}

// --- configureMetricsSidecar tests ---

func TestConfigureMetricsSidecar(t *testing.T) {
	t.Run("should add otel-collector sidecar with default image and port", func(t *testing.T) {
		objects := makeTestDeploymentForMetrics()
		instance := testClawWithMetrics(true, nil)

		require.NoError(t, configureMetricsSidecar(objects, instance, ""))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		require.Len(t, containers, 2)

		sidecar := containers[1].(map[string]any)
		assert.Equal(t, OTelCollectorContainerName, sidecar["name"])
		assert.Equal(t, DefaultOTelCollectorImage, sidecar["image"])

		args := sidecar["args"].([]any)
		assert.Equal(t, "--config=/etc/otel-collector/config.yaml", args[0])

		ports := sidecar["ports"].([]any)
		require.Len(t, ports, 1)
		portEntry := ports[0].(map[string]any)
		assert.Equal(t, "metrics", portEntry["name"])
		assert.Equal(t, int64(DefaultMetricsPort), portEntry["containerPort"])

		secCtx := sidecar["securityContext"].(map[string]any)
		assert.Equal(t, false, secCtx["allowPrivilegeEscalation"])
		assert.Equal(t, true, secCtx["readOnlyRootFilesystem"])
		assert.Equal(t, true, secCtx["runAsNonRoot"])

		volumeMounts := sidecar["volumeMounts"].([]any)
		require.Len(t, volumeMounts, 1)
		vm := volumeMounts[0].(map[string]any)
		assert.Equal(t, "config", vm["name"])
		assert.Equal(t, "/etc/otel-collector/config.yaml", vm["mountPath"])
		assert.Equal(t, "otel-collector.yaml", vm["subPath"])
		assert.Equal(t, true, vm["readOnly"])
	})

	t.Run("should use custom image when provided", func(t *testing.T) {
		objects := makeTestDeploymentForMetrics()
		instance := testClawWithMetrics(true, nil)
		customImage := "my-registry/otel-collector:custom"

		require.NoError(t, configureMetricsSidecar(objects, instance, customImage))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		sidecar := containers[1].(map[string]any)
		assert.Equal(t, customImage, sidecar["image"])
	})

	t.Run("should use custom port when specified", func(t *testing.T) {
		objects := makeTestDeploymentForMetrics()
		customPort := int32(8888)
		instance := testClawWithMetrics(true, &customPort)

		require.NoError(t, configureMetricsSidecar(objects, instance, ""))

		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		sidecar := containers[1].(map[string]any)
		ports := sidecar["ports"].([]any)
		portEntry := ports[0].(map[string]any)
		assert.Equal(t, int64(customPort), portEntry["containerPort"])
	})

	t.Run("should return error when deployment not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithMetrics(true, nil)

		err := configureMetricsSidecar(objects, instance, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})
}

// --- injectOTelCollectorConfig tests ---

func TestInjectOTelCollectorConfig(t *testing.T) {
	t.Run("should add otel-collector.yaml to ConfigMap with default port", func(t *testing.T) {
		cm := makeTestConfigMapForMetrics()
		objects := []*unstructured.Unstructured{cm}
		instance := testClawWithMetrics(true, nil)

		require.NoError(t, injectOTelCollectorConfig(objects, instance))

		collectorYAML, found, err := unstructured.NestedString(cm.Object, "data", "otel-collector.yaml")
		require.NoError(t, err)
		require.True(t, found)
		assert.Contains(t, collectorYAML, "127.0.0.1:4318")
		assert.Contains(t, collectorYAML, "0.0.0.0:9464")
		assert.Contains(t, collectorYAML, "receivers: [otlp]")
		assert.Contains(t, collectorYAML, "exporters: [prometheus]")
	})

	t.Run("should use custom port in prometheus exporter endpoint", func(t *testing.T) {
		cm := makeTestConfigMapForMetrics()
		objects := []*unstructured.Unstructured{cm}
		customPort := int32(8888)
		instance := testClawWithMetrics(true, &customPort)

		require.NoError(t, injectOTelCollectorConfig(objects, instance))

		collectorYAML, _, _ := unstructured.NestedString(cm.Object, "data", "otel-collector.yaml")
		assert.Contains(t, collectorYAML, "0.0.0.0:8888")
		assert.NotContains(t, collectorYAML, "0.0.0.0:9464")
	})

	t.Run("should return error when ConfigMap not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithMetrics(true, nil)

		err := injectOTelCollectorConfig(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})
}

// --- injectMetricsConfig tests ---

func TestInjectMetricsConfig(t *testing.T) {
	t.Run("should inject diagnostics.otel when metrics enabled", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMetrics(true, nil)

		injectMetricsConfig(config, instance)

		diagnostics, ok := config["diagnostics"].(map[string]any)
		require.True(t, ok)
		otel, ok := diagnostics["otel"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, otel["metrics"])
		assert.Equal(t, "http://localhost:4318", otel["endpoint"])
	})

	t.Run("should deep-merge sidecar keys into user-configured diagnostics.otel", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{},
			"diagnostics": map[string]any{
				"otel": map[string]any{
					"enabled":  true,
					"endpoint": "http://langfuse.svc:3000/api/public/otel/v1/traces",
					"traces":   true,
				},
			},
		}
		instance := testClawWithMetrics(true, nil)

		injectMetricsConfig(config, instance)

		diagnostics := config["diagnostics"].(map[string]any)
		otel := diagnostics["otel"].(map[string]any)
		assert.Equal(t, "http://langfuse.svc:3000/api/public/otel/v1/traces", otel["endpoint"],
			"user endpoint should be preserved")
		assert.Equal(t, true, otel["traces"], "user traces should be preserved")
		assert.Equal(t, true, otel["enabled"], "user enabled should be preserved")
		assert.Equal(t, true, otel["metrics"], "metrics should be injected")
		assert.Equal(t, "http://localhost:4318", otel["metricsEndpoint"],
			"metricsEndpoint should be injected for sidecar")
	})

	t.Run("should not override user-set metricsEndpoint", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{},
			"diagnostics": map[string]any{
				"otel": map[string]any{
					"enabled":         true,
					"metricsEndpoint": "http://custom-collector:4318",
				},
			},
		}
		instance := testClawWithMetrics(true, nil)

		injectMetricsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, "http://custom-collector:4318", otel["metricsEndpoint"],
			"user metricsEndpoint should not be overridden")
		assert.Equal(t, true, otel["metrics"], "metrics should be injected when absent")
	})

	t.Run("should not override user-set metrics false", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{},
			"diagnostics": map[string]any{
				"otel": map[string]any{
					"enabled": true,
					"metrics": false,
				},
			},
		}
		instance := testClawWithMetrics(true, nil)

		injectMetricsConfig(config, instance)

		otel := config["diagnostics"].(map[string]any)["otel"].(map[string]any)
		assert.Equal(t, false, otel["metrics"],
			"user metrics:false should be respected")
		assert.Equal(t, "http://localhost:4318", otel["metricsEndpoint"],
			"metricsEndpoint should still be injected")
	})

	t.Run("should be no-op when metrics not enabled", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := testClawWithMetrics(false, nil)

		injectMetricsConfig(config, instance)

		_, hasDiagnostics := config["diagnostics"]
		assert.False(t, hasDiagnostics)
	})

	t.Run("should be no-op when metrics spec is nil", func(t *testing.T) {
		config := map[string]any{"gateway": map[string]any{}}
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName

		injectMetricsConfig(config, instance)

		_, hasDiagnostics := config["diagnostics"]
		assert.False(t, hasDiagnostics)
	})
}

// --- addMetricsPortToService tests ---

func TestAddMetricsPortToService(t *testing.T) {
	t.Run("should add metrics port to Service with default port", func(t *testing.T) {
		svc := makeTestServiceForMetrics()
		objects := []*unstructured.Unstructured{svc}
		instance := testClawWithMetrics(true, nil)

		require.NoError(t, addMetricsPortToService(objects, instance))

		ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
		require.Len(t, ports, 2)

		metricsPort := ports[1].(map[string]any)
		assert.Equal(t, "metrics", metricsPort["name"])
		assert.Equal(t, int64(DefaultMetricsPort), metricsPort["port"])
		assert.Equal(t, int64(DefaultMetricsPort), metricsPort["targetPort"])
		assert.Equal(t, "TCP", metricsPort["protocol"])
	})

	t.Run("should use custom port", func(t *testing.T) {
		svc := makeTestServiceForMetrics()
		objects := []*unstructured.Unstructured{svc}
		customPort := int32(8888)
		instance := testClawWithMetrics(true, &customPort)

		require.NoError(t, addMetricsPortToService(objects, instance))

		ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
		metricsPort := ports[1].(map[string]any)
		assert.Equal(t, int64(8888), metricsPort["port"])
		assert.Equal(t, int64(8888), metricsPort["targetPort"])
	})

	t.Run("should return error when Service not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithMetrics(true, nil)

		err := addMetricsPortToService(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})
}

// --- addMetricsIngressRule tests ---

func TestAddMetricsIngressRule(t *testing.T) {
	t.Run("should add monitoring ingress rule with default port", func(t *testing.T) {
		np := makeTestNetworkPolicyForMetrics()
		objects := []*unstructured.Unstructured{np}
		instance := testClawWithMetrics(true, nil)

		require.NoError(t, addMetricsIngressRule(objects, instance))

		ingress, _, _ := unstructured.NestedSlice(np.Object, "spec", "ingress")
		require.Len(t, ingress, 2)

		metricsRule := ingress[1].(map[string]any)

		from := metricsRule["from"].([]any)
		require.Len(t, from, 1)
		peer := from[0].(map[string]any)
		nsSelector := peer["namespaceSelector"].(map[string]any)
		matchLabels := nsSelector["matchLabels"].(map[string]any)
		assert.Equal(t, "monitoring", matchLabels["network.openshift.io/policy-group"])

		ports := metricsRule["ports"].([]any)
		require.Len(t, ports, 1)
		portEntry := ports[0].(map[string]any)
		assert.Equal(t, int64(DefaultMetricsPort), portEntry["port"])
		assert.Equal(t, "TCP", portEntry["protocol"])
	})

	t.Run("should use custom port in ingress rule", func(t *testing.T) {
		np := makeTestNetworkPolicyForMetrics()
		objects := []*unstructured.Unstructured{np}
		customPort := int32(8888)
		instance := testClawWithMetrics(true, &customPort)

		require.NoError(t, addMetricsIngressRule(objects, instance))

		ingress, _, _ := unstructured.NestedSlice(np.Object, "spec", "ingress")
		metricsRule := ingress[1].(map[string]any)
		ports := metricsRule["ports"].([]any)
		portEntry := ports[0].(map[string]any)
		assert.Equal(t, int64(8888), portEntry["port"])
	})

	t.Run("should return error when NetworkPolicy not found", func(t *testing.T) {
		objects := []*unstructured.Unstructured{}
		instance := testClawWithMetrics(true, nil)

		err := addMetricsIngressRule(objects, instance)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifests")
	})
}

// --- helper function tests ---

func TestMetricsHelpers(t *testing.T) {
	t.Run("metricsPort returns default when nil", func(t *testing.T) {
		instance := testClawWithMetrics(true, nil)
		assert.Equal(t, DefaultMetricsPort, metricsPort(instance))
	})

	t.Run("metricsPort returns custom value", func(t *testing.T) {
		customPort := int32(1234)
		instance := testClawWithMetrics(true, &customPort)
		assert.Equal(t, int32(1234), metricsPort(instance))
	})

	t.Run("metricsEnabled false when nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		assert.False(t, metricsEnabled(instance))
	})

	t.Run("metricsEnabled false when disabled", func(t *testing.T) {
		instance := testClawWithMetrics(false, nil)
		assert.False(t, metricsEnabled(instance))
	})

	t.Run("serviceMonitorEnabled defaults to true when metrics enabled", func(t *testing.T) {
		instance := testClawWithMetrics(true, nil)
		assert.True(t, serviceMonitorEnabled(instance))
	})

	t.Run("serviceMonitorEnabled respects explicit false", func(t *testing.T) {
		instance := testClawWithMetrics(true, nil)
		instance.Spec.Metrics.ServiceMonitor = &clawv1alpha1.ServiceMonitorSpec{
			Enabled: ptr.To(false),
		}
		assert.False(t, serviceMonitorEnabled(instance))
	})

	t.Run("serviceMonitorInterval returns custom value", func(t *testing.T) {
		instance := testClawWithMetrics(true, nil)
		instance.Spec.Metrics.ServiceMonitor = &clawv1alpha1.ServiceMonitorSpec{
			Interval: "15s",
		}
		assert.Equal(t, "15s", serviceMonitorInterval(instance))
	})

	t.Run("serviceMonitorInterval returns default", func(t *testing.T) {
		instance := testClawWithMetrics(true, nil)
		assert.Equal(t, "30s", serviceMonitorInterval(instance))
	})

	t.Run("getServiceMonitorName", func(t *testing.T) {
		assert.Equal(t, "my-claw-metrics", getServiceMonitorName("my-claw"))
	})
}

// --- Integration test: full metrics enrichment pipeline ---

func TestMetricsEnrichmentPipeline(t *testing.T) {
	t.Run("should enrich all resources when metrics enabled", func(t *testing.T) {
		dep := makeTestDeploymentForMetrics()
		cm := makeTestConfigMapForMetrics()
		svc := makeTestServiceForMetrics()
		np := makeTestNetworkPolicyForMetrics()

		objects := append(dep, cm, svc, np)
		instance := testClawWithMetrics(true, nil)

		require.NoError(t, configureMetricsSidecar(objects, instance, ""))
		require.NoError(t, injectOTelCollectorConfig(objects, instance))
		require.NoError(t, addMetricsPortToService(objects, instance))
		require.NoError(t, addMetricsIngressRule(objects, instance))

		config := map[string]any{"gateway": map[string]any{}}
		injectMetricsConfig(config, instance)

		// Verify deployment has sidecar
		containers, _, _ := unstructured.NestedSlice(
			objects[0].Object, "spec", "template", "spec", "containers",
		)
		assert.Len(t, containers, 2)

		// Verify ConfigMap has collector config
		collectorYAML, found, _ := unstructured.NestedString(cm.Object, "data", "otel-collector.yaml")
		assert.True(t, found)
		assert.Contains(t, collectorYAML, "otlp")

		// Verify Service has metrics port
		ports, _, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
		assert.Len(t, ports, 2)

		// Verify NetworkPolicy has monitoring rule
		ingress, _, _ := unstructured.NestedSlice(np.Object, "spec", "ingress")
		assert.Len(t, ingress, 2)

		// Verify operator.json config
		diagnostics := config["diagnostics"].(map[string]any)
		otel := diagnostics["otel"].(map[string]any)
		assert.Equal(t, true, otel["metrics"])
	})
}

// --- Metrics config merge test with JSON round-trip ---

func TestInjectMetricsConfigJSONRoundTrip(t *testing.T) {
	t.Run("should produce valid JSON after metrics injection", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"models": {"providers": {}}
		}`

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

		instance := testClawWithMetrics(true, nil)
		injectMetricsConfig(config, instance)

		result, err := json.Marshal(config)
		require.NoError(t, err)

		var roundTripped map[string]any
		require.NoError(t, json.Unmarshal(result, &roundTripped))

		diagnostics := roundTripped["diagnostics"].(map[string]any)
		otel := diagnostics["otel"].(map[string]any)
		assert.Equal(t, true, otel["metrics"])
		assert.Equal(t, "http://localhost:4318", otel["endpoint"])
	})

	t.Run("should produce valid JSON after deep-merge into user config", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"diagnostics": {"otel": {"enabled": true, "endpoint": "http://langfuse:3000/otel", "traces": true}}
		}`

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))

		instance := testClawWithMetrics(true, nil)
		injectMetricsConfig(config, instance)

		result, err := json.Marshal(config)
		require.NoError(t, err)

		var roundTripped map[string]any
		require.NoError(t, json.Unmarshal(result, &roundTripped))

		diagnostics := roundTripped["diagnostics"].(map[string]any)
		otel := diagnostics["otel"].(map[string]any)
		assert.Equal(t, true, otel["enabled"], "user enabled preserved")
		assert.Equal(t, "http://langfuse:3000/otel", otel["endpoint"], "user endpoint preserved")
		assert.Equal(t, true, otel["traces"], "user traces preserved")
		assert.Equal(t, true, otel["metrics"], "metrics injected")
		assert.Equal(t, "http://localhost:4318", otel["metricsEndpoint"], "metricsEndpoint injected")
	})
}

// --- Integration tests (envtest + reconcile) ---

func TestMetricsIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should inject metrics sidecar and config after reconciliation", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify Deployment has otel-collector sidecar
		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		var hasSidecar bool
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == OTelCollectorContainerName {
				hasSidecar = true
				assert.Equal(t, DefaultOTelCollectorImage, c.Image)
				require.Len(t, c.Ports, 1)
				assert.Equal(t, DefaultMetricsPort, c.Ports[0].ContainerPort)
				assert.Equal(t, "metrics", c.Ports[0].Name)
				break
			}
		}
		assert.True(t, hasSidecar, "Deployment should have otel-collector sidecar")

		// Verify ConfigMap has otel-collector.yaml
		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		collectorYAML, ok := cm.Data["otel-collector.yaml"]
		assert.True(t, ok, "ConfigMap should have otel-collector.yaml key")
		assert.Contains(t, collectorYAML, "127.0.0.1:4318")
		assert.Contains(t, collectorYAML, "0.0.0.0:9464")

		// Verify operator.json has diagnostics.otel.metrics
		operatorJSON, ok := cm.Data["operator.json"]
		require.True(t, ok)
		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(operatorJSON), &config))
		diagnostics, ok := config["diagnostics"].(map[string]any)
		require.True(t, ok, "operator.json should have diagnostics section")
		otel, ok := diagnostics["otel"].(map[string]any)
		require.True(t, ok, "diagnostics should have otel section")
		assert.Equal(t, true, otel["metrics"])
		assert.Equal(t, "http://localhost:4318", otel["endpoint"])

		// Verify Service has metrics port
		svc := &corev1.Service{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getServiceName(testInstanceName),
				Namespace: namespace,
			}, svc) == nil
		}, "Service should be created")

		var hasMetricsPort bool
		for _, p := range svc.Spec.Ports {
			if p.Name == "metrics" {
				hasMetricsPort = true
				assert.Equal(t, DefaultMetricsPort, p.Port)
				break
			}
		}
		assert.True(t, hasMetricsPort, "Service should have metrics port")

		// Verify NetworkPolicy has monitoring ingress rule
		np := &netv1.NetworkPolicy{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getIngressNetworkPolicyName(testInstanceName),
				Namespace: namespace,
			}, np) == nil
		}, "NetworkPolicy should be created")

		var hasMonitoringRule bool
		for _, rule := range np.Spec.Ingress {
			for _, from := range rule.From {
				if from.NamespaceSelector != nil {
					if val, ok := from.NamespaceSelector.MatchLabels["network.openshift.io/policy-group"]; ok && val == "monitoring" {
						hasMonitoringRule = true
						break
					}
				}
			}
		}
		assert.True(t, hasMonitoringRule, "NetworkPolicy should have monitoring ingress rule")
	})

	t.Run("should not inject metrics when disabled", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify Deployment does NOT have otel-collector sidecar
		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		for _, c := range deployment.Spec.Template.Spec.Containers {
			assert.NotEqual(t, OTelCollectorContainerName, c.Name,
				"Deployment should not have otel-collector sidecar when metrics disabled")
		}

		// Verify ConfigMap does NOT have otel-collector.yaml
		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		_, hasCollectorConfig := cm.Data["otel-collector.yaml"]
		assert.False(t, hasCollectorConfig, "ConfigMap should not have otel-collector.yaml when metrics disabled")

		// Verify Service does NOT have metrics port
		svc := &corev1.Service{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getServiceName(testInstanceName),
				Namespace: namespace,
			}, svc) == nil
		}, "Service should be created")

		for _, p := range svc.Spec.Ports {
			assert.NotEqual(t, "metrics", p.Name,
				"Service should not have metrics port when metrics disabled")
		}
	})

	t.Run("should gracefully handle missing ServiceMonitor CRD", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
	})

	t.Run("should use custom metrics port", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		customPort := int32(8888)
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{
			Enabled: true,
			Port:    &customPort,
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify sidecar has custom port
		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == OTelCollectorContainerName {
				require.Len(t, c.Ports, 1)
				assert.Equal(t, int32(8888), c.Ports[0].ContainerPort)
				break
			}
		}

		// Verify Service has custom port
		svc := &corev1.Service{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getServiceName(testInstanceName),
				Namespace: namespace,
			}, svc) == nil
		}, "Service should be created")

		for _, p := range svc.Spec.Ports {
			if p.Name == "metrics" {
				assert.Equal(t, int32(8888), p.Port)
				break
			}
		}

		// Verify collector config has custom port
		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		collectorYAML := cm.Data["otel-collector.yaml"]
		assert.Contains(t, collectorYAML, "0.0.0.0:8888")
		assert.NotContains(t, collectorYAML, "0.0.0.0:9464")
	})

	t.Run("should use custom OTelCollectorImage from reconciler field", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}
		require.NoError(t, k8sClient.Create(ctx, instance))

		customImage := "my-registry.io/otel/collector:v1.0.0"
		reconciler := &ClawResourceReconciler{
			Client:             k8sClient,
			Scheme:             k8sClient.Scheme(),
			UserSecretReader:   k8sClient,
			OTelCollectorImage: customImage,
		}
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "Deployment should be created")

		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == OTelCollectorContainerName {
				assert.Equal(t, customImage, c.Image)
				return
			}
		}
		t.Fatal("otel-collector sidecar not found in deployment")
	})

	t.Run("should deep-merge sidecar keys into user diagnostics.otel from spec.config.raw", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}
		instance.Spec.Config = &clawv1alpha1.ConfigSpec{
			Raw: &clawv1alpha1.RawConfig{
				RawExtension: runtime.RawExtension{
					Raw: []byte(`{"diagnostics":{"otel":{"enabled":true,"endpoint":"http://langfuse.svc:3000/api/public/otel/v1/traces","traces":true}}}`),
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

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		diagnostics, ok := config["diagnostics"].(map[string]any)
		require.True(t, ok)
		otel, ok := diagnostics["otel"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "http://langfuse.svc:3000/api/public/otel/v1/traces", otel["endpoint"],
			"user-configured endpoint should be preserved")
		assert.Equal(t, true, otel["traces"],
			"user-configured traces should be preserved")
		assert.Equal(t, true, otel["metrics"],
			"operator should inject metrics: true")
		assert.Equal(t, "http://localhost:4318", otel["metricsEndpoint"],
			"operator should inject metricsEndpoint for sidecar")
	})

	// Must be last: installing/deleting a CRD at runtime invalidates the discovery cache
	t.Run("should create ServiceMonitor when CRD exists", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
			crdToDelete := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "servicemonitors.monitoring.coreos.com",
				},
			}
			_ = k8sClient.Delete(ctx, crdToDelete)
			waitFor(t, timeout*3, interval, func() bool {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "servicemonitors.monitoring.coreos.com"}, crd)
				return apierrors.IsNotFound(err)
			}, "ServiceMonitor CRD should be deleted")
		})

		smCRD := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "servicemonitors.monitoring.coreos.com",
			},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: "monitoring.coreos.com",
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Plural:   "servicemonitors",
					Singular: "servicemonitor",
					Kind:     "ServiceMonitor",
					ListKind: "ServiceMonitorList",
				},
				Scope: apiextensionsv1.NamespaceScoped,
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
					{
						Name:    "v1",
						Served:  true,
						Storage: true,
						Schema: &apiextensionsv1.CustomResourceValidation{
							OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"spec": {
										Type:                   "object",
										XPreserveUnknownFields: boolPtr(true),
									},
								},
							},
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, smCRD)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			require.NoError(t, err, "failed to create ServiceMonitor CRD")
		}

		waitFor(t, timeout, interval, func() bool {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "servicemonitors.monitoring.coreos.com"}, crd)
			if err != nil {
				return false
			}
			for _, cond := range crd.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return true
				}
			}
			return false
		}, "ServiceMonitor CRD should be established")

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Metrics = &clawv1alpha1.MetricsSpec{Enabled: true}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		sm := &unstructured.Unstructured{}
		sm.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})

		smName := getServiceMonitorName(testInstanceName)
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      smName,
				Namespace: namespace,
			}, sm) == nil
		}, "ServiceMonitor should be created")

		labels := sm.GetLabels()
		assert.Equal(t, "claw", labels["app.kubernetes.io/name"])
		assert.Equal(t, testInstanceName, labels["claw.sandbox.redhat.com/instance"])

		spec, _, _ := unstructured.NestedMap(sm.Object, "spec")
		selector, _, _ := unstructured.NestedMap(spec, "selector")
		matchLabels, _, _ := unstructured.NestedStringMap(selector, "matchLabels")
		assert.Equal(t, "claw", matchLabels["app"])
		assert.Equal(t, testInstanceName, matchLabels["claw.sandbox.redhat.com/instance"])

		endpoints, _, _ := unstructured.NestedSlice(spec, "endpoints")
		require.Len(t, endpoints, 1)
		ep := endpoints[0].(map[string]any)
		assert.Equal(t, "metrics", ep["port"])
		assert.Equal(t, "30s", ep["interval"])
		assert.Equal(t, "/metrics", ep["path"])
	})
}
