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
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	netv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

func TestOpenClawDeploymentController(t *testing.T) {

	// NOTE: The unified controller creates all resources atomically via server-side apply,
	// so ConfigMap dependency tests are no longer relevant. All resources are created together.

	t.Run("When reconciling an OpenClaw named 'instance'", func(t *testing.T) {
		const resourceName = ClawInstanceName
		ctx := context.Background()

		t.Run("should create Deployment for OpenClaw named 'instance'", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// given
			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			// when
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			// check if Deployment was created
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")
		})

		t.Run("should set correct owner reference on Deployment", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// given
			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			// when
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			// check if Deployment was created
			deployment := &appsv1.Deployment{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawDeploymentName,
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, "Deployment should be created")

			// check Deployment has correct owner reference
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawDeploymentName,
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				if len(deployment.OwnerReferences) == 0 {
					return false
				}
				ownerRef := deployment.OwnerReferences[0]
				return ownerRef.Kind == ClawResourceKind &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, "Deployment should have correct owner reference")
		})

		t.Run("should create ingress NetworkPolicy with correct owner reference", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
			})

			// given
			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			// when
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)

			// then
			np := &netv1.NetworkPolicy{}
			waitFor(t, timeout, interval, func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      ClawIngressNetworkPolicyName,
					Namespace: namespace,
				}, np)
				return err == nil
			}, "Ingress NetworkPolicy should be created")

			// verify owner reference
			require.NotEmpty(t, np.OwnerReferences, "NetworkPolicy should have owner references")
			ownerRef := np.OwnerReferences[0]
			require.Equal(t, ClawResourceKind, ownerRef.Kind)
			require.Equal(t, resourceName, ownerRef.Name)
			require.NotNil(t, ownerRef.Controller)
			require.True(t, *ownerRef.Controller)
		})
	})

	t.Run("When reconciling an OpenClaw with different name", func(t *testing.T) {
		const resourceName = "other-instance"
		ctx := context.Background()

		t.Run("should skip Deployment creation for non-matching names", func(t *testing.T) {
			t.Cleanup(func() {
				deleteAndWaitAllResources(t, namespace)
				if err := deleteAndWait(&openclawv1alpha1.Claw{}, client.ObjectKey{Name: resourceName, Namespace: namespace}); err != nil {
					t.Fatalf("cleanup failed: %v", err)
				}
			})

			// given
			createClawInstance(t, ctx, resourceName, namespace)
			reconciler := createClawReconciler()

			// when
			reconcileClaw(t, ctx, reconciler, resourceName, namespace)
			// verify Deployment was NOT created
			// Sleep to give reconciler time to (incorrectly) create resources
			time.Sleep(2 * time.Second)

			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      ClawDeploymentName,
				Namespace: namespace,
			}, deployment)
			require.Error(t, err, "Deployment should not have been created for non-instance OpenClaw")
		})
	})
}
