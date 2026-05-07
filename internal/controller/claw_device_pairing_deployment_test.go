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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDevicePairingDeployment(t *testing.T) {

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

		container := containers[0].(map[string]any)
		secCtx := container["securityContext"].(map[string]any)
		assert.Equal(t, false, secCtx["allowPrivilegeEscalation"])
		assert.Equal(t, true, secCtx["readOnlyRootFilesystem"])

		caps := secCtx["capabilities"].(map[string]any)
		drop := caps["drop"].([]any)
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

		container := containers[0].(map[string]any)
		envVars, _ := container["env"].([]any)
		require.NotEmpty(t, envVars, "container should have env vars")

		envMap := map[string]any{}
		for _, e := range envVars {
			env := e.(map[string]any)
			envMap[env["name"].(string)] = env
		}

		nsEnv, ok := envMap["NAMESPACE"]
		require.True(t, ok, "NAMESPACE env var should exist")
		nsEnvMap := nsEnv.(map[string]any)
		valueFrom := nsEnvMap["valueFrom"].(map[string]any)
		fieldRef := valueFrom["fieldRef"].(map[string]any)
		assert.Equal(t, "metadata.namespace", fieldRef["fieldPath"])

		clawEnv, ok := envMap["CLAW_INSTANCE"]
		require.True(t, ok, "CLAW_INSTANCE env var should exist")
		clawEnvMap := clawEnv.(map[string]any)
		assert.Equal(t, testInstanceName, clawEnvMap["value"], "CLAW_INSTANCE should be the instance name after template replacement")
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
			Client: k8sClient,
			Scheme: scheme.Scheme,
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
