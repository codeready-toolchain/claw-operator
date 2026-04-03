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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
	"github.com/codeready-toolchain/openclaw-operator/internal/assets"
)

const (
	OpenClawResourceKind   = "OpenClaw"
	OpenClawInstanceName   = "instance"
	OpenClawConfigMapName  = "openclaw-config"
	OpenClawPVCName        = "openclaw-home-pvc"
	OpenClawDeploymentName = "openclaw"
)

// OpenClawResourceReconciler reconciles all resources for OpenClaw
type OpenClawResourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=openclaw.sandbox.redhat.com,resources=openclaws,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
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

	// Apply all resources via Kustomize and server-side apply
	if err := r.applyKustomizedResources(ctx, instance); err != nil {
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

// SetupWithManager sets up the controller with the Manager.
func (r *OpenClawResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&openclawv1alpha1.OpenClaw{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appsv1.Deployment{}).
		Named("openclaw").
		Complete(r)
}
