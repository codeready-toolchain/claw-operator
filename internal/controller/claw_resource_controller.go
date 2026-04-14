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
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
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

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
	"github.com/codeready-toolchain/claw-operator/internal/assets"
)

const (
	ClawResourceKind = "Claw"
	ClawInstanceName = "instance"

	// Core resources
	ClawConfigMapName            = "openclaw-config"
	ClawPVCName                  = "openclaw-home-pvc"
	ClawNetworkPolicyName        = "openclaw-egress"
	ClawIngressNetworkPolicyName = "openclaw-ingress"
	ClawRouteName                = "openclaw"
	ClawServiceName              = "openclaw"
	ClawDeploymentName           = "openclaw"
	ClawGatewaySecretName        = "openclaw-gateway-token"
	GatewayTokenKeyName          = "token"
	ClawProxyServiceName         = "openclaw-proxy"
	ClawProxyConfigMapName       = "openclaw-proxy-config"
	ClawProxyDeploymentName      = "openclaw-proxy"
	ClawProxyCACertSecretName    = "openclaw-proxy-ca"
	ClawProxyContainerName       = "proxy"
	// Kubernetes resource kinds
	RouteKind      = "Route"
	DeploymentKind = "Deployment"
	ConfigMapKind  = "ConfigMap"
)

// ClawResourceReconciler reconciles all resources for Claw
type ClawResourceReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	ProxyImage string
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

	// Validate all credential entries (Secrets exist, type-specific config present)
	if err := r.validateCredentials(ctx, instance); err != nil {
		logger.Error(err, "Credential validation failed")
		setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
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

	// Generate proxy config JSON once; used by both applyProxyConfigMap and stampProxyConfigHash
	proxyConfigJSON, err := generateProxyConfig(instance.Spec.Credentials)
	if err != nil {
		logger.Error(err, "Failed to generate proxy config")
		setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after proxy config failure")
		}
		return ctrl.Result{}, err
	}

	if err := r.applyProxyConfigMap(ctx, instance, proxyConfigJSON); err != nil {
		logger.Error(err, "Failed to apply proxy config")
		setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after proxy config failure")
		}
		return ctrl.Result{}, err
	}
	setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionTrue, clawv1alpha1.ConditionReasonConfigured, "Proxy config generated successfully")

	// Build kustomized objects
	objects, err := r.buildKustomizedObjects()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Override proxy image if configured (set via PROXY_IMAGE env var)
	if err := configureProxyImage(objects, r.ProxyImage); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to configure proxy image: %w", err)
	}

	// Configure proxy deployment with credential env vars and volume mounts
	if err := configureProxyForCredentials(objects, instance.Spec.Credentials); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to configure proxy deployment for credentials: %w", err)
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

// buildKustomizedObjects builds Kustomize manifests and returns parsed unstructured objects
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

// generateGatewayToken generates a cryptographically secure random token
// using crypto/rand. Returns a 64-character hex string (32 random bytes).
func generateGatewayToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

// applyGatewaySecret creates or updates the openclaw-gateway-token Secret with the gateway token
func (r *ClawResourceReconciler) applyGatewaySecret(ctx context.Context, instance *clawv1alpha1.Claw) error {
	logger := log.FromContext(ctx)

	// check if the secret already exists
	existingSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Namespace: instance.Namespace,
		Name:      ClawGatewaySecretName,
	}
	if err := r.Get(ctx, secretKey, existingSecret); err == nil {
		// Secret exists - check if it has the token entry
		if existingToken, exists := existingSecret.Data[GatewayTokenKeyName]; exists && len(existingToken) > 0 {
			logger.Info("Gateway secret already exists with token, skipping generation", "name", ClawGatewaySecretName)
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

func (r *ClawResourceReconciler) doCreateGatewaySecret(ctx context.Context, instance *clawv1alpha1.Claw, token string) error {
	logger := log.FromContext(ctx)
	// Create the Secret object
	secret := &corev1.Secret{}
	secret.SetName(ClawGatewaySecretName)
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
		FieldManager: "claw-operator",
		Force:        &[]bool{true}[0],
	}); err != nil {
		return fmt.Errorf("failed to apply gateway secret: %w", err)
	}

	logger.Info("Successfully applied gateway secret")
	return nil
}

// validateCredentials validates all credential entries: checks that referenced Secrets exist
// and that type-specific configuration is present. Returns an error describing all failures.
func (r *ClawResourceReconciler) validateCredentials(ctx context.Context, instance *clawv1alpha1.Claw) error {
	var errs []error

	for _, cred := range instance.Spec.Credentials {
		// Validate SecretRef exists for types that require it
		if cred.Type != clawv1alpha1.CredentialTypeNone {
			if cred.SecretRef == nil {
				errs = append(errs, fmt.Errorf("credential %q (type %s): secretRef is required", cred.Name, cred.Type))
				continue
			}
			secret := &corev1.Secret{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: cred.SecretRef.Name}, secret); err != nil {
				if apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("credential %q: Secret %q not found", cred.Name, cred.SecretRef.Name))
				} else {
					errs = append(errs, fmt.Errorf("credential %q: failed to get Secret %q: %w", cred.Name, cred.SecretRef.Name, err))
				}
				continue
			}
			if _, ok := secret.Data[cred.SecretRef.Key]; !ok {
				errs = append(errs, fmt.Errorf("credential %q: key %q not found in Secret %q", cred.Name, cred.SecretRef.Key, cred.SecretRef.Name))
			}
		}

		// Type-specific validation (defense-in-depth beyond CEL)
		switch cred.Type {
		case clawv1alpha1.CredentialTypeAPIKey:
			if cred.APIKey == nil {
				errs = append(errs, fmt.Errorf("credential %q: apiKey config is required for type apiKey", cred.Name))
			}
		case clawv1alpha1.CredentialTypeGCP:
			if cred.GCP == nil {
				errs = append(errs, fmt.Errorf("credential %q: gcp config is required for type gcp", cred.Name))
			}
		case clawv1alpha1.CredentialTypePathToken:
			if cred.PathToken == nil {
				errs = append(errs, fmt.Errorf("credential %q: pathToken config is required for type pathToken", cred.Name))
			}
		case clawv1alpha1.CredentialTypeOAuth2:
			if cred.OAuth2 == nil {
				errs = append(errs, fmt.Errorf("credential %q: oauth2 config is required for type oauth2", cred.Name))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("credential validation failed: %w", errors.Join(errs...))
	}
	return nil
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
		Force:        &[]bool{true}[0],
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
	ClientID       string            `json:"clientID,omitempty"`
	TokenURL       string            `json:"tokenURL,omitempty"`
	Scopes         []string          `json:"scopes,omitempty"`
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`
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

// generateProxyConfig builds the proxy config JSON from spec.credentials[].
// Exact-match domains are emitted before suffix-match domains for predictable matching.
func generateProxyConfig(credentials []clawv1alpha1.CredentialSpec) ([]byte, error) {
	var exact, suffix []proxyRoute

	for _, cred := range credentials {
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
		Force:        &[]bool{true}[0],
	}); err != nil {
		return fmt.Errorf("failed to apply proxy config: %w", err)
	}

	logger.Info("Applied proxy config ConfigMap")
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
	return fmt.Errorf("openclaw-proxy deployment not found in manifests")
}

// configureProxyForCredentials adds credential env vars and volume mounts to the
// openclaw-proxy Deployment based on spec.credentials[]. This modifies the parsed
// kustomize objects in-place before they are applied via SSA.
func configureProxyForCredentials(objects []*unstructured.Unstructured, credentials []clawv1alpha1.CredentialSpec) error {
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

		for _, cred := range credentials {
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
	return fmt.Errorf("openclaw-proxy deployment not found in manifests")
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
	return fmt.Errorf("openclaw-proxy deployment not found for config hash stamping")
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
	return fmt.Errorf("openclaw-proxy deployment not found for secret version stamping")
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
func (r *ClawResourceReconciler) getDeploymentAvailableStatus(ctx context.Context, namespace, name string) (bool, error) {
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

	// check for Available condition
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable {
			return condition.Status == corev1.ConditionTrue, nil
		}
	}

	// No Available condition found
	return false, nil
}

// checkDeploymentsReady checks if both openclaw and openclaw-proxy Deployments are ready
func (r *ClawResourceReconciler) checkDeploymentsReady(ctx context.Context, namespace string) (bool, []string, error) {
	openclawReady, err := r.getDeploymentAvailableStatus(ctx, namespace, ClawDeploymentName)
	if err != nil {
		return false, nil, err
	}

	proxyReady, err := r.getDeploymentAvailableStatus(ctx, namespace, ClawProxyDeploymentName)
	if err != nil {
		return false, nil, err
	}

	var pending []string
	if !openclawReady {
		pending = append(pending, ClawDeploymentName)
	}
	if !proxyReady {
		pending = append(pending, ClawProxyDeploymentName)
	}

	return len(pending) == 0, pending, nil
}

// getRouteURL fetches the Route and returns the HTTPS URL, or empty string if not found
func (r *ClawResourceReconciler) getRouteURL(ctx context.Context, instance *clawv1alpha1.Claw) (string, error) {
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

// setCondition is a generic helper to set a condition on the Claw instance.
func setCondition(instance *clawv1alpha1.Claw, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.Generation,
	})
}

// setReadyCondition sets the Ready condition on the Claw instance based on deployment readiness
func setReadyCondition(instance *clawv1alpha1.Claw, ready bool, pendingDeployments []string) {
	var status metav1.ConditionStatus
	var reason, message string

	if ready {
		status = metav1.ConditionTrue
		reason = clawv1alpha1.ConditionReasonReady
		message = "Claw instance is ready"
	} else {
		status = metav1.ConditionFalse
		reason = clawv1alpha1.ConditionReasonProvisioning
		if len(pendingDeployments) > 0 {
			message = "Waiting for deployments to become ready"
		} else {
			message = "Provisioning in progress"
		}
	}

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               clawv1alpha1.ConditionTypeReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.Generation,
	})
}

// getGatewayToken fetches the gateway token from the openclaw-gateway-token Secret and Base64-decodes it.
// Returns the token string, or empty string if the Secret cannot be read.
func (r *ClawResourceReconciler) getGatewayToken(ctx context.Context, namespace string) string {
	logger := log.FromContext(ctx)

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      ClawGatewaySecretName,
	}, secret); err != nil {
		logger.Error(err, "Failed to get gateway secret for status URL", "secret", ClawGatewaySecretName)
		return ""
	}

	tokenBytes, exists := secret.Data[GatewayTokenKeyName]
	if !exists || len(tokenBytes) == 0 {
		logger.Info("Gateway token not found in secret", "secret", ClawGatewaySecretName, "key", GatewayTokenKeyName)
		return ""
	}

	// Secret data is already raw bytes (not Base64-encoded in the Data field)
	// Kubernetes automatically handles Base64 decoding when accessing Secret.Data
	return string(tokenBytes)
}

// encodeFragmentValue percent-encodes a string for safe use in a URL fragment.
// This ensures special characters don't break URL parsing.
func encodeFragmentValue(v string) string {
	return url.QueryEscape(v)
}

// buildClawURL constructs the Claw status URL by appending the gateway token
// as a URL fragment if both routeURL and token are provided.
// Returns empty string if routeURL is empty.
func buildClawURL(routeURL, token string) string {
	if routeURL == "" {
		return ""
	}
	if token == "" {
		return routeURL
	}
	return routeURL + "#token=" + encodeFragmentValue(token)
}

// updateStatus updates the Claw status with current deployment conditions
func (r *ClawResourceReconciler) updateStatus(ctx context.Context, instance *clawv1alpha1.Claw) error {
	// check deployment readiness
	ready, pending, err := r.checkDeploymentsReady(ctx, instance.Namespace)
	if err != nil {
		return fmt.Errorf("failed to check deployment readiness: %w", err)
	}

	// Set Ready condition
	setReadyCondition(instance, ready, pending)

	// Expose gateway secret name in status
	instance.Status.GatewayTokenSecretRef = ClawGatewaySecretName

	// Populate URL field only when both deployments are ready
	if ready {
		routeURL, err := r.getRouteURL(ctx, instance)
		if err != nil {
			return fmt.Errorf("failed to get Route URL: %w", err)
		}

		token := r.getGatewayToken(ctx, instance.Namespace)
		instance.Status.URL = buildClawURL(routeURL, token)
	} else {
		// Clear URL when deployments are not ready
		instance.Status.URL = ""
	}

	// Update status subresource
	if err := r.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("failed to update Claw status: %w", err)
	}
	return nil
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

	// Skip operator-managed secrets (openclaw-gateway-token for gateway token)
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
