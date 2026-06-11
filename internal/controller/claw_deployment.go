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
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// configureClawImage overrides the OpenClaw container image tag on the gateway
// Deployment when spec.version is set. Affects init-volume, init-config (init
// containers) and gateway (regular container).
func configureClawImage(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Version == "" {
		return nil
	}

	image := OpenClawImageBase + ":" + instance.Spec.Version
	gatewayName := getClawDeploymentName(instance.Name)
	clawContainers := map[string]bool{
		ClawInitVolumeContainerName: true,
		ClawInitConfigContainerName: true,
		ClawGatewayContainerName:    true,
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		found := make(map[string]bool, len(clawContainers))
		paths := [][]string{
			{"spec", "template", "spec", "initContainers"},
			{"spec", "template", "spec", "containers"},
		}

		for _, path := range paths {
			containers, ok, err := unstructured.NestedSlice(obj.Object, path...)
			if err != nil {
				return fmt.Errorf("failed to get %s from %s: %w", path[len(path)-1], obj.GetName(), err)
			}
			if !ok {
				continue
			}
			for _, c := range containers {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				name, _, _ := unstructured.NestedString(cm, "name")
				if clawContainers[name] {
					found[name] = true
				}
			}
		}

		var missing []string
		for name := range clawContainers {
			if !found[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("OpenClaw containers not found in deployment: %v", missing)
		}

		for _, path := range paths {
			containers, ok, _ := unstructured.NestedSlice(obj.Object, path...)
			if !ok {
				continue
			}
			for i, c := range containers {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				name, _, _ := unstructured.NestedString(cm, "name")
				if clawContainers[name] {
					cm["image"] = image
					containers[i] = cm
				}
			}
			if err := unstructured.SetNestedSlice(obj.Object, containers, path...); err != nil {
				return fmt.Errorf("failed to set %s in %s: %w", path[len(path)-1], obj.GetName(), err)
			}
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

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
	setInstanceLabel(cm, instance.Name)
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
	mode := string(clawv1alpha1.ConfigModeMerge)
	if instance.Spec.Config != nil && instance.Spec.Config.MergeMode != "" {
		mode = string(instance.Spec.Config.MergeMode)
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

func userManagedConfig(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.Config != nil && instance.Spec.Config.Management == clawv1alpha1.ConfigManagementUser
}

func agentFilesApplyPolicy(instance *clawv1alpha1.Claw) string {
	if instance.Spec.AgentFiles != nil && instance.Spec.AgentFiles.ApplyPolicy != "" {
		return string(instance.Spec.AgentFiles.ApplyPolicy)
	}
	return string(clawv1alpha1.AgentFilesApplyPolicyIfMissing)
}

func agentFilesConfigMapKey(ref *clawv1alpha1.AgentFilesConfigMapRef) string {
	if ref != nil && ref.Key != "" {
		return ref.Key
	}
	return "agentfiles.tgz"
}

func setOrAppendEnv(envVars []any, name, value string) []any {
	for i, e := range envVars {
		env, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if env["name"] == name {
			env["value"] = value
			delete(env, "valueFrom")
			envVars[i] = env
			return envVars
		}
	}
	return append(envVars, map[string]any{"name": name, "value": value})
}

func setOrAppendVolumeMount(mounts []any, mount map[string]any) []any {
	name, _ := mount["name"].(string)
	mountPath, _ := mount["mountPath"].(string)
	for i, existing := range mounts {
		existingMount, ok := existing.(map[string]any)
		if !ok {
			continue
		}
		if existingMount["name"] == name && existingMount["mountPath"] == mountPath {
			mounts[i] = mount
			return mounts
		}
	}
	return append(mounts, mount)
}

func removeHomeVolumeMounts(mounts []any) []any {
	filtered := mounts[:0]
	for _, m := range mounts {
		mount, ok := m.(map[string]any)
		if !ok {
			filtered = append(filtered, m)
			continue
		}
		name, _ := mount["name"].(string)
		mountPath, _ := mount["mountPath"].(string)
		if name == "claw-home" && strings.HasPrefix(mountPath, "/home/node") {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func configureWholeHomeMount(container map[string]any) error {
	mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
	mounts = removeHomeVolumeMounts(mounts)
	mounts = setOrAppendVolumeMount(mounts, map[string]any{
		"name":      "claw-home",
		"mountPath": "/home/node",
		"subPath":   "home",
	})
	return unstructured.SetNestedSlice(container, mounts, "volumeMounts")
}

func setOrAppendVolume(volumes []any, volume map[string]any) []any {
	name, _ := volume["name"].(string)
	for i, existing := range volumes {
		existingVolume, ok := existing.(map[string]any)
		if !ok {
			continue
		}
		if existingVolume["name"] == name {
			volumes[i] = volume
			return volumes
		}
	}
	return append(volumes, volume)
}

// configureUserManagedOpenClawFiles switches user-managed deployments to mount
// the full OpenClaw home and wires optional agent file seed sources into
// init-config. Default operator-managed deployments keep the existing subPath
// layout and CR-oriented workspace/skill injection.
func configureUserManagedOpenClawFiles(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if !userManagedConfig(instance) {
		return nil
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

		initConfigFound := false
		for i, ic := range initContainers {
			container, ok := ic.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(container, "name")
			switch name {
			case ClawInitConfigContainerName:
				initConfigFound = true
				envVars, _, _ := unstructured.NestedSlice(container, "env")
				envVars = setOrAppendEnv(envVars, ClawConfigManagementEnvVar, string(clawv1alpha1.ConfigManagementUser))
				envVars = setOrAppendEnv(envVars, "AGENT_FILES_APPLY_POLICY", agentFilesApplyPolicy(instance))
				if instance.Spec.AgentFiles != nil && instance.Spec.AgentFiles.ConfigMapRef != nil {
					key := agentFilesConfigMapKey(instance.Spec.AgentFiles.ConfigMapRef)
					envVars = setOrAppendEnv(envVars, "AGENT_FILES_SOURCE", "configmap")
					envVars = setOrAppendEnv(envVars, "AGENT_FILES_CONFIGMAP_PATH", "/agent-files/"+key)
					mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
					mounts = setOrAppendVolumeMount(mounts, map[string]any{
						"name":      "agent-files",
						"mountPath": "/agent-files",
						"readOnly":  true,
					})
					if err := unstructured.SetNestedSlice(container, mounts, "volumeMounts"); err != nil {
						return fmt.Errorf("failed to set init-config agent files mount: %w", err)
					}
				}
				if instance.Spec.AgentFiles != nil && instance.Spec.AgentFiles.Git != nil {
					envVars = setOrAppendEnv(envVars, "AGENT_FILES_SOURCE", "git")
					envVars = setOrAppendEnv(envVars, "AGENT_FILES_GIT_URL", instance.Spec.AgentFiles.Git.URL)
					if instance.Spec.AgentFiles.Git.Ref != "" {
						envVars = setOrAppendEnv(envVars, "AGENT_FILES_GIT_REF", instance.Spec.AgentFiles.Git.Ref)
					}
					if instance.Spec.AgentFiles.Git.Path != "" {
						envVars = setOrAppendEnv(envVars, "AGENT_FILES_GIT_PATH", instance.Spec.AgentFiles.Git.Path)
					}
				}
				if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
					return fmt.Errorf("failed to set init-config env vars: %w", err)
				}
				if err := configureWholeHomeMount(container); err != nil {
					return fmt.Errorf("failed to configure init-config home mount: %w", err)
				}
				initContainers[i] = container
			case PluginsInitContainerName:
				if err := configureWholeHomeMount(container); err != nil {
					return fmt.Errorf("failed to configure init-plugins home mount: %w", err)
				}
				initContainers[i] = container
			}
		}
		if !initConfigFound {
			return fmt.Errorf("container %q not found in claw deployment", ClawInitConfigContainerName)
		}
		if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
			return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from claw deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in claw deployment")
		}
		gatewayFound := false
		for i, c := range containers {
			container, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(container, "name")
			if name != ClawGatewayContainerName {
				continue
			}
			gatewayFound = true
			if err := configureWholeHomeMount(container); err != nil {
				return fmt.Errorf("failed to configure gateway home mount: %w", err)
			}
			containers[i] = container
		}
		if !gatewayFound {
			return fmt.Errorf("container %q not found in claw deployment", ClawGatewayContainerName)
		}
		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("failed to set containers on claw deployment: %w", err)
		}

		if instance.Spec.AgentFiles != nil && instance.Spec.AgentFiles.ConfigMapRef != nil {
			volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
			volumes = setOrAppendVolume(volumes, map[string]any{
				"name": "agent-files",
				"configMap": map[string]any{
					"name": instance.Spec.AgentFiles.ConfigMapRef.Name,
				},
			})
			if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
				return fmt.Errorf("failed to set volumes on claw deployment: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// configureGatewayForMcpServers adds secret-backed environment variables to the gateway
// container for MCP servers using tier 3 envFrom. Each envFrom entry becomes an env var
// with valueFrom.secretKeyRef, so the real secret value is available at runtime.
//
// Entries are sorted deterministically by env var name, then secret name, then key.
// If multiple entries share the same env var name, the last one wins (stable because sorted).
// Existing container env vars with the same name and matching secretKeyRef are left as-is;
// mismatched ones are replaced.
func configureGatewayForMcpServers(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	desired := collectAndDeduplicateMcpEnvFrom(instance)
	if len(desired) == 0 {
		return nil
	}

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
		envVars = mergeEnvFromIntoSlice(envVars, desired)

		if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
			return fmt.Errorf("failed to set env vars on claw deployment: %w", err)
		}
		containers[containerIdx] = container
		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("failed to set containers on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// collectAndDeduplicateMcpEnvFrom gathers envFrom entries from all MCP servers,
// sorts them deterministically, and deduplicates by env var name (last wins).
func collectAndDeduplicateMcpEnvFrom(instance *clawv1alpha1.Claw) []clawv1alpha1.McpEnvFromSecret {
	type sortableEntry struct {
		serverName string
		entry      clawv1alpha1.McpEnvFromSecret
	}

	var all []sortableEntry
	for serverName, spec := range instance.Spec.McpServers {
		for _, ef := range spec.EnvFrom {
			all = append(all, sortableEntry{serverName: serverName, entry: ef})
		}
	}

	slices.SortFunc(all, func(a, b sortableEntry) int {
		if c := cmp.Compare(a.serverName, b.serverName); c != 0 {
			return c
		}
		if c := cmp.Compare(a.entry.Name, b.entry.Name); c != 0 {
			return c
		}
		if c := cmp.Compare(a.entry.SecretRef.Name, b.entry.SecretRef.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.entry.SecretRef.Key, b.entry.SecretRef.Key)
	})

	seen := make(map[string]int)
	var deduped []clawv1alpha1.McpEnvFromSecret
	for _, se := range all {
		if idx, exists := seen[se.entry.Name]; exists {
			deduped[idx] = se.entry
		} else {
			seen[se.entry.Name] = len(deduped)
			deduped = append(deduped, se.entry)
		}
	}
	return deduped
}

// mergeEnvFromIntoSlice merges desired envFrom entries into an existing env var slice.
// If an existing entry has the same name and matching secretKeyRef, it's left as-is.
// If the name matches but secretKeyRef differs, it's replaced. Otherwise, the entry is appended.
func mergeEnvFromIntoSlice(existing []any, desired []clawv1alpha1.McpEnvFromSecret) []any {
	for _, ef := range desired {
		newEntry := map[string]any{
			"name": ef.Name,
			"valueFrom": map[string]any{
				"secretKeyRef": map[string]any{
					"name": ef.SecretRef.Name,
					"key":  ef.SecretRef.Key,
				},
			},
		}

		replaced := false
		for i, e := range existing {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(em, "name")
			if name != ef.Name {
				continue
			}
			existingSecretName, _, _ := unstructured.NestedString(em, "valueFrom", "secretKeyRef", "name")
			existingSecretKey, _, _ := unstructured.NestedString(em, "valueFrom", "secretKeyRef", "key")
			if existingSecretName == ef.SecretRef.Name && existingSecretKey == ef.SecretRef.Key {
				replaced = true
				break
			}
			existing[i] = newEntry
			replaced = true
			break
		}
		if !replaced {
			existing = append(existing, newEntry)
		}
	}
	return existing
}

// stampMcpSecretVersionAnnotation stamps the gateway deployment pod template with
// ResourceVersions of Secrets referenced by MCP envFrom entries. This triggers a
// rollout when any MCP secret data changes.
func (r *ClawResourceReconciler) stampMcpSecretVersionAnnotation(
	ctx context.Context,
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	versions := make(map[string]string)
	serverNames := make([]string, 0, len(instance.Spec.McpServers))
	for name := range instance.Spec.McpServers {
		serverNames = append(serverNames, name)
	}
	slices.Sort(serverNames)

	for _, serverName := range serverNames {
		spec := instance.Spec.McpServers[serverName]
		for _, ef := range spec.EnvFrom {
			secret := &corev1.Secret{}
			if err := r.UserSecretReader.Get(ctx, client.ObjectKey{
				Namespace: instance.Namespace,
				Name:      ef.SecretRef.Name,
			}, secret); err != nil {
				return fmt.Errorf("failed to get Secret %q for MCP server %q env %q: %w",
					ef.SecretRef.Name, serverName, ef.Name, err)
			}
			key := mcpAnnotationKey(serverName, ef.Name)
			versions[key] = secret.ResourceVersion
		}
	}

	if len(versions) == 0 {
		return nil
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
		if annotations == nil {
			annotations = make(map[string]string)
		}
		for key, rv := range versions {
			annotations[clawv1alpha1.AnnotationPrefixMcpSecretVersion+key+clawv1alpha1.AnnotationSuffixMcpSecretVersion] = rv
		}
		if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
			return fmt.Errorf("failed to set MCP secret version annotations: %w", err)
		}
		return nil
	}
	return fmt.Errorf("gateway deployment not found for MCP secret version stamping")
}

// mcpAnnotationKey produces a safe, deterministic, short annotation key segment
// from an MCP server name and env var name. The full annotation becomes:
//
//	claw.sandbox.redhat.com/mcp-<key>-secret-version
//
// The key is a 12-char hex SHA-256 prefix of "serverName/envName", which is always
// valid in Kubernetes annotations and stays well under the 63-char name limit.
func mcpAnnotationKey(serverName, envName string) string {
	h := sha256.Sum256([]byte(serverName + "/" + envName))
	return fmt.Sprintf("%x", h[:6])
}

// stampGatewayConfigHash computes a SHA-256 hash of the gateway ConfigMap data
// (plus the declared plugin list) and stamps it on the gateway pod template.
// This triggers a rollout when operator.json, other operator-managed config
// files, or the plugin install list change.
func stampGatewayConfigHash(objects []*unstructured.Unstructured, instanceName string, plugins []string) error {
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
	if len(plugins) > 0 {
		sorted := make([]string, len(plugins))
		copy(sorted, plugins)
		sort.Strings(sorted)
		_, _ = fmt.Fprintf(h, "plugins=%s\n", strings.Join(sorted, ","))
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

// noProxySuffix is appended to NO_PROXY/no_proxy when inClusterBypass is true,
// allowing the gateway to reach in-cluster services directly.
const noProxySuffix = ",.svc,.svc.cluster.local"
const envNoProxyLower = "no_proxy"

// configureGatewayNoProxy appends .svc,.svc.cluster.local to NO_PROXY/no_proxy
// on the gateway container when inClusterBypass is true.
func configureGatewayNoProxy(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if !inClusterBypassEnabled(instance) {
		return nil
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from gateway deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in gateway deployment")
		}

		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name != ClawGatewayContainerName {
				continue
			}
			appendNoProxySuffix(cm)
			containers[i] = cm
			return unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")
		}
		return fmt.Errorf("container %q not found in gateway deployment", ClawGatewayContainerName)
	}
	return fmt.Errorf("gateway deployment %s not found", gatewayName)
}

// appendNoProxySuffix appends noProxySuffix to NO_PROXY and no_proxy env vars.
func appendNoProxySuffix(container map[string]any) {
	envVars, _, _ := unstructured.NestedSlice(container, "env")
	for i, e := range envVars {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(em, "name")
		if name == "NO_PROXY" || name == envNoProxyLower {
			if val, _, _ := unstructured.NestedString(em, "value"); val != "" {
				em["value"] = val + noProxySuffix
				envVars[i] = em
			}
		}
	}
	_ = unstructured.SetNestedSlice(container, envVars, "env")
}

// inClusterBypassEnabled returns true if spec.network.inClusterBypass is explicitly true.
func inClusterBypassEnabled(instance *clawv1alpha1.Claw) bool {
	return instance.Spec.Network != nil &&
		instance.Spec.Network.InClusterBypass != nil &&
		*instance.Spec.Network.InClusterBypass
}
