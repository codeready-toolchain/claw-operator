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
	"os"
	"path/filepath"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

var (
	cfg                *rest.Config
	k8sClient          client.Client
	testEnv            *envtest.Environment
	ctx                context.Context
	cancel             context.CancelFunc
	namespace          = "default"
	aiModelSecret      = "test-gemini-secret"
	aiModelSecretKey   = "api-key"
	aiModelSecretValue = "test-api-key"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

// waitFor polls a condition function until it returns true or timeout is exceeded.
// This helper replaces Gomega's Eventually for standard library tests.
func waitFor(t *testing.T, timeout, interval time.Duration, condition func() bool, message string) { //nolint:unparam
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("timeout waiting for condition: %s", message)
}

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	// Setup envtest
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	if err != nil {
		panic(err)
	}
	if cfg == nil {
		panic("cfg is nil")
	}

	err = clawv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}
	err = routev1.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}
	if k8sClient == nil {
		panic("k8sClient is nil")
	}

	// Run tests
	code := m.Run()

	// Cleanup
	cancel()
	if err := testEnv.Stop(); err != nil {
		panic(err)
	}

	os.Exit(code)
}

func deleteAndWaitAllResources(t *testing.T, namespace string) {
	t.Helper()
	resources := []struct {
		obj client.Object
		key client.ObjectKey
	}{
		{&clawv1alpha1.Claw{}, client.ObjectKey{Name: ClawInstanceName, Namespace: namespace}},
		{&corev1.ConfigMap{}, client.ObjectKey{Name: ClawConfigMapName, Namespace: namespace}},
		{&netv1.NetworkPolicy{}, client.ObjectKey{Name: ClawNetworkPolicyName, Namespace: namespace}},
		{&netv1.NetworkPolicy{}, client.ObjectKey{Name: ClawIngressNetworkPolicyName, Namespace: namespace}},
		{&corev1.Secret{}, client.ObjectKey{Name: ClawGatewaySecretName, Namespace: namespace}},
		{&corev1.Secret{}, client.ObjectKey{Name: aiModelSecret, Namespace: namespace}},
		{&corev1.PersistentVolumeClaim{}, client.ObjectKey{Name: ClawPVCName, Namespace: namespace}},
		{&corev1.Service{}, client.ObjectKey{Name: ClawServiceName, Namespace: namespace}},
		{&appsv1.Deployment{}, client.ObjectKey{Name: ClawDeploymentName, Namespace: namespace}},
		{&corev1.Service{}, client.ObjectKey{Name: ClawProxyServiceName, Namespace: namespace}},
		{&appsv1.Deployment{}, client.ObjectKey{Name: ClawProxyDeploymentName, Namespace: namespace}},
	}

	for _, r := range resources {
		if err := deleteAndWait(r.obj, r.key); err != nil {
			t.Fatalf("cleanup failed for %s: %v", r.key.String(), err)
		}
	}
}

// deleteAndWait deletes an object and waits until the API server confirms it's gone.
// Retries the entire get-strip-delete cycle to handle conflicts from stale ResourceVersions.
// Strips finalizers since envtest doesn't run controllers to process them (e.g. PVC protection).
// Returns an error if the object could not be deleted within the timeout period.
func deleteAndWait(obj client.Object, key client.ObjectKey) error {
	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fresh := obj.DeepCopyObject().(client.Object)
		if err := k8sClient.Get(ctx, key, fresh); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			time.Sleep(interval)
			continue
		}
		if len(fresh.GetFinalizers()) > 0 {
			fresh.SetFinalizers(nil)
			if err := k8sClient.Update(ctx, fresh); err != nil {
				time.Sleep(interval)
				continue
			}
		}
		if err := k8sClient.Delete(ctx, fresh); err != nil && !apierrors.IsNotFound(err) {
			time.Sleep(interval)
			continue
		}
		err := k8sClient.Get(ctx, key, obj.DeepCopyObject().(client.Object))
		if apierrors.IsNotFound(err) {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timeout waiting for object deletion: %s", key.String())
}

// createTestAPIKeySecret creates a test Secret containing an API key for use in tests
// It ensures any existing Secret with the same name is deleted first to avoid conflicts
func createTestAPIKeySecret(name, namespace, key, value string) *corev1.Secret { //nolint:unparam
	// Delete any existing Secret with this name (ignore errors)
	existing := &corev1.Secret{}
	existing.Name = name
	existing.Namespace = namespace
	_ = k8sClient.Delete(context.Background(), existing)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			key: []byte(value),
		},
	}
}

// createTestGatewaySecret creates a test Secret containing a gateway token for use in tests
// It ensures any existing Secret with the same name is deleted first to avoid conflicts
func createTestGatewaySecret(t *testing.T, name, namespace string) *corev1.Secret { //nolint:unparam
	// Delete any existing Secret with this name (ignore errors)
	existing := &corev1.Secret{}
	existing.Name = name
	existing.Namespace = namespace
	_ = k8sClient.Delete(context.Background(), existing)

	token, err := generateGatewayToken()
	require.NoError(t, err)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			GatewayTokenKeyName: []byte(token),
		},
	}
}

// createClawInstance creates a Claw instance with a credentials[] entry for the test API key Secret.
func createClawInstance(t *testing.T, ctx context.Context, name, namespace string) {
	t.Helper()

	// Create API key Secret
	secret := createTestAPIKeySecret(aiModelSecret, namespace, aiModelSecretKey, aiModelSecretValue)
	require.NoError(t, k8sClient.Create(ctx, secret), "failed to create API key Secret")

	// Create Claw instance with a single apiKey credential
	instance := &clawv1alpha1.Claw{}
	instance.Name = name
	instance.Namespace = namespace
	instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
		{
			Name:     "gemini",
			Type:     clawv1alpha1.CredentialTypeAPIKey,
			Provider: "google",
			SecretRef: &clawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			},
			Domain: ".googleapis.com",
			APIKey: &clawv1alpha1.APIKeyConfig{
				Header: "x-goog-api-key",
			},
		},
	}
	require.NoError(t, k8sClient.Create(ctx, instance), "failed to create Claw instance")
}

// testCredentials returns a standard credentials slice for tests.
func testCredentials() []clawv1alpha1.CredentialSpec {
	return []clawv1alpha1.CredentialSpec{
		{
			Name:     "gemini",
			Type:     clawv1alpha1.CredentialTypeAPIKey,
			Provider: "google",
			SecretRef: &clawv1alpha1.SecretRef{
				Name: aiModelSecret,
				Key:  aiModelSecretKey,
			},
			Domain: ".googleapis.com",
			APIKey: &clawv1alpha1.APIKeyConfig{
				Header: "x-goog-api-key",
			},
		},
	}
}

// createClawReconciler creates a ClawResourceReconciler for testing.
func createClawReconciler() *ClawResourceReconciler {
	return &ClawResourceReconciler{
		Client: k8sClient,
		Scheme: scheme.Scheme,
	}
}

// reconcileClaw performs a reconciliation for the given Claw resource.
func reconcileClaw(t *testing.T, ctx context.Context, reconciler *ClawResourceReconciler, name, namespace string) {
	t.Helper()

	_, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: client.ObjectKey{
			Name:      name,
			Namespace: namespace,
		},
	})
	require.NoError(t, err, "reconcile failed")
}
