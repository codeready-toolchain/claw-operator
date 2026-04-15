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
	"fmt"
	"net/url"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

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

// checkDeploymentsReady checks if both claw and claw-proxy Deployments are ready
func (r *ClawResourceReconciler) checkDeploymentsReady(ctx context.Context, namespace string) (bool, []string, error) {
	clawReady, err := r.getDeploymentAvailableStatus(ctx, namespace, ClawDeploymentName)
	if err != nil {
		return false, nil, err
	}

	proxyReady, err := r.getDeploymentAvailableStatus(ctx, namespace, ClawProxyDeploymentName)
	if err != nil {
		return false, nil, err
	}

	var pending []string
	if !clawReady {
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
		Name:      ClawRouteName,
	}, route); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			// Route not found (or CRD not registered on non-OpenShift clusters)
			logger.Info("Route not found or CRD not registered", "name", ClawRouteName)
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

// getGatewayToken fetches the gateway token from the claw-gateway-token Secret and Base64-decodes it.
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
