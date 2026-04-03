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

const (
	OpenClawConfigMapName = "openclaw-config"
)

// OpenClawConfigMapReconciler reconciles ConfigMap resources for OpenClaw
type OpenClawConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=openclaw.sandbox.redhat.com,resources=openclaws,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create

// Reconcile manages ConfigMap lifecycle for OpenClaw resources
func (r *OpenClawConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling OpenClaw for ConfigMap", "name", req.Name, "namespace", req.Namespace)

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

	// Parse the embedded ConfigMap manifest
	decode := serializer.NewCodecFactory(r.Scheme).UniversalDeserializer().Decode
	configMap := &corev1.ConfigMap{}
	_, _, err = decode(assets.ConfigMapManifest, nil, configMap)
	if err != nil {
		logger.Error(err, "Failed to decode ConfigMap manifest")
		return ctrl.Result{}, err
	}

	// Set the ConfigMap namespace to match the OpenClaw namespace
	configMap.Namespace = instance.Namespace

	// Set the OpenClaw as the controller owner reference
	if err := controllerutil.SetControllerReference(instance, configMap, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference on ConfigMap")
		return ctrl.Result{}, err
	}

	// Create the ConfigMap
	err = r.Create(ctx, configMap)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("ConfigMap already exists, skipping creation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to create ConfigMap")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully created ConfigMap")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenClawConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to filter ConfigMap events by name
	configMapNamePredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == OpenClawConfigMapName
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&openclawv1alpha1.OpenClaw{}).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(configMapNamePredicate)).
		Named("openclawconfigmap").
		Complete(r)
}
