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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// NodePairingRequestApprovalReconciler reconciles a NodePairingRequestApproval object
type NodePairingRequestApprovalReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=nodepairingrequestapprovals,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=nodepairingrequestapprovals/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=claw.sandbox.redhat.com,resources=nodepairingrequestapprovals/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NodePairingRequestApprovalReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling NodePairingRequestApproval", "name", req.Name, "namespace", req.Namespace)

	// Fetch the NodePairingRequestApproval instance
	instance := &clawv1alpha1.NodePairingRequestApproval{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			logger.Info("NodePairingRequestApproval resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get NodePairingRequestApproval")
		return ctrl.Result{}, err
	}

	logger.Info("NodePairingRequestApproval reconciled successfully",
		"name", instance.Name,
		"namespace", instance.Namespace,
		"requestID", instance.Spec.RequestID)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePairingRequestApprovalReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clawv1alpha1.NodePairingRequestApproval{}).
		Complete(r)
}
