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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
	"github.com/codeready-toolchain/claw-operator/internal/assets"
)

const (
	ClawResourceKind = "Claw"
	ClawInstanceName = "instance"

	// Core resources
	ClawConfigMapName            = "claw-config"
	ClawPVCName                  = "claw-home-pvc"
	ClawNetworkPolicyName        = "claw-egress"
	ClawIngressNetworkPolicyName = "claw-ingress"
	ClawRouteName                = "claw"
	ClawServiceName              = "claw"
	ClawDeploymentName           = "claw"
	ClawGatewaySecretName        = "claw-gateway-token"
	GatewayTokenKeyName          = "token"
	ClawProxyServiceName         = "claw-proxy"
	ClawProxyConfigMapName       = "claw-proxy-config"
	ClawProxyDeploymentName      = "claw-proxy"
	ClawProxyCACertSecretName    = "claw-proxy-ca"
	ClawProxyContainerName       = "proxy"
	ClawGatewayContainerName     = "gateway"
	ClawVertexADCConfigMapName   = "claw-vertex-adc"
	// Kubernetes resource kinds
	RouteKind      = "Route"
	DeploymentKind = "Deployment"
	ConfigMapKind  = "ConfigMap"
)

// ClawResourceReconciler reconciles all resources for Claw
type ClawResourceReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ProxyImage      string
	ImagePullPolicy string
}

// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=claws,verbs=get;list;watch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=claws/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=claws/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages the complete lifecycle of resources for Claw instances
func (r *ClawResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Claw", "name", req.Name, "namespace", req.Namespace)

	// Fetch the Claw resource
	instance := &clawv1alpha1.Claw{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Claw resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Claw")
		return ctrl.Result{}, err
	}

	// Only reconcile resources named "instance"
	if instance.Name != ClawInstanceName {
		logger.Info("Skipping reconciliation for Claw with non-matching name", "name", instance.Name)
		return ctrl.Result{}, nil
	}

	// Create or update the gateway Secret with token
	if err := r.applyGatewaySecret(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply gateway secret")
		return ctrl.Result{}, err
	}

	// Resolve provider defaults (domain, apiKey) for known providers before validation
	for i := range instance.Spec.Credentials {
		if err := resolveProviderDefaults(&instance.Spec.Credentials[i]); err != nil {
			logger.Error(err, "Failed to resolve provider defaults")
			setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				logger.Error(statusErr, "Failed to update status after provider defaults failure")
			}
			return ctrl.Result{}, err
		}
	}

	// Validate all credential entries (Secrets exist, type-specific config present)
	if err := r.validateCredentials(ctx, instance); err != nil {
		logger.Error(err, "Credential validation failed")
		setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
		setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after credential validation failure")
		}
		return ctrl.Result{}, err
	}
	setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionTrue, clawv1alpha1.ConditionReasonResolved, "All credential Secrets are valid")

	// Ensure proxy CA certificate exists for MITM proxy
	if err := r.applyProxyCA(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply proxy CA")
		return ctrl.Result{}, err
	}

	// Generate proxy config, apply ConfigMaps (proxy config + Vertex AI stub ADC)
	proxyConfigJSON, err := r.applyProxyResources(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Build kustomized objects
	objects, err := r.buildKustomizedObjects()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Apply deployment overrides (proxy image, pull policy, credentials)
	if err := r.configureDeployments(objects, instance); err != nil {
		return ctrl.Result{}, err
	}

	// Stamp proxy config hash to trigger rollout on config changes
	proxyConfigHash := fmt.Sprintf("%x", sha256.Sum256(proxyConfigJSON))
	if err := stampProxyConfigHash(objects, proxyConfigHash); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to stamp proxy config hash: %w", err)
	}

	// Stamp Secret ResourceVersions to trigger rollout when Secret data changes
	if err := r.stampSecretVersionAnnotation(ctx, objects, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to stamp secret version annotations: %w", err)
	}

	// Apply Route and wait for ingress host to be populated
	var routeHost string
	var routeApplied int
	routeApplied, err = r.applyRouteOnly(ctx, objects, instance)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to apply Route: %w", err)
	}

	// Only try to fetch Route URL if Route was actually applied (CRD available)
	if routeApplied > 0 {
		routeHost, err = r.getRouteURL(ctx, instance)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get Route URL: %w", err)
		}
		if routeHost == "" {
			// Route exists but status not yet populated - requeue
			logger.Info("Route exists but status not populated, requeuing")
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
	} else {
		// Route CRD not registered - proceed with localhost fallback
		logger.Info("Route CRD not registered, using localhost fallback for CORS")
	}

	// Phase 3: Inject Route host into ConfigMap and apply remaining resources

	// Inject Route host into ConfigMap
	if err := r.injectRouteHostIntoConfigMap(objects, routeHost); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject Route host into ConfigMap: %w", err)
	}

	// Inject LLM providers into ConfigMap based on credentials with Provider set
	if err := injectProvidersIntoConfigMap(objects, instance.Spec.Credentials); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject providers into ConfigMap: %w", err)
	}

	// Filter out Route (applied in phase above) and proxy ConfigMap (controller-managed)
	remainingObjects := []*unstructured.Unstructured{}
	for _, obj := range objects {
		if obj.GetKind() == RouteKind {
			continue
		}
		if obj.GetKind() == ConfigMapKind && obj.GetName() == ClawProxyConfigMapName {
			continue
		}
		remainingObjects = append(remainingObjects, obj)
	}

	// Set namespace and owner references
	for _, obj := range remainingObjects {
		obj.SetNamespace(instance.Namespace)
		if err := controllerutil.SetControllerReference(instance, obj, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set controller reference: %w", err)
		}
	}

	// Apply remaining resources (ConfigMap, Deployments, Services, NetworkPolicies)
	if _, err := r.applyResources(ctx, remainingObjects); err != nil {
		return ctrl.Result{}, err
	}

	// Update status based on deployment readiness
	if err := r.updateStatus(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status (will retry): %w", err)
	}

	return ctrl.Result{}, nil
}

// configureDeployments applies deployment overrides (proxy image, pull policy, credentials)
func (r *ClawResourceReconciler) configureDeployments(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
) error {
	if err := configureProxyImage(objects, r.ProxyImage); err != nil {
		return fmt.Errorf("failed to configure proxy image: %w", err)
	}
	if err := configureImagePullPolicy(objects, r.ImagePullPolicy); err != nil {
		return fmt.Errorf("failed to configure image pull policy: %w", err)
	}
	if err := configureProxyForCredentials(objects, instance.Spec.Credentials); err != nil {
		return fmt.Errorf("failed to configure proxy deployment for credentials: %w", err)
	}
	if err := configureClawDeploymentForVertex(objects, instance.Spec.Credentials); err != nil {
		return fmt.Errorf("failed to configure claw deployment for Vertex AI: %w", err)
	}
	return nil
}

// applyProxyResources generates the proxy config, applies the proxy ConfigMap and
// (when needed) the Vertex AI stub ADC ConfigMap. Returns the proxy config JSON
// for use in config hash stamping.
func (r *ClawResourceReconciler) applyProxyResources(ctx context.Context, instance *clawv1alpha1.Claw) ([]byte, error) {
	logger := log.FromContext(ctx)

	proxyConfigJSON, err := generateProxyConfig(instance.Spec.Credentials)
	if err != nil {
		logger.Error(err, "Failed to generate proxy config")
		setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after proxy config failure")
		}
		return nil, err
	}

	if err := r.applyProxyConfigMap(ctx, instance, proxyConfigJSON); err != nil {
		logger.Error(err, "Failed to apply proxy config")
		setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after proxy config failure")
		}
		return nil, err
	}
	setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionTrue, clawv1alpha1.ConditionReasonConfigured, "Proxy config generated successfully")

	if err := r.applyVertexADCConfigMap(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply Vertex ADC config")
		return nil, err
	}

	return proxyConfigJSON, nil
}

func (r *ClawResourceReconciler) buildKustomizedObjects() ([]*unstructured.Unstructured, error) {
	// Write all manifest files (including kustomization.yaml) to in-memory filesystem
	fs := filesys.MakeFsInMemory()
	manifestFiles := map[string][]byte{
		"manifests/kustomization.yaml":         readEmbeddedFile("manifests/kustomization.yaml"),
		"manifests/configmap.yaml":             readEmbeddedFile("manifests/configmap.yaml"),
		"manifests/pvc.yaml":                   readEmbeddedFile("manifests/pvc.yaml"),
		"manifests/deployment.yaml":            readEmbeddedFile("manifests/deployment.yaml"),
		"manifests/service.yaml":               readEmbeddedFile("manifests/service.yaml"),
		"manifests/route.yaml":                 readEmbeddedFile("manifests/route.yaml"),
		"manifests/proxy-configmap.yaml":       readEmbeddedFile("manifests/proxy-configmap.yaml"),
		"manifests/proxy-deployment.yaml":      readEmbeddedFile("manifests/proxy-deployment.yaml"),
		"manifests/proxy-service.yaml":         readEmbeddedFile("manifests/proxy-service.yaml"),
		"manifests/networkpolicy.yaml":         readEmbeddedFile("manifests/networkpolicy.yaml"),
		"manifests/ingress-networkpolicy.yaml": readEmbeddedFile("manifests/ingress-networkpolicy.yaml"),
	}
	for path, content := range manifestFiles {
		if err := fs.WriteFile(path, content); err != nil {
			return nil, fmt.Errorf("failed to write manifest to in-memory filesystem: %w", err)
		}
	}

	// Build manifests using Kustomize
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := kustomizer.Run(fs, "manifests")
	if err != nil {
		return nil, fmt.Errorf("failed to run kustomize build: %w", err)
	}
	// Convert resource map to unstructured objects
	resources, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource map to YAML: %w", err)
	}
	// Parse YAML into unstructured objects
	objects, err := parseYAMLToObjects(resources)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML to objects: %w", err)
	}

	return objects, nil
}

// applyResources applies a list of unstructured objects using server-side apply
// Returns the number of resources successfully applied (excluding skipped resources)
func (r *ClawResourceReconciler) applyResources(ctx context.Context, objects []*unstructured.Unstructured) (int, error) {
	logger := log.FromContext(ctx)
	appliedCount := 0

	for _, obj := range objects {
		if err := r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
			FieldManager: "claw-operator",
			Force:        ptr.To(true),
		}); err != nil {
			// Skip resources whose CRDs are not registered (e.g., Route on non-OpenShift clusters)
			if meta.IsNoMatchError(err) {
				logger.Info("Skipping resource - CRD not registered in cluster", "kind", obj.GetKind(), "name", obj.GetName())
				continue
			}
			return 0, fmt.Errorf("failed to apply resource: %w", err)
		}
		appliedCount++
	}
	logger.Info("Successfully applied resources", "count", appliedCount)
	return appliedCount, nil
}

// applyRouteOnly applies only the Route resource from provided objects
// Returns number of routes applied (0 if CRD not registered)
func (r *ClawResourceReconciler) applyRouteOnly(ctx context.Context, objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) (int, error) {
	// Handle empty objects safely (len() on nil slice returns 0)
	if len(objects) == 0 {
		return 0, nil
	}

	// Filter for Route only
	routeObjects := []*unstructured.Unstructured{}
	for _, obj := range objects {
		if obj.GetKind() == RouteKind {
			routeObjects = append(routeObjects, obj)
		}
	}

	// Set namespace and owner references
	for _, obj := range routeObjects {
		obj.SetNamespace(instance.Namespace)
		if err := controllerutil.SetControllerReference(instance, obj, r.Scheme); err != nil {
			return 0, fmt.Errorf("failed to set controller reference: %w", err)
		}
	}

	// Apply Route and return count
	return r.applyResources(ctx, routeObjects)
}

// injectRouteHostIntoConfigMap replaces OPENCLAW_ROUTE_HOST placeholder in ConfigMap with actual Route host
// If routeHost is empty (vanilla Kubernetes), uses localhost fallback
func (r *ClawResourceReconciler) injectRouteHostIntoConfigMap(objects []*unstructured.Unstructured, routeHost string) error {
	// Determine replacement value
	replacement := routeHost
	if replacement == "" {
		// Vanilla Kubernetes fallback
		replacement = "http://localhost:18789"
	}

	// Find ConfigMap in objects
	for _, obj := range objects {
		if obj.GetKind() == ConfigMapKind && obj.GetName() == ClawConfigMapName {
			// Extract openclaw.json data
			openclawJSON, found, err := unstructured.NestedString(obj.Object, "data", "openclaw.json")
			if err != nil {
				return fmt.Errorf("failed to extract openclaw.json from ConfigMap: %w", err)
			}
			if !found {
				return fmt.Errorf("openclaw.json not found in ConfigMap data")
			}

			// Replace placeholder with Route host
			updatedJSON := strings.ReplaceAll(openclawJSON, "OPENCLAW_ROUTE_HOST", replacement)

			// Set modified JSON back into ConfigMap
			if err := unstructured.SetNestedField(obj.Object, updatedJSON, "data", "openclaw.json"); err != nil {
				return fmt.Errorf("failed to set updated openclaw.json in ConfigMap: %w", err)
			}

			return nil
		}
	}

	return fmt.Errorf("ConfigMap %s not found in manifests", ClawConfigMapName)
}

// injectProvidersIntoConfigMap dynamically builds the models.providers section of openclaw.json
// from credentials that have Provider set. Gateway-routed providers get a baseUrl pointing to
// the proxy. Vertex SDK providers (GCP + non-Google) get the real Vertex AI URL since traffic
// flows through the MITM proxy which injects GCP credentials transparently.
func injectProvidersIntoConfigMap(objects []*unstructured.Unstructured, credentials []clawv1alpha1.CredentialSpec) error {
	providers := map[string]any{}
	for _, cred := range credentials {
		if cred.Provider == "" {
			continue
		}
		// PathToken uses PathPrefix for token injection in the URL path (e.g., /bot<TOKEN>/...),
		// not for gateway routing — skip provider entry to avoid referencing a non-existent gateway route.
		if cred.Type == clawv1alpha1.CredentialTypePathToken {
			continue
		}

		if usesVertexSDK(cred) {
			providerKey := cred.Provider + "-vertex"
			if _, exists := providers[providerKey]; exists {
				return fmt.Errorf("duplicate provider %q in credentials", providerKey)
			}
			entry := map[string]any{
				"baseUrl": "https://" + cred.GCP.Location + "-aiplatform.googleapis.com",
				"apiKey":  "gcp-vertex-credentials",
				"models":  []any{},
			}
			if api, ok := vertexProviderAPIMapping[cred.Provider]; ok {
				entry["api"] = api
			}
			providers[providerKey] = entry
		} else {
			if _, exists := providers[cred.Provider]; exists {
				return fmt.Errorf("duplicate provider %q in credentials", cred.Provider)
			}
			info := resolveProviderInfo(cred)
			baseURL := "http://claw-proxy:8080/" + strings.ToLower(cred.Name) + info.BasePath
			providers[cred.Provider] = map[string]any{
				"baseUrl": baseURL,
				"apiKey":  "ah-ah-ah-you-didnt-say-the-magic-word",
				"models":  []any{},
			}
		}
	}

	for _, obj := range objects {
		if obj.GetKind() != ConfigMapKind || obj.GetName() != ClawConfigMapName {
			continue
		}

		openclawJSON, found, err := unstructured.NestedString(obj.Object, "data", "openclaw.json")
		if err != nil {
			return fmt.Errorf("failed to extract openclaw.json from ConfigMap: %w", err)
		}
		if !found {
			return fmt.Errorf("openclaw.json not found in ConfigMap data")
		}

		var config map[string]any
		if err := json.Unmarshal([]byte(openclawJSON), &config); err != nil {
			return fmt.Errorf("failed to parse openclaw.json: %w", err)
		}

		models, _ := config["models"].(map[string]any)
		if models == nil {
			models = map[string]any{}
			config["models"] = models
		}
		models["providers"] = providers

		remapVertexProviderModels(config, providers)
		filterAgentDefaultsForProviders(config, providers)

		updatedJSON, err := json.MarshalIndent(config, "    ", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal openclaw.json: %w", err)
		}

		if err := unstructured.SetNestedField(obj.Object, string(updatedJSON), "data", "openclaw.json"); err != nil {
			return fmt.Errorf("failed to set updated openclaw.json in ConfigMap: %w", err)
		}
		return nil
	}

	return fmt.Errorf("ConfigMap %s not found in manifests", ClawConfigMapName)
}

// remapVertexProviderModels renames model entries in agents.defaults from "provider/model"
// to "provider-vertex/model" when a Vertex variant exists but the base provider does not.
// For example, "anthropic/claude-sonnet-4-6" becomes "anthropic-vertex/claude-sonnet-4-6"
// when anthropic-vertex is configured but anthropic is not.
func remapVertexProviderModels(config map[string]any, providers map[string]any) {
	agents, _ := config["agents"].(map[string]any)
	if agents == nil {
		return
	}
	defaults, _ := agents["defaults"].(map[string]any)
	if defaults == nil {
		return
	}

	if modelMap, ok := defaults["models"].(map[string]any); ok {
		for modelName, val := range modelMap {
			providerKey, modelID, ok := strings.Cut(modelName, "/")
			if !ok {
				continue
			}
			vertexKey := providerKey + "-vertex"
			_, hasBase := providers[providerKey]
			_, hasVertex := providers[vertexKey]
			if !hasBase && hasVertex {
				delete(modelMap, modelName)
				modelMap[vertexKey+"/"+modelID] = val
			}
		}
	}

	if primary, _ := defaults["model"].(map[string]any); primary != nil {
		if primaryName, _ := primary["primary"].(string); primaryName != "" {
			providerKey, modelID, ok := strings.Cut(primaryName, "/")
			if ok {
				vertexKey := providerKey + "-vertex"
				_, hasBase := providers[providerKey]
				_, hasVertex := providers[vertexKey]
				if !hasBase && hasVertex {
					primary["primary"] = vertexKey + "/" + modelID
				}
			}
		}
	}
}

// filterAgentDefaultsForProviders removes model entries from agents.defaults whose
// provider (the part before "/" in the model name) is not in the injected providers map,
// and clears agents.defaults.model.primary if its provider is not available.
func filterAgentDefaultsForProviders(config map[string]any, providers map[string]any) {
	agents, _ := config["agents"].(map[string]any)
	if agents == nil {
		return
	}
	defaults, _ := agents["defaults"].(map[string]any)
	if defaults == nil {
		return
	}

	if modelMap, ok := defaults["models"].(map[string]any); ok {
		var available []string
		for modelName := range modelMap {
			providerKey, _, _ := strings.Cut(modelName, "/")
			if _, exists := providers[providerKey]; !exists {
				delete(modelMap, modelName)
			} else {
				available = append(available, modelName)
			}
		}
		sort.Strings(available)

		if primary, _ := defaults["model"].(map[string]any); primary != nil {
			if primaryName, _ := primary["primary"].(string); primaryName != "" {
				providerKey, _, _ := strings.Cut(primaryName, "/")
				if _, exists := providers[providerKey]; !exists {
					if len(available) > 0 {
						primary["primary"] = available[0]
					} else {
						primary["primary"] = ""
					}
				}
			}
		}
	}
}

// readEmbeddedFile reads a file from the embedded filesystem
func readEmbeddedFile(path string) []byte {
	data, err := assets.ManifestsFS.ReadFile(path)
	if err != nil {
		// Return empty if file not found - will be caught during kustomize build
		return []byte{}
	}
	return data
}

// parseYAMLToObjects parses multi-document YAML into unstructured objects
func parseYAMLToObjects(yamlData []byte) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	// Split YAML documents by separator
	docs := bytes.Split(yamlData, []byte("\n---\n"))
	for _, doc := range docs {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(doc, &obj.Object); err != nil {
			return nil, err
		}

		if len(obj.Object) > 0 {
			objects = append(objects, obj)
		}
	}

	return objects, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClawResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1alpha1.Claw{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appsv1.Deployment{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findClawsReferencingSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("claw").
		Complete(r)
}

// findClawsReferencingSecret maps a Secret to all Claw CRs that reference it
func (r *ClawResourceReconciler) findClawsReferencingSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	// Skip operator-managed secrets (claw-gateway-token for gateway token)
	if secret.Name == ClawGatewaySecretName {
		return nil
	}

	// List all Claw CRs in the same namespace
	openClawList := &clawv1alpha1.ClawList{}
	if err := r.List(ctx, openClawList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	// Find Claw CRs that reference this Secret
	var requests []reconcile.Request
	for _, instance := range openClawList.Items {
		if instance.Name != ClawInstanceName {
			continue
		}

		for _, cred := range instance.Spec.Credentials {
			if cred.SecretRef != nil && cred.SecretRef.Name == secret.Name {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      instance.Name,
						Namespace: instance.Namespace,
					},
				})
				break
			}
		}
	}

	return requests
}
