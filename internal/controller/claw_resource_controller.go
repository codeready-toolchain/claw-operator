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
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
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

	GatewayTokenKeyName         = "token"
	ClawProxyContainerName      = "proxy"
	ClawGatewayContainerName    = "gateway"
	ClawInitConfigContainerName = "init-config"
	ClawConfigModeEnvVar        = "CLAW_CONFIG_MODE"
	DefaultKubectlImage         = "quay.io/openshift/origin-cli:4.21"

	// Kubernetes resource kinds
	RouteKind                 = "Route"
	DeploymentKind            = "Deployment"
	ConfigMapKind             = "ConfigMap"
	NetworkPolicyKind         = "NetworkPolicy"
	ServiceKind               = "Service"
	PersistentVolumeClaimKind = "PersistentVolumeClaim"
)

// sanitizeLabelValue ensures a value conforms to Kubernetes label constraints (max 63 chars,
// alphanumeric start/end). If the name fits, it is returned as-is. Otherwise it is truncated
// and a short hash suffix is appended to keep the value unique and deterministic.
func sanitizeLabelValue(name string) string {
	const maxLen = 63
	if len(name) <= maxLen {
		return name
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:8]
	// Leave room for "-" separator + 8-char hash = 9 chars
	return name[:maxLen-9] + "-" + hash
}

// injectInstanceLabels adds the claw.sandbox.redhat.com/instance label to all resources
// and injects it into Deployment/Service/NetworkPolicy selectors for multi-instance discrimination.
// Resource names are already set via CLAW_INSTANCE_NAME template replacement in buildKustomizedObjects.
func injectInstanceLabels(objects []*unstructured.Unstructured, instanceName string) error {
	instanceLabel := "claw.sandbox.redhat.com/instance"
	labelValue := sanitizeLabelValue(instanceName)

	for _, obj := range objects {
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[instanceLabel] = labelValue
		obj.SetLabels(labels)

		switch obj.GetKind() {
		case DeploymentKind:
			if err := injectDeploymentInstanceLabels(obj, instanceLabel, labelValue); err != nil {
				return err
			}
		case ServiceKind:
			if err := injectServiceInstanceLabels(obj, instanceLabel, labelValue); err != nil {
				return err
			}
		case NetworkPolicyKind:
			if err := injectNetworkPolicyInstanceLabels(obj, instanceLabel, labelValue); err != nil {
				return err
			}
		}
	}

	return nil
}

// injectDeploymentInstanceLabels injects instance labels into Deployment selectors and pod template labels
func injectDeploymentInstanceLabels(obj *unstructured.Unstructured, instanceLabel, instanceName string) error {
	// Update spec.selector.matchLabels
	selector, found, err := unstructured.NestedMap(obj.Object, "spec", "selector", "matchLabels")
	if err != nil {
		return fmt.Errorf("failed to get selector from Deployment: %w", err)
	}
	if found && selector != nil {
		selector[instanceLabel] = instanceName
		if err := unstructured.SetNestedMap(obj.Object, selector, "spec", "selector", "matchLabels"); err != nil {
			return fmt.Errorf("failed to set selector on Deployment: %w", err)
		}
	}

	// Update spec.template.metadata.labels
	templateLabels, found, err := unstructured.NestedMap(obj.Object, "spec", "template", "metadata", "labels")
	if err != nil {
		return fmt.Errorf("failed to get template labels from Deployment: %w", err)
	}
	if found && templateLabels != nil {
		templateLabels[instanceLabel] = instanceName
		if err := unstructured.SetNestedMap(obj.Object, templateLabels, "spec", "template", "metadata", "labels"); err != nil {
			return fmt.Errorf("failed to set template labels on Deployment: %w", err)
		}
	}

	return nil
}

// injectServiceInstanceLabels injects instance labels into Service selector
func injectServiceInstanceLabels(obj *unstructured.Unstructured, instanceLabel, instanceName string) error {
	selector, found, err := unstructured.NestedMap(obj.Object, "spec", "selector")
	if err != nil {
		return fmt.Errorf("failed to get selector from Service: %w", err)
	}
	if found && selector != nil {
		selector[instanceLabel] = instanceName
		if err := unstructured.SetNestedMap(obj.Object, selector, "spec", "selector"); err != nil {
			return fmt.Errorf("failed to set selector on Service: %w", err)
		}
	}

	return nil
}

// injectNetworkPolicyInstanceLabels injects instance labels into NetworkPolicy podSelector and peer podSelectors
func injectNetworkPolicyInstanceLabels(obj *unstructured.Unstructured, instanceLabel, instanceName string) error {
	// Update spec.podSelector.matchLabels
	podSelector, found, err := unstructured.NestedMap(obj.Object, "spec", "podSelector", "matchLabels")
	if err != nil {
		return fmt.Errorf("failed to get podSelector from NetworkPolicy: %w", err)
	}
	if found && podSelector != nil {
		podSelector[instanceLabel] = instanceName
		if err := unstructured.SetNestedMap(obj.Object, podSelector, "spec", "podSelector", "matchLabels"); err != nil {
			return fmt.Errorf("failed to set podSelector on NetworkPolicy: %w", err)
		}
	}

	// Update egress peer podSelectors
	if err := injectNetworkPolicyEgressLabels(obj, instanceLabel, instanceName); err != nil {
		return err
	}

	// Update ingress peer podSelectors
	if err := injectNetworkPolicyIngressLabels(obj, instanceLabel, instanceName); err != nil {
		return err
	}

	return nil
}

// injectNetworkPolicyEgressLabels injects instance labels into NetworkPolicy egress peer podSelectors
func injectNetworkPolicyEgressLabels(obj *unstructured.Unstructured, instanceLabel, instanceName string) error {
	egress, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
	if err != nil {
		return fmt.Errorf("failed to get egress from NetworkPolicy: %w", err)
	}
	if !found || egress == nil {
		return nil
	}

	for i, egressRule := range egress {
		egressMap, ok := egressRule.(map[string]any)
		if !ok {
			continue
		}
		to, found, err := unstructured.NestedSlice(egressMap, "to")
		if err != nil {
			return fmt.Errorf("failed to get to peers from NetworkPolicy egress rule: %w", err)
		}
		if found && to != nil {
			for j, peer := range to {
				peerMap, ok := peer.(map[string]any)
				if !ok {
					continue
				}
				podSelector, found, err := unstructured.NestedMap(peerMap, "podSelector", "matchLabels")
				if err != nil {
					return fmt.Errorf("failed to get podSelector from NetworkPolicy egress peer: %w", err)
				}
				if found && podSelector != nil {
					podSelector[instanceLabel] = instanceName
					if err := unstructured.SetNestedMap(peerMap, podSelector, "podSelector", "matchLabels"); err != nil {
						return fmt.Errorf("failed to set podSelector on NetworkPolicy egress peer: %w", err)
					}
					to[j] = peerMap
				}
			}
			egressMap["to"] = to
		}
		egress[i] = egressMap
	}

	if err := unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress"); err != nil {
		return fmt.Errorf("failed to set egress on NetworkPolicy: %w", err)
	}

	return nil
}

// injectNetworkPolicyIngressLabels injects instance labels into NetworkPolicy ingress peer podSelectors
func injectNetworkPolicyIngressLabels(obj *unstructured.Unstructured, instanceLabel, instanceName string) error {
	ingress, found, err := unstructured.NestedSlice(obj.Object, "spec", "ingress")
	if err != nil {
		return fmt.Errorf("failed to get ingress from NetworkPolicy: %w", err)
	}
	if !found || ingress == nil {
		return nil
	}

	for i, ingressRule := range ingress {
		ingressMap, ok := ingressRule.(map[string]any)
		if !ok {
			continue
		}
		from, found, err := unstructured.NestedSlice(ingressMap, "from")
		if err != nil {
			return fmt.Errorf("failed to get from peers from NetworkPolicy ingress rule: %w", err)
		}
		if found && from != nil {
			for j, peer := range from {
				peerMap, ok := peer.(map[string]any)
				if !ok {
					continue
				}
				podSelector, found, err := unstructured.NestedMap(peerMap, "podSelector", "matchLabels")
				if err != nil {
					return fmt.Errorf("failed to get podSelector from NetworkPolicy ingress peer: %w", err)
				}
				if found && podSelector != nil {
					podSelector[instanceLabel] = instanceName
					if err := unstructured.SetNestedMap(peerMap, podSelector, "podSelector", "matchLabels"); err != nil {
						return fmt.Errorf("failed to set podSelector on NetworkPolicy ingress peer: %w", err)
					}
					from[j] = peerMap
				}
			}
			ingressMap["from"] = from
		}
		ingress[i] = ingressMap
	}

	if err := unstructured.SetNestedSlice(obj.Object, ingress, "spec", "ingress"); err != nil {
		return fmt.Errorf("failed to set ingress on NetworkPolicy: %w", err)
	}

	return nil
}

// Resource naming helper functions
func getClawDeploymentName(instanceName string) string {
	return instanceName
}

func getProxyDeploymentName(instanceName string) string {
	return instanceName + "-proxy"
}

func getGatewaySecretName(instanceName string) string {
	return instanceName + "-gateway-token"
}

func getConfigMapName(instanceName string) string { //nolint:unparam // called only from tests today but must stay parametric
	return instanceName + "-config"
}

func getPVCName(instanceName string) string { //nolint:unparam // called only from tests today but must stay parametric
	return instanceName + "-home-pvc"
}

func getServiceName(instanceName string) string {
	return instanceName
}

func getProxyServiceName(instanceName string) string {
	return instanceName + "-proxy"
}

func getRouteName(instanceName string) string {
	return instanceName
}

func getProxyCAConfigMapName(instanceName string) string {
	return instanceName + "-proxy-ca"
}

func getVertexADCConfigMapName(instanceName string) string {
	return instanceName + "-vertex-adc"
}

func getKubeConfigMapName(instanceName string) string {
	return instanceName + "-kube-config"
}

func getIngressNetworkPolicyName(instanceName string) string {
	return instanceName + "-ingress"
}

func getEgressNetworkPolicyName(instanceName string) string {
	return instanceName + "-egress"
}

func getProxyEgressNetworkPolicyName(instanceName string) string {
	return instanceName + "-proxy-egress"
}

func getProxyConfigMapName(instanceName string) string {
	return instanceName + "-proxy-config"
}

// ClawResourceReconciler reconciles all resources for Claw
type ClawResourceReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ProxyImage      string
	KubectlImage    string
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

	// Create or update the gateway Secret with token
	if err := r.applyGatewaySecret(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply gateway secret")
		return ctrl.Result{}, err
	}

	// Resolve credentials (provider defaults, validation, kubeconfig parsing)
	resolvedCreds, err := r.resolveAndApplyCredentials(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Generate proxy config, apply ConfigMaps (proxy config + Vertex AI stub ADC)
	proxyConfigJSON, err := r.applyProxyResources(ctx, instance, resolvedCreds)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Build kustomized objects
	objects, err := r.buildKustomizedObjects(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Apply deployment overrides (proxy image, pull policy, credentials)
	if err := r.configureDeployments(instance, objects, resolvedCreds); err != nil {
		return ctrl.Result{}, err
	}

	// Stamp proxy config hash to trigger rollout on config changes
	proxyConfigHash := fmt.Sprintf("%x", sha256.Sum256(proxyConfigJSON))
	if err := stampProxyConfigHash(objects, instance, proxyConfigHash); err != nil {
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
	if err := r.injectRouteHostIntoConfigMap(objects, routeHost, instance.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject Route host into ConfigMap: %w", err)
	}

	// Inject LLM providers into ConfigMap based on credentials with Provider set
	if err := injectProvidersIntoConfigMap(objects, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject providers into ConfigMap: %w", err)
	}

	// Inject Kubernetes skill into ConfigMap for OpenClaw skill auto-discovery
	if err := injectKubernetesSkill(objects, resolvedCreds, instance.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject Kubernetes skill: %w", err)
	}

	// Inject non-443 ports from kubernetes credentials into proxy egress NetworkPolicy
	if err := injectKubePortsIntoNetworkPolicy(objects, resolvedCreds, instance.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject Kubernetes ports into NetworkPolicy: %w", err)
	}

	// Stamp gateway config hash to trigger rollout when ConfigMap content changes
	if err := stampGatewayConfigHash(objects, instance.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to stamp gateway config hash: %w", err)
	}

	// Filter out Route (applied in phase above) and proxy ConfigMap (controller-managed)
	remainingObjects := []*unstructured.Unstructured{}
	for _, obj := range objects {
		if obj.GetKind() == RouteKind {
			continue
		}
		if obj.GetKind() == ConfigMapKind && obj.GetName() == getProxyConfigMapName(instance.Name) {
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

// resolveAndApplyCredentials handles provider defaults, credential resolution/validation,
// sanitized kubeconfig creation, and proxy CA generation in one cohesive step.
func (r *ClawResourceReconciler) resolveAndApplyCredentials(ctx context.Context, instance *clawv1alpha1.Claw) ([]resolvedCredential, error) {
	logger := log.FromContext(ctx)

	for i := range instance.Spec.Credentials {
		if err := resolveProviderDefaults(&instance.Spec.Credentials[i]); err != nil {
			logger.Error(err, "Failed to resolve provider defaults")
			setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				logger.Error(statusErr, "Failed to update status after provider defaults failure")
			}
			return nil, err
		}
	}

	resolvedCreds, err := r.resolveCredentials(ctx, instance)
	if err != nil {
		logger.Error(err, "Credential validation failed")
		setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
		setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonValidationFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after credential validation failure")
		}
		return nil, err
	}
	setCondition(instance, clawv1alpha1.ConditionTypeCredentialsResolved, metav1.ConditionTrue, clawv1alpha1.ConditionReasonResolved, "All credential Secrets are valid")

	if err := r.applySanitizedKubeconfig(ctx, instance, resolvedCreds); err != nil {
		logger.Error(err, "Failed to apply sanitized kubeconfig")
		return nil, err
	}

	if err := r.applyProxyCA(ctx, instance); err != nil {
		logger.Error(err, "Failed to apply proxy CA")
		return nil, err
	}

	return resolvedCreds, nil
}

// configureDeployments applies deployment overrides (proxy image, pull policy, credentials)
func (r *ClawResourceReconciler) configureDeployments(
	instance *clawv1alpha1.Claw,
	objects []*unstructured.Unstructured,
	resolvedCreds []resolvedCredential,
) error {
	if err := configureProxyImage(objects, instance, r.ProxyImage); err != nil {
		return fmt.Errorf("failed to configure proxy image: %w", err)
	}
	if err := configureImagePullPolicy(objects, r.ImagePullPolicy); err != nil {
		return fmt.Errorf("failed to configure image pull policy: %w", err)
	}
	if err := configureProxyForCredentials(objects, instance, resolvedCreds); err != nil {
		return fmt.Errorf("failed to configure proxy deployment for credentials: %w", err)
	}
	if err := configureClawDeploymentForVertex(objects, resolvedCreds, instance.Name); err != nil {
		return fmt.Errorf("failed to configure claw deployment for Vertex AI: %w", err)
	}
	kubectlImage := r.KubectlImage
	if kubectlImage == "" {
		kubectlImage = DefaultKubectlImage
	}
	if err := configureClawDeploymentForKubernetes(objects, resolvedCreds, kubectlImage, instance.Name); err != nil {
		return fmt.Errorf("failed to configure claw deployment for Kubernetes: %w", err)
	}
	if err := configureClawDeploymentConfigMode(objects, instance); err != nil {
		return fmt.Errorf("failed to configure claw deployment config mode: %w", err)
	}
	return nil
}

// applyProxyResources generates the proxy config, applies the proxy ConfigMap and
// (when needed) the Vertex AI stub ADC ConfigMap. Returns the proxy config JSON
// for use in config hash stamping.
func (r *ClawResourceReconciler) applyProxyResources(ctx context.Context, instance *clawv1alpha1.Claw, resolvedCreds []resolvedCredential) ([]byte, error) {
	logger := log.FromContext(ctx)

	proxyConfigJSON, err := generateProxyConfig(resolvedCreds)
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
	if err := r.applyVertexADCConfigMap(ctx, instance, resolvedCreds); err != nil {
		logger.Error(err, "Failed to apply Vertex ADC config")
		setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, clawv1alpha1.ConditionReasonConfigFailed, err.Error())
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after Vertex ADC config failure")
		}
		return nil, err
	}
	setCondition(instance, clawv1alpha1.ConditionTypeProxyConfigured, metav1.ConditionTrue, clawv1alpha1.ConditionReasonConfigured, "Proxy config generated successfully")

	return proxyConfigJSON, nil
}

// buildKustomizeFromPath builds kustomize manifests from a specific component directory
func (r *ClawResourceReconciler) buildKustomizeFromPath(fsys filesys.FileSystem, componentPath string) ([]*unstructured.Unstructured, error) {
	// Build manifests using Kustomize
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := kustomizer.Run(fsys, componentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to run kustomize build for %s: %w", componentPath, err)
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

func (r *ClawResourceReconciler) buildKustomizedObjects(instance *clawv1alpha1.Claw) ([]*unstructured.Unstructured, error) {
	// Write all manifest files to in-memory filesystem
	fs := filesys.MakeFsInMemory()

	// TODO: could we have something more generic here?
	// For example, we could use a glob to get all the manifests in a directory
	// and then write them to the in-memory filesystem
	// Claw component manifests
	clawManifests := map[string][]byte{
		"manifests/claw/kustomization.yaml":  readEmbeddedFile("manifests/claw/kustomization.yaml"),
		"manifests/claw/configmap.yaml":      readEmbeddedFile("manifests/claw/configmap.yaml"),
		"manifests/claw/pvc.yaml":            readEmbeddedFile("manifests/claw/pvc.yaml"),
		"manifests/claw/deployment.yaml":     readEmbeddedFile("manifests/claw/deployment.yaml"),
		"manifests/claw/service.yaml":        readEmbeddedFile("manifests/claw/service.yaml"),
		"manifests/claw/route.yaml":          readEmbeddedFile("manifests/claw/route.yaml"),
		"manifests/claw/network-policy.yaml": readEmbeddedFile("manifests/claw/network-policy.yaml"),
	}

	// Proxy component manifests
	proxyManifests := map[string][]byte{
		"manifests/claw-proxy/kustomization.yaml":    readEmbeddedFile("manifests/claw-proxy/kustomization.yaml"),
		"manifests/claw-proxy/proxy-deployment.yaml": readEmbeddedFile("manifests/claw-proxy/proxy-deployment.yaml"),
		"manifests/claw-proxy/proxy-service.yaml":    readEmbeddedFile("manifests/claw-proxy/proxy-service.yaml"),
		"manifests/claw-proxy/network-policies.yaml": readEmbeddedFile("manifests/claw-proxy/network-policies.yaml"),
	}

	// Write all files to in-memory filesystem
	allManifests := make(map[string][]byte, len(clawManifests)+len(proxyManifests))
	maps.Copy(allManifests, clawManifests)
	maps.Copy(allManifests, proxyManifests)

	for path, content := range allManifests {
		replaced := bytes.ReplaceAll(content, []byte("CLAW_INSTANCE_NAME"), []byte(instance.Name))
		if err := fs.WriteFile(path, replaced); err != nil {
			return nil, fmt.Errorf("failed to write manifest to in-memory filesystem: %w", err)
		}
	}

	// Build claw component
	clawObjects, err := r.buildKustomizeFromPath(fs, "manifests/claw")
	if err != nil {
		return nil, err
	}

	// Build proxy component
	proxyObjects, err := r.buildKustomizeFromPath(fs, "manifests/claw-proxy")
	if err != nil {
		return nil, err
	}

	// Merge both object lists
	allObjects := append(clawObjects, proxyObjects...)

	// Inject instance labels into selectors for multi-instance discrimination
	if err := injectInstanceLabels(allObjects, instance.Name); err != nil {
		return nil, fmt.Errorf("failed to inject instance labels: %w", err)
	}

	return allObjects, nil
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

// injectRouteHostIntoConfigMap replaces OPENCLAW_ROUTE_HOST placeholder in operator.json with actual Route host.
// If routeHost is empty (vanilla Kubernetes), uses localhost fallback.
func (r *ClawResourceReconciler) injectRouteHostIntoConfigMap(objects []*unstructured.Unstructured, routeHost, instanceName string) error {
	replacement := routeHost
	if replacement == "" {
		replacement = "http://localhost:18789"
	}

	configMapName := getConfigMapName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() == ConfigMapKind && obj.GetName() == configMapName {
			operatorJSON, found, err := unstructured.NestedString(obj.Object, "data", "operator.json")
			if err != nil {
				return fmt.Errorf("failed to extract operator.json from ConfigMap: %w", err)
			}
			if !found {
				return fmt.Errorf("operator.json not found in ConfigMap data")
			}

			updatedJSON := strings.ReplaceAll(operatorJSON, "OPENCLAW_ROUTE_HOST", replacement)

			if err := unstructured.SetNestedField(obj.Object, updatedJSON, "data", "operator.json"); err != nil {
				return fmt.Errorf("failed to set updated operator.json in ConfigMap: %w", err)
			}

			return nil
		}
	}

	return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
}

// injectProvidersIntoConfigMap dynamically builds the models.providers section of operator.json
// from credentials that have Provider set. Gateway-routed providers get a baseUrl pointing to
// the proxy. Vertex SDK providers (GCP + non-Google) get the real Vertex AI URL since traffic
// flows through the MITM proxy which injects GCP credentials transparently.
// Agent defaults (model aliases, primary model) are in the user-owned openclaw.json seed
// and are not modified here — users control their own model preferences.
func injectProvidersIntoConfigMap(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	credentials := instance.Spec.Credentials
	providers := map[string]any{}
	for _, cred := range credentials {
		if cred.Provider == "" {
			continue
		}
		if cred.Type == clawv1alpha1.CredentialTypePathToken {
			continue
		}

		if usesVertexSDK(cred) {
			providerKey := cred.Provider + "-vertex"
			if _, exists := providers[providerKey]; exists {
				return fmt.Errorf("duplicate provider %q in credentials", providerKey)
			}
			baseURL := vertexAIBaseURL(cred.GCP.Location)
			entry := map[string]any{
				"baseUrl":   baseURL,
				"apiKey":    "gcp-vertex-credentials",
				"maxTokens": 128000,
				"models":    []any{},
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
			providers[cred.Provider] = map[string]any{
				"baseUrl": info.Upstream + info.BasePath,
				"apiKey":  "ah-ah-ah-you-didnt-say-the-magic-word",
				"models":  []any{},
			}
		}
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

		models, _ := config["models"].(map[string]any)
		if models == nil {
			models = map[string]any{}
			config["models"] = models
		}
		models["providers"] = providers

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

// injectKubePortsIntoNetworkPolicy adds non-443 ports from kubernetes credentials to
// the proxy egress NetworkPolicy. This allows the proxy to reach API servers on
// non-standard ports (e.g., 6443).
func injectKubePortsIntoNetworkPolicy(objects []*unstructured.Unstructured, resolvedCreds []resolvedCredential, instanceName string) error {
	uniquePorts := map[string]bool{}
	for _, rc := range resolvedCreds {
		if rc.KubeConfig == nil {
			continue
		}
		for _, cluster := range rc.KubeConfig.Clusters {
			if cluster.Port != "443" {
				uniquePorts[cluster.Port] = true
			}
		}
	}
	if len(uniquePorts) == 0 {
		return nil
	}

	npName := getProxyEgressNetworkPolicyName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != NetworkPolicyKind || obj.GetName() != npName {
			continue
		}

		egress, found, err := unstructured.NestedSlice(obj.Object, "spec", "egress")
		if err != nil {
			return fmt.Errorf("failed to get egress rules from proxy NetworkPolicy: %w", err)
		}
		if !found || len(egress) == 0 {
			return fmt.Errorf("egress rules not found in proxy NetworkPolicy")
		}

		// First egress rule is the HTTPS rule (ports only, no `to` restriction)
		httpsRule, ok := egress[0].(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected egress rule type in proxy NetworkPolicy")
		}

		ports, _, _ := unstructured.NestedSlice(httpsRule, "ports")

		sortedPorts := make([]int, 0, len(uniquePorts))
		for port := range uniquePorts {
			portInt, err := strconv.Atoi(port)
			if err != nil {
				return fmt.Errorf("invalid port %q from kubeconfig: %w", port, err)
			}
			sortedPorts = append(sortedPorts, portInt)
		}
		slices.Sort(sortedPorts)

		for _, portInt := range sortedPorts {
			ports = append(ports, map[string]any{
				"port":     int64(portInt),
				"protocol": "TCP",
			})
		}

		if err := unstructured.SetNestedSlice(httpsRule, ports, "ports"); err != nil {
			return fmt.Errorf("failed to set ports on proxy egress rule: %w", err)
		}
		egress[0] = httpsRule
		if err := unstructured.SetNestedSlice(obj.Object, egress, "spec", "egress"); err != nil {
			return fmt.Errorf("failed to set egress rules on proxy NetworkPolicy: %w", err)
		}
		return nil
	}
	return fmt.Errorf("NetworkPolicy %q not found in manifests", npName)
}

// injectKubernetesSkill writes a KUBERNETES.md key into the claw-config ConfigMap
// when kubernetes credentials are present. The init container copies this into
// skills/kubernetes/SKILL.md so OpenClaw auto-discovers it as a workspace skill.
func injectKubernetesSkill(objects []*unstructured.Unstructured, resolvedCreds []resolvedCredential, instanceName string) error {
	var allContexts []kubeconfigContext
	for _, rc := range resolvedCreds {
		if rc.KubeConfig == nil {
			continue
		}
		allContexts = append(allContexts, rc.KubeConfig.Contexts...)
	}
	if len(allContexts) == 0 {
		return nil
	}

	slices.SortFunc(allContexts, func(a, b kubeconfigContext) int {
		return cmp.Compare(a.Name, b.Name)
	})

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: kubernetes\n")
	sb.WriteString("description: \"Kubernetes/OpenShift cluster access. Use when the user asks about ")
	sb.WriteString("deployments, pods, services, builds, routes, or any cluster resource.\"\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# Kubernetes Access\n\n")
	sb.WriteString("You have access to Kubernetes/OpenShift clusters. Both `kubectl` and `oc` are\n")
	sb.WriteString("available and your KUBECONFIG is pre-configured — authentication is handled\n")
	sb.WriteString("transparently by the proxy.\n\n")
	sb.WriteString("Available contexts:\n")

	for _, ctx := range allContexts {
		entry := fmt.Sprintf("- `%s` (cluster: %s", ctx.Name, ctx.Cluster)
		if ctx.Namespace != "" {
			entry += ", namespace: " + ctx.Namespace
		}
		entry += ")"
		if ctx.Current {
			entry += " [current]"
		}
		sb.WriteString(entry + "\n")
	}

	sb.WriteString("\nWhen the user asks about deployments, pods, services, routes, builds, logs,\n")
	sb.WriteString("or anything cluster-related, use kubectl/oc directly to help them.\n\n")
	sb.WriteString("Do not attempt to manage tokens, certificates, or kubeconfig yourself.\n")

	configMapName := getConfigMapName(instanceName)
	for _, obj := range objects {
		if obj.GetKind() != ConfigMapKind || obj.GetName() != configMapName {
			continue
		}

		if err := unstructured.SetNestedField(obj.Object, sb.String(), "data", "KUBERNETES.md"); err != nil {
			return fmt.Errorf("failed to set KUBERNETES.md in ConfigMap: %w", err)
		}
		return nil
	}
	return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
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
	for doc := range bytes.SplitSeq(yamlData, []byte("\n---\n")) {
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

	// Skip operator-managed secrets (owned by a Claw controller).
	// These are already handled by Owns(&corev1.Secret{}) in the controller setup.
	if metav1.GetControllerOf(secret) != nil &&
		metav1.GetControllerOf(secret).Kind == ClawResourceKind {
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
