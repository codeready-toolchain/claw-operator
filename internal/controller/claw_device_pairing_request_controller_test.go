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

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestClawDevicePairingRequestController(t *testing.T) {
	t.Run("ClawDevicePairingRequest creation with RequestID field", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-pairing-request"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest with RequestID
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "test-request-123",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create ClawDevicePairingRequest")

		// Verify resource was created with correct RequestID
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		assert.Equal(t, "test-request-123", fetched.Spec.RequestID)
	})

	t.Run("controller reconciliation on resource creation", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-reconcile-create"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "create-test-456",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create ClawDevicePairingRequest")

		// Setup reconciler
		reconciler := &ClawDevicePairingRequestReconciler{
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
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "update-test-789",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create ClawDevicePairingRequest")

		// Update RequestID
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		fetched.Spec.RequestID = "updated-request-999"
		require.NoError(t, k8sClient.Update(ctx, fetched), "failed to update ClawDevicePairingRequest")

		// Setup reconciler
		reconciler := &ClawDevicePairingRequestReconciler{
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
		updated := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		assert.Equal(t, "updated-request-999", updated.Spec.RequestID)
	})

	t.Run("Status subresource update independence", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-status-independence"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "status-test-111",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create ClawDevicePairingRequest")

		// Update Status.Conditions
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
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
		updated := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		assert.Equal(t, originalRequestID, updated.Spec.RequestID, "Spec.RequestID should remain unchanged")
		assert.Len(t, updated.Status.Conditions, 1, "Status.Conditions should have one condition")
		assert.Equal(t, "TestCondition", updated.Status.Conditions[0].Type)
	})

	t.Run("Status.Conditions field accessibility", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-conditions-field"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "conditions-test-222",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create ClawDevicePairingRequest")

		// Verify Conditions field is accessible and initially empty (or nil)
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		// Conditions can be nil or empty initially (omitempty tag means API server may not return the field)
		assert.Empty(t, fetched.Status.Conditions, "Status.Conditions should be empty initially")
	})

	t.Run("ClawDevicePairingRequest creation with valid selector", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-with-selector"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest with selector
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "selector-test-001",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":      "claw",
						"instance": "my-claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create ClawDevicePairingRequest with selector")

		// Verify resource was created with correct selector
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		assert.Equal(t, "selector-test-001", fetched.Spec.RequestID)
		assert.NotNil(t, fetched.Spec.Selector.MatchLabels)
		assert.Equal(t, "claw", fetched.Spec.Selector.MatchLabels["app"])
		assert.Equal(t, "my-claw", fetched.Spec.Selector.MatchLabels["instance"])
	})

	t.Run("Selector validation - empty selector rejected", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-empty-selector"

		// Attempt to create ClawDevicePairingRequest with empty selector (no matchLabels, no matchExpressions)
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "empty-selector-test",
				Selector:  metav1.LabelSelector{}, // Empty selector
			},
		}

		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "should fail to create ClawDevicePairingRequest with empty selector")
		assert.Contains(t, err.Error(), "selector must include at least one matchLabels or matchExpressions entry")
	})

	t.Run("Selector validation - matchLabels only accepted", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-matchlabels-only"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest with only matchLabels
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "matchlabels-only-test",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "should succeed with matchLabels only")

		// Verify resource was created
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		assert.Equal(t, "claw", fetched.Spec.Selector.MatchLabels["app"])
	})

	t.Run("Selector validation - matchExpressions only accepted", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-matchexpressions-only"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest with only matchExpressions
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "matchexpressions-only-test",
				Selector: metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"claw"},
						},
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "should succeed with matchExpressions only")

		// Verify resource was created
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		assert.Len(t, fetched.Spec.Selector.MatchExpressions, 1)
		assert.Equal(t, "app", fetched.Spec.Selector.MatchExpressions[0].Key)
	})

	t.Run("Selector validation - both matchLabels and matchExpressions accepted", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-both-match-types"

		t.Cleanup(func() {
			deleteAndWaitClawDevicePairingRequest(t, namespace, resourceName)
		})

		// Create ClawDevicePairingRequest with both matchLabels and matchExpressions
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "both-match-types-test",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "claw",
					},
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "instance",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"test-instance"},
						},
					},
				},
			},
		}

		require.NoError(t, k8sClient.Create(ctx, instance), "should succeed with both matchLabels and matchExpressions")

		// Verify resource was created
		fetched := &clawv1alpha1.ClawDevicePairingRequest{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, fetched))
		assert.Equal(t, "claw", fetched.Spec.Selector.MatchLabels["app"])
		assert.Len(t, fetched.Spec.Selector.MatchExpressions, 1)
	})

	t.Run("Selector validation - empty matchLabels map rejected", func(t *testing.T) {
		ctx := context.Background()
		resourceName := "test-empty-matchlabels"

		// Attempt to create ClawDevicePairingRequest with empty matchLabels map
		instance := &clawv1alpha1.ClawDevicePairingRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: clawv1alpha1.ClawDevicePairingRequestSpec{
				RequestID: "empty-matchlabels-test",
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{}, // Empty map
				},
			},
		}

		err := k8sClient.Create(ctx, instance)
		require.Error(t, err, "should fail to create ClawDevicePairingRequest with empty matchLabels map")
		assert.Contains(t, err.Error(), "selector must include at least one matchLabels or matchExpressions entry")
	})
}

// deleteAndWaitClawDevicePairingRequest deletes a ClawDevicePairingRequest and waits for it to be removed
func deleteAndWaitClawDevicePairingRequest(t *testing.T, namespace, name string) {
	t.Helper()
	ctx := context.Background()

	instance := &clawv1alpha1.ClawDevicePairingRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := k8sClient.Delete(ctx, instance)
	if err != nil && client.IgnoreNotFound(err) != nil {
		t.Logf("failed to delete ClawDevicePairingRequest %s/%s: %v", namespace, name, err)
	}

	// Wait for resource to be deleted
	waitFor(t, timeout, interval, func() bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance)
		return apierrors.IsNotFound(err)
	}, "ClawDevicePairingRequest should be deleted")
}
