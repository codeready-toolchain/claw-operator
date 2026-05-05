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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// ClawDevicePairingRequestReconciler reconciles a ClawDevicePairingRequest object
type ClawDevicePairingRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=clawdevicepairingrequests,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=clawdevicepairingrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=clawdevicepairingrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClawDevicePairingRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling ClawDevicePairingRequest", "name", req.Name, "namespace", req.Namespace)

	// Fetch the ClawDevicePairingRequest instance
	instance := &clawv1alpha1.ClawDevicePairingRequest{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			logger.Info("ClawDevicePairingRequest resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get ClawDevicePairingRequest")
		return ctrl.Result{}, err
	}

	logger.Info("ClawDevicePairingRequest found",
		"name", instance.Name,
		"namespace", instance.Namespace,
		"requestID", instance.Spec.RequestID)

	// Convert selector to labels.Selector
	selector, err := metav1.LabelSelectorAsSelector(&instance.Spec.Selector)
	if err != nil {
		logger.Error(err, "Invalid selector", "selector", instance.Spec.Selector)
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "InvalidSelector", fmt.Sprintf("Invalid selector: %v", err))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after selector validation failure")
		}
		return ctrl.Result{}, fmt.Errorf("invalid selector: %w", err)
	}

	// Query for pods using the selector
	podList := &corev1.PodList{}
	err = r.List(ctx, podList, &client.ListOptions{
		Namespace:     instance.Namespace,
		LabelSelector: selector,
	})
	if err != nil {
		logger.Error(err, "Failed to list pods with selector")
		return ctrl.Result{}, err
	}

	// Handle different pod match scenarios
	podCount := len(podList.Items)
	switch {
	case podCount == 0:
		// No matching pods
		logger.Info("No pods match selector", "selector", selector.String())
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "NoMatchingPod", fmt.Sprintf("No pods match selector: %s", selector.String()))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status for no matching pods")
		}
		return ctrl.Result{}, nil

	case podCount > 1:
		// Multiple matching pods
		logger.Info("Multiple pods match selector", "selector", selector.String(), "count", podCount)
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "MultipleMatchingPods", fmt.Sprintf("%d pods match selector: %s", podCount, selector.String()))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status for multiple matching pods")
		}
		return ctrl.Result{}, nil

	default:
		// Exactly one matching pod - process the pairing request
		pod := podList.Items[0]
		logger.Info("Found matching pod for device pairing request",
			"pod", pod.Name,
			"requestID", instance.Spec.RequestID)

		// TODO: Implement device pairing approval logic here
		// For now, just set a condition indicating readiness
		setDevicePairingCondition(instance, "Ready", metav1.ConditionTrue, "Ready", fmt.Sprintf("Found target pod: %s", pod.Name))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status for ready state")
		}
		return ctrl.Result{}, nil
	}
}

// setDevicePairingCondition sets a condition on the ClawDevicePairingRequest instance.
func setDevicePairingCondition(instance *clawv1alpha1.ClawDevicePairingRequest, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClawDevicePairingRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1alpha1.ClawDevicePairingRequest{}).
		Complete(r)
}
