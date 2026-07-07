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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// setupOperatorNamespace creates a fresh namespace to stand in for the
// operator's own runtime namespace (WATCH_NAMESPACE), so ClawOperatorConfig
// gating tests don't collide with each other or with the "default" tenant
// namespace used by other tests.
func setupOperatorNamespace(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	require.NoError(t, k8sClient.Create(ctx, ns))
	t.Cleanup(func() {
		_ = k8sClient.Delete(ctx, ns)
	})
	return name
}

func testClawWithMergeMode(name, namespace string, mode clawv1alpha1.ConfigMode) *clawv1alpha1.Claw {
	instance := &clawv1alpha1.Claw{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	if mode != "" {
		instance.Spec.Config = &clawv1alpha1.ConfigSpec{MergeMode: mode}
	}
	return instance
}

func TestClawOperatorConfigGating(t *testing.T) {
	ctx := context.Background()

	t.Run("disallowed mode sets Ready False with ConfigModeNotAllowed", func(t *testing.T) {
		resourceName := "gating-disallowed"
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace, resourceName) })

		opNamespace := setupOperatorNamespace(t, ctx, "gating-disallowed-op")
		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: opNamespace,
			},
			Spec: clawv1alpha1.ClawOperatorConfigSpec{
				AllowedConfigModes: []clawv1alpha1.ConfigMode{clawv1alpha1.ConfigModeMerge},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, opConfig))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, opConfig) })

		instance := testClawWithMergeMode(resourceName, namespace, clawv1alpha1.ConfigModeSeedOnly)
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: opNamespace,
		}

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: resourceName, Namespace: namespace},
		})
		require.NoError(t, err, "gating denial should not return an error (stable policy state, not transient failure)")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		cond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, cond, "Ready condition should be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, cond.Reason)
	})

	t.Run("allowed mode has no gating effect", func(t *testing.T) {
		resourceName := "gating-allowed"
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace, resourceName) })

		opNamespace := setupOperatorNamespace(t, ctx, "gating-allowed-op")
		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: opNamespace,
			},
			Spec: clawv1alpha1.ClawOperatorConfigSpec{
				AllowedConfigModes: []clawv1alpha1.ConfigMode{
					clawv1alpha1.ConfigModeMerge,
					clawv1alpha1.ConfigModeSeedOnly,
				},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, opConfig))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, opConfig) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := testClawWithMergeMode(resourceName, namespace, clawv1alpha1.ConfigModeSeedOnly)
		instance.Spec.Credentials = testCredentials()
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: opNamespace,
		}

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: resourceName, Namespace: namespace},
		})
		require.NoError(t, err, "reconcile should succeed for an allowed mode")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		cond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		if cond != nil {
			assert.NotEqual(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, cond.Reason,
				"an allowed mode must never produce the gating failure reason")
		}
	})

	t.Run("no ClawOperatorConfig singleton fails open", func(t *testing.T) {
		resourceName := "gating-fail-open"
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace, resourceName) })

		opNamespace := setupOperatorNamespace(t, ctx, "gating-fail-open-op")
		// Deliberately no ClawOperatorConfig singleton created in opNamespace.

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := testClawWithMergeMode(resourceName, namespace, clawv1alpha1.ConfigModeOverwrite)
		instance.Spec.Credentials = testCredentials()
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: opNamespace,
		}

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: resourceName, Namespace: namespace},
		})
		require.NoError(t, err, "reconcile should succeed when no ClawOperatorConfig singleton exists (fail-open)")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		cond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		if cond != nil {
			assert.NotEqual(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, cond.Reason,
				"missing ClawOperatorConfig must fail open, not deny")
		}
	})

	t.Run("singleton with empty allowedConfigModes allows everything", func(t *testing.T) {
		resourceName := "gating-empty-allowlist"
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace, resourceName) })

		opNamespace := setupOperatorNamespace(t, ctx, "gating-empty-op")
		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: opNamespace,
			},
			Spec: clawv1alpha1.ClawOperatorConfigSpec{},
		}
		require.NoError(t, k8sClient.Create(ctx, opConfig))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, opConfig) })

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := testClawWithMergeMode(resourceName, namespace, clawv1alpha1.ConfigModeSeedOnly)
		instance.Spec.Credentials = testCredentials()
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: opNamespace,
		}

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: resourceName, Namespace: namespace},
		})
		require.NoError(t, err, "reconcile should succeed when allowedConfigModes is empty (unrestricted)")

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		cond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		if cond != nil {
			assert.NotEqual(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, cond.Reason,
				"empty allowedConfigModes must allow every mode")
		}
	})
}

func TestFindAllClaws(t *testing.T) {
	ctx := context.Background()

	t.Run("should map ClawOperatorConfig change to every Claw CR", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()

		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: namespace,
			},
		}

		requests := reconciler.findAllClaws(ctx, opConfig)
		require.Len(t, requests, 1)
		assert.Equal(t, testInstanceName, requests[0].Name)
		assert.Equal(t, namespace, requests[0].Namespace)
	})

	t.Run("should return empty when no Claw CRs exist", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		reconciler := createClawReconciler()

		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: namespace,
			},
		}

		requests := reconciler.findAllClaws(ctx, opConfig)
		assert.Empty(t, requests)
	})
}

func TestEffectiveConfigMode(t *testing.T) {
	t.Run("defaults to merge when config is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{}
		assert.Equal(t, clawv1alpha1.ConfigModeMerge, effectiveConfigMode(instance))
	})

	t.Run("defaults to merge when mergeMode is empty", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{Spec: clawv1alpha1.ClawSpec{Config: &clawv1alpha1.ConfigSpec{}}}
		assert.Equal(t, clawv1alpha1.ConfigModeMerge, effectiveConfigMode(instance))
	})

	t.Run("returns the explicit mergeMode when set", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{Config: &clawv1alpha1.ConfigSpec{MergeMode: clawv1alpha1.ConfigModeSeedOnly}},
		}
		assert.Equal(t, clawv1alpha1.ConfigModeSeedOnly, effectiveConfigMode(instance))
	})
}
