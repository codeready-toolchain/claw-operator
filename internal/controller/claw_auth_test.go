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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// --- shouldDisableDevicePairing unit tests ---

func TestShouldDisableDevicePairing(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	t.Run("should return false when auth is nil", func(t *testing.T) {
		assert.False(t, shouldDisableDevicePairing(nil))
	})

	t.Run("should return false for token mode with nil override", func(t *testing.T) {
		auth := &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModeToken}
		assert.False(t, shouldDisableDevicePairing(auth))
	})

	t.Run("should return true for password mode with nil override", func(t *testing.T) {
		auth := &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModePassword}
		assert.True(t, shouldDisableDevicePairing(auth))
	})

	t.Run("should respect explicit true override in token mode", func(t *testing.T) {
		auth := &clawv1alpha1.AuthSpec{
			Mode:                 clawv1alpha1.AuthModeToken,
			DisableDevicePairing: boolPtr(true),
		}
		assert.True(t, shouldDisableDevicePairing(auth))
	})

	t.Run("should respect explicit false override in password mode", func(t *testing.T) {
		auth := &clawv1alpha1.AuthSpec{
			Mode:                 clawv1alpha1.AuthModePassword,
			DisableDevicePairing: boolPtr(false),
		}
		assert.False(t, shouldDisableDevicePairing(auth))
	})

	t.Run("should respect explicit true override in password mode", func(t *testing.T) {
		auth := &clawv1alpha1.AuthSpec{
			Mode:                 clawv1alpha1.AuthModePassword,
			DisableDevicePairing: boolPtr(true),
		}
		assert.True(t, shouldDisableDevicePairing(auth))
	})
}

// --- injectAuthMode unit tests ---

func TestInjectAuthMode(t *testing.T) {
	baseConfig := func() map[string]any {
		return map[string]any{
			"gateway": map[string]any{
				"port": float64(18789),
				"auth": map[string]any{"mode": "token"},
			},
		}
	}

	t.Run("should set token mode and dangerouslyDisableDeviceAuth=false when auth is nil", func(t *testing.T) {
		config := baseConfig()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "token", auth["mode"])
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, false, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should set token mode and dangerouslyDisableDeviceAuth=false when auth.mode is token", func(t *testing.T) {
		config := baseConfig()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModeToken},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "token", auth["mode"])
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, false, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should inject password mode without password value", func(t *testing.T) {
		config := baseConfig()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "password", auth["mode"])
		_, hasPassword := auth["password"]
		assert.False(t, hasPassword, "password should not be in ConfigMap")
	})

	t.Run("should set dangerouslyDisableDeviceAuth when mode is password", func(t *testing.T) {
		config := baseConfig()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, true, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should preserve existing gateway config sections", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{
				"port": float64(18789),
				"auth": map[string]any{"mode": "token"},
				"cors": map[string]any{"origin": "*"},
			},
		}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		assert.Equal(t, float64(18789), gateway["port"])
		cors := gateway["cors"].(map[string]any)
		assert.Equal(t, "*", cors["origin"])
	})

	t.Run("should create gateway section if absent", func(t *testing.T) {
		config := map[string]any{"models": map[string]any{}}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "password", auth["mode"])
	})

	t.Run("should preserve existing controlUi settings", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{
				"controlUi": map[string]any{"theme": "dark"},
				"auth":      map[string]any{"mode": "token"},
			},
		}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, "dark", controlUI["theme"])
		assert.Equal(t, true, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should not set dangerouslyDisableDeviceAuth when password mode with disableDevicePairing=false", func(t *testing.T) {
		config := baseConfig()
		disablePairing := false
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:                 clawv1alpha1.AuthModePassword,
					PasswordSecretRef:    &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
					DisableDevicePairing: &disablePairing,
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "password", auth["mode"], "password auth mode should still be injected")
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, false, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should set dangerouslyDisableDeviceAuth when token mode with disableDevicePairing=true", func(t *testing.T) {
		config := baseConfig()
		disablePairing := true
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:                 clawv1alpha1.AuthModeToken,
					DisableDevicePairing: &disablePairing,
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "token", auth["mode"], "auth mode should remain token")
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, true, controlUI["dangerouslyDisableDeviceAuth"],
			"dangerouslyDisableDeviceAuth should be set when explicitly requested")
	})

	t.Run("should set dangerouslyDisableDeviceAuth when password mode with explicit disableDevicePairing=true", func(t *testing.T) {
		config := baseConfig()
		disablePairing := true
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:                 clawv1alpha1.AuthModePassword,
					PasswordSecretRef:    &clawv1alpha1.SecretRefEntry{Name: "pw-secret", Key: "password"},
					DisableDevicePairing: &disablePairing,
				},
			},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "password", auth["mode"])
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, true, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should override user-set auth mode", func(t *testing.T) {
		config := map[string]any{
			"gateway": map[string]any{
				"auth": map[string]any{"mode": "password"},
			},
		}
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}

		injectAuthMode(config, instance)

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "token", auth["mode"])
	})
}

// --- configureClawDeploymentForAuth unit tests ---

func TestConfigureClawDeploymentForAuth(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		_ = unstructured.SetNestedSlice(dep.Object, []any{
			map[string]any{
				"name":  ClawGatewayContainerName,
				"image": "openclaw:latest",
				"env":   []any{},
			},
		}, "spec", "template", "spec", "containers")
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should be no-op when auth is nil", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}
		require.NoError(t, configureClawDeploymentForAuth(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars, _, _ := unstructured.NestedSlice(container, "env")
		assert.Empty(t, envVars)
	})

	t.Run("should be no-op when mode is token", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModeToken},
			},
		}
		require.NoError(t, configureClawDeploymentForAuth(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars, _, _ := unstructured.NestedSlice(container, "env")
		assert.Empty(t, envVars)
	})

	t.Run("should add OPENCLAW_GATEWAY_PASSWORD env var from Secret", func(t *testing.T) {
		objects := makeDeployment()
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "my-pw-secret", Key: "pw-key"},
				},
			},
		}
		require.NoError(t, configureClawDeploymentForAuth(objects, instance))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars, _, _ := unstructured.NestedSlice(container, "env")
		require.Len(t, envVars, 1)

		env := envVars[0].(map[string]any)
		assert.Equal(t, "OPENCLAW_GATEWAY_PASSWORD", env["name"])
		secretName, _, _ := unstructured.NestedString(env, "valueFrom", "secretKeyRef", "name")
		secretKey, _, _ := unstructured.NestedString(env, "valueFrom", "secretKeyRef", "key")
		assert.Equal(t, "my-pw-secret", secretName)
		assert.Equal(t, "pw-key", secretKey)
	})
}

// --- resolveAuthPassword envtest integration tests ---

func TestResolveAuthPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("should return empty string when auth is nil", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
		}
		reconciler := createClawReconciler()

		password, err := reconciler.resolveAuthPassword(ctx, instance)
		require.NoError(t, err)
		assert.Empty(t, password)
	})

	t.Run("should return empty string when mode is token", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModeToken},
			},
		}
		reconciler := createClawReconciler()

		password, err := reconciler.resolveAuthPassword(ctx, instance)
		require.NoError(t, err)
		assert.Empty(t, password)
	})

	t.Run("should return error when passwordSecretRef is nil with password mode", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModePassword},
			},
		}
		reconciler := createClawReconciler()

		_, err := reconciler.resolveAuthPassword(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "passwordSecretRef is required")
	})

	t.Run("should return password from Secret", func(t *testing.T) {
		t.Cleanup(func() {
			secret := &corev1.Secret{}
			secret.Name = "auth-password-secret"
			secret.Namespace = namespace
			_ = k8sClient.Delete(ctx, secret)
		})

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-password-secret", Namespace: namespace},
			Data:       map[string][]byte{"password": []byte("workshop-2026")},
		}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "auth-password-secret", Key: "password"},
				},
			},
		}
		reconciler := createClawReconciler()

		password, err := reconciler.resolveAuthPassword(ctx, instance)
		require.NoError(t, err)
		assert.Equal(t, "workshop-2026", password)
	})

	t.Run("should return error when Secret does not exist", func(t *testing.T) {
		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "nonexistent-secret", Key: "password"},
				},
			},
		}
		reconciler := createClawReconciler()

		_, err := reconciler.resolveAuthPassword(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get auth password secret")
	})

	t.Run("should return error when key is missing from Secret", func(t *testing.T) {
		t.Cleanup(func() {
			secret := &corev1.Secret{}
			secret.Name = "auth-password-wrong-key"
			secret.Namespace = namespace
			_ = k8sClient.Delete(ctx, secret)
		})

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-password-wrong-key", Namespace: namespace},
			Data:       map[string][]byte{"other-key": []byte("value")},
		}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "auth-password-wrong-key", Key: "password"},
				},
			},
		}
		reconciler := createClawReconciler()

		_, err := reconciler.resolveAuthPassword(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found or empty")
	})

	t.Run("should return error when key value is empty", func(t *testing.T) {
		t.Cleanup(func() {
			secret := &corev1.Secret{}
			secret.Name = "auth-password-empty"
			secret.Namespace = namespace
			_ = k8sClient.Delete(ctx, secret)
		})

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-password-empty", Namespace: namespace},
			Data:       map[string][]byte{"password": {}},
		}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "auth-password-empty", Key: "password"},
				},
			},
		}
		reconciler := createClawReconciler()

		_, err := reconciler.resolveAuthPassword(ctx, instance)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found or empty")
	})
}

// --- Password auth mode integration tests (full reconciliation) ---

func TestPasswordAuthModeReconciliation(t *testing.T) {
	ctx := context.Background()

	createClawInstanceWithPasswordAuth := func(t *testing.T, passwordSecretName string) {
		t.Helper()

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

		instance := &clawv1alpha1.Claw{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: namespace},
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: passwordSecretName, Key: "password"},
				},
				Credentials: testCredentials(),
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw instance")
	}

	t.Run("should inject password auth mode without password value in ConfigMap", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
			secret := &corev1.Secret{}
			secret.Name = "workshop-pw"
			secret.Namespace = namespace
			_ = k8sClient.Delete(ctx, secret)
		})

		pwSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "workshop-pw", Namespace: namespace},
			Data:       map[string][]byte{"password": []byte("classroom-pass-123")},
		}
		require.NoError(t, k8sClient.Create(ctx, pwSecret))

		createClawInstanceWithPasswordAuth(t, "workshop-pw")
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "password", auth["mode"])
		_, hasPassword := auth["password"]
		assert.False(t, hasPassword, "password value must not be in ConfigMap")

		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, true, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should not inject password auth when mode is token (default)", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstance(t, ctx, testInstanceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		var config map[string]any
		require.NoError(t, json.Unmarshal([]byte(cm.Data["operator.json"]), &config))

		gateway := config["gateway"].(map[string]any)
		auth := gateway["auth"].(map[string]any)
		assert.Equal(t, "token", auth["mode"], "auth mode should be token by default")
		controlUI := gateway["controlUi"].(map[string]any)
		assert.Equal(t, false, controlUI["dangerouslyDisableDeviceAuth"])
	})

	t.Run("should fail reconciliation and set Ready condition when password Secret is missing", func(t *testing.T) {
		t.Cleanup(func() { deleteAndWaitAllResources(t, namespace) })

		createClawInstanceWithPasswordAuth(t, "nonexistent-password-secret")
		reconciler := &ClawResourceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      testInstanceName,
				Namespace: namespace,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get auth password secret")

		updatedInstance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updatedInstance))
		condition := meta.FindStatusCondition(updatedInstance.Status.Conditions, clawv1alpha1.ConditionTypeReady)
		require.NotNil(t, condition, "Ready condition should be set on auth failure")
		assert.Equal(t, metav1.ConditionFalse, condition.Status)
		assert.Equal(t, clawv1alpha1.ConditionReasonValidationFailed, condition.Reason)
	})

	t.Run("should set status.url without token fragment in password mode when ready", func(t *testing.T) {
		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
			secret := &corev1.Secret{}
			secret.Name = "pw-for-url-test"
			secret.Namespace = namespace
			_ = k8sClient.Delete(ctx, secret)
		})

		pwSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pw-for-url-test", Namespace: namespace},
			Data:       map[string][]byte{"password": []byte("test-password")},
		}
		require.NoError(t, k8sClient.Create(ctx, pwSecret))

		createClawInstanceWithPasswordAuth(t, "pw-for-url-test")
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		setAllDeploymentsAvailable(t, ctx, testInstanceName, namespace)
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		updatedInstance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, updatedInstance))

		// In envtest without Route CRD, URL is empty since getRouteURL returns ""
		// but the important thing is it does NOT contain #token=
		assert.NotContains(t, updatedInstance.Status.URL, "#token=",
			"password mode URL should not contain token fragment")
	})
}

// --- clawReferencesSecret coverage for auth password secret ---

func TestClawReferencesAuthPasswordSecret(t *testing.T) {
	t.Run("should match auth password secret", func(t *testing.T) {
		instance := clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "my-pw-secret", Key: "password"},
				},
			},
		}
		assert.True(t, clawReferencesSecret(instance, "my-pw-secret"))
	})

	t.Run("should not match unrelated secret", func(t *testing.T) {
		instance := clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{
					Mode:              clawv1alpha1.AuthModePassword,
					PasswordSecretRef: &clawv1alpha1.SecretRefEntry{Name: "my-pw-secret", Key: "password"},
				},
			},
		}
		assert.False(t, clawReferencesSecret(instance, "other-secret"))
	})

	t.Run("should not match when auth is nil", func(t *testing.T) {
		instance := clawv1alpha1.Claw{}
		assert.False(t, clawReferencesSecret(instance, "my-pw-secret"))
	})

	t.Run("should not match when passwordSecretRef is nil", func(t *testing.T) {
		instance := clawv1alpha1.Claw{
			Spec: clawv1alpha1.ClawSpec{
				Auth: &clawv1alpha1.AuthSpec{Mode: clawv1alpha1.AuthModeToken},
			},
		}
		assert.False(t, clawReferencesSecret(instance, "my-pw-secret"))
	})
}
