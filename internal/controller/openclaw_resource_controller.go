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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
	"github.com/codeready-toolchain/openclaw-operator/internal/assets"
)

const (
	OpenClawResourceKind                      = "OpenClaw"
	OpenClawInstanceName                      = "instance"
	OpenClawConfigMapName                     = "openclaw-config"
	OpenClawPVCName                           = "openclaw-home-pvc"
	OpenClawDeploymentName                    = "openclaw"
	OpenClawGatewaySecretName                 = "openclaw-secrets"
	GatewayTokenKeyName                       = "OPENCLAW_GATEWAY_TOKEN"
	OpenClawProxyDeploymentName               = "openclaw-proxy"
	OpenClawProxyDeploymentContainerName      = "proxy"
	OpenClawProxyDeploymentGeminiAPiKeyEnvKey = "GEMINI_API_KEY"

	// Kubernetes resource kinds
	RouteKind      = "Route"
	DeploymentKind = "Deployment"
	ConfigMapKind  = "ConfigMap"
)

// OpenClawResourceReconciler reconciles all resources for OpenClaw
type OpenClawResourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=openclaw.sandbox.redhat.com,resources=openclaws,verbs=get;list;watch
// +kubebuilder:rbac:groups=openclaw.sandbox.redhat.com,resources=openclaws/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openclaw.sandbox.redhat.com,resources=openclaws/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages the complete lifecycle of resources for OpenClaw instances
func (r *OpenClawResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling OpenClaw", "name", req.Name, "namespace", req.Namespace)

	// Fetch the OpenClaw resource
	instance := &openclawv1alpha1.OpenClaw{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("OpenClaw resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get OpenClaw")
		return ctrl.Result{}, err
	}

	// Only reconcile resources named "instance"
	if instance.Name != OpenClawInstanceName {
		logger.Info("Skipping reconciliation for OpenClaw with non-matching name", "name", instance.Name)
		return ctrl.Result{}, nil
	}

	// Phase 1: Create or update the gateway secrets Secret with token
	if err := r.applyGatewaySecret(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply gateway secret")
		return ctrl.Result{}, err
	}

	// Build kustomized objects once (before Phase 2)
	objects, err := r.buildKustomizedObjects(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Phase 2: Apply Route and wait for ingress host to be populated
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

	// Filter for remaining resources (non-Route)
	remainingObjects := []*unstructured.Unstructured{}
	for _, obj := range objects {
		if obj.GetKind() != RouteKind {
			remainingObjects = append(remainingObjects, obj)
		}
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

// buildKustomizedObjects builds Kustomize manifests and returns parsed objects with proxy configuration
func (r *OpenClawResourceReconciler) buildKustomizedObjects(ctx context.Context, instance *openclawv1alpha1.OpenClaw) ([]*unstructured.Unstructured, error) {
	// Write all manifest files (including kustomization.yaml) to in-memory filesystem
	fs := filesys.MakeFsInMemory()
	manifestFiles := map[string][]byte{
		"manifests/kustomization.yaml":    readEmbeddedFile("manifests/kustomization.yaml"),
		"manifests/configmap.yaml":        readEmbeddedFile("manifests/configmap.yaml"),
		"manifests/pvc.yaml":              readEmbeddedFile("manifests/pvc.yaml"),
		"manifests/deployment.yaml":       readEmbeddedFile("manifests/deployment.yaml"),
		"manifests/service.yaml":          readEmbeddedFile("manifests/service.yaml"),
		"manifests/route.yaml":            readEmbeddedFile("manifests/route.yaml"),
		"manifests/proxy-configmap.yaml":  readEmbeddedFile("manifests/proxy-configmap.yaml"),
		"manifests/proxy-deployment.yaml": readEmbeddedFile("manifests/proxy-deployment.yaml"),
		"manifests/proxy-service.yaml":    readEmbeddedFile("manifests/proxy-service.yaml"),
		"manifests/networkpolicy.yaml":    readEmbeddedFile("manifests/networkpolicy.yaml"),
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
	// Configure proxy deployment with user's Gemini API key Secret reference
	if err := r.configureProxyDeployment(objects, instance); err != nil {
		return nil, fmt.Errorf("failed to configure proxy deployment: %w", err)
	}
	// Stamp Secret version annotation to trigger restarts on Secret value changes
	if err := r.stampSecretVersionAnnotation(ctx, objects, instance); err != nil {
		return nil, fmt.Errorf("failed to stamp Secret version annotation: %w", err)
	}

	return objects, nil
}

// applyResources applies a list of unstructured objects using server-side apply
// Returns the number of resources successfully applied (excluding skipped resources)
func (r *OpenClawResourceReconciler) applyResources(ctx context.Context, objects []*unstructured.Unstructured) (int, error) {
	logger := log.FromContext(ctx)
	appliedCount := 0

	for _, obj := range objects {
		if err := r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
			FieldManager: "openclaw-operator",
			Force:        &[]bool{true}[0],
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
func (r *OpenClawResourceReconciler) applyRouteOnly(ctx context.Context, objects []*unstructured.Unstructured, instance *openclawv1alpha1.OpenClaw) (int, error) {
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
func (r *OpenClawResourceReconciler) injectRouteHostIntoConfigMap(objects []*unstructured.Unstructured, routeHost string) error {
	// Determine replacement value
	replacement := routeHost
	if replacement == "" {
		// Vanilla Kubernetes fallback
		replacement = "http://localhost:18789"
	}

	// Find ConfigMap in objects
	for _, obj := range objects {
		if obj.GetKind() == ConfigMapKind && obj.GetName() == OpenClawConfigMapName {
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

	return fmt.Errorf("ConfigMap %s not found in manifests", OpenClawConfigMapName)
}

// generateGatewayToken generates a cryptographically secure random token
// using crypto/rand. Returns a 64-character hex string (32 random bytes).
func generateGatewayToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

// applyGatewaySecret creates or updates the openclaw-secrets Secret with the gateway token
func (r *OpenClawResourceReconciler) applyGatewaySecret(ctx context.Context, instance *openclawv1alpha1.OpenClaw) error {
	logger := log.FromContext(ctx)

	// Check if the secret already exists
	existingSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      OpenClawGatewaySecretName,
	}
	if err := r.Get(ctx, secretKey, existingSecret); err == nil {
		// Secret exists - check if it has the token entry
		if existingToken, exists := existingSecret.Data[GatewayTokenKeyName]; exists && len(existingToken) > 0 {
			logger.Info("Gateway secret already exists with token, skipping generation", "name", OpenClawGatewaySecretName)
			// no need to generate new token, just ensure owner reference is set
			return r.doCreateGatewaySecret(ctx, instance, string(existingToken))
		} else {
			// Secret exists but missing or empty token - generate new one
			logger.Info("Gateway secret exists but missing token, generating new one")
			token, err := generateGatewayToken()
			if err != nil {
				return fmt.Errorf("failed to generate gateway token: %w", err)
			}
			return r.doCreateGatewaySecret(ctx, instance, token)
		}
	} else if apierrors.IsNotFound(err) {
		// Secret doesn't exist - generate new token
		logger.Info("Gateway secret does not exist, generating new token")
		token, err := generateGatewayToken()
		if err != nil {
			return fmt.Errorf("failed to generate gateway token: %w", err)
		}
		return r.doCreateGatewaySecret(ctx, instance, token)
	} else {
		// Error fetching secret
		return fmt.Errorf("failed to check for existing gateway secret: %w", err)
	}
}

func (r *OpenClawResourceReconciler) doCreateGatewaySecret(ctx context.Context, instance *openclawv1alpha1.OpenClaw, token string) error {
	logger := log.FromContext(ctx)
	// Create the Secret object
	secret := &corev1.Secret{}
	secret.SetName(OpenClawGatewaySecretName)
	secret.SetNamespace(instance.Namespace)
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	secret.Data = map[string][]byte{
		GatewayTokenKeyName: []byte(token),
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference on gateway secret: %w", err)
	}

	// Apply the Secret using server-side apply
	logger.Info("Applying gateway secret", "name", secret.Name)
	if err := r.Patch(ctx, secret, client.Apply, &client.PatchOptions{
		FieldManager: "openclaw-operator",
		Force:        &[]bool{true}[0],
	}); err != nil {
		return fmt.Errorf("failed to apply gateway secret: %w", err)
	}

	logger.Info("Successfully applied gateway secret")
	return nil
}

// configureProxyDeployment configures the openclaw-proxy deployment's GEMINI_API_KEY env var
// to reference the user's Secret. This is done BEFORE applying resources so pod template changes
// trigger automatic pod restarts when the Secret reference changes.
func (r *OpenClawResourceReconciler) configureProxyDeployment(objects []*unstructured.Unstructured, instance *openclawv1alpha1.OpenClaw) error {
	secretRef := instance.Spec.GeminiAPIKey

	// Find the openclaw-proxy Deployment
	for _, obj := range objects {
		if obj.GetKind() == DeploymentKind && obj.GetName() == OpenClawProxyDeploymentName {
			// Navigate to spec.template.spec.containers
			containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
			if err != nil || !found {
				return fmt.Errorf("failed to get containers from proxy deployment: %w", err)
			}

			// Find the proxy container
			for i, c := range containers {
				container, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if name, ok := container["name"].(string); ok && name == OpenClawProxyDeploymentContainerName {
					// Get env vars
					envVars, _, err := unstructured.NestedSlice(container, "env")
					if err != nil {
						return fmt.Errorf("failed to get env vars: %w", err)
					}

					// Find and update GEMINI_API_KEY
					found := false
					for j, e := range envVars {
						envVar, ok := e.(map[string]any)
						if !ok {
							continue
						}
						if envName, ok := envVar["name"].(string); ok && envName == OpenClawProxyDeploymentGeminiAPiKeyEnvKey {
							// Update the secretKeyRef
							envVar["valueFrom"] = map[string]any{
								"secretKeyRef": map[string]any{
									"name":     secretRef.Name,
									"key":      secretRef.Key,
									"optional": false,
								},
							}
							envVars[j] = envVar
							found = true
							break
						}
					}

					if !found {
						return fmt.Errorf("GEMINI_API_KEY env var not found in proxy container")
					}

					// Set the updated env vars back
					if err := unstructured.SetNestedSlice(container, envVars, "env"); err != nil {
						return fmt.Errorf("failed to set env vars: %w", err)
					}

					// Set the updated container back
					containers[i] = container
					if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
						return fmt.Errorf("failed to set containers: %w", err)
					}

					return nil
				}
			}
			return fmt.Errorf("proxy container not found in openclaw-proxy deployment")
		}
	}
	return fmt.Errorf("openclaw-proxy deployment not found in manifests")
}

// stampSecretVersionAnnotation adds an annotation to the openclaw-proxy pod template with the
// referenced Secret's ResourceVersion. This causes the pod template to change whenever the Secret
// data changes, triggering automatic pod restarts via Deployment rollout.
func (r *OpenClawResourceReconciler) stampSecretVersionAnnotation(ctx context.Context, objects []*unstructured.Unstructured, instance *openclawv1alpha1.OpenClaw) error {
	secretRef := instance.Spec.GeminiAPIKey

	// Fetch the Secret to get its ResourceVersion
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      secretRef.Name,
	}

	if err := r.Get(ctx, secretKey, secret); err != nil {
		// If Secret doesn't exist, skip stamping - pods will fail to start with clear error
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get Secret %s for version stamping: %w", secretRef.Name, err)
	}

	// Find the openclaw-proxy Deployment
	for _, obj := range objects {
		if obj.GetKind() == DeploymentKind && obj.GetName() == OpenClawProxyDeploymentName {
			// Get current pod template annotations
			annotations, _, err := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
			if err != nil {
				return fmt.Errorf("failed to get pod template annotations: %w", err)
			}

			// Initialize map if nil
			if annotations == nil {
				annotations = make(map[string]string)
			}

			// Stamp the Secret's ResourceVersion
			annotations["openclaw.sandbox.redhat.com/gemini-secret-version"] = secret.ResourceVersion

			// Set the updated annotations back
			if err := unstructured.SetNestedStringMap(obj.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
				return fmt.Errorf("failed to set pod template annotations: %w", err)
			}

			return nil
		}
	}

	return fmt.Errorf("openclaw-proxy deployment not found for Secret version stamping")
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

// getDeploymentAvailableStatus fetches a Deployment and returns whether its Available condition is True
func (r *OpenClawResourceReconciler) getDeploymentAvailableStatus(ctx context.Context, namespace, name string) (bool, error) {
	logger := log.FromContext(ctx)
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, deployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Deployment not found", "name", name)
			return false, nil
		}
		return false, err
	}

	// Check for Available condition
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable {
			return condition.Status == corev1.ConditionTrue, nil
		}
	}

	// No Available condition found
	return false, nil
}

// checkDeploymentsReady checks if both openclaw and openclaw-proxy Deployments are ready
func (r *OpenClawResourceReconciler) checkDeploymentsReady(ctx context.Context, namespace string) (bool, []string, error) {
	openclawReady, err := r.getDeploymentAvailableStatus(ctx, namespace, OpenClawDeploymentName)
	if err != nil {
		return false, nil, err
	}

	proxyReady, err := r.getDeploymentAvailableStatus(ctx, namespace, OpenClawProxyDeploymentName)
	if err != nil {
		return false, nil, err
	}

	var pending []string
	if !openclawReady {
		pending = append(pending, OpenClawDeploymentName)
	}
	if !proxyReady {
		pending = append(pending, OpenClawProxyDeploymentName)
	}

	return len(pending) == 0, pending, nil
}

// getRouteURL fetches the Route and returns the HTTPS URL, or empty string if not found
func (r *OpenClawResourceReconciler) getRouteURL(ctx context.Context, instance *openclawv1alpha1.OpenClaw) (string, error) {
	logger := log.FromContext(ctx)

	// Create an unstructured object to fetch the Route (OpenShift-specific resource)
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "route.openshift.io",
		Version: "v1",
		Kind:    RouteKind,
	})

	if err := r.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      "openclaw",
	}, route); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			// Route not found (or CRD not registered on non-OpenShift clusters)
			logger.Info("Route not found or CRD not registered", "name", "openclaw")
			return "", nil
		}
		return "", fmt.Errorf("failed to get Route: %w", err)
	}

	// Extract host from Route.Status.Ingress[0].Host (authoritative source)
	ingress, found, err := unstructured.NestedSlice(route.Object, "status", "ingress")
	if err != nil {
		return "", fmt.Errorf("failed to extract ingress from Route status: %w", err)
	}
	if !found || len(ingress) == 0 {
		// Route exists but status not yet populated by OpenShift router
		return "", nil
	}

	// Get first ingress entry (primary router)
	firstIngress, ok := ingress[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("failed to parse ingress entry")
	}

	host, found, err := unstructured.NestedString(firstIngress, "host")
	if err != nil {
		return "", fmt.Errorf("failed to extract host from ingress: %w", err)
	}
	if !found || host == "" {
		// Ingress entry exists but host not yet populated
		return "", nil
	}

	return "https://" + host, nil
}

// setAvailableCondition sets the Available condition on the OpenClaw instance based on deployment readiness
func setAvailableCondition(instance *openclawv1alpha1.OpenClaw, ready bool, pendingDeployments []string) {
	var status metav1.ConditionStatus
	var reason, message string

	if ready {
		status = metav1.ConditionTrue
		reason = "Ready"
		message = "OpenClaw instance is ready"
	} else {
		status = metav1.ConditionFalse
		reason = "Provisioning"
		if len(pendingDeployments) > 0 {
			message = "Waiting for deployments to become ready"
		} else {
			message = "Provisioning in progress"
		}
	}

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.Generation,
	})
}

// updateStatus updates the OpenClaw status with current deployment conditions
func (r *OpenClawResourceReconciler) updateStatus(ctx context.Context, instance *openclawv1alpha1.OpenClaw) error {
	// Check deployment readiness
	ready, pending, err := r.checkDeploymentsReady(ctx, instance.Namespace)
	if err != nil {
		return fmt.Errorf("failed to check deployment readiness: %w", err)
	}

	// Set Available condition
	setAvailableCondition(instance, ready, pending)

	// Populate URL field only when both deployments are ready
	if ready {
		url, err := r.getRouteURL(ctx, instance)
		if err != nil {
			return fmt.Errorf("failed to get Route URL: %w", err)
		}
		instance.Status.URL = url
	} else {
		// Clear URL when deployments are not ready
		instance.Status.URL = ""
	}

	// Update status subresource
	if err := r.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("failed to update OpenClaw status: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenClawResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openclawv1alpha1.OpenClaw{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appsv1.Deployment{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findOpenClawsReferencingSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("openclaw").
		Complete(r)
}

// findOpenClawsReferencingSecret maps a Secret to all OpenClaw CRs that reference it
func (r *OpenClawResourceReconciler) findOpenClawsReferencingSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	// Skip operator-managed secrets (openclaw-secrets for gateway token)
	if secret.Name == OpenClawGatewaySecretName {
		return nil
	}

	// List all OpenClaw CRs in the same namespace
	openClawList := &openclawv1alpha1.OpenClawList{}
	if err := r.List(ctx, openClawList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	// Find OpenClaw CRs that reference this Secret
	var requests []reconcile.Request
	for _, instance := range openClawList.Items {
		// Only reconcile instances named "instance"
		if instance.Name != OpenClawInstanceName {
			continue
		}

		// Check if this instance references the Secret
		if instance.Spec.GeminiAPIKey != nil && instance.Spec.GeminiAPIKey.Name == secret.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      instance.Name,
					Namespace: instance.Namespace,
				},
			})
		}
	}

	return requests
}
