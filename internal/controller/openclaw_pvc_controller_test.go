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
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawPersistentVolumeClaimController(t *testing.T) {

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create PVC for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Check if PVC was created
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawPVCName,
					Namespace: namespace,
				}, pvc)
				return err == nil
			}, "PVC should be created")
		})

		t.Run("should set correct owner reference on PVC", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Check PVC has correct owner reference
			pvc := &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawPVCName,
					Namespace: namespace,
				}, pvc)
				if err != nil {
					return false
				}
				if len(pvc.OwnerReferences) == 0 {
					return false
				}
				ownerRef := pvc.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "PVC should have correct owner reference")
		})
	})

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip PVC creation for non-matching names", func(t *testing.T) {
			// Setup: cleanup any instance named "instance" from previous tests
			instance := &openclawv1alpha1.Claw{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawInstanceName, Namespace: namespace}, instance)
			if err == nil {
				_ = k8sClient.Delete(ctx, instance)
			}

			// Force delete PVC by removing finalizers
			pvc := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}, pvc)
			if err == nil {
				pvc.Finalizers = []string{}
				_ = k8sClient.Update(ctx, pvc)
				_ = k8sClient.Delete(ctx, pvc)

				// Wait for PVC to be fully deleted
				waitFor(t, timeout, interval, func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}, pvc)
					return err != nil
				}, "PVC should be deleted before test")
			}

			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
				if err := deleteAndWait(&openclawv1alpha1.Claw{}, client.ObjectKey{Name: resourceName, Namespace: namespace}); err != nil {
					t.Fatalf("cleanup failed: %v", err)
				}
			})

			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// Verify PVC was NOT created
			pvc = &corev1.PersistentVolumeClaim{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}, pvc)
				return apierrors.IsNotFound(err)
			}, "PVC should not have been created for non-instance OpenClaw")
		})
	})
}
