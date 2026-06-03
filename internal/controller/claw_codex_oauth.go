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
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	codexOAuthDefaultProfileID = "openai:chatgpt-default"
	codexOAuthDefaultModel     = "gpt-5.5"
	codexOAuthProviderOpenAI   = "openai"
	codexOAuthProviderLegacy   = "openai-codex"
	codexOAuthProviderCodex    = "codex"
	codexOAuthRuntimeID        = "codex"
	codexOAuthMountName        = "codex-oauth"
	codexOAuthMountPath        = "/codex-oauth"
	codexOAuthMountedFileName  = "auth.json"
	codexOAuthProfileEnvVar    = "CODEX_OAUTH_PROFILE_ID"
)

type codexCLIAuthJSON struct {
	AuthMode string `json:"auth_mode"`
	Tokens   struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
}

func normalizeCodexOAuthProfileID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return codexOAuthDefaultProfileID
	}
	separator := strings.Index(trimmed, ":")
	if separator < 0 {
		return codexOAuthProviderOpenAI + ":" + trimmed
	}
	provider := trimmed[:separator]
	profile := trimmed[separator+1:]
	if profile == "" {
		profile = "default"
	}
	if provider == codexOAuthProviderLegacy || provider == codexOAuthProviderCodex {
		return codexOAuthProviderOpenAI + ":chatgpt-" + profile
	}
	return trimmed
}

func normalizeCodexModelRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = codexOAuthDefaultModel
	}
	for _, prefix := range []string{
		codexOAuthProviderCodex + "/",
		codexOAuthProviderOpenAI + "/",
		codexOAuthProviderLegacy + "/",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			model := strings.TrimPrefix(trimmed, prefix)
			if prefix == codexOAuthProviderCodex+"/" {
				return trimmed
			}
			return codexOAuthProviderCodex + "/" + model
		}
	}
	return codexOAuthProviderCodex + "/" + trimmed
}

func codexModelIDFromRef(ref string) string {
	return strings.TrimPrefix(normalizeCodexModelRef(ref), codexOAuthProviderCodex+"/")
}

func configuredCodexModelRefs(spec *clawv1alpha1.CodexOAuthSpec) []string {
	if spec == nil {
		return nil
	}
	seen := map[string]bool{}
	var refs []string
	for _, raw := range append([]string{spec.Model}, spec.Models...) {
		ref := normalizeCodexModelRef(raw)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs
}

func validateCodexCLIAuthJSON(data []byte) error {
	var auth codexCLIAuthJSON
	if err := json.Unmarshal(data, &auth); err != nil {
		return fmt.Errorf("Codex OAuth auth.json is not valid JSON: %w", err)
	}
	if auth.AuthMode != "chatgpt" {
		return fmt.Errorf(`Codex OAuth auth.json must have auth_mode "chatgpt"`)
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" || strings.TrimSpace(auth.Tokens.RefreshToken) == "" {
		return fmt.Errorf("Codex OAuth auth.json is missing tokens.access_token or tokens.refresh_token")
	}
	return nil
}

func (r *ClawResourceReconciler) validateCodexOAuthConfig(ctx context.Context, instance *clawv1alpha1.Claw) error {
	if instance.Spec.CodexOAuth == nil {
		return nil
	}
	ref := instance.Spec.CodexOAuth.SecretRef
	secret := &corev1.Secret{}
	if err := r.UserSecretReader.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: ref.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("codexOAuth: Secret %q not found", ref.Name)
		}
		return fmt.Errorf("codexOAuth: failed to get Secret %q: %w", ref.Name, err)
	}
	data, ok := secret.Data[ref.Key]
	if !ok {
		return fmt.Errorf("codexOAuth: key %q not found in Secret %q", ref.Key, ref.Name)
	}
	if err := validateCodexCLIAuthJSON(data); err != nil {
		return fmt.Errorf("codexOAuth: %w", err)
	}
	return nil
}

func injectCodexOAuthConfig(config map[string]any, instance *clawv1alpha1.Claw) {
	if instance.Spec.CodexOAuth == nil {
		return
	}

	plugins := ensureNestedMap(config, "plugins")
	allow := getStringSlice(config, "plugins", "allow")
	allow = appendIfMissing(allow, codexOAuthProviderOpenAI)
	allow = appendIfMissing(allow, codexOAuthRuntimeID)
	plugins["allow"] = stringsToAny(allow)

	entries := ensureNestedMap(plugins, "entries")
	ensureNestedMap(entries, codexOAuthProviderOpenAI)["enabled"] = true
	ensureNestedMap(entries, codexOAuthRuntimeID)["enabled"] = true

	profileID := normalizeCodexOAuthProfileID(instance.Spec.CodexOAuth.ProfileID)
	auth := ensureNestedMap(config, "auth")
	profiles := ensureNestedMap(auth, "profiles")
	profiles[profileID] = map[string]any{
		"provider": codexOAuthProviderOpenAI,
		"mode":     "oauth",
	}
	order := ensureNestedMap(auth, "order")
	order[codexOAuthProviderOpenAI] = []any{profileID}

	defaults := ensureNestedMap(ensureNestedMap(config, "agents"), "defaults")
	models := ensureNestedMap(defaults, "models")
	modelRefs := configuredCodexModelRefs(instance.Spec.CodexOAuth)
	for _, ref := range modelRefs {
		existing, _ := models[ref].(map[string]any)
		if existing == nil {
			existing = map[string]any{}
		}
		if _, ok := existing["alias"]; !ok {
			existing["alias"] = codexModelIDFromRef(ref)
		}
		runtime, _ := existing["agentRuntime"].(map[string]any)
		if runtime == nil {
			runtime = map[string]any{}
		}
		runtime["id"] = codexOAuthRuntimeID
		existing["agentRuntime"] = runtime
		models[ref] = existing
	}

	modelMap := ensureNestedMap(defaults, "model")
	if existing, _ := modelMap["primary"].(string); existing == "" && len(modelRefs) > 0 {
		modelMap["primary"] = modelRefs[0]
	}
}

func configureClawDeploymentForCodexOAuth(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.CodexOAuth == nil {
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

		for i, c := range initContainers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name != ClawInitConfigContainerName {
				continue
			}

			envVars, _, _ := unstructured.NestedSlice(cm, "env")
			envVars = append(envVars, map[string]any{
				"name":  codexOAuthProfileEnvVar,
				"value": normalizeCodexOAuthProfileID(instance.Spec.CodexOAuth.ProfileID),
			})
			if err := unstructured.SetNestedSlice(cm, envVars, "env"); err != nil {
				return fmt.Errorf("failed to set env vars on init-config: %w", err)
			}

			volumeMounts, _, _ := unstructured.NestedSlice(cm, "volumeMounts")
			volumeMounts = append(volumeMounts, map[string]any{
				"name":      codexOAuthMountName,
				"mountPath": codexOAuthMountPath,
				"readOnly":  true,
			})
			if err := unstructured.SetNestedSlice(cm, volumeMounts, "volumeMounts"); err != nil {
				return fmt.Errorf("failed to set volume mounts on init-config: %w", err)
			}

			initContainers[i] = cm
			if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
				return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
			}

			volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
			volumes = append(volumes, map[string]any{
				"name": codexOAuthMountName,
				"secret": map[string]any{
					"secretName": instance.Spec.CodexOAuth.SecretRef.Name,
					"items": []any{
						map[string]any{
							"key":  instance.Spec.CodexOAuth.SecretRef.Key,
							"path": codexOAuthMountedFileName,
						},
					},
				},
			})
			if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
				return fmt.Errorf("failed to set volumes on claw deployment: %w", err)
			}
			return nil
		}
		return fmt.Errorf("init-config container not found in claw deployment")
	}
	return fmt.Errorf("claw deployment not found in manifests")
}
