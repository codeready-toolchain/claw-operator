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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

type searchProviderInfo struct {
	Domain   string
	Injector string
	Header   string // only for api_key injector
}

// knownSearchProviders maps search provider names to their proxy route configuration.
// Providers not in this map are checked against llmAsSearchProviders.
var knownSearchProviders = map[string]searchProviderInfo{
	"brave":      {Domain: "api.search.brave.com", Injector: "api_key", Header: "X-Subscription-Token"},
	"tavily":     {Domain: "api.tavily.com", Injector: "bearer"},
	"duckduckgo": {Domain: "html.duckduckgo.com", Injector: injectorNone},
}

// llmAsSearchProviders maps search provider names to the LLM credential provider
// that must exist in spec.credentials.
var llmAsSearchProviders = map[string]string{
	"gemini": "google",
}

const (
	placeholderAPIKey   = "ah-ah-ah-you-didnt-say-the-magic-word"
	injectorNone        = "none"
	webSearchCredPrefix = "websearch"
)

// webSearchNeedsSecret returns true if the provider requires a dedicated search API key.
func webSearchNeedsSecret(provider string) bool {
	info, ok := knownSearchProviders[provider]
	return ok && info.Injector != injectorNone
}

func (r *ClawResourceReconciler) validateWebSearchConfig(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
) error {
	ws := instance.Spec.WebSearch
	if ws == nil {
		meta.RemoveStatusCondition(&instance.Status.Conditions, clawv1alpha1.ConditionTypeWebSearchConfigured)
		return nil
	}

	provider := ws.Provider

	if info, ok := knownSearchProviders[provider]; ok {
		if info.Injector == injectorNone {
			setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionTrue,
				clawv1alpha1.ConditionReasonConfigured,
				fmt.Sprintf("web search provider %q configured", provider))
			return nil
		}

		if ws.SecretRef == nil {
			err := fmt.Errorf("web search provider %q requires secretRef", provider)
			setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			return err
		}

		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: instance.Namespace,
			Name:      ws.SecretRef.Name,
		}, secret); err != nil {
			err = fmt.Errorf("web search Secret %q not found: %w", ws.SecretRef.Name, err)
			setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			return err
		}
		if _, ok := secret.Data[ws.SecretRef.Key]; !ok {
			err := fmt.Errorf("key %q not found in web search Secret %q", ws.SecretRef.Key, ws.SecretRef.Name)
			setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			return err
		}

		setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionTrue,
			clawv1alpha1.ConditionReasonConfigured,
			fmt.Sprintf("web search provider %q configured", provider))
		return nil
	}

	if requiredProvider, ok := llmAsSearchProviders[provider]; ok {
		found := false
		for _, cred := range instance.Spec.Credentials {
			if cred.Provider == requiredProvider {
				found = true
				break
			}
		}
		if !found {
			err := fmt.Errorf(
				"web search provider %q requires a %q credential in spec.credentials",
				provider, requiredProvider,
			)
			setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse,
				clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			return err
		}

		setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionTrue,
			clawv1alpha1.ConditionReasonConfigured,
			fmt.Sprintf("web search provider %q configured (using %q credential)", provider, requiredProvider))
		return nil
	}

	err := fmt.Errorf("unknown web search provider %q", provider)
	setCondition(instance, clawv1alpha1.ConditionTypeWebSearchConfigured, metav1.ConditionFalse,
		clawv1alpha1.ConditionReasonValidationFailed, err.Error())
	setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse,
		clawv1alpha1.ConditionReasonValidationFailed, err.Error())
	return err
}

// injectWebSearch sets tools.web.search, tools.web.fetch, and plugin entries
// for the search provider. Always-win for operator-declared tools.
func injectWebSearch(config map[string]any, instance *clawv1alpha1.Claw) error {
	if instance.Spec.WebSearch == nil && instance.Spec.WebFetch == nil {
		return nil
	}

	web := ensureNestedMap(ensureNestedMap(config, "tools"), "web")

	if ws := instance.Spec.WebSearch; ws != nil {
		web["search"] = map[string]any{
			"enabled":  true,
			"provider": ws.Provider,
		}

		if err := injectSearchPluginEntry(config, ws); err != nil {
			return err
		}
	}

	if wf := instance.Spec.WebFetch; wf != nil {
		web["fetch"] = map[string]any{
			"enabled": wf.Enabled,
		}
	}

	return nil
}

// injectSearchPluginEntry adds the plugin entry for the search provider into
// plugins.entries.<pluginID>.config.webSearch.
func injectSearchPluginEntry(config map[string]any, ws *clawv1alpha1.WebSearchSpec) error {
	pluginID := ws.Provider

	// Gemini uses the "google" plugin entry
	if requiredProvider, ok := llmAsSearchProviders[ws.Provider]; ok {
		pluginID = requiredProvider
	}

	webSearchConfig := map[string]any{}

	// API-keyed providers get a placeholder key
	if webSearchNeedsSecret(ws.Provider) {
		webSearchConfig["apiKey"] = placeholderAPIKey
	}

	// Merge user-provided config
	if ws.Config != nil && ws.Config.Raw != nil {
		var userConfig map[string]any
		if err := json.Unmarshal(ws.Config.Raw, &userConfig); err != nil {
			return fmt.Errorf("failed to parse webSearch.config: %w", err)
		}
		for k, v := range userConfig {
			webSearchConfig[k] = v
		}
	}

	// Key-free providers with no user config need no plugin entry
	if len(webSearchConfig) == 0 {
		return nil
	}

	existingPlugins, _ := config["plugins"].(map[string]any)
	if existingPlugins == nil {
		existingPlugins = map[string]any{}
	}
	existingEntries, _ := existingPlugins["entries"].(map[string]any)
	if existingEntries == nil {
		existingEntries = map[string]any{}
	}

	pluginEntry, _ := existingEntries[pluginID].(map[string]any)
	if pluginEntry == nil {
		pluginEntry = map[string]any{}
	}
	pluginConfig, _ := pluginEntry["config"].(map[string]any)
	if pluginConfig == nil {
		pluginConfig = map[string]any{}
	}

	pluginConfig["webSearch"] = webSearchConfig
	pluginEntry["config"] = pluginConfig
	existingEntries[pluginID] = pluginEntry
	existingPlugins["entries"] = existingEntries
	config["plugins"] = existingPlugins

	return nil
}

// configureProxyForWebSearch mounts the search API key Secret as an env var
// on the proxy container. Only applies to API-keyed providers.
func configureProxyForWebSearch(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	if instance.Spec.WebSearch == nil {
		return nil
	}
	if !webSearchNeedsSecret(instance.Spec.WebSearch.Provider) {
		return nil
	}
	ref := instance.Spec.WebSearch.SecretRef
	if ref == nil {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(instance.GetName()) {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from proxy deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in proxy deployment")
		}

		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name != ClawProxyContainerName {
				continue
			}

			envVars, _, _ := unstructured.NestedSlice(cm, "env")
			envVars = append(envVars, map[string]any{
				"name": credEnvVarName(webSearchCredPrefix),
				"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{
						"name": ref.Name,
						"key":  ref.Key,
					},
				},
			})

			if err := unstructured.SetNestedSlice(cm, envVars, "env"); err != nil {
				return fmt.Errorf("failed to set env vars: %w", err)
			}
			containers[i] = cm
			if err := unstructured.SetNestedSlice(
				obj.Object, containers, "spec", "template", "spec", "containers",
			); err != nil {
				return fmt.Errorf("failed to set containers: %w", err)
			}
			return nil
		}
		return fmt.Errorf("container %q not found in proxy deployment", ClawProxyContainerName)
	}
	return fmt.Errorf("claw-proxy deployment not found in manifests")
}
