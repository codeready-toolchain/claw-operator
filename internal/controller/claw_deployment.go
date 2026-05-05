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
	"crypto/sha256"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// configureImagePullPolicy overrides imagePullPolicy on all containers in all
// Deployment objects. If policy is empty, the embedded defaults are preserved.
func configureImagePullPolicy(objects []*unstructured.Unstructured, policy string) error {
	if policy == "" {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind {
			continue
		}

		for _, path := range [][]string{
			{"spec", "template", "spec", "containers"},
			{"spec", "template", "spec", "initContainers"},
		} {
			containers, found, err := unstructured.NestedSlice(obj.Object, path...)
			if err != nil {
				return fmt.Errorf("failed to get %s from %s: %w", path[len(path)-1], obj.GetName(), err)
			}
			if !found {
				continue
			}

			for i, c := range containers {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				cm["imagePullPolicy"] = policy
				containers[i] = cm
			}
			if err := unstructured.SetNestedSlice(obj.Object, containers, path...); err != nil {
				return fmt.Errorf("failed to set %s in %s: %w", path[len(path)-1], obj.GetName(), err)
			}
		}
	}
	return nil
}

// hasVertexSDKCredentials returns true if any credential uses the native Vertex SDK.
func hasVertexSDKCredentials(credentials []resolvedCredential) bool {
	for _, rc := range credentials {
		if usesVertexSDK(rc.CredentialSpec) {
			return true
		}
	}
	return false
}

// applyVertexADCConfigMap creates or updates the stub ADC ConfigMap used by the
// OpenClaw pod's Vertex SDK to bootstrap GCP token refresh. The stub contains
// dummy credentials — the MITM proxy replaces tokens with real ones.
func (r *ClawResourceReconciler) applyVertexADCConfigMap(ctx context.Context, instance *clawv1alpha1.Claw, resolvedCreds []resolvedCredential) error {
	configMapName := getVertexADCConfigMapName(instance.Name)
	if !hasVertexSDKCredentials(resolvedCreds) {
		existing := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: instance.Namespace}, existing); err == nil {
			log.FromContext(ctx).Info("Cleaning up orphaned Vertex ADC ConfigMap")
			return r.Delete(ctx, existing)
		}
		return nil
	}

	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	cm.SetName(configMapName)
	cm.SetNamespace(instance.Namespace)
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	cm.Data = map[string]string{
		"adc.json": `{"type":"authorized_user","client_id":"stub.apps.googleusercontent.com","client_secret":"stub","refresh_token":"proxy-managed-token"}`,
	}

	if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on vertex ADC config: %w", err)
	}

	if err := r.Patch(ctx, cm, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	}); err != nil {
		return fmt.Errorf("failed to apply vertex ADC config: %w", err)
	}

	logger.Info("Applied Vertex ADC ConfigMap")
	return nil
}

// configureClawDeploymentForVertex adds Vertex AI environment variables and the stub
// ADC volume mount to the claw (gateway) deployment when any credential uses the
// native Vertex SDK (GCP + non-Google provider). The stub ADC allows google-auth-library
// to bootstrap token refresh, which the MITM proxy intercepts with real credentials.
func configureClawDeploymentForVertex(objects []*unstructured.Unstructured, credentials []resolvedCredential, instanceName string) error {
	var vertexCreds []clawv1alpha1.CredentialSpec
	for _, rc := range credentials {
		if usesVertexSDK(rc.CredentialSpec) {
			vertexCreds = append(vertexCreds, rc.CredentialSpec)
		}
	}
	if len(vertexCreds) == 0 {
		return nil
	}

	gatewayName := getClawDeploymentName(instanceName)
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

		containerIdx := -1
		var container map[string]any
		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawGatewayContainerName {
				containerIdx = i
				container = cm
				break
			}
		}
		if containerIdx < 0 {
			return fmt.Errorf("container %q not found in claw deployment", ClawGatewayContainerName)
		}

		envVars, _, _ := unstructured.NestedSlice(container, "env")
		volumeMounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
		volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")

		envVars = append(envVars, map[string]any{
			"name":  "GOOGLE_APPLICATION_CREDENTIALS",
			"value": "/etc/vertex-adc/adc.json",
		})

		for _, cred := range vertexCreds {
			if cred.Provider == "anthropic" && cred.GCP != nil {
				envVars = append(envVars, map[string]any{
					"name":  "ANTHROPIC_VERTEX_PROJECT_ID",
					"value": cred.GCP.Project,
				})
				break
			}
		}

		volumeMounts = append(volumeMounts, map[string]any{
			"name":      "vertex-adc",
			"mountPath": "/etc/vertex-adc",
			"readOnly":  true,
		})
		volumes = append(volumes, map[string]any{
			"name": "vertex-adc",
			"configMap": map[string]any{
				"name": getVertexADCConfigMapName(instanceName),
			},
		})

		if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
			return fmt.Errorf("failed to set env vars on claw deployment: %w", err)
		}
		if err := unstructured.SetNestedSlice(container, volumeMounts, "volumeMounts"); err != nil {
			return fmt.Errorf("failed to set volume mounts on claw deployment: %w", err)
		}
		containers[containerIdx] = container
		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("failed to set containers on claw deployment: %w", err)
		}
		if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
			return fmt.Errorf("failed to set volumes on claw deployment: %w", err)
		}

		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// configureClawDeploymentForKubernetes mounts the sanitized kubeconfig ConfigMap,
// sets KUBECONFIG and PATH env vars, and adds an init container that copies kubectl
// into a shared volume on the claw (gateway) deployment when a kubernetes credential is present.
func configureClawDeploymentForKubernetes(objects []*unstructured.Unstructured, credentials []resolvedCredential, kubectlImage, instanceName string) error {
	if !hasKubernetesCredentials(credentials) {
		return nil
	}

	gatewayName := getClawDeploymentName(instanceName)
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

		containerIdx := -1
		var container map[string]any
		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawGatewayContainerName {
				containerIdx = i
				container = cm
				break
			}
		}
		if containerIdx < 0 {
			return fmt.Errorf("container %q not found in claw deployment", ClawGatewayContainerName)
		}

		envVars, _, _ := unstructured.NestedSlice(container, "env")
		volumeMounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
		volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
		initContainers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")

		envVars = append(envVars,
			map[string]any{
				"name":  "KUBECONFIG",
				"value": "/etc/kube/config",
			},
			map[string]any{
				"name":  "PATH",
				"value": "/opt/kube-tools:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			},
		)

		volumeMounts = append(volumeMounts,
			map[string]any{
				"name":      "kube-config",
				"mountPath": "/etc/kube",
				"readOnly":  true,
			},
			map[string]any{
				"name":      "kubectl-bin",
				"mountPath": "/opt/kube-tools",
				"readOnly":  true,
			},
		)

		volumes = append(volumes,
			map[string]any{
				"name": "kube-config",
				"configMap": map[string]any{
					"name": getKubeConfigMapName(instanceName),
				},
			},
			map[string]any{
				"name":     "kubectl-bin",
				"emptyDir": map[string]any{},
			},
		)

		initContainers = append(initContainers, map[string]any{
			"name":            "init-kubectl",
			"image":           kubectlImage,
			"imagePullPolicy": "IfNotPresent",
			"command":         []any{"sh", "-c", "cp /usr/bin/oc /usr/bin/kubectl /tools/"},
			"securityContext": map[string]any{
				"runAsNonRoot":             true,
				"allowPrivilegeEscalation": false,
				"readOnlyRootFilesystem":   true,
				"capabilities":             map[string]any{"drop": []any{"ALL"}},
			},
			"resources": map[string]any{
				"requests": map[string]any{"memory": "32Mi", "cpu": "50m"},
				"limits":   map[string]any{"memory": "64Mi", "cpu": "100m"},
			},
			"volumeMounts": []any{
				map[string]any{
					"name":      "kubectl-bin",
					"mountPath": "/tools",
				},
			},
		})

		if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
			return fmt.Errorf("failed to set env vars on claw deployment: %w", err)
		}
		if err := unstructured.SetNestedSlice(container, volumeMounts, "volumeMounts"); err != nil {
			return fmt.Errorf("failed to set volume mounts on claw deployment: %w", err)
		}
		containers[containerIdx] = container
		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("failed to set containers on claw deployment: %w", err)
		}
		if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
			return fmt.Errorf("failed to set volumes on claw deployment: %w", err)
		}
		if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
			return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
		}

		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// configureClawDeploymentConfigMode sets the CLAW_CONFIG_MODE env var on the
// init-config init container in the claw (gateway) deployment. This controls
// whether the merge script deep-merges operator config into the existing user
// config ("merge") or fully overwrites it ("overwrite").
func configureClawDeploymentConfigMode(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	mode := string(instance.Spec.ConfigMode)
	if mode == "" {
		mode = string(clawv1alpha1.ConfigModeMerge)
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		initContainers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")
		if err != nil {
			return fmt.Errorf("failed to get init containers from claw deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("initContainers field not found in claw deployment")
		}

		for i, ic := range initContainers {
			container, ok := ic.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(container, "name")
			if name != ClawInitConfigContainerName {
				continue
			}

			envVars, _, _ := unstructured.NestedSlice(container, "env")

			updated := false
			for j, e := range envVars {
				env, ok := e.(map[string]any)
				if !ok {
					continue
				}
				if env["name"] == ClawConfigModeEnvVar {
					env["value"] = mode
					envVars[j] = env
					updated = true
					break
				}
			}
			if !updated {
				envVars = append(envVars, map[string]any{
					"name":  ClawConfigModeEnvVar,
					"value": mode,
				})
			}

			if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
				return fmt.Errorf("failed to set env vars on init-config container: %w", err)
			}
			initContainers[i] = container
			return unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers")
		}
		return fmt.Errorf("container %q not found in claw deployment", ClawInitConfigContainerName)
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// stampGatewayConfigHash computes a SHA-256 hash of the gateway ConfigMap data and
// stamps it on the gateway pod template. This triggers a rollout when operator.json
// or other operator-managed config files change (e.g., after an operator upgrade).
func stampGatewayConfigHash(objects []*unstructured.Unstructured, instanceName string) error {
	configMapName := getConfigMapName(instanceName)
	var configData map[string]any
	for _, obj := range objects {
		if obj.GetKind() == ConfigMapKind && obj.GetName() == configMapName {
			var found bool
			var err error
			configData, found, err = unstructured.NestedMap(obj.Object, "data")
			if err != nil {
				return fmt.Errorf("failed to extract data from ConfigMap %s: %w", configMapName, err)
			}
			if !found {
				return fmt.Errorf("data not found in ConfigMap %s", configMapName)
			}
			break
		}
	}
	if configData == nil {
		return fmt.Errorf("ConfigMap %s not found in manifests", configMapName)
	}

	keys := make([]string, 0, len(configData))
	for k := range configData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s=%v\n", k, configData[k])
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))

	gatewayName := getClawDeploymentName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[clawv1alpha1.AnnotationKeyGatewayConfigHash] = hash

		if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
			return fmt.Errorf("failed to set gateway config hash annotation: %w", err)
		}
		return nil
	}
	return fmt.Errorf("gateway deployment %s not found for config hash stamping", gatewayName)
}
