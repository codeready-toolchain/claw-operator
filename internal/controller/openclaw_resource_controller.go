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

	// Create or update the gateway secrets Secret with token
	if err := r.applyGatewaySecret(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply gateway secret")
		return ctrl.Result{}, err
	}

	// Validate that the referenced Gemini API key Secret exists
	if err := r.validateGeminiAPIKeySecret(ctx, instance); err != nil {
		logger.Error(err, "Failed to validate Gemini API key Secret")
		// Set status condition for Secret-related errors
		r.setSecretErrorCondition(instance, err)
		// Update status before returning
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after Secret error")
		}
		return ctrl.Result{}, err
	}

	// Apply all resources via Kustomize and server-side apply
	if err := r.applyKustomizedResources(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	// Patch the proxy deployment to reference the user's Gemini API key Secret
	if err := r.patchProxyDeploymentWithSecretRef(ctx, instance); err != nil {
		logger.Error(err, "Failed to patch proxy deployment with Secret reference")
		return ctrl.Result{}, err
	}

	// Update status based on deployment readiness
	if err := r.updateStatus(ctx, instance); err != nil {
		logger.Error(err, "Failed to update status, will retry")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// applyKustomizedResources builds manifests using Kustomize and applies them via server-side apply
func (r *OpenClawResourceReconciler) applyKustomizedResources(ctx context.Context, instance *openclawv1alpha1.OpenClaw) error {
	logger := log.FromContext(ctx)

	// Build manifests using Kustomize
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())

	// Create an in-memory filesystem from embedded assets
	fs := filesys.MakeFsInMemory()

	// Write all manifest files (including kustomization.yaml) to in-memory filesystem
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
			logger.Error(err, "Failed to write manifest to in-memory filesystem", "path", path)
			return err
		}
	}

	// Run kustomize build
	resMap, err := kustomizer.Run(fs, "manifests")
	if err != nil {
		logger.Error(err, "Failed to run kustomize build")
		return err
	}

	// Convert resource map to unstructured objects
	resources, err := resMap.AsYaml()
	if err != nil {
		logger.Error(err, "Failed to convert resource map to YAML")
		return err
	}

	logger.Info("Successfully built manifests with kustomize", "resourceCount", resMap.Size())

	// Parse YAML into unstructured objects
	objects, err := parseYAMLToObjects(resources)
	if err != nil {
		logger.Error(err, "Failed to parse YAML to objects")
		return err
	}

	// Transform resources: set namespace and owner references
	for _, obj := range objects {
		// Set namespace to match instance
		obj.SetNamespace(instance.Namespace)

		// Set owner reference
		if err := controllerutil.SetControllerReference(instance, obj, r.Scheme); err != nil {
			logger.Error(err, "Failed to set controller reference", "resource", obj.GetName())
			return err
		}
	}

	// Apply resources using server-side apply
	for _, obj := range objects {
		logger.Info("Applying resource", "kind", obj.GetKind(), "name", obj.GetName())

		err := r.Patch(ctx, obj, client.Apply, &client.PatchOptions{
			FieldManager: "openclaw-operator",
			Force:        &[]bool{true}[0],
		})
		if err != nil {
			// Skip resources whose CRDs are not registered (e.g., Route on non-OpenShift clusters)
			if meta.IsNoMatchError(err) {
				logger.Info("Skipping resource - CRD not registered in cluster", "kind", obj.GetKind(), "name", obj.GetName())
				continue
			}
			logger.Error(err, "Failed to apply resource", "kind", obj.GetKind(), "name", obj.GetName())
			return err
		}
	}

	logger.Info("Successfully applied all resources", "count", len(objects))
	return nil
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
	err := r.Get(ctx, secretKey, existingSecret)

	var token string
	if err == nil {
		// Secret exists - check if it has the token entry
		if existingToken, exists := existingSecret.Data[GatewayTokenKeyName]; exists && len(existingToken) > 0 {
			logger.Info("Gateway secret already exists with token, skipping generation", "name", OpenClawGatewaySecretName)
			token = string(existingToken)
		} else {
			// Secret exists but missing token - generate new one
			logger.Info("Gateway secret exists but missing token, generating new one")
			token, err = generateGatewayToken()
			if err != nil {
				logger.Error(err, "Failed to generate gateway token")
				return err
			}
		}
	} else if apierrors.IsNotFound(err) {
		// Secret doesn't exist - generate new token
		logger.Info("Gateway secret does not exist, generating new token")
		token, err = generateGatewayToken()
		if err != nil {
			logger.Error(err, "Failed to generate gateway token")
			return err
		}
	} else {
		// Error fetching secret
		logger.Error(err, "Failed to check for existing gateway secret")
		return err
	}

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
		logger.Error(err, "Failed to set controller reference on gateway secret")
		return err
	}

	// Apply the Secret using server-side apply
	logger.Info("Applying gateway secret", "name", secret.Name)
	err = r.Patch(ctx, secret, client.Apply, &client.PatchOptions{
		FieldManager: "openclaw-operator",
		Force:        &[]bool{true}[0],
	})
	if err != nil {
		logger.Error(err, "Failed to apply gateway secret")
		return err
	}

	logger.Info("Successfully applied gateway secret")
	return nil
}

// validateGeminiAPIKeySecret validates that the referenced Gemini API key Secret exists and has the required key
func (r *OpenClawResourceReconciler) validateGeminiAPIKeySecret(ctx context.Context, instance *openclawv1alpha1.OpenClaw) error {
	logger := log.FromContext(ctx)

	// Note: GeminiAPIKey, Name, and Key are all enforced as non-nil/non-empty by admission validation
	secretRef := instance.Spec.GeminiAPIKey

	// Fetch the referenced Secret
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      secretRef.Name,
	}

	if err := r.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Referenced Secret not found", "secret", secretRef.Name)
			return fmt.Errorf("secret %s not found in namespace %s", secretRef.Name, instance.Namespace)
		}
		return fmt.Errorf("failed to get secret %s: %w", secretRef.Name, err)
	}

	// Validate the API key exists in the Secret data
	apiKeyBytes, exists := secret.Data[secretRef.Key]
	if !exists {
		logger.Info("API key not found in Secret", "secret", secretRef.Name, "key", secretRef.Key)
		return fmt.Errorf("key %s not found in secret %s", secretRef.Key, secretRef.Name)
	}

	if len(apiKeyBytes) == 0 {
		return fmt.Errorf("key %s in secret %s is empty", secretRef.Key, secretRef.Name)
	}

	logger.Info("Successfully validated Gemini API key Secret", "secret", secretRef.Name, "key", secretRef.Key)
	return nil
}

// patchProxyDeploymentWithSecretRef patches the openclaw-proxy deployment to reference the user's Gemini API key Secret
func (r *OpenClawResourceReconciler) patchProxyDeploymentWithSecretRef(ctx context.Context, instance *openclawv1alpha1.OpenClaw) error {
	logger := log.FromContext(ctx)

	if instance.Spec.GeminiAPIKey == nil {
		return fmt.Errorf("geminiAPIKey field is required")
	}

	secretRef := instance.Spec.GeminiAPIKey

	// Fetch the deployment
	deployment := &appsv1.Deployment{}
	deploymentKey := client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      "openclaw-proxy",
	}

	if err := r.Get(ctx, deploymentKey, deployment); err != nil {
		logger.Error(err, "Failed to get openclaw-proxy deployment")
		return err
	}

	// Find the proxy container and update the GEMINI_API_KEY env var
	updated := false
	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		if container.Name == "proxy" {
			// Find or add the GEMINI_API_KEY env var
			envVarFound := false
			for j := range container.Env {
				if container.Env[j].Name == OpenClawProxyDeploymentGeminiAPiKeyEnvKey {
					// Update existing env var to reference the user's Secret
					container.Env[j].ValueFrom = &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretRef.Name,
							},
							Key:      secretRef.Key,
							Optional: &[]bool{false}[0],
						},
					}
					envVarFound = true
					updated = true
					break
				}
			}

			// If env var doesn't exist, add it
			if !envVarFound {
				container.Env = append(container.Env, corev1.EnvVar{
					Name: OpenClawProxyDeploymentGeminiAPiKeyEnvKey,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretRef.Name,
							},
							Key:      secretRef.Key,
							Optional: &[]bool{false}[0],
						},
					},
				})
				updated = true
			}
			break
		}
	}

	if !updated {
		return fmt.Errorf("proxy container not found in openclaw-proxy deployment")
	}

	// Update the deployment
	logger.Info("Patching openclaw-proxy deployment with Secret reference", "secret", secretRef.Name, "key", secretRef.Key)
	if err := r.Update(ctx, deployment); err != nil {
		logger.Error(err, "Failed to update openclaw-proxy deployment")
		return err
	}

	logger.Info("Successfully patched openclaw-proxy deployment")
	return nil
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
		Kind:    "Route",
	})

	err := r.Get(ctx, client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      "openclaw",
	}, route)

	if err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			// Route not found (or CRD not registered on non-OpenShift clusters)
			logger.Info("Route not found or CRD not registered", "name", "openclaw")
			return "", nil
		}
		logger.Error(err, "Failed to get Route")
		return "", err
	}

	// Extract host from Route.Spec.Host
	host, found, err := unstructured.NestedString(route.Object, "spec", "host")
	if err != nil {
		logger.Error(err, "Failed to extract host from Route")
		return "", err
	}
	if !found || host == "" {
		logger.Info("Route host not found or empty")
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

// setSecretErrorCondition sets the Available condition based on Secret-related errors
func (r *OpenClawResourceReconciler) setSecretErrorCondition(instance *openclawv1alpha1.OpenClaw, err error) {
	var reason, message string
	errMsg := err.Error()

	// Detect type of Secret error based on error message
	// Check for key-specific errors first (more specific match)
	if containsString(errMsg, "key") && containsString(errMsg, "not found") {
		reason = "SecretKeyNotFound"
		message = fmt.Sprintf("Key not found in Secret: %v", err)
	} else if containsString(errMsg, "not found in namespace") || containsString(errMsg, "not found") {
		reason = "SecretNotFound"
		message = fmt.Sprintf("Referenced Secret not found: %v", err)
	} else {
		reason = "SecretError"
		message = fmt.Sprintf("Error accessing Secret: %v", err)
	}

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.Generation,
	})
}

// containsString checks if a string contains a substring (case-insensitive helper)
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && bytes.Contains([]byte(s), []byte(substr))))
}

// updateStatus updates the OpenClaw status with current deployment conditions
func (r *OpenClawResourceReconciler) updateStatus(ctx context.Context, instance *openclawv1alpha1.OpenClaw) error {
	logger := log.FromContext(ctx)

	// Check deployment readiness
	ready, pending, err := r.checkDeploymentsReady(ctx, instance.Namespace)
	if err != nil {
		logger.Error(err, "Failed to check deployment readiness")
		return err
	}

	// Set Available condition
	setAvailableCondition(instance, ready, pending)

	// Populate URL field only when both deployments are ready
	if ready {
		url, err := r.getRouteURL(ctx, instance)
		if err != nil {
			logger.Error(err, "Failed to get Route URL")
			return err
		}
		instance.Status.URL = url
	} else {
		// Clear URL when deployments are not ready
		instance.Status.URL = ""
	}

	// Update status subresource
	if err := r.Status().Update(ctx, instance); err != nil {
		logger.Error(err, "Failed to update OpenClaw status")
		return err
	}

	logger.Info("Successfully updated OpenClaw status", "available", ready, "url", instance.Status.URL)
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
