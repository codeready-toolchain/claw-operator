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
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// proxyRoute is a single route entry in the proxy config JSON.
type proxyRoute struct {
	Domain         string            `json:"domain"`
	Injector       string            `json:"injector"`
	Header         string            `json:"header,omitempty"`
	ValuePrefix    string            `json:"valuePrefix,omitempty"`
	EnvVar         string            `json:"envVar,omitempty"`
	SAFilePath     string            `json:"saFilePath,omitempty"`
	GCPProject     string            `json:"gcpProject,omitempty"`
	GCPLocation    string            `json:"gcpLocation,omitempty"`
	PathPrefix     string            `json:"pathPrefix,omitempty"`
	Upstream       string            `json:"upstream,omitempty"`
	ClientID       string            `json:"clientID,omitempty"`
	TokenURL       string            `json:"tokenURL,omitempty"`
	Scopes         []string          `json:"scopes,omitempty"`
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`
	KubeconfigPath string            `json:"kubeconfigPath,omitempty"`
	CACert         string            `json:"caCert,omitempty"`
}

// proxyConfig is the top-level proxy configuration JSON.
type proxyConfig struct {
	Routes []proxyRoute `json:"routes"`
}

// credEnvVarName derives the proxy env var name from a credential entry name.
// e.g., "gemini" -> "CRED_GEMINI", "vertex-ai" -> "CRED_VERTEX_AI"
func credEnvVarName(credName string) string {
	upper := strings.ToUpper(credName)
	return "CRED_" + strings.ReplaceAll(upper, "-", "_")
}

// usesVertexSDK returns true when a credential should use the native Vertex AI SDK
// instead of a gateway proxy route. This applies to non-Google GCP providers (e.g.,
// Anthropic via Vertex AI), where the provider's SDK format doesn't match Vertex AI's
// URL structure and the native @anthropic-ai/vertex-sdk handles it correctly.
func usesVertexSDK(cred clawv1alpha1.CredentialSpec) bool {
	return cred.Type == clawv1alpha1.CredentialTypeGCP && cred.Provider != "" && cred.Provider != "google"
}

// vertexProviderAPIMapping maps provider names to their OpenClaw API identifiers for Vertex AI.
var vertexProviderAPIMapping = map[string]string{
	"anthropic": "anthropic-messages",
}

// apiKeyProviderDefault holds the default domain and header for a known provider using type: apiKey.
type apiKeyProviderDefault struct {
	Domain string
	Header string
}

// knownAPIKeyProviders maps provider names to their default domain and header.
var knownAPIKeyProviders = map[string]apiKeyProviderDefault{
	"google":    {Domain: "generativelanguage.googleapis.com", Header: "x-goog-api-key"},
	"anthropic": {Domain: "api.anthropic.com", Header: "x-api-key"},
}

// resolveProviderDefaults fills in missing Domain and APIKey fields for known providers.
// Explicit values are preserved (escape hatch). Returns an error if required fields
// are still missing after applying defaults (unknown provider without domain/apiKey).
func resolveProviderDefaults(cred *clawv1alpha1.CredentialSpec) error {
	switch cred.Type {
	case clawv1alpha1.CredentialTypeAPIKey:
		if defaults, ok := knownAPIKeyProviders[cred.Provider]; ok {
			if cred.Domain == "" {
				cred.Domain = defaults.Domain
			}
			if cred.APIKey == nil {
				cred.APIKey = &clawv1alpha1.APIKeyConfig{Header: defaults.Header}
			}
		}

	case clawv1alpha1.CredentialTypeGCP:
		if cred.Domain == "" {
			cred.Domain = ".googleapis.com"
		}

	case clawv1alpha1.CredentialTypeKubernetes:
		// Domains are derived from the kubeconfig, not the domain field
		return nil
	}

	if cred.Domain == "" {
		return fmt.Errorf("credential %q: domain is required (no default for provider %q with type %q)", cred.Name, cred.Provider, cred.Type)
	}
	if cred.Type == clawv1alpha1.CredentialTypeAPIKey && cred.APIKey == nil {
		return fmt.Errorf("credential %q: apiKey config is required (no default for provider %q)", cred.Name, cred.Provider)
	}
	return nil
}

// providerInfo holds the resolved upstream host and base path for a provider's gateway route.
type providerInfo struct {
	Upstream string
	BasePath string
}

// resolveProviderInfo returns the upstream and base path for a credential's provider.
// GCP credentials route through Vertex AI with the provider name as the publisher
// (works for google, anthropic, meta, etc.). Google + apiKey uses the Gemini REST API.
// All other combos: upstream = domain, basePath = "".
func resolveProviderInfo(cred clawv1alpha1.CredentialSpec) providerInfo {
	if cred.Type == clawv1alpha1.CredentialTypeGCP && cred.GCP != nil {
		return providerInfo{
			Upstream: "https://" + cred.GCP.Location + "-aiplatform.googleapis.com",
			BasePath: "/v1/projects/" + cred.GCP.Project + "/locations/" + cred.GCP.Location + "/publishers/" + cred.Provider,
		}
	}

	if cred.Provider == "google" {
		return providerInfo{
			Upstream: "https://generativelanguage.googleapis.com",
			BasePath: "/v1beta",
		}
	}

	domain := strings.TrimPrefix(cred.Domain, ".")
	return providerInfo{Upstream: "https://" + domain}
}

// builtinPassthroughDomains are domains the proxy always allows without credential
// injection. OpenClaw's gateway fetches model pricing from OpenRouter's public API
// to power cost estimation in the UI.
var builtinPassthroughDomains = []string{
	"openrouter.ai",
}

// generateProxyConfig builds the proxy config JSON from resolved credentials.
// Exact-match domains are emitted before suffix-match domains for predictable matching.
func generateProxyConfig(credentials []resolvedCredential) ([]byte, error) {
	var exact, suffix []proxyRoute

	coveredDomains := make(map[string]bool)
	for _, rc := range credentials {
		coveredDomains[strings.ToLower(rc.Domain)] = true
	}

	for _, domain := range builtinPassthroughDomains {
		if !coveredDomains[domain] {
			exact = append(exact, proxyRoute{Domain: domain, Injector: "none"})
		}
	}

	for _, rc := range credentials {
		cred := rc.CredentialSpec

		if cred.Type == clawv1alpha1.CredentialTypeKubernetes {
			if rc.KubeConfig == nil {
				continue
			}
			kubeconfigPath := "/etc/proxy/credentials/" + cred.Name + "/kubeconfig"
			for _, cluster := range rc.KubeConfig.Clusters {
				route := proxyRoute{
					Domain:         cluster.Hostname + ":" + cluster.Port,
					Injector:       "kubernetes",
					KubeconfigPath: kubeconfigPath,
					DefaultHeaders: cred.DefaultHeaders,
				}
				if len(cluster.CAData) > 0 {
					route.CACert = base64.StdEncoding.EncodeToString(cluster.CAData)
				}
				exact = append(exact, route)
			}
			continue
		}

		route := proxyRoute{
			Domain:         cred.Domain,
			DefaultHeaders: cred.DefaultHeaders,
		}

		switch cred.Type {
		case clawv1alpha1.CredentialTypeAPIKey:
			route.Injector = "api_key"
			route.EnvVar = credEnvVarName(cred.Name)
			if cred.APIKey != nil {
				route.Header = cred.APIKey.Header
				route.ValuePrefix = cred.APIKey.ValuePrefix
			}
		case clawv1alpha1.CredentialTypeBearer:
			route.Injector = "bearer"
			route.EnvVar = credEnvVarName(cred.Name)
		case clawv1alpha1.CredentialTypeGCP:
			route.Injector = "gcp"
			route.SAFilePath = "/etc/proxy/credentials/" + cred.Name + "/sa-key.json"
			if cred.GCP != nil {
				route.GCPProject = cred.GCP.Project
				route.GCPLocation = cred.GCP.Location
			}
		case clawv1alpha1.CredentialTypeNone:
			route.Injector = "none"
		case clawv1alpha1.CredentialTypePathToken:
			route.Injector = "path_token"
			route.EnvVar = credEnvVarName(cred.Name)
			if cred.PathToken != nil {
				route.PathPrefix = cred.PathToken.Prefix
			}
		case clawv1alpha1.CredentialTypeOAuth2:
			route.Injector = "oauth2"
			route.EnvVar = credEnvVarName(cred.Name)
			if cred.OAuth2 != nil {
				route.ClientID = cred.OAuth2.ClientID
				route.TokenURL = cred.OAuth2.TokenURL
				route.Scopes = cred.OAuth2.Scopes
			}
		}

		// Configure gateway routing when provider is set.
		// PathToken routes are excluded because they use PathPrefix for token injection.
		// Vertex SDK providers (GCP + non-Google) are excluded because the native SDK
		// talks directly to *.googleapis.com through the MITM proxy.
		if cred.Provider != "" && cred.Type != clawv1alpha1.CredentialTypePathToken && !usesVertexSDK(cred) {
			info := resolveProviderInfo(cred)
			route.PathPrefix = "/" + strings.ToLower(cred.Name)
			route.Upstream = info.Upstream
		}

		if strings.HasPrefix(cred.Domain, ".") {
			suffix = append(suffix, route)
		} else {
			exact = append(exact, route)
		}
	}

	// Stable ordering: exact before suffix, alphabetical within each group
	sort.Slice(exact, func(i, j int) bool { return exact[i].Domain < exact[j].Domain })
	sort.Slice(suffix, func(i, j int) bool { return suffix[i].Domain < suffix[j].Domain })

	cfg := proxyConfig{Routes: append(exact, suffix...)}
	return json.Marshal(cfg)
}

// applyProxyConfigMap creates or updates the proxy config ConfigMap with the precomputed JSON.
func (r *ClawResourceReconciler) applyProxyConfigMap(ctx context.Context, instance *clawv1alpha1.Claw, configJSON []byte) error {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	cm.SetName(ClawProxyConfigMapName)
	cm.SetNamespace(instance.Namespace)
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	cm.Data = map[string]string{
		"proxy-config.json": string(configJSON),
	}

	if err := controllerutil.SetControllerReference(instance, cm, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on proxy config: %w", err)
	}

	if err := r.Patch(ctx, cm, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	}); err != nil {
		return fmt.Errorf("failed to apply proxy config: %w", err)
	}

	logger.Info("Applied proxy config ConfigMap")
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
	if !hasVertexSDKCredentials(resolvedCreds) {
		existing := &corev1.ConfigMap{}
		if err := r.Get(ctx, client.ObjectKey{Name: ClawVertexADCConfigMapName, Namespace: instance.Namespace}, existing); err == nil {
			log.FromContext(ctx).Info("Cleaning up orphaned Vertex ADC ConfigMap")
			return r.Delete(ctx, existing)
		}
		return nil
	}

	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	cm.SetName(ClawVertexADCConfigMapName)
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

// configureProxyImage overrides the proxy Deployment's container image.
// If image is empty, the embedded default is preserved.
func configureProxyImage(objects []*unstructured.Unstructured, image string) error {
	if image == "" {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != ClawProxyDeploymentName {
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
			if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawProxyContainerName {
				cm["image"] = image
				containers[i] = cm
				return unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers")
			}
		}
		return fmt.Errorf("container %q not found in proxy deployment", ClawProxyContainerName)
	}
	return fmt.Errorf("claw-proxy deployment not found in manifests")
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

// configureProxyForCredentials adds credential env vars and volume mounts to the
// claw-proxy Deployment based on resolved credentials. This modifies the parsed
// kustomize objects in-place before they are applied via SSA.
func configureProxyForCredentials(objects []*unstructured.Unstructured, credentials []resolvedCredential) error {
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != ClawProxyDeploymentName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from proxy deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in proxy deployment")
		}

		containerIdx := -1
		var container map[string]any
		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawProxyContainerName {
				containerIdx = i
				container = cm
				break
			}
		}
		if containerIdx < 0 {
			return fmt.Errorf("container %q not found in proxy deployment", ClawProxyContainerName)
		}

		envVars, _, _ := unstructured.NestedSlice(container, "env")
		volumeMounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
		volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")

		for _, rc := range credentials {
			cred := rc.CredentialSpec
			switch cred.Type {
			case clawv1alpha1.CredentialTypeAPIKey, clawv1alpha1.CredentialTypeBearer,
				clawv1alpha1.CredentialTypePathToken, clawv1alpha1.CredentialTypeOAuth2:
				if cred.SecretRef == nil {
					continue
				}
				envVars = append(envVars, map[string]any{
					"name": credEnvVarName(cred.Name),
					"valueFrom": map[string]any{
						"secretKeyRef": map[string]any{
							"name": cred.SecretRef.Name,
							"key":  cred.SecretRef.Key,
						},
					},
				})

			case clawv1alpha1.CredentialTypeGCP:
				if cred.SecretRef == nil {
					continue
				}
				volName := "cred-" + cred.Name
				volumes = append(volumes, map[string]any{
					"name": volName,
					"secret": map[string]any{
						"secretName": cred.SecretRef.Name,
						"items": []any{
							map[string]any{
								"key":  cred.SecretRef.Key,
								"path": "sa-key.json",
							},
						},
					},
				})
				volumeMounts = append(volumeMounts, map[string]any{
					"name":      volName,
					"mountPath": "/etc/proxy/credentials/" + cred.Name,
					"readOnly":  true,
				})

			case clawv1alpha1.CredentialTypeKubernetes:
				if cred.SecretRef == nil {
					continue
				}
				volName := "cred-" + cred.Name
				volumes = append(volumes, map[string]any{
					"name": volName,
					"secret": map[string]any{
						"secretName": cred.SecretRef.Name,
						"items": []any{
							map[string]any{
								"key":  cred.SecretRef.Key,
								"path": "kubeconfig",
							},
						},
					},
				})
				volumeMounts = append(volumeMounts, map[string]any{
					"name":      volName,
					"mountPath": "/etc/proxy/credentials/" + cred.Name,
					"readOnly":  true,
				})
			}
		}

		if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
			return fmt.Errorf("failed to set env vars: %w", err)
		}
		if err := unstructured.SetNestedSlice(container, volumeMounts, "volumeMounts"); err != nil {
			return fmt.Errorf("failed to set volume mounts: %w", err)
		}
		containers[containerIdx] = container
		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("failed to set containers: %w", err)
		}
		if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
			return fmt.Errorf("failed to set volumes: %w", err)
		}

		return nil
	}
	return fmt.Errorf("claw-proxy deployment not found in manifests")
}

// configureClawDeploymentForVertex adds Vertex AI environment variables and the stub
// ADC volume mount to the claw (gateway) deployment when any credential uses the
// native Vertex SDK (GCP + non-Google provider). The stub ADC allows google-auth-library
// to bootstrap token refresh, which the MITM proxy intercepts with real credentials.
func configureClawDeploymentForVertex(objects []*unstructured.Unstructured, credentials []resolvedCredential) error {
	var vertexCreds []clawv1alpha1.CredentialSpec
	for _, rc := range credentials {
		if usesVertexSDK(rc.CredentialSpec) {
			vertexCreds = append(vertexCreds, rc.CredentialSpec)
		}
	}
	if len(vertexCreds) == 0 {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != ClawDeploymentName {
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
				"name": ClawVertexADCConfigMapName,
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

// configureClawDeploymentForKubernetes mounts the sanitized kubeconfig ConfigMap and
// sets KUBECONFIG env var on the claw (gateway) deployment when a kubernetes credential is present.
func configureClawDeploymentForKubernetes(objects []*unstructured.Unstructured, credentials []resolvedCredential) error {
	if !hasKubernetesCredentials(credentials) {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != ClawDeploymentName {
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
			"name":  "KUBECONFIG",
			"value": "/etc/kube/config",
		})

		volumeMounts = append(volumeMounts, map[string]any{
			"name":      "kube-config",
			"mountPath": "/etc/kube",
			"readOnly":  true,
		})
		volumes = append(volumes, map[string]any{
			"name": "kube-config",
			"configMap": map[string]any{
				"name": ClawKubeConfigMapName,
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

// stampProxyConfigHash adds a hash annotation to the proxy pod template to trigger
// rollouts when the proxy config changes.
func stampProxyConfigHash(objects []*unstructured.Unstructured, hash string) error {
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != ClawProxyDeploymentName {
			continue
		}

		annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[clawv1alpha1.AnnotationKeyProxyConfigHash] = hash

		if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
			return fmt.Errorf("failed to set pod template annotations: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw-proxy deployment not found for config hash stamping")
}

// stampSecretVersionAnnotation fetches each credential's referenced Secret and stamps
// its ResourceVersion as a pod template annotation. This ensures that when Secret data
// changes (without any Claw CR spec change), the pod template differs and Kubernetes
// triggers a rolling update.
func (r *ClawResourceReconciler) stampSecretVersionAnnotation(
	ctx context.Context,
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	versions := make(map[string]string)
	for _, cred := range instance.Spec.Credentials {
		if cred.SecretRef == nil {
			continue
		}
		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: instance.Namespace,
			Name:      cred.SecretRef.Name,
		}, secret); err != nil {
			return fmt.Errorf("failed to get Secret %q for credential %q: %w", cred.SecretRef.Name, cred.Name, err)
		}
		versions[cred.Name] = secret.ResourceVersion
	}

	if len(versions) == 0 {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != ClawProxyDeploymentName {
			continue
		}

		annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
		if annotations == nil {
			annotations = make(map[string]string)
		}
		for credName, rv := range versions {
			annotations[clawv1alpha1.AnnotationPrefixSecretVersion+credName+clawv1alpha1.AnnotationSuffixSecretVersion] = rv
		}
		if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
			return fmt.Errorf("failed to set secret version annotations: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw-proxy deployment not found for secret version stamping")
}

// applyProxyCA ensures the proxy CA Secret exists with a valid CA certificate and key.
// If the Secret is missing or lacks valid data, a new P-256 ECDSA CA is generated.
func (r *ClawResourceReconciler) applyProxyCA(ctx context.Context, instance *clawv1alpha1.Claw) error {
	logger := log.FromContext(ctx)

	existing := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: ClawProxyCACertSecretName}, existing)
	if err == nil {
		if len(existing.Data["ca.crt"]) > 0 && len(existing.Data["ca.key"]) > 0 {
			logger.Info("Proxy CA secret already exists, skipping generation")
			return nil
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing proxy CA secret: %w", err)
	}

	certPEM, keyPEM, err := generateCACertificate()
	if err != nil {
		return fmt.Errorf("failed to generate proxy CA: %w", err)
	}

	secret := &corev1.Secret{}
	secret.SetName(ClawProxyCACertSecretName)
	secret.SetNamespace(instance.Namespace)
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	secret.Data = map[string][]byte{
		"ca.crt": certPEM,
		"ca.key": keyPEM,
	}

	if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on proxy CA secret: %w", err)
	}

	if err := r.Patch(ctx, secret, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	}); err != nil {
		return fmt.Errorf("failed to apply proxy CA secret: %w", err)
	}

	logger.Info("Generated and applied proxy CA secret")
	return nil
}

// generateCACertificate creates a self-signed CA certificate and private key.
// Returns PEM-encoded cert and key bytes.
func generateCACertificate() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Claw Operator"},
			CommonName:   "Claw Proxy CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	certBuf := &bytes.Buffer{}
	if err := pem.Encode(certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return nil, nil, fmt.Errorf("failed to PEM-encode certificate: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal CA key: %w", err)
	}
	keyBuf := &bytes.Buffer{}
	if err := pem.Encode(keyBuf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		return nil, nil, fmt.Errorf("failed to PEM-encode key: %w", err)
	}

	return certBuf.Bytes(), keyBuf.Bytes(), nil
}
