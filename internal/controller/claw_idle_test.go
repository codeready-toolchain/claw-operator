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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestClawIdling(t *testing.T) {
	ctx := context.Background()

	t.Run("should scale all deployments to zero when idle is set", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify deployments exist with replicas > 0
		for _, name := range []string{
			getClawDeploymentName(testInstanceName),
			getProxyDeploymentName(testInstanceName),
			getDevicePairingDeploymentName(testInstanceName),
		} {
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment) == nil
			}, name+" Deployment should be created")
			require.NotNil(t, deployment.Spec.Replicas, "replicas should be set on %s", name)
			assert.Equal(t, int32(1), *deployment.Spec.Replicas, "expected 1 replica on %s", name)
		}

		// Set idle
		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.Idle = true
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// All deployments should be scaled to 0
		for _, name := range []string{
			getClawDeploymentName(testInstanceName),
			getProxyDeploymentName(testInstanceName),
			getDevicePairingDeploymentName(testInstanceName),
		} {
			deployment := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment))
			require.NotNil(t, deployment.Spec.Replicas, "replicas should be set on %s", name)
			assert.Equal(t, int32(0), *deployment.Spec.Replicas, "expected 0 replicas on %s after idle", name)
		}
	})

	t.Run("should set correct status conditions when idled", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
		setAllDeploymentsAvailable(t, ctx, testInstanceName, namespace)
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify Ready=True before idling
		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		readyCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionTrue, readyCond.Status)

		// Set idle
		instance.Spec.Idle = true
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify status
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))

		idleCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		require.NotNil(t, idleCond, "Idle condition should be present")
		assert.Equal(t, metav1.ConditionTrue, idleCond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonIdledByRequest, idleCond.Reason)

		readyCond = meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond, "Ready condition should be present")
		assert.Equal(t, metav1.ConditionFalse, readyCond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonIdle, readyCond.Reason)

		assert.Empty(t, instance.Status.URL, "status.url should be cleared when idled") //nolint:staticcheck
		assert.Empty(t, instance.Status.GatewayURL, "status.gatewayURL should be cleared when idled")
		assert.Empty(t, instance.Status.DevicePairingURL, "status.devicePairingURL should be cleared when idled")
	})

	t.Run("should restore normal operation on unidle", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		// Initial reconcile to create resources
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Idle
		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.Idle = true
		require.NoError(t, k8sClient.Update(ctx, instance))
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify idled
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		idleCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		require.NotNil(t, idleCond, "Idle condition should be present after idling")

		// Unidle
		instance.Spec.Idle = false
		require.NoError(t, k8sClient.Update(ctx, instance))
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify unidled: Idle condition removed
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		idleCond = meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		assert.Nil(t, idleCond, "Idle condition should be removed after unidle")

		// Deployments should be back to replicas 1
		for _, name := range []string{
			getClawDeploymentName(testInstanceName),
			getProxyDeploymentName(testInstanceName),
			getDevicePairingDeploymentName(testInstanceName),
		} {
			deployment := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment))
			require.NotNil(t, deployment.Spec.Replicas, "replicas should be set on %s", name)
			assert.Equal(t, int32(1), *deployment.Spec.Replicas, "expected 1 replica on %s after unidle", name)
		}
	})

	t.Run("should be idempotent when already idled", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Idle
		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.Idle = true
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Reconcile again while still idled — should succeed without errors
		// Need to re-fetch since status was updated
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify still idled
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		idleCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		require.NotNil(t, idleCond, "Idle condition should still be present")
		assert.Equal(t, metav1.ConditionTrue, idleCond.Status)

		for _, name := range []string{
			getClawDeploymentName(testInstanceName),
			getProxyDeploymentName(testInstanceName),
			getDevicePairingDeploymentName(testInstanceName),
		} {
			deployment := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment))
			require.NotNil(t, deployment.Spec.Replicas, "replicas should be set on %s", name)
			assert.Equal(t, int32(0), *deployment.Spec.Replicas, "expected 0 replicas on %s", name)
		}
	})

	t.Run("should succeed when idled before deployments exist", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		// Create instance with idle already set
		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Idle = true
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()

		// Reconcile should succeed (no deployments to scale, NotFound skipped)
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify status is set correctly
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))

		idleCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		require.NotNil(t, idleCond, "Idle condition should be present")
		assert.Equal(t, metav1.ConditionTrue, idleCond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonIdledByRequest, idleCond.Reason)

		readyCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond, "Ready condition should be present")
		assert.Equal(t, metav1.ConditionFalse, readyCond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonIdle, readyCond.Reason)
	})

	t.Run("should complete full status transition Ready→Idle→Ready", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		// Phase 1: Ready
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
		setAllDeploymentsAvailable(t, ctx, testInstanceName, namespace)
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		readyCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionTrue, readyCond.Status, "should be Ready before idle")
		assert.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle),
			"Idle condition should not exist when running")

		// Phase 2: Idle
		instance.Spec.Idle = true
		require.NoError(t, k8sClient.Update(ctx, instance))
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		readyCond = meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionFalse, readyCond.Status, "should not be Ready when idled")
		assert.Equal(t, clawv1alpha1.ConditionReasonIdle, readyCond.Reason)
		idleCond := meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		require.NotNil(t, idleCond)
		assert.Equal(t, metav1.ConditionTrue, idleCond.Status)

		// Phase 3: Unidle → Ready
		instance.Spec.Idle = false
		require.NoError(t, k8sClient.Update(ctx, instance))
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)
		setAllDeploymentsAvailable(t, ctx, testInstanceName, namespace)
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		readyCond = meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionTrue, readyCond.Status, "should be Ready after unidle")
		assert.Equal(t, clawv1alpha1.ConditionReasonReady, readyCond.Reason)
		assert.Nil(t, meta.FindStatusCondition(instance.Status.Conditions, clawv1alpha1.ConditionTypeIdle),
			"Idle condition should be removed after unidle")
	})
}
