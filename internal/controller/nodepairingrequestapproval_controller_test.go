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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestNodePairingRequestApprovalController(t *testing.T) {
	t.Run("NodePairingRequestApproval creation with RequestID field", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-pairing-request"

		t.Cleanup(func() {
			deleteAndWaitNodePairingRequestApproval(t, namespace, resourceName)
		})

		// Create NodePairingRequestApproval with RequestID
		instance := &openclawv1alpha1.NodePairingRequestApproval{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: openclawv1alpha1.NodePairingRequestApprovalSpec{
				RequestID: "test-request-123",
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create NodePairingRequestApproval")

		// Verify resource was created with correct RequestID
		fetched := &openclawv1alpha1.NodePairingRequestApproval{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		assert.Equal(t, "test-request-123", fetched.Spec.RequestID)
	})

	t.Run("controller reconciliation on resource creation", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-reconcile-create"

		t.Cleanup(func() {
			deleteAndWaitNodePairingRequestApproval(t, namespace, resourceName)
		})

		// Create NodePairingRequestApproval
		instance := &openclawv1alpha1.NodePairingRequestApproval{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: openclawv1alpha1.NodePairingRequestApprovalSpec{
				RequestID: "create-test-456",
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create NodePairingRequestApproval")

		// Setup reconciler
		reconciler := &NodePairingRequestApprovalReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}

		// Reconcile the resource
		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      resourceName,
				Namespace: namespace,
			},
		})

		require.NoError(t, err, "reconcile failed")
		assert.Equal(t, ctrl.Result{}, result, "should not requeue")
	})

	t.Run("controller reconciliation on resource update", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-reconcile-update"

		t.Cleanup(func() {
			deleteAndWaitNodePairingRequestApproval(t, namespace, resourceName)
		})

		// Create NodePairingRequestApproval
		instance := &openclawv1alpha1.NodePairingRequestApproval{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: openclawv1alpha1.NodePairingRequestApprovalSpec{
				RequestID: "update-test-789",
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create NodePairingRequestApproval")

		// Update RequestID
		fetched := &openclawv1alpha1.NodePairingRequestApproval{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		fetched.Spec.RequestID = "updated-request-999"
		require.NoError(t, k8sClient.Update(ctx, fetched), "failed to update NodePairingRequestApproval")

		// Setup reconciler
		reconciler := &NodePairingRequestApprovalReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}

		// Reconcile the updated resource
		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      resourceName,
				Namespace: namespace,
			},
		})

		require.NoError(t, err, "reconcile after update failed")
		assert.Equal(t, ctrl.Result{}, result, "should not requeue")

		// Verify updated RequestID
		updated := &openclawv1alpha1.NodePairingRequestApproval{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		assert.Equal(t, "updated-request-999", updated.Spec.RequestID)
	})

	t.Run("Status subresource update independence", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-status-independence"

		t.Cleanup(func() {
			deleteAndWaitNodePairingRequestApproval(t, namespace, resourceName)
		})

		// Create NodePairingRequestApproval
		instance := &openclawv1alpha1.NodePairingRequestApproval{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: openclawv1alpha1.NodePairingRequestApprovalSpec{
				RequestID: "status-test-111",
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create NodePairingRequestApproval")

		// Update Status.Conditions
		fetched := &openclawv1alpha1.NodePairingRequestApproval{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))

		originalRequestID := fetched.Spec.RequestID
		fetched.Status.Conditions = []metav1.Condition{
			{
				Type:               "TestCondition",
				Status:             metav1.ConditionTrue,
				Reason:             "TestReason",
				Message:            "Test message",
				LastTransitionTime: metav1.Now(),
			},
		}
		require.NoError(t, k8sClient.Status().Update(ctx, fetched), "failed to update status")

		// Verify Spec.RequestID unchanged
		updated := &openclawv1alpha1.NodePairingRequestApproval{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		assert.Equal(t, originalRequestID, updated.Spec.RequestID, "Spec.RequestID should remain unchanged")
		assert.Len(t, updated.Status.Conditions, 1, "Status.Conditions should have one condition")
		assert.Equal(t, "TestCondition", updated.Status.Conditions[0].Type)
	})

	t.Run("Status.Conditions field accessibility", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-conditions-field"

		t.Cleanup(func() {
			deleteAndWaitNodePairingRequestApproval(t, namespace, resourceName)
		})

		// Create NodePairingRequestApproval
		instance := &openclawv1alpha1.NodePairingRequestApproval{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: openclawv1alpha1.NodePairingRequestApprovalSpec{
				RequestID: "conditions-test-222",
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create NodePairingRequestApproval")

		// Verify Conditions field is accessible and initially empty (or nil)
		fetched := &openclawv1alpha1.NodePairingRequestApproval{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		// Conditions can be nil or empty initially (omitempty tag means API server may not return the field)
		assert.Empty(t, fetched.Status.Conditions, "Status.Conditions should be empty initially")
	})
}

// deleteAndWaitNodePairingRequestApproval deletes a NodePairingRequestApproval and waits for it to be removed
func deleteAndWaitNodePairingRequestApproval(t *testing.T, namespace, name string) {
	t.Helper()
	ctx := context.Background()

	instance := &openclawv1alpha1.NodePairingRequestApproval{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := k8sClient.Delete(ctx, instance)
	if err != nil && client.IgnoreNotFound(err) != nil {
		t.Logf("failed to delete NodePairingRequestApproval %s/%s: %v", namespace, name, err)
	}

	// Wait for resource to be deleted
	waitFor(t, timeout, interval, func() bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance)
		return apierrors.IsNotFound(err)
	}, "NodePairingRequestApproval should be deleted")
}
