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

// injectAuthModeIntoConfigMap updates the gateway.auth block in operator.json
// based on spec.auth. When mode is "password", it sets the password and disables
// device pairing auth. When mode is "token" (or auth is nil), the template default
// ("token") is left in place.
func injectAuthModeIntoConfigMap(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw, password string) error {
	if instance.Spec.Auth == nil || instance.Spec.Auth.Mode == clawv1alpha1.AuthModeToken {
		return nil // template default is "token" — nothing to change
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

		// Set password auth mode
		gateway["auth"] = map[string]any{
			"mode":     "password",
			"password": password,
		}

		// Disable device pairing when using password auth
		controlUI, _ := gateway["controlUi"].(map[string]any)
		if controlUI == nil {
			controlUI = map[string]any{}
		}
		controlUI["dangerouslyDisableDeviceAuth"] = true
		gateway["controlUi"] = controlUI

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
