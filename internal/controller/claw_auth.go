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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// resolveAuthPassword reads the password value from the Secret referenced by
// spec.auth.passwordSecretRef. Returns empty string if auth is nil or mode is not "password".
func (r *ClawResourceReconciler) resolveAuthPassword(ctx context.Context, instance *clawv1alpha1.Claw) (string, error) {
	if instance.Spec.Auth == nil || instance.Spec.Auth.Mode != clawv1alpha1.AuthModePassword {
		return "", nil
	}
	if instance.Spec.Auth.PasswordSecretRef == nil {
		return "", fmt.Errorf("spec.auth.passwordSecretRef is required when auth.mode is \"password\"")
	}

	ref := instance.Spec.Auth.PasswordSecretRef
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      ref.Name,
	}, secret); err != nil {
		return "", fmt.Errorf("failed to get auth password secret %q: %w", ref.Name, err)
	}

	val, exists := secret.Data[ref.Key]
	if !exists || len(val) == 0 {
		return "", fmt.Errorf("key %q not found or empty in secret %q", ref.Key, ref.Name)
	}

	return string(val), nil
}

// shouldDisableDevicePairing returns whether device identity checks should be
// disabled based on the auth spec. When DisableDevicePairing is explicitly set,
// that value is used. Otherwise it defaults to true for password mode and false
// for token mode.
func shouldDisableDevicePairing(auth *clawv1alpha1.AuthSpec) bool {
	if auth != nil && auth.DisableDevicePairing != nil {
		return *auth.DisableDevicePairing
	}
	return auth != nil && auth.Mode == clawv1alpha1.AuthModePassword
}

// injectAuthModeIntoConfigMap updates the gateway config in operator.json based
// on spec.auth. Only non-sensitive metadata is written: auth mode and device
// pairing flag. The actual password is delivered via OPENCLAW_GATEWAY_PASSWORD
// env var (see configureClawDeploymentForAuth).
func injectAuthModeIntoConfigMap(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	needsPasswordMode := instance.Spec.Auth != nil && instance.Spec.Auth.Mode == clawv1alpha1.AuthModePassword
	disablePairing := shouldDisableDevicePairing(instance.Spec.Auth)

	if !needsPasswordMode && !disablePairing {
		return nil
	}

	configMapName := getConfigMapName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != ConfigMapKind || obj.GetName() != configMapName {
			continue
		}

		operatorJSON, found, err := unstructured.NestedString(obj.Object, "data", "operator.json")
		if err != nil {
			return fmt.Errorf("failed to extract operator.json from ConfigMap: %w", err)
		}
		if !found {
			return fmt.Errorf("operator.json not found in ConfigMap data")
		}

		var config map[string]any
		if err := json.Unmarshal([]byte(operatorJSON), &config); err != nil {
			return fmt.Errorf("failed to parse operator.json: %w", err)
		}

		gateway, _ := config["gateway"].(map[string]any)
		if gateway == nil {
			gateway = map[string]any{}
		}

		if needsPasswordMode {
			gateway["auth"] = map[string]any{
				"mode": "password",
			}
		}

		if disablePairing {
			controlUI, _ := gateway["controlUi"].(map[string]any)
			if controlUI == nil {
				controlUI = map[string]any{}
			}
			controlUI["dangerouslyDisableDeviceAuth"] = true
			gateway["controlUi"] = controlUI
		}

		config["gateway"] = gateway

		updatedJSON, err := json.MarshalIndent(config, "    ", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal operator.json: %w", err)
		}

		if err := unstructured.SetNestedField(obj.Object, string(updatedJSON), "data", "operator.json"); err != nil {
			return fmt.Errorf("failed to set updated operator.json in ConfigMap: %w", err)
		}
		return nil
	}

	return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
}

// configureClawDeploymentForAuth adds the OPENCLAW_GATEWAY_PASSWORD env var to the
// gateway container, sourced from the password Secret via secretKeyRef. OpenClaw
// reads this env var as a fallback when gateway.auth.password is not in the config.
func configureClawDeploymentForAuth(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Auth == nil || instance.Spec.Auth.Mode != clawv1alpha1.AuthModePassword {
		return nil
	}
	ref := instance.Spec.Auth.PasswordSecretRef
	if ref == nil {
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
		envVars = append(envVars, map[string]any{
			"name": "OPENCLAW_GATEWAY_PASSWORD",
			"valueFrom": map[string]any{
				"secretKeyRef": map[string]any{
					"name": ref.Name,
					"key":  ref.Key,
				},
			},
		})

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
