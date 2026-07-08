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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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

// TestClawOperatorConfigGating table-drives the four ways checkConfigModeAllowed
// can resolve for a given Claw: an explicit deny, an explicit allow, fail-open
// on a missing singleton, and fail-open on an empty allowlist. Each case
// shares the same create-singleton/create-Claw/reconcile/assert-condition
// shape; only the singleton's presence/allowlist, the Claw's mergeMode, and
// the expected outcome vary per case.
func TestClawOperatorConfigGating(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		// singleton is nil when no ClawOperatorConfig should be created at
		// all (exercises the fail-open "missing singleton" path); otherwise
		// it's the AllowedConfigModes to set on the created singleton (an
		// empty-but-non-nil slice exercises the fail-open "empty allowlist"
		// path).
		singleton  []clawv1alpha1.ConfigMode
		clawMode   clawv1alpha1.ConfigMode
		wantDenied bool
	}{
		{
			name:       "disallowed mode sets Ready False with ConfigModeNotAllowed",
			singleton:  []clawv1alpha1.ConfigMode{clawv1alpha1.ConfigModeMerge},
			clawMode:   clawv1alpha1.ConfigModeSeedOnly,
			wantDenied: true,
		},
		{
			name:       "allowed mode has no gating effect",
			singleton:  []clawv1alpha1.ConfigMode{clawv1alpha1.ConfigModeMerge, clawv1alpha1.ConfigModeSeedOnly},
			clawMode:   clawv1alpha1.ConfigModeSeedOnly,
			wantDenied: false,
		},
		{
			name:       "no ClawOperatorConfig singleton fails open",
			singleton:  nil,
			clawMode:   clawv1alpha1.ConfigModeOverwrite,
			wantDenied: false,
		},
		{
			name:       "singleton with empty allowedConfigModes allows everything",
			singleton:  []clawv1alpha1.ConfigMode{},
			clawMode:   clawv1alpha1.ConfigModeSeedOnly,
			wantDenied: false,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resourceName := fmt.Sprintf("gating-case-%d", i)
			t.Cleanup(func() { deleteAndWaitAllResources(t, namespace, resourceName) })

			opNamespace := setupOperatorNamespace(t, ctx, resourceName+"-op")
			if tc.singleton != nil {
				opConfig := &clawv1alpha1.ClawOperatorConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
						Namespace: opNamespace,
					},
					Spec: clawv1alpha1.ClawOperatorConfigSpec{AllowedConfigModes: tc.singleton},
				}
				require.NoError(t, k8sClient.Create(ctx, opConfig))
				t.Cleanup(func() { _ = k8sClient.Delete(ctx, opConfig) })
			}

			instance := testClawWithMergeMode(resourceName, namespace, tc.clawMode)
			if !tc.wantDenied {
				// Only the allowed/fail-open cases reconcile far enough to need
				// real credentials; the denied case halts before credential
				// resolution, so a missing secret there would false-pass.
				secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
				require.NoError(t, k8sClient.Create(ctx, secret))
				instance.Spec.Credentials = testCredentials()
			}
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
			require.NoError(t, err, "reconcile should not return an error regardless of gating outcome "+
				"(a denial is a stable policy state, not a transient failure)")

			updated := &clawv1alpha1.Claw{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
			cond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
			if tc.wantDenied {
				require.NotNil(t, cond, "Ready condition should be set")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, cond.Reason)
			} else if cond != nil {
				assert.NotEqual(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, cond.Reason,
					"an allowed/fail-open case must never produce the gating failure reason")
			}
		})
	}
}

// TestConfigModeRetroactiveEnforcement covers the gap left by gating alone:
// checkConfigModeAllowed only ever ran on a Claw's very first reconcile, so
// an already-running instance whose mode became disallowed later (an admin
// tightening policy) kept running in the disallowed mode forever, with only
// a Ready:False status flag to show for it. handlePolicyIdle closes that gap
// by actively scaling an out-of-policy instance to zero, the same way
// spec.idle does, so policy is enforceable fleet-wide and not just for
// brand-new Claws. See docs/adr/0021-seed-only-config-mode.md.
func TestConfigModeRetroactiveEnforcement(t *testing.T) {
	ctx := context.Background()
	resourceName := "retroactive-enforcement"
	t.Cleanup(func() { deleteAndWaitAllResources(t, namespace, resourceName) })

	opNamespace := setupOperatorNamespace(t, ctx, resourceName+"-op")
	opConfig := &clawv1alpha1.ClawOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
			Namespace: opNamespace,
		},
		Spec: clawv1alpha1.ClawOperatorConfigSpec{
			AllowedConfigModes: []clawv1alpha1.ConfigMode{clawv1alpha1.ConfigModeMerge, clawv1alpha1.ConfigModeSeedOnly},
		},
	}
	require.NoError(t, k8sClient.Create(ctx, opConfig))

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

	coreDeployments := []string{
		getClawDeploymentName(resourceName),
		getProxyDeploymentName(resourceName),
	}

	t.Run("runs normally while seedOnly is allowed", func(t *testing.T) {
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)
		setCoreDeploymentsAvailable(t, ctx, resourceName, namespace)
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		for _, name := range coreDeployments {
			deployment := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment))
			require.NotNil(t, deployment.Spec.Replicas)
			assert.Equal(t, int32(1), *deployment.Spec.Replicas, "expected 1 replica on %s", name)
		}

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))
		readyCond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionTrue, readyCond.Status)
	})

	t.Run("tightening policy scales the running instance to zero", func(t *testing.T) {
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: clawv1alpha1.ClawOperatorConfigSingletonName, Namespace: opNamespace,
		}, opConfig))
		opConfig.Spec.AllowedConfigModes = []clawv1alpha1.ConfigMode{clawv1alpha1.ConfigModeMerge}
		require.NoError(t, k8sClient.Update(ctx, opConfig))

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		for _, name := range coreDeployments {
			deployment := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment))
			require.NotNil(t, deployment.Spec.Replicas)
			assert.Equal(t, int32(0), *deployment.Spec.Replicas,
				"expected %s to be scaled to zero once seedOnly became disallowed", name)
		}

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))

		readyCond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionFalse, readyCond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonConfigModeNotAllowed, readyCond.Reason)

		idleCond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeIdle)
		require.NotNil(t, idleCond, "Idle condition should be set when scaled to zero by policy")
		assert.Equal(t, metav1.ConditionTrue, idleCond.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonIdledByPolicy, idleCond.Reason)

		// Spec is never mutated on the user's behalf — the CR still literally
		// requests seedOnly; only the running Deployments were touched.
		assert.Equal(t, clawv1alpha1.ConfigModeSeedOnly, updated.Spec.Config.MergeMode)
	})

	t.Run("widening policy again restores normal operation", func(t *testing.T) {
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{
			Name: clawv1alpha1.ClawOperatorConfigSingletonName, Namespace: opNamespace,
		}, opConfig))
		opConfig.Spec.AllowedConfigModes = []clawv1alpha1.ConfigMode{clawv1alpha1.ConfigModeMerge, clawv1alpha1.ConfigModeSeedOnly}
		require.NoError(t, k8sClient.Update(ctx, opConfig))

		reconcileClaw(t, ctx, reconciler, resourceName, namespace)
		setCoreDeploymentsAvailable(t, ctx, resourceName, namespace)
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		for _, name := range coreDeployments {
			deployment := &appsv1.Deployment{}
			require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deployment))
			require.NotNil(t, deployment.Spec.Replicas)
			assert.Equal(t, int32(1), *deployment.Spec.Replicas, "expected %s restored to 1 replica", name)
		}

		updated := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, updated))

		assert.Nil(t, meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeIdle),
			"Idle condition should be removed once policy allows the mode again")

		readyCond := meta.FindStatusCondition(updated.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, readyCond)
		assert.Equal(t, metav1.ConditionTrue, readyCond.Status)
	})
}

func TestClawOperatorConfigNameValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects a name other than the singleton at admission time", func(t *testing.T) {
		opNamespace := setupOperatorNamespace(t, ctx, "name-validation-op")
		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster", Namespace: opNamespace},
		}

		err := k8sClient.Create(ctx, opConfig)
		require.Error(t, err, "the API server must reject a non-singleton name via the CEL XValidation rule")
		assert.Contains(t, err.Error(), "must be named 'cluster'")
	})

	t.Run("accepts the singleton name", func(t *testing.T) {
		opNamespace := setupOperatorNamespace(t, ctx, "name-validation-ok-op")
		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: opNamespace,
			},
		}

		require.NoError(t, k8sClient.Create(ctx, opConfig))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, opConfig) })
	})
}

func TestFindAllClaws(t *testing.T) {
	ctx := context.Background()

	t.Run("should map the singleton ClawOperatorConfig change to every Claw CR", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: namespace,
		}

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
		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: namespace,
		}

		opConfig := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: namespace,
			},
		}

		requests := reconciler.findAllClaws(ctx, opConfig)
		assert.Empty(t, requests)
	})

	t.Run("should ignore a ClawOperatorConfig with the wrong name", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: namespace,
		}

		notTheSingleton := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster", Namespace: namespace},
		}

		requests := reconciler.findAllClaws(ctx, notTheSingleton)
		assert.Empty(t, requests, "only the singleton name should trigger a cluster-wide fan-out")
	})

	t.Run("should ignore a same-named ClawOperatorConfig outside the operator namespace", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })
		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := &ClawResourceReconciler{
			Client:            k8sClient,
			Scheme:            scheme.Scheme,
			UserSecretReader:  k8sClient,
			OperatorNamespace: "some-other-operator-namespace",
		}

		wrongNamespace := &clawv1alpha1.ClawOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clawv1alpha1.ClawOperatorConfigSingletonName,
				Namespace: namespace,
			},
		}

		requests := reconciler.findAllClaws(ctx, wrongNamespace)
		assert.Empty(t, requests, "a same-named object outside the operator's own namespace is not the policy singleton")
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
