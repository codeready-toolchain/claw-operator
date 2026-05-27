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
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	DefaultOTelCollectorImage  = "mirror.gcr.io/otel/opentelemetry-collector:0.152.1"
	DefaultMetricsPort         = int32(9464)
	OTelCollectorContainerName = "otel-collector"
	ServiceMonitorKind         = "ServiceMonitor"
)

// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete

func getServiceMonitorName(instanceName string) string {
	return instanceName + "-metrics"
}

func metricsPort(instance *clawv1alpha1.Claw) int32 {
	if instance.Spec.Metrics != nil && instance.Spec.Metrics.Port != nil {
		return *instance.Spec.Metrics.Port
	}
	return DefaultMetricsPort
}

func metricsEnabled(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.Metrics != nil && instance.Spec.Metrics.Enabled
}

func serviceMonitorEnabled(instance *clawv1alpha1.Claw) bool {
	if !metricsEnabled(instance) {
		return false
	}
	if instance.Spec.Metrics.ServiceMonitor != nil && instance.Spec.Metrics.ServiceMonitor.Enabled != nil {
		return *instance.Spec.Metrics.ServiceMonitor.Enabled
	}
	return true
}

func serviceMonitorInterval(instance *clawv1alpha1.Claw) string {
	if instance.Spec.Metrics != nil && instance.Spec.Metrics.ServiceMonitor != nil &&
		instance.Spec.Metrics.ServiceMonitor.Interval != "" {
		return instance.Spec.Metrics.ServiceMonitor.Interval
	}
	return "30s"
}

// configureMetricsSidecar adds the OTel Collector sidecar container to the gateway
// Deployment when metrics are enabled.
func configureMetricsSidecar(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	otelImage string,
) error {
	if otelImage == "" {
		otelImage = DefaultOTelCollectorImage
	}

	port := metricsPort(instance)
	gatewayName := getClawDeploymentName(instance.Name)

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from claw deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in claw deployment")
		}

		sidecar := map[string]any{
			"name":  OTelCollectorContainerName,
			"image": otelImage,
			"args":  []any{"--config=/etc/otel-collector/config.yaml"},
			"ports": []any{
				map[string]any{
					"name":          "metrics",
					"containerPort": int64(port),
					"protocol":      "TCP",
				},
			},
			"resources": map[string]any{
				"requests": map[string]any{"memory": "32Mi", "cpu": "10m"},
				"limits":   map[string]any{"memory": "128Mi", "cpu": "100m"},
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false,
				"readOnlyRootFilesystem":   true,
				"runAsNonRoot":             true,
				"capabilities":             map[string]any{"drop": []any{"ALL"}},
			},
			"volumeMounts": []any{
				map[string]any{
					"name":      "config",
					"mountPath": "/etc/otel-collector/config.yaml",
					"subPath":   "otel-collector.yaml",
					"readOnly":  true,
				},
			},
		}

		containers = append(containers, sidecar)
		if err := unstructured.SetNestedSlice(
			obj.Object, containers, "spec", "template", "spec", "containers",
		); err != nil {
			return fmt.Errorf("failed to set containers on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// injectOTelCollectorConfig adds the otel-collector.yaml key to the gateway ConfigMap.
func injectOTelCollectorConfig(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	port := metricsPort(instance)
	configMapName := getConfigMapName(instance.Name)

	collectorConfig := `receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:4318
exporters:
  prometheus:
    endpoint: 0.0.0.0:` + strconv.Itoa(int(port)) + `
service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: [prometheus]
`

	for _, obj := range objects {
		if obj.GetKind() != ConfigMapKind || obj.GetName() != configMapName {
			continue
		}
		if err := unstructured.SetNestedField(
			obj.Object, collectorConfig, "data", "otel-collector.yaml",
		); err != nil {
			return fmt.Errorf("failed to set otel-collector.yaml in ConfigMap: %w", err)
		}
		return nil
	}
	return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
}

// injectMetricsConfig injects diagnostics.otel.metrics and endpoint into the
// operator.json config map. Only sets if user hasn't already configured diagnostics.otel.
func injectMetricsConfig(config map[string]any, instance *clawv1alpha1.Claw) {
	if !metricsEnabled(instance) {
		return
	}

	diagnostics, _ := config["diagnostics"].(map[string]any)
	if diagnostics == nil {
		diagnostics = map[string]any{}
		config["diagnostics"] = diagnostics
	}

	if _, exists := diagnostics["otel"]; exists {
		return
	}

	diagnostics["otel"] = map[string]any{
		"metrics":  true,
		"endpoint": "http://localhost:4318",
	}
}

// addMetricsPortToService appends the metrics port to the gateway Service.
func addMetricsPortToService(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	port := metricsPort(instance)
	serviceName := getServiceName(instance.Name)

	for _, obj := range objects {
		if obj.GetKind() != ServiceKind || obj.GetName() != serviceName {
			continue
		}

		ports, found, err := unstructured.NestedSlice(obj.Object, "spec", "ports")
		if err != nil {
			return fmt.Errorf("failed to get ports from Service: %w", err)
		}
		if !found {
			ports = []any{}
		}

		ports = append(ports, map[string]any{
			"name":       "metrics",
			"port":       int64(port),
			"targetPort": int64(port),
			"protocol":   "TCP",
		})

		if err := unstructured.SetNestedSlice(obj.Object, ports, "spec", "ports"); err != nil {
			return fmt.Errorf("failed to set ports on Service: %w", err)
		}
		return nil
	}
	return fmt.Errorf("service %q not found in manifests", serviceName)
}

// addMetricsIngressRule appends an ingress rule to the gateway NetworkPolicy
// allowing Prometheus scraping from the monitoring namespace.
func addMetricsIngressRule(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	port := metricsPort(instance)
	npName := getIngressNetworkPolicyName(instance.Name)

	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}

		ingress, found, err := unstructured.NestedSlice(obj.Object, "spec", "ingress")
		if err != nil {
			return fmt.Errorf("failed to get ingress rules from NetworkPolicy: %w", err)
		}
		if !found {
			ingress = []any{}
		}

		metricsRule := map[string]any{
			"from": []any{
				map[string]any{
					"namespaceSelector": map[string]any{
						"matchLabels": map[string]any{
							"network.openshift.io/policy-group": "monitoring",
						},
					},
				},
			},
			"ports": []any{
				map[string]any{
					"port":     int64(port),
					"protocol": "TCP",
				},
			},
		}

		ingress = append(ingress, metricsRule)
		if err := unstructured.SetNestedSlice(obj.Object, ingress, "spec", "ingress"); err != nil {
			return fmt.Errorf("failed to set ingress rules on NetworkPolicy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

// reconcileServiceMonitor creates or deletes the ServiceMonitor for this Claw instance.
func (r *ClawResourceReconciler) reconcileServiceMonitor(ctx context.Context, instance *clawv1alpha1.Claw) error {
	logger := log.FromContext(ctx)
	smName := getServiceMonitorName(instance.Name)

	if !serviceMonitorEnabled(instance) {
		return r.deleteServiceMonitorIfExists(ctx, instance.Namespace, smName)
	}

	port := metricsPort(instance)
	interval := serviceMonitorInterval(instance)

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    ServiceMonitorKind,
	})
	sm.SetName(smName)
	sm.SetNamespace(instance.Namespace)
	sm.SetLabels(map[string]string{
		"app.kubernetes.io/name":           "claw",
		"claw.sandbox.redhat.com/instance": sanitizeLabelValue(instance.Name),
	})

	sm.Object["spec"] = map[string]any{
		"selector": map[string]any{
			"matchLabels": map[string]any{
				"app":                              "claw",
				"claw.sandbox.redhat.com/instance": sanitizeLabelValue(instance.Name),
			},
		},
		"endpoints": []any{
			map[string]any{
				"port":     "metrics",
				"interval": interval,
				"path":     "/metrics",
			},
		},
		"namespaceSelector": map[string]any{
			"matchNames": []any{instance.Namespace},
		},
		"targetLabels": []any{
			"claw.sandbox.redhat.com/instance",
		},
	}

	if err := controllerutil.SetControllerReference(instance, sm, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on ServiceMonitor: %w", err)
	}

	if err := r.Patch(ctx, sm, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	}); err != nil {
		if isNoMatchErr(err) {
			logger.Info("ServiceMonitor CRD not registered, skipping metrics ServiceMonitor",
				"port", port)
			return nil
		}
		return fmt.Errorf("failed to apply ServiceMonitor: %w", err)
	}

	logger.Info("Applied ServiceMonitor", "name", smName)
	return nil
}

func (r *ClawResourceReconciler) deleteServiceMonitorIfExists(ctx context.Context, namespace, name string) error {
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    ServiceMonitorKind,
	})
	sm.SetName(name)
	sm.SetNamespace(namespace)

	if err := r.Delete(ctx, sm); err != nil {
		if apierrors.IsNotFound(err) || isNoMatchErr(err) {
			return nil
		}
		return fmt.Errorf("failed to delete ServiceMonitor %q: %w", name, err)
	}
	log.FromContext(ctx).Info("Deleted ServiceMonitor", "name", name)
	return nil
}

func isNoMatchErr(err error) bool {
	return meta.IsNoMatchError(err)
}
