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
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
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

// Proxy injector identifiers used in route configs.
const (
	injectorAPIKey     = "api_key"
	injectorBearer     = "bearer"
	injectorGCP        = "gcp"
	injectorNone       = "none"
	injectorPathToken  = "path_token"
	injectorOAuth2     = "oauth2"
	injectorKubernetes = "kubernetes"
)

// proxyRoute is a single route entry in the proxy config JSON.
type proxyRoute struct {
	Domain         string            `json:"domain"`
	Injector       string            `json:"injector"`
	Header         string            `json:"header,omitempty"`
	ValuePrefix    string            `json:"valuePrefix,omitempty"`
	EnvVar         string            `json:"envVar,omitempty"`
	CredentialFile string            `json:"credentialFile,omitempty"`
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
	AllowedPaths   []string          `json:"allowedPaths,omitempty"`
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

func vaultValue(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func vaultKVVersion(version int) string {
	if version == 0 {
		return VaultDefaultKVVersion
	}
	return fmt.Sprint(version)
}

func vaultCredentialFileName(cred clawv1alpha1.CredentialSpec) string {
	name := strings.ToLower(cred.Name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteByte('-')
		default:
			b.WriteByte('-')
		}
	}
	file := strings.Trim(b.String(), "-")
	if file == "" {
		file = "credential"
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(cred.Name)))[:8]
	if len(file) > 54 {
		file = file[:54]
	}
	return file + "-" + hash
}

func vaultCredentialFilePath(cred clawv1alpha1.CredentialSpec) string {
	return VaultAgentSecretsPath + "/" + vaultCredentialFileName(cred)
}

func vaultDataPath(vault *clawv1alpha1.VaultSpec, ref clawv1alpha1.VaultRefEntry) (string, string, error) {
	parsed, err := parseVaultRefID(ref.ID)
	if err != nil {
		return "", "", err
	}
	mount := strings.Trim(vaultValue(vault.KVMount, VaultDefaultKVMount), "/")
	if mount == "" {
		return "", "", fmt.Errorf("vault kvMount must not be empty")
	}
	if vaultKVVersion(vault.KVVersion) == "2" {
		return mount + "/data/" + parsed.secretPath, parsed.field, nil
	}
	return mount + "/" + parsed.secretPath, parsed.field, nil
}

func vaultAgentTemplate(vault *clawv1alpha1.VaultSpec, ref clawv1alpha1.VaultRefEntry) (string, error) {
	secretPath, field, err := vaultDataPath(vault, ref)
	if err != nil {
		return "", err
	}
	data := ".Data"
	if vaultKVVersion(vault.KVVersion) == "2" {
		data = ".Data.data"
	}
	return fmt.Sprintf(`{{- with secret %q -}}{{- index %s %q -}}{{- end -}}`, secretPath, data, field), nil
}

// builtinPassthrough defines a domain the proxy always allows without credential injection.
type builtinPassthrough struct {
	Domain       string
	AllowedPaths []string
}

// builtinPassthroughDomains are domains the proxy always allows without credential
// injection. These support core gateway functionality:
//   - clawhub.ai: ClawHub plugin registry for plugin installs and skill downloads
//   - openrouter.ai: model pricing API for cost estimation in the UI
//   - github.com + codeload.github.com: git HTTPS clones and tarball fetches for npm packages with git dependencies (e.g. @whiskeysockets/baileys → libsignal-node)
//   - raw.githubusercontent.com: LiteLLM model pricing data and WhatsApp Baileys library defaults (path-restricted)
//   - registry.npmjs.org: npm packages for plugin runtime dependencies
var builtinPassthroughDomains = []builtinPassthrough{
	{Domain: "clawhub.ai"},
	{Domain: "openrouter.ai"},
	{Domain: "github.com"},
	{Domain: "codeload.github.com"},
	{Domain: "raw.githubusercontent.com", AllowedPaths: []string{"/BerriAI/litellm/", "/WhiskeySockets/Baileys/"}},
	{Domain: "registry.npmjs.org"},
}

// generateProxyConfig builds the proxy config JSON from resolved credentials.
// HTTP MCP server URLs are auto-extracted as passthrough routes when not already
// covered by a credential or builtin domain.
// Exact-match domains are emitted before suffix-match domains for predictable matching.
func generateProxyConfig(
	credentials []resolvedCredential,
	mcpServers map[string]clawv1alpha1.McpServerSpec,
	webSearch *clawv1alpha1.WebSearchSpec,
) ([]byte, error) {
	var exact []proxyRoute

	coveredDomains := make(map[string]bool)
	for _, rc := range credentials {
		coveredDomains[strings.ToLower(rc.Domain)] = true
	}

	for _, bp := range builtinPassthroughDomains {
		if !coveredDomains[bp.Domain] {
			coveredDomains[bp.Domain] = true
			exact = append(exact, proxyRoute{Domain: bp.Domain, Injector: injectorNone, AllowedPaths: bp.AllowedPaths})
		}
	}

	if wsRoute, ok := webSearchRoute(webSearch); ok {
		wsDomain := strings.ToLower(wsRoute.Domain)
		if !domainCovered(wsDomain, coveredDomains) {
			coveredDomains[wsDomain] = true
			exact = append(exact, wsRoute)
		}
	}

	mcpCredRoutes, err := mcpCredentialRoutes(mcpServers, credentials, coveredDomains)
	if err != nil {
		return nil, err
	}
	exact = append(exact, mcpCredRoutes...)

	exact = append(exact, mcpPassthroughRoutes(mcpServers, coveredDomains)...)

	credExact, credSuffix := credentialRoutes(credentials)
	exact = append(exact, credExact...)
	suffix := make([]proxyRoute, 0, len(credSuffix))
	suffix = append(suffix, credSuffix...)

	// Deterministic ordering: exact before suffix, alphabetical within each group.
	// Within the same domain, routes with AllowedPaths sort before catch-all routes
	// so the proxy's MatchRoute picks the specific route first.
	// Uses SliceStable + Injector tie-breaker to guarantee identical output across reconciles.
	routeLess := func(a, b proxyRoute) bool {
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if (len(a.AllowedPaths) > 0) != (len(b.AllowedPaths) > 0) {
			return len(a.AllowedPaths) > 0
		}
		return a.Injector < b.Injector
	}
	sort.SliceStable(exact, func(i, j int) bool { return routeLess(exact[i], exact[j]) })
	sort.SliceStable(suffix, func(i, j int) bool { return routeLess(suffix[i], suffix[j]) })

	cfg := proxyConfig{Routes: append(exact, suffix...)}
	return json.Marshal(cfg)
}

// credentialRoutes builds proxy routes from resolved credentials, returning
// exact-match and suffix-match routes separately for deterministic ordering.
func credentialRoutes(credentials []resolvedCredential) (exact, suffix []proxyRoute) {
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
					Injector:       injectorKubernetes,
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

		if cred.Domain != "" {
			route := buildCredentialRoute(cred)

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

		for _, companion := range generateCompanionRoutes(cred) {
			if strings.HasPrefix(companion.Domain, ".") {
				suffix = append(suffix, companion)
			} else {
				exact = append(exact, companion)
			}
		}
	}
	return exact, suffix
}

// buildCredentialRoute maps a credential spec to its proxy route with the
// correct injector, env var, and type-specific fields.
func buildCredentialRoute(cred clawv1alpha1.CredentialSpec) proxyRoute {
	route := proxyRoute{
		Domain:         cred.Domain,
		DefaultHeaders: cred.DefaultHeaders,
		AllowedPaths:   cred.AllowedPaths,
	}
	applyProxyCredentialSource := func() {
		if proxyVaultRefForCredential(cred) != nil {
			route.CredentialFile = vaultCredentialFilePath(cred)
			return
		}
		route.EnvVar = credEnvVarName(cred.Name)
	}

	switch cred.Type {
	case clawv1alpha1.CredentialTypeAPIKey:
		route.Injector = injectorAPIKey
		applyProxyCredentialSource()
		if cred.APIKey != nil {
			route.Header = cred.APIKey.Header
			route.ValuePrefix = cred.APIKey.ValuePrefix
		}
	case clawv1alpha1.CredentialTypeBearer:
		route.Injector = injectorBearer
		applyProxyCredentialSource()
	case clawv1alpha1.CredentialTypeGCP:
		route.Injector = injectorGCP
		if proxyVaultRefForCredential(cred) != nil {
			route.CredentialFile = vaultCredentialFilePath(cred)
		} else {
			route.SAFilePath = "/etc/proxy/credentials/" + cred.Name + "/sa-key.json"
		}
		if cred.GCP != nil {
			route.GCPProject = cred.GCP.Project
			route.GCPLocation = cred.GCP.Location
		}
	case clawv1alpha1.CredentialTypeNone:
		route.Injector = injectorNone
	case clawv1alpha1.CredentialTypePathToken:
		route.Injector = injectorPathToken
		applyProxyCredentialSource()
		if cred.PathToken != nil {
			route.PathPrefix = cred.PathToken.Prefix
		}
	case clawv1alpha1.CredentialTypeOAuth2:
		route.Injector = injectorOAuth2
		applyProxyCredentialSource()
		if cred.OAuth2 != nil {
			route.ClientID = cred.OAuth2.ClientID
			route.TokenURL = cred.OAuth2.TokenURL
			route.Scopes = cred.OAuth2.Scopes
		}
	}

	return route
}

// webSearchRoute builds a proxy route for the web search provider, if applicable.
// Returns false for LLM-as-search providers (e.g., gemini) and unknown providers.
func webSearchRoute(webSearch *clawv1alpha1.WebSearchSpec) (proxyRoute, bool) {
	if webSearch == nil {
		return proxyRoute{}, false
	}
	info, ok := knownSearchProviders[webSearch.Provider]
	if !ok {
		return proxyRoute{}, false
	}
	route := proxyRoute{
		Domain:   info.Domain,
		Injector: info.Injector,
	}
	switch info.Injector {
	case injectorAPIKey:
		route.EnvVar = credEnvVarName(webSearchCredPrefix)
		route.Header = info.Header
	case injectorBearer:
		route.EnvVar = credEnvVarName(webSearchCredPrefix)
	}
	return route, true
}

// mcpPassthroughRoutes extracts domains from HTTP MCP server URLs and returns
// passthrough routes for domains not already covered by credentials or builtins.
// Suffix-match credentials (e.g. ".googleapis.com") cover subdomains, so an MCP
// URL like "https://us-central1-aiplatform.googleapis.com/..." won't emit a
// redundant exact route that would shadow the credential's auth injection.
func mcpPassthroughRoutes(
	mcpServers map[string]clawv1alpha1.McpServerSpec,
	coveredDomains map[string]bool,
) []proxyRoute {
	var routes []proxyRoute
	for _, mcp := range mcpServers {
		if mcp.URL == "" || mcp.CredentialRef != "" {
			continue
		}
		parsed, err := url.Parse(mcp.URL)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		domain := strings.ToLower(parsed.Hostname())
		if domainCovered(domain, coveredDomains) {
			continue
		}
		coveredDomains[domain] = true
		routes = append(routes, proxyRoute{Domain: domain, Injector: injectorNone})
	}
	return routes
}

// mcpCredentialRoutes builds credential-injecting proxy routes for HTTP MCP
// servers that declare a credentialRef. The referenced credential is looked up
// from the resolved credentials list.
func mcpCredentialRoutes(
	mcpServers map[string]clawv1alpha1.McpServerSpec,
	credentials []resolvedCredential,
	coveredDomains map[string]bool,
) ([]proxyRoute, error) {
	credByName := make(map[string]resolvedCredential, len(credentials))
	for _, rc := range credentials {
		credByName[rc.Name] = rc
	}

	var routes []proxyRoute
	for serverName, mcp := range mcpServers {
		if mcp.URL == "" || mcp.CredentialRef == "" {
			continue
		}
		rc, ok := credByName[mcp.CredentialRef]
		if !ok {
			return nil, fmt.Errorf(
				"MCP server %q references credential %q which does not exist in spec.credentials",
				serverName, mcp.CredentialRef,
			)
		}

		parsed, err := url.Parse(mcp.URL)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		domain := strings.ToLower(parsed.Hostname())
		if domainCovered(domain, coveredDomains) {
			continue
		}
		coveredDomains[domain] = true

		route := buildCredentialRoute(rc.CredentialSpec)
		route.Domain = domain
		route.AllowedPaths = nil
		route.DefaultHeaders = nil
		routes = append(routes, route)
	}
	return routes, nil
}

// domainCovered returns true if domain is already covered by an exact entry or
// a suffix entry (leading dot) in coveredDomains. This prevents MCP passthrough
// routes from shadowing credential injection on suffix-matched domains.
func domainCovered(domain string, coveredDomains map[string]bool) bool {
	if coveredDomains[domain] {
		return true
	}
	for covered := range coveredDomains {
		if strings.HasPrefix(covered, ".") && strings.HasSuffix(domain, covered) {
			return true
		}
	}
	return false
}

// applyProxyConfigMap creates or updates the proxy config ConfigMap with the precomputed JSON.
func (r *ClawResourceReconciler) applyProxyConfigMap(ctx context.Context, instance *clawv1alpha1.Claw, configJSON []byte) error {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	cm.SetName(getProxyConfigMapName(instance.Name))
	cm.SetNamespace(instance.Namespace)
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	setInstanceLabel(cm, instance.Name)
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

// configureProxyImage overrides the proxy Deployment's container image.
// If image is empty, the embedded default is preserved.
func configureProxyImage(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw, image string) error {
	if image == "" {
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

// configureProxyForCredentials adds credential env vars and volume mounts to the
// claw-proxy Deployment based on resolved credentials. This modifies the parsed
// kustomize objects in-place before they are applied via SSA.
func configureProxyForCredentials(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw, credentials []resolvedCredential) error {
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
			ref := proxySecretForCredential(cred)
			switch cred.Type {
			case clawv1alpha1.CredentialTypeAPIKey, clawv1alpha1.CredentialTypeBearer,
				clawv1alpha1.CredentialTypePathToken, clawv1alpha1.CredentialTypeOAuth2:
				if credentialUsesVault(cred) {
					continue
				}
				if ref == nil {
					continue
				}
				envVars = append(envVars, map[string]any{
					"name": credEnvVarName(cred.Name),
					"valueFrom": map[string]any{
						"secretKeyRef": map[string]any{
							"name": ref.Name,
							"key":  ref.Key,
						},
					},
				})

			case clawv1alpha1.CredentialTypeGCP:
				if credentialUsesVault(cred) {
					continue
				}
				if ref == nil {
					continue
				}
				volName := "cred-" + cred.Name
				volumes = append(volumes, map[string]any{
					"name": volName,
					"secret": map[string]any{
						"secretName": ref.Name,
						"items": []any{
							map[string]any{
								"key":  ref.Key,
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
				if ref == nil {
					continue
				}
				volName := "cred-" + cred.Name
				volumes = append(volumes, map[string]any{
					"name": volName,
					"secret": map[string]any{
						"secretName": ref.Name,
						"items": []any{
							map[string]any{
								"key":  ref.Key,
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

func anyCredentialUsesVault(credentials []resolvedCredential) bool {
	for _, rc := range credentials {
		if credentialUsesVault(rc.CredentialSpec) {
			return true
		}
	}
	return false
}

func configureProxyForVault(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw, credentials []resolvedCredential) error {
	if instance.Spec.Vault == nil || !anyCredentialUsesVault(credentials) {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(instance.GetName()) {
			continue
		}

		vault := instance.Spec.Vault
		if vault.AuthRole == "" {
			return fmt.Errorf("vault.authRole is required for Vault Agent auth")
		}

		if err := unstructured.SetNestedField(obj.Object, instance.Name, "spec", "template", "spec", "serviceAccountName"); err != nil {
			return fmt.Errorf("failed to set serviceAccountName on proxy deployment: %w", err)
		}
		if err := unstructured.SetNestedField(obj.Object, true, "spec", "template", "spec", "automountServiceAccountToken"); err != nil {
			return fmt.Errorf("failed to enable service account token automount on proxy deployment: %w", err)
		}

		annotations, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["vault.hashicorp.com/agent-inject"] = "true"
		annotations["vault.hashicorp.com/agent-inject-status"] = "update"
		annotations["vault.hashicorp.com/role"] = vault.AuthRole
		annotations["vault.hashicorp.com/auth-type"] = "kubernetes"
		annotations["vault.hashicorp.com/auth-path"] = "auth/kubernetes"
		if vault.Namespace != "" {
			annotations["vault.hashicorp.com/namespace"] = vault.Namespace
		}

		for _, rc := range credentials {
			cred := rc.CredentialSpec
			ref := proxyVaultRefForCredential(cred)
			if ref == nil {
				continue
			}
			fileName := vaultCredentialFileName(cred)
			secretPath, _, err := vaultDataPath(vault, *ref)
			if err != nil {
				return fmt.Errorf("credential %q: %w", cred.Name, err)
			}
			template, err := vaultAgentTemplate(vault, *ref)
			if err != nil {
				return fmt.Errorf("credential %q: %w", cred.Name, err)
			}
			annotations["vault.hashicorp.com/agent-inject-secret-"+fileName] = secretPath
			annotations["vault.hashicorp.com/agent-inject-template-"+fileName] = template
			annotations["vault.hashicorp.com/agent-inject-file-"+fileName] = fileName
		}

		if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
			return fmt.Errorf("failed to set Vault Agent annotations: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw-proxy deployment not found in manifests")
}

// stampProxyConfigHash adds a hash annotation to the proxy pod template to trigger
// rollouts when the proxy config changes.
func stampProxyConfigHash(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw, hash string) error {
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(instance.GetName()) {
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
		for _, ref := range cred.SecretRef {
			secret := &corev1.Secret{}
			if err := r.UserSecretReader.Get(ctx, client.ObjectKey{
				Namespace: instance.Namespace,
				Name:      ref.Name,
			}, secret); err != nil {
				return fmt.Errorf("failed to get Secret %q for credential %q: %w", ref.Name, cred.Name, err)
			}
			key := cred.Name
			if ref.Role != "" {
				key = cred.Name + "-" + ref.Role
			}
			versions[key] = secret.ResourceVersion
		}
	}

	if ws := instance.Spec.WebSearch; ws != nil && ws.SecretRef != nil {
		secret := &corev1.Secret{}
		if err := r.UserSecretReader.Get(ctx, client.ObjectKey{
			Namespace: instance.Namespace,
			Name:      ws.SecretRef.Name,
		}, secret); err != nil {
			return fmt.Errorf("failed to get Secret %q for web search: %w", ws.SecretRef.Name, err)
		}
		versions[webSearchCredPrefix] = secret.ResourceVersion
	}

	if len(versions) == 0 {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(instance.GetName()) {
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
func (r *ClawResourceReconciler) applyProxyCA(ctx context.Context, instance *clawv1alpha1.Claw) ([]byte, error) {
	logger := log.FromContext(ctx)
	secretName := getProxyCAConfigMapName(instance.Name)

	existing := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: secretName}, existing)
	if err == nil {
		if len(existing.Data["ca.crt"]) > 0 && len(existing.Data["ca.key"]) > 0 {
			logger.Info("Proxy CA secret already exists, skipping generation")
			return existing.Data["ca.crt"], nil
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check for existing proxy CA secret: %w", err)
	}

	certPEM, keyPEM, err := generateCACertificate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate proxy CA: %w", err)
	}

	secret := &corev1.Secret{}
	secret.SetName(secretName)
	secret.SetNamespace(instance.Namespace)
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	setInstanceLabel(secret, instance.Name)
	secret.Data = map[string][]byte{
		"ca.crt": certPEM,
		"ca.key": keyPEM,
	}

	if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference on proxy CA secret: %w", err)
	}

	if err := r.Patch(ctx, secret, client.Apply, &client.PatchOptions{
		FieldManager: "claw-operator",
		Force:        ptr.To(true),
	}); err != nil {
		return nil, fmt.Errorf("failed to apply proxy CA secret: %w", err)
	}

	logger.Info("Generated and applied proxy CA secret")
	return certPEM, nil
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
