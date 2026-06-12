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
	"path/filepath"
	"sort"
	"strings"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	personaGuardVolumeName = "persona-guard"
	workspaceDir           = "/home/node/.openclaw/workspace"
)

// configurePersonaGuard adds read-only subPath volume mounts for each key in
// the persona ConfigMap. The mounts overlay individual files on top of the
// writable PVC, enforcing immutability at the filesystem level.
func configurePersonaGuard(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	personaKeys []string,
) error {
	if instance.Spec.Restrictions == nil || instance.Spec.Restrictions.PersonaRef == nil || len(personaKeys) == 0 {
		return nil
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
		volumes = setOrAppendVolume(volumes, map[string]any{
			"name": personaGuardVolumeName,
			"configMap": map[string]any{
				"name": instance.Spec.Restrictions.PersonaRef.Name,
			},
		})
		if err := unstructured.SetNestedSlice(
			obj.Object, volumes, "spec", "template", "spec", "volumes",
		); err != nil {
			return fmt.Errorf("failed to set persona guard volume: %w", err)
		}

		containers, found, err := unstructured.NestedSlice(
			obj.Object, "spec", "template", "spec", "containers",
		)
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

			mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
			for _, key := range personaKeys {
				mounts = setOrAppendVolumeMount(mounts, map[string]any{
					"name":      personaGuardVolumeName,
					"mountPath": workspaceDir + "/" + key,
					"subPath":   key,
					"readOnly":  true,
				})
			}
			if err := unstructured.SetNestedSlice(container, mounts, "volumeMounts"); err != nil {
				return fmt.Errorf("failed to set persona guard mounts on gateway: %w", err)
			}
			containers[i] = container
		}

		if !gatewayFound {
			return fmt.Errorf("container %q not found in claw deployment", ClawGatewayContainerName)
		}
		if err := unstructured.SetNestedSlice(
			obj.Object, containers, "spec", "template", "spec", "containers",
		); err != nil {
			return fmt.Errorf("failed to set containers on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// validatePersonaKeys checks that all persona ConfigMap keys are safe
// filenames for mounting into the workspace directory.
func validatePersonaKeys(keys []string) error {
	for _, key := range keys {
		if key == "" {
			return fmt.Errorf("persona ConfigMap key must not be empty")
		}
		if strings.Contains(key, "/") || strings.Contains(key, "\\") {
			return fmt.Errorf("persona ConfigMap key %q must not contain path separators", key)
		}
		if key == "." || key == ".." {
			return fmt.Errorf("persona ConfigMap key %q is invalid", key)
		}
		if filepath.Clean(key) != key {
			return fmt.Errorf("persona ConfigMap key %q is invalid: must be a clean filename", key)
		}
	}
	return nil
}

// sortedPersonaKeys returns the data keys from a ConfigMap sorted
// deterministically. Sorted order ensures stable volume mount ordering
// across reconcile loops, avoiding unnecessary deployment rollouts.
func sortedPersonaKeys(data map[string]string) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pluginInstallationDisabled returns true when the operator should skip
// installing plugins, either because restrictions.pluginInstallation is
// explicitly set to false.
func pluginInstallationDisabled(instance *clawv1alpha1.Claw) bool {
	if instance.Spec.Restrictions == nil || instance.Spec.Restrictions.PluginInstallation == nil {
		return false
	}
	return !*instance.Spec.Restrictions.PluginInstallation
}

// resolvePersonaRef reads the persona ConfigMap and returns its sorted keys
// and raw data. Returns nil slices when no personaRef is configured.
// Sets the RestrictionsEnforced status condition on success or failure.
func (r *ClawResourceReconciler) resolvePersonaRef(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) ([]string, map[string]string, error) {
	logger := log.FromContext(ctx)

	if instance.Spec.Restrictions == nil || instance.Spec.Restrictions.PersonaRef == nil {
		return nil, nil, nil
	}

	ref := instance.Spec.Restrictions.PersonaRef
	cm := &corev1.ConfigMap{}
	// Read directly from the API server, bypassing the informer cache.
	// The cache only includes ConfigMaps with the instance label;
	// user-created persona ConfigMaps are unlabeled.
	if err := r.UserSecretReader.Get(ctx, types.NamespacedName{
		Name:      ref.Name,
		Namespace: instance.Namespace,
	}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("persona ConfigMap %q not found", ref.Name)
			logger.Error(err, msg)
			setCondition(instance, clawv1alpha1.ConditionTypeRestrictionsEnforced,
				metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, msg)
			return nil, nil, fmt.Errorf("%s: %w", msg, err)
		}
		return nil, nil, fmt.Errorf("failed to get persona ConfigMap %q: %w", ref.Name, err)
	}

	if len(cm.Data) == 0 {
		msg := fmt.Sprintf("persona ConfigMap %q has no data keys", ref.Name)
		logger.Info(msg)
		setCondition(instance, clawv1alpha1.ConditionTypeRestrictionsEnforced,
			metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, msg)
		return nil, nil, fmt.Errorf("%s", msg)
	}

	keys := sortedPersonaKeys(cm.Data)
	if err := validatePersonaKeys(keys); err != nil {
		setCondition(instance, clawv1alpha1.ConditionTypeRestrictionsEnforced,
			metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
		return nil, nil, err
	}

	setCondition(instance, clawv1alpha1.ConditionTypeRestrictionsEnforced,
		metav1.ConditionTrue, clawv1alpha1.ConditionReasonConfigured,
		fmt.Sprintf("persona guard active: %s", strings.Join(keys, ", ")))

	return keys, cm.Data, nil
}

// stampPersonaConfigHash computes a SHA-256 hash of the persona ConfigMap data
// and stamps it as an annotation on the gateway pod template. This triggers a
// rollout when persona files change.
func stampPersonaConfigHash(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	personaData map[string]string,
) error {
	if len(personaData) == 0 {
		return nil
	}

	keys := sortedPersonaKeys(personaData)
	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s=%s\n", k, personaData[k])
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		annotations, _, _ := unstructured.NestedStringMap(
			obj.Object, "spec", "template", "metadata", "annotations",
		)
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["claw.sandbox.redhat.com/persona-config-hash"] = hash
		if err := unstructured.SetNestedStringMap(
			obj.Object, annotations, "spec", "template", "metadata", "annotations",
		); err != nil {
			return fmt.Errorf("failed to set persona config hash annotation: %w", err)
		}
		return nil
	}
	return nil
}
