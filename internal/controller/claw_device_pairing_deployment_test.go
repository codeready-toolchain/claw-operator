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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func TestDevicePairingDeployment(t *testing.T) {

	t.Run("buildKustomizedObjects excludes device-pairing resources when disableDevicePairing is true", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		instance.Spec.Auth = &clawv1alpha1.AuthSpec{
			DisableDevicePairing: boolPtr(true),
		}
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err, "buildKustomizedObjects failed")

		devicePairingKinds := map[string]string{
			"ServiceAccount": getDevicePairingServiceAccountName(testInstanceName),
			"Deployment":     getDevicePairingDeploymentName(testInstanceName),
			"Service":        getDevicePairingServiceName(testInstanceName),
			"Route":          getDevicePairingRouteName(testInstanceName),
		}

		for kind, name := range devicePairingKinds {
			for _, obj := range objects {
				if obj.GetKind() == kind && obj.GetName() == name {
					t.Errorf("unexpected %s/%s in buildKustomizedObjects output when device pairing disabled", kind, name)
				}
			}
		}

		// Claw and proxy resources should still be present
		var foundClawDeployment, foundProxyDeployment bool
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == getClawDeploymentName(testInstanceName) {
				foundClawDeployment = true
			}
			if obj.GetKind() == DeploymentKind && obj.GetName() == getProxyDeploymentName(testInstanceName) {
				foundProxyDeployment = true
			}
		}
		assert.True(t, foundClawDeployment, "claw Deployment should be present")
		assert.True(t, foundProxyDeployment, "proxy Deployment should be present")
	})

	t.Run("buildKustomizedObjects includes device-pairing resources", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err, "buildKustomizedObjects failed")

		expectedResources := map[string]string{
			"ServiceAccount": getDevicePairingServiceAccountName(testInstanceName),
			"Deployment":     getDevicePairingDeploymentName(testInstanceName),
			"Service":        getDevicePairingServiceName(testInstanceName),
			"Route":          getDevicePairingRouteName(testInstanceName),
		}

		for kind, name := range expectedResources {
			found := false
			for _, obj := range objects {
				if obj.GetKind() == kind && obj.GetName() == name {
					found = true
					break
				}
			}
			assert.True(t, found, "expected %s/%s in buildKustomizedObjects output", kind, name)
		}
	})

	t.Run("CLAW_INSTANCE_NAME replacement works for device-pairing resources", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		for _, obj := range objects {
			name := obj.GetName()
			assert.NotContains(t, name, "CLAW_INSTANCE_NAME",
				"resource %s/%s still contains placeholder", obj.GetKind(), name)
		}
	})

	t.Run("device-pairing Route has correct path", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		var dpRoute *unstructured.Unstructured
		for _, obj := range objects {
			if obj.GetKind() == RouteKind && obj.GetName() == getDevicePairingRouteName(testInstanceName) {
				dpRoute = obj
				break
			}
		}
		require.NotNil(t, dpRoute, "device-pairing Route not found")

		path, found, err := unstructured.NestedString(dpRoute.Object, "spec", "path")
		require.NoError(t, err)
		assert.True(t, found, ".spec.path should be set")
		assert.Equal(t, "/integration/device-pairing/", path)
	})

	t.Run("device-pairing Route host injection sets spec.host", func(t *testing.T) {
		dpRoute := &unstructured.Unstructured{}
		dpRoute.SetKind(RouteKind)
		dpRoute.SetName(getDevicePairingRouteName(testInstanceName))
		dpRoute.Object["spec"] = map[string]any{
			"path": "/integration/device-pairing",
		}

		objects := []*unstructured.Unstructured{dpRoute}
		require.NoError(t, injectRouteHostIntoDevicePairingRoute(objects, "https://claw.example.com", testInstanceName))

		host, found, err := unstructured.NestedString(dpRoute.Object, "spec", "host")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "claw.example.com", host, "should strip https:// prefix and set bare hostname")
	})

	t.Run("device-pairing Route host injection errors when route not found", func(t *testing.T) {
		otherRoute := &unstructured.Unstructured{}
		otherRoute.SetKind(RouteKind)
		otherRoute.SetName("other-route")
		otherRoute.Object["spec"] = map[string]any{
			"path": "/something-else",
		}

		objects := []*unstructured.Unstructured{otherRoute}
		err := injectRouteHostIntoDevicePairingRoute(objects, "https://claw.example.com", testInstanceName)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in rendered manifests")

		_, found, _ := unstructured.NestedString(otherRoute.Object, "spec", "host")
		assert.False(t, found, "should not set host on other routes")
	})

	t.Run("device-pairing Deployment has correct security context", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		var dpDeployment *unstructured.Unstructured
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == getDevicePairingDeploymentName(testInstanceName) {
				dpDeployment = obj
				break
			}
		}
		require.NotNil(t, dpDeployment, "device-pairing Deployment not found")

		containers, found, err := unstructured.NestedSlice(dpDeployment.Object, "spec", "template", "spec", "containers")
		require.NoError(t, err)
		require.True(t, found, "containers not found")
		require.NotEmpty(t, containers)

		container, ok := containers[0].(map[string]any)
		require.True(t, ok, "container should be a map")
		secCtx, ok := container["securityContext"].(map[string]any)
		require.True(t, ok, "securityContext should be a map")
		assert.Equal(t, false, secCtx["allowPrivilegeEscalation"])
		assert.Equal(t, true, secCtx["readOnlyRootFilesystem"])

		caps, ok := secCtx["capabilities"].(map[string]any)
		require.True(t, ok, "capabilities should be a map")
		drop, ok := caps["drop"].([]any)
		require.True(t, ok, "drop should be a slice")
		assert.Contains(t, drop, "ALL")
	})

	t.Run("device-pairing Deployment references correct ServiceAccount", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		var dpDeployment *unstructured.Unstructured
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == getDevicePairingDeploymentName(testInstanceName) {
				dpDeployment = obj
				break
			}
		}
		require.NotNil(t, dpDeployment)

		sa, found, err := unstructured.NestedString(dpDeployment.Object, "spec", "template", "spec", "serviceAccountName")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, getDevicePairingServiceAccountName(testInstanceName), sa)
	})

	t.Run("device-pairing Deployment has NAMESPACE and CLAW_INSTANCE env vars", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		var dpDeployment *unstructured.Unstructured
		for _, obj := range objects {
			if obj.GetKind() == DeploymentKind && obj.GetName() == getDevicePairingDeploymentName(testInstanceName) {
				dpDeployment = obj
				break
			}
		}
		require.NotNil(t, dpDeployment)

		containers, _, err := unstructured.NestedSlice(dpDeployment.Object, "spec", "template", "spec", "containers")
		require.NoError(t, err)
		require.NotEmpty(t, containers)

		container, ok := containers[0].(map[string]any)
		require.True(t, ok, "container should be a map")
		envVars, ok := container["env"].([]any)
		require.True(t, ok, "env should be a slice")
		require.NotEmpty(t, envVars, "container should have env vars")

		envMap := map[string]any{}
		for _, e := range envVars {
			env, ok := e.(map[string]any)
			require.True(t, ok, "env entry should be a map")
			name, ok := env["name"].(string)
			require.True(t, ok, "env name should be a string")
			envMap[name] = env
		}

		nsEnv, ok := envMap["NAMESPACE"]
		require.True(t, ok, "NAMESPACE env var should exist")
		nsEnvMap, ok := nsEnv.(map[string]any)
		require.True(t, ok, "NAMESPACE env should be a map")
		valueFrom, ok := nsEnvMap["valueFrom"].(map[string]any)
		require.True(t, ok, "valueFrom should be a map")
		fieldRef, ok := valueFrom["fieldRef"].(map[string]any)
		require.True(t, ok, "fieldRef should be a map")
		assert.Equal(t, "metadata.namespace", fieldRef["fieldPath"])

		clawEnv, ok := envMap["CLAW_INSTANCE"]
		require.True(t, ok, "CLAW_INSTANCE env var should exist")
		clawEnvMap, ok := clawEnv.(map[string]any)
		require.True(t, ok, "CLAW_INSTANCE env should be a map")
		assert.Equal(t, testInstanceName, clawEnvMap["value"], "CLAW_INSTANCE should be the instance name after template replacement")
	})

	t.Run("device-pairing Role is namespace-scoped with correct rules", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		roleName := testInstanceName + "-device-pairing"
		var dpRole *unstructured.Unstructured
		for _, obj := range objects {
			if obj.GetKind() == "Role" && obj.GetName() == roleName {
				dpRole = obj
				break
			}
		}
		require.NotNil(t, dpRole, "device-pairing Role not found")

		// Verify no ClusterRole exists
		for _, obj := range objects {
			if obj.GetKind() == "ClusterRole" && obj.GetName() == roleName {
				t.Errorf("unexpected ClusterRole %s — should be a namespace-scoped Role", roleName)
			}
		}

		rules, found, err := unstructured.NestedSlice(dpRole.Object, "rules")
		require.NoError(t, err)
		require.True(t, found, "rules should be present")
		require.Len(t, rules, 1)

		rule, ok := rules[0].(map[string]any)
		require.True(t, ok)

		apiGroups, _, _ := unstructured.NestedStringSlice(rule, "apiGroups")
		assert.Equal(t, []string{"claw.sandbox.redhat.com"}, apiGroups)

		resources, _, _ := unstructured.NestedStringSlice(rule, "resources")
		assert.Equal(t, []string{"clawdevicepairingrequests"}, resources)

		verbs, _, _ := unstructured.NestedStringSlice(rule, "verbs")
		assert.Equal(t, []string{"create", "get"}, verbs)
	})

	t.Run("device-pairing RoleBinding references Role not ClusterRole", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		rbName := testInstanceName + "-device-pairing"
		var dpRB *unstructured.Unstructured
		for _, obj := range objects {
			if obj.GetKind() == "RoleBinding" && obj.GetName() == rbName {
				dpRB = obj
				break
			}
		}
		require.NotNil(t, dpRB, "device-pairing RoleBinding not found")

		roleRefKind, found, err := unstructured.NestedString(dpRB.Object, "roleRef", "kind")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "Role", roleRefKind, "roleRef.kind should be Role, not ClusterRole")

		roleRefName, found, err := unstructured.NestedString(dpRB.Object, "roleRef", "name")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, rbName, roleRefName)
	})

	t.Run("device-pairing resources have app.kubernetes.io/name label", func(t *testing.T) {
		reconciler := createClawReconciler()
		instance := testClawWithCredentials(testCredentials())
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)

		dpNames := map[string]bool{
			getDevicePairingServiceAccountName(testInstanceName): true,
			getDevicePairingDeploymentName(testInstanceName):     true,
			getDevicePairingServiceName(testInstanceName):        true,
			getDevicePairingRouteName(testInstanceName):          true,
		}

		for _, obj := range objects {
			if dpNames[obj.GetName()] {
				labels := obj.GetLabels()
				assert.Equal(t, "claw-device-pairing", labels["app.kubernetes.io/name"],
					"%s/%s should have app.kubernetes.io/name=claw-device-pairing", obj.GetKind(), obj.GetName())
			}
		}
	})
}

func TestDevicePairingReconciliation(t *testing.T) {

	t.Run("should not create device-pairing resources when disableDevicePairing is true", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

		instance := &clawv1alpha1.Claw{}
		instance.Name = resourceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Auth = &clawv1alpha1.AuthSpec{
			DisableDevicePairing: boolPtr(true),
		}
		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw instance")

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		// Core resources should exist
		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(resourceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "claw Deployment should be created")

		// Device-pairing resources should NOT exist
		dpDeployment := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      getDevicePairingDeploymentName(resourceName),
			Namespace: namespace,
		}, dpDeployment)
		assert.True(t, apierrors.IsNotFound(err), "device-pairing Deployment should not exist")

		dpService := &corev1.Service{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      getDevicePairingServiceName(resourceName),
			Namespace: namespace,
		}, dpService)
		assert.True(t, apierrors.IsNotFound(err), "device-pairing Service should not exist")

		dpSA := &corev1.ServiceAccount{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      getDevicePairingServiceAccountName(resourceName),
			Namespace: namespace,
		}, dpSA)
		assert.True(t, apierrors.IsNotFound(err), "device-pairing ServiceAccount should not exist")
	})

	t.Run("should clean up device-pairing resources when disableDevicePairing is toggled to true", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		// Create with device pairing enabled (default)
		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		// Verify device-pairing resources were created
		dpDeployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingDeploymentName(resourceName),
				Namespace: namespace,
			}, dpDeployment) == nil
		}, "device-pairing Deployment should be created initially")

		// Toggle disableDevicePairing to true
		instance := &clawv1alpha1.Claw{}
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance))
		instance.Spec.Auth = &clawv1alpha1.AuthSpec{
			DisableDevicePairing: boolPtr(true),
		}
		require.NoError(t, k8sClient.Update(ctx, instance))
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		// Device-pairing resources should be cleaned up
		waitFor(t, timeout, interval, func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingDeploymentName(resourceName),
				Namespace: namespace,
			}, dpDeployment)
			return err != nil
		}, "device-pairing Deployment should be deleted after disabling")
	})

	t.Run("should recreate device-pairing resources when toggled back to false", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
		require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

		instance := &clawv1alpha1.Claw{}
		instance.Name = resourceName
		instance.Namespace = namespace
		instance.Spec.Credentials = testCredentials()
		instance.Spec.Auth = &clawv1alpha1.AuthSpec{
			DisableDevicePairing: boolPtr(true),
		}
		require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw instance")

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		// Verify device-pairing resources do NOT exist
		dpDeployment := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      getDevicePairingDeploymentName(resourceName),
			Namespace: namespace,
		}, dpDeployment)
		assert.True(t, apierrors.IsNotFound(err),
			"device-pairing Deployment should not exist when disabled")

		dpService := &corev1.Service{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      getDevicePairingServiceName(resourceName),
			Namespace: namespace,
		}, dpService)
		assert.True(t, apierrors.IsNotFound(err),
			"device-pairing Service should not exist when disabled")

		dpSA := &corev1.ServiceAccount{}
		err = k8sClient.Get(ctx, client.ObjectKey{
			Name:      getDevicePairingServiceAccountName(resourceName),
			Namespace: namespace,
		}, dpSA)
		assert.True(t, apierrors.IsNotFound(err),
			"device-pairing ServiceAccount should not exist when disabled")

		// Toggle disableDevicePairing to false
		require.NoError(t, k8sClient.Get(ctx,
			client.ObjectKey{Name: resourceName, Namespace: namespace}, instance))
		instance.Spec.Auth = &clawv1alpha1.AuthSpec{
			DisableDevicePairing: boolPtr(false),
		}
		require.NoError(t, k8sClient.Update(ctx, instance))
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		// Verify device-pairing resources ARE now created
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingDeploymentName(resourceName),
				Namespace: namespace,
			}, dpDeployment) == nil
		}, "device-pairing Deployment should be created after re-enabling")

		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingServiceName(resourceName),
				Namespace: namespace,
			}, dpService) == nil
		}, "device-pairing Service should be created after re-enabling")

		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingServiceAccountName(resourceName),
				Namespace: namespace,
			}, dpSA) == nil
		}, "device-pairing ServiceAccount should be created after re-enabling")
	})

	t.Run("should create device-pairing resources after reconcile", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		sa := &corev1.ServiceAccount{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingServiceAccountName(resourceName),
				Namespace: namespace,
			}, sa) == nil
		}, "device-pairing ServiceAccount should be created")

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingDeploymentName(resourceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "device-pairing Deployment should be created")

		svc := &corev1.Service{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingServiceName(resourceName),
				Namespace: namespace,
			}, svc) == nil
		}, "device-pairing Service should be created")
	})

	t.Run("should set correct owner references on device-pairing resources", func(t *testing.T) {
		const resourceName = testInstanceName
		ctx := context.Background()

		t.Cleanup(func() {
			deleteAndWaitAllResources(t, namespace)
		})

		createClawInstance(t, ctx, resourceName, namespace)
		reconciler := &ClawResourceReconciler{
			Client:           k8sClient,
			Scheme:           scheme.Scheme,
			UserSecretReader: k8sClient,
		}
		reconcileClaw(t, ctx, reconciler, resourceName, namespace)

		sa := &corev1.ServiceAccount{}
		waitFor(t, timeout, interval, func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      getDevicePairingServiceAccountName(resourceName),
				Namespace: namespace,
			}, sa)
			if err != nil {
				return false
			}
			if len(sa.OwnerReferences) == 0 {
				return false
			}
			ownerRef := sa.OwnerReferences[0]
			return ownerRef.Kind == ClawResourceKind &&
				ownerRef.Name == resourceName &&
				ownerRef.Controller != nil &&
				*ownerRef.Controller
		}, "device-pairing ServiceAccount should have correct owner reference")
	})
}
