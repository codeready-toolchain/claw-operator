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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// PodExecFunc executes a command in a pod and returns stdout, stderr, and error.
type PodExecFunc func(ctx context.Context, podName, namespace, requestID string) (stdout string, stderr string, err error)

// ClawDevicePairingRequestReconciler reconciles a ClawDevicePairingRequest object
type ClawDevicePairingRequestReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Config    *rest.Config
	Clientset kubernetes.Interface
	ExecFn    PodExecFunc
}

// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=clawdevicepairingrequests,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=clawdevicepairingrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=clawdevicepairingrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=list
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create

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
			logger.Info("ClawDevicePairingRequest resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ClawDevicePairingRequest")
		return ctrl.Result{}, err
	}

	logger.Info("ClawDevicePairingRequest found",
		"name", instance.Name,
		"namespace", instance.Namespace,
		"requestID", instance.Spec.RequestID)

	// Guard: skip if already paired
	readyCond := meta.FindStatusCondition(instance.Status.Conditions, "Ready")
	if readyCond != nil && readyCond.Status == metav1.ConditionTrue && readyCond.Reason == "DevicePaired" {
		logger.Info("Device pairing already completed, skipping")
		return ctrl.Result{}, nil
	}

	// Convert selector to labels.Selector
	selector, err := metav1.LabelSelectorAsSelector(&instance.Spec.Selector)
	if err != nil {
		logger.Error(err, "Invalid selector", "selector", instance.Spec.Selector)
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "InvalidSelector", fmt.Sprintf("Invalid selector: %v", err))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after selector validation failure")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil
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
		logger.Info("No pods match selector", "selector", selector.String())
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "NoMatchingPod", fmt.Sprintf("No pods match selector: %s", selector.String()))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status for no matching pods")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil

	case podCount > 1:
		logger.Info("Multiple pods match selector", "selector", selector.String(), "count", podCount)
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "MultipleMatchingPods", fmt.Sprintf("%d pods match selector: %s", podCount, selector.String()))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status for multiple matching pods")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil

	default:
		pod := podList.Items[0]
		logger.Info("Found matching pod for device pairing request",
			"pod", pod.Name,
			"requestID", instance.Spec.RequestID)

		// Set Processing condition before exec
		setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "Processing", fmt.Sprintf("Processing device pairing on pod: %s", pod.Name))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status to Processing")
			return ctrl.Result{}, statusErr
		}

		// Execute device approval command in the pod
		execFn := r.ExecFn
		if execFn == nil {
			execFn = r.execInPod
		}
		stdout, stderr, execErr := execFn(ctx, pod.Name, instance.Namespace, instance.Spec.RequestID)
		if execErr != nil {
			logger.Error(execErr, "Device pairing exec failed",
				"pod", pod.Name,
				"requestID", instance.Spec.RequestID,
				"stderr", stderr)
			// Re-fetch to avoid conflict after the Processing status update
			if fetchErr := r.Get(ctx, req.NamespacedName, instance); fetchErr != nil {
				return ctrl.Result{}, fetchErr
			}
			setDevicePairingCondition(instance, "Ready", metav1.ConditionFalse, "PairingFailed", fmt.Sprintf("Exec failed on pod %s: %v", pod.Name, execErr))
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				logger.Error(statusErr, "Failed to update status after exec failure")
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, nil
		}

		logger.Info("Device pairing approved",
			"pod", pod.Name,
			"requestID", instance.Spec.RequestID,
			"output", stdout)
		// Re-fetch to avoid conflict after the Processing status update
		if fetchErr := r.Get(ctx, req.NamespacedName, instance); fetchErr != nil {
			return ctrl.Result{}, fetchErr
		}
		setDevicePairingCondition(instance, "Ready", metav1.ConditionTrue, "DevicePaired", fmt.Sprintf("Device paired via pod: %s", pod.Name))
		if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
			logger.Error(statusErr, "Failed to update status after successful pairing")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil
	}
}

func (r *ClawDevicePairingRequestReconciler) execInPod(ctx context.Context, podName, namespace, requestID string) (string, string, error) {
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	execReq := r.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		Param("container", ClawGatewayContainerName).
		Param("command", "openclaw").
		Param("command", "devices").
		Param("command", "approve").
		Param("command", requestID).
		Param("command", "--json").
		Param("stdout", "true").
		Param("stderr", "true")

	exec, err := remotecommand.NewSPDYExecutor(r.Config, "POST", execReq.URL())
	if err != nil {
		return "", "", fmt.Errorf("creating SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("exec stream: %w", err)
	}

	return stdout.String(), stderr.String(), nil
}

// setDevicePairingCondition sets a condition on the ClawDevicePairingRequest instance.
func setDevicePairingCondition(instance *clawv1alpha1.ClawDevicePairingRequest, condType string, status metav1.ConditionStatus, reason, message string) { //nolint:unparam
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
