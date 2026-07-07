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
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// handleIdle short-circuits the reconcile loop when spec.idle is true.
// It scales all managed Deployments to zero replicas and updates status
// to reflect the idled state.
func (r *ClawResourceReconciler) handleIdle(ctx context.Context, instance *clawv1alpha1.Claw) (ctrl.Result, error) {
	return r.scaleToZeroAndSetIdleStatus(ctx, instance,
		clawv1alpha1.ConditionReasonIdledByRequest, "Instance scaled to zero by spec.idle",
		clawv1alpha1.ConditionReasonIdle, "Instance is idled — set spec.idle to false to resume")
}

// handlePolicyIdle short-circuits the reconcile loop when spec.config.mergeMode
// is disallowed by the ClawOperatorConfig singleton. Unlike gating alone (which
// only ever blocked a Claw's *first* reconcile, leaving an already-running
// instance's Deployment — and the mode it was last reconciled with — untouched
// forever if policy tightens later), this actively converges an existing
// instance to zero replicas so cluster-admin policy is enforceable fleet-wide,
// not just for brand-new Claws. It's non-destructive: Secrets, the PVC, and
// the Claw CR itself are never touched, so fixing the mode (or the policy)
// brings the instance straight back on the next reconcile with no data loss.
// See docs/adr/0021-seed-only-config-mode.md.
func (r *ClawResourceReconciler) handlePolicyIdle(ctx context.Context, instance *clawv1alpha1.Claw) (ctrl.Result, error) {
	msg := fmt.Sprintf("mergeMode %q is not allowed by ClawOperatorConfig", effectiveConfigMode(instance))
	return r.scaleToZeroAndSetIdleStatus(ctx, instance,
		clawv1alpha1.ConditionReasonIdledByPolicy, "Instance scaled to zero — "+msg,
		clawv1alpha1.ConditionReasonConfigModeNotAllowed, msg)
}

// scaleToZeroAndSetIdleStatus is the shared implementation behind handleIdle
// and handlePolicyIdle: it scales all managed Deployments to zero replicas
// and sets the Idle/Ready conditions accordingly, using caller-supplied
// reasons/messages so status accurately reflects *why* the instance is idle.
func (r *ClawResourceReconciler) scaleToZeroAndSetIdleStatus(
	ctx context.Context,
	instance *clawv1alpha1.Claw,
	idleReason, idleMessage, readyReason, readyMessage string,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Instance is idled, scaling deployments to zero", "reason", idleReason)

	deploymentNames := []string{
		getClawDeploymentName(instance.Name),
		getProxyDeploymentName(instance.Name),
	}

	for _, name := range deploymentNames {
		if err := r.scaleDeploymentToZero(ctx, instance, name); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to scale deployment %s to zero: %w", name, err)
		}
	}

	idleCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
	readyCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
	alreadyIdled := idleCond != nil && idleCond.Status == metav1.ConditionTrue && idleCond.Reason == idleReason &&
		readyCond != nil && readyCond.Status == metav1.ConditionFalse && readyCond.Reason == readyReason &&
		instance.Status.URL == "" //nolint:staticcheck // deprecated but still checked

	if alreadyIdled {
		return ctrl.Result{}, nil
	}

	setCondition(instance, clawv1alpha1.ConditionTypeIdle, metav1.ConditionTrue, idleReason, idleMessage)
	setCondition(instance, clawv1alpha1.ConditionTypeReady, metav1.ConditionFalse, readyReason, readyMessage)
	instance.Status.URL = "" //nolint:staticcheck // deprecated but still populated
	instance.Status.GatewayURL = ""

	recordClawMetrics(instance)

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status after idling: %w", err)
	}

	return ctrl.Result{}, nil
}

// scaleDeploymentToZero patches a Deployment's replicas to 0.
// Returns nil if the Deployment does not exist (NotFound) or is not owned by
// the given Claw instance (defensive guard against same-named Deployments).
func (r *ClawResourceReconciler) scaleDeploymentToZero(ctx context.Context, instance *clawv1alpha1.Claw, name string) error {
	logger := log.FromContext(ctx)

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: name}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Deployment not found during idle, skipping", "name", name)
			return nil
		}
		return fmt.Errorf("failed to get deployment %s: %w", name, err)
	}

	if !metav1.IsControlledBy(deployment, instance) {
		logger.Info("Deployment not owned by this Claw instance, skipping", "name", name)
		return nil
	}

	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
		return nil
	}

	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"replicas": 0,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to marshal scale patch: %w", err)
	}

	if err := r.Patch(ctx, deployment, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("failed to patch deployment %s to zero replicas: %w", name, err)
	}

	logger.Info("Scaled deployment to zero", "name", name)
	return nil
}
