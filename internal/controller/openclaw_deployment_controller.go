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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
	"github.com/codeready-toolchain/openclaw-operator/internal/assets"
)

// OpenClawDeploymentReconciler reconciles Deployment resources for OpenClawInstance
type OpenClawDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=openclaw.sandbox.redhat.com,resources=openclawinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create

// Reconcile manages Deployment lifecycle for OpenClawInstance resources
func (r *OpenClawDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling OpenClawInstance for Deployment", "name", req.Name, "namespace", req.Namespace)

	// Fetch the OpenClawInstance resource
	instance := &openclawv1alpha1.OpenClawInstance{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("OpenClawInstance resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get OpenClawInstance")
		return ctrl.Result{}, err
	}

	// Only reconcile resources named "instance"
	if instance.Name != "instance" {
		logger.Info("Skipping reconciliation for OpenClawInstance with non-matching name", "name", instance.Name)
		return ctrl.Result{}, nil
	}

	// Check if the openclaw-config ConfigMap exists before creating Deployment
	existingConfigMap := &corev1.ConfigMap{}
	err = r.Get(ctx, client.ObjectKey{Name: OpenClawConfigMapName, Namespace: instance.Namespace}, existingConfigMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ConfigMap 'openclaw-config' not found yet, skipping Deployment creation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}

	// Parse the embedded Deployment manifest
	decode := serializer.NewCodecFactory(r.Scheme).UniversalDeserializer().Decode
	deployment := &appsv1.Deployment{}
	_, _, err = decode(assets.DeploymentManifest, nil, deployment)
	if err != nil {
		logger.Error(err, "Failed to decode Deployment manifest")
		return ctrl.Result{}, err
	}

	// Set the Deployment namespace to match the OpenClawInstance namespace
	deployment.Namespace = instance.Namespace

	// Set the OpenClawInstance as the controller owner reference
	if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference on Deployment")
		return ctrl.Result{}, err
	}

	// Create the Deployment
	err = r.Create(ctx, deployment)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("Deployment already exists, skipping creation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to create Deployment")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully created Deployment")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenClawDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to filter Deployment events by name
	deploymentNamePredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == "openclaw"
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&openclawv1alpha1.OpenClawInstance{}).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(deploymentNamePredicate)).
		Named("openclawdeployment").
		Complete(r)
}
