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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

var _ = Describe("OpenClaw Route Configuration", func() {
	const (
		namespace       = "default"
		apiKey          = "test-api-key"
		apiKeySecret    = "test-gemini-secret"
		apiKeySecretKey = "api-key"
	)

	Context("ConfigMap injection logic", func() {
		It("should replace OPENCLAW_ROUTE_HOST placeholder with Route host", func() {
			// Create a mock ConfigMap object with placeholder
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(OpenClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example-openclaw.apps.cluster.com"

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			// Inject Route host
			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			Expect(err).NotTo(HaveOccurred())

			// Verify replacement
			openclawJSON, found, err := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(openclawJSON).To(ContainSubstring(routeHost))
			Expect(openclawJSON).NotTo(ContainSubstring("OPENCLAW_ROUTE_HOST"))
		})

		It("should replace all occurrences of OPENCLAW_ROUTE_HOST placeholder", func() {
			// Create a mock ConfigMap object with multiple placeholders
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(OpenClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST","OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "https://example.com"

			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			Expect(err).NotTo(HaveOccurred())

			openclawJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			// Count occurrences of Route host
			hostCount := strings.Count(openclawJSON, routeHost)
			Expect(hostCount).To(Equal(2))
			// Ensure no placeholder remains
			Expect(openclawJSON).NotTo(ContainSubstring("OPENCLAW_ROUTE_HOST"))
		})

		It("should use localhost fallback when routeHost is empty", func() {
			configMap := &unstructured.Unstructured{}
			configMap.SetKind(ConfigMapKind)
			configMap.SetName(OpenClawConfigMapName)
			configMap.Object["data"] = map[string]any{
				"openclaw.json": `{"gateway":{"controlUi":{"allowedOrigins":["OPENCLAW_ROUTE_HOST"]}}}`,
			}

			objects := []*unstructured.Unstructured{configMap}
			routeHost := "" // Empty = vanilla Kubernetes

			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			err := reconciler.injectRouteHostIntoConfigMap(objects, routeHost)
			Expect(err).NotTo(HaveOccurred())

			openclawJSON, _, _ := unstructured.NestedString(configMap.Object, "data", "openclaw.json")
			Expect(openclawJSON).To(ContainSubstring("http://localhost:18789"))
			Expect(openclawJSON).NotTo(ContainSubstring("OPENCLAW_ROUTE_HOST"))
		})
	})

	Context("When reconciling with Route CRD not registered", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		BeforeEach(func() {
			// Ensure cleanup before test starts (in case previous test didn't clean up)
			instance := &openclawv1alpha1.OpenClaw{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance); err == nil {
				_ = k8sClient.Delete(ctx, instance)
				// Wait for deletion to complete
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
					return err != nil
				}, timeout, interval).Should(BeTrue())
			}
		})

		AfterEach(func() {
			deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
			deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: apiKeySecret, Namespace: namespace})
			deleteAndWait(ctx, &corev1.ConfigMap{}, client.ObjectKey{Name: OpenClawConfigMapName, Namespace: namespace})
		})

		It("should create ConfigMap with localhost fallback when Route CRD not available", func() {
			By("Creating a new OpenClaw instance")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace

			// Create API key Secret
			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Reconciling the created resource (Route CRD not available in envtest)")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if ConfigMap contains localhost fallback")
			configMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      OpenClawConfigMapName,
					Namespace: namespace,
				}, configMap)
				if err != nil {
					return false
				}
				openclawJSON, ok := configMap.Data["openclaw.json"]
				if !ok {
					return false
				}
				// Should contain localhost fallback since Route CRD is not registered
				return strings.Contains(openclawJSON, "http://localhost:18789")
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Proxy deployment configuration", func() {
		const resourceName = OpenClawInstanceName
		ctx := context.Background()

		AfterEach(func() {
			deleteAndWait(ctx, &openclawv1alpha1.OpenClaw{}, client.ObjectKey{Name: resourceName, Namespace: namespace})
			deleteAndWait(ctx, &corev1.Secret{}, client.ObjectKey{Name: apiKeySecret, Namespace: namespace})
		})

		It("should still configure proxy deployment and stamp secret version", func() {
			By("Creating a new OpenClaw instance")
			instance := &openclawv1alpha1.OpenClaw{}
			instance.Name = resourceName
			instance.Namespace = namespace

			secret := createTestAPIKeySecret(apiKeySecret, namespace, apiKeySecretKey, apiKey)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			instance.Spec.GeminiAPIKey = &openclawv1alpha1.SecretRef{
				Name: apiKeySecret,
				Key:  apiKeySecretKey,
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			reconciler := &OpenClawResourceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      resourceName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying buildKustomizedObjects still calls configureProxyDeployment and stampSecretVersionAnnotation")
			objects, err := reconciler.buildKustomizedObjects(ctx, instance)
			Expect(err).NotTo(HaveOccurred())

			// Find proxy deployment
			var proxyDeployment *unstructured.Unstructured
			for _, obj := range objects {
				if obj.GetKind() == DeploymentKind && obj.GetName() == OpenClawProxyDeploymentName {
					proxyDeployment = obj
					break
				}
			}
			Expect(proxyDeployment).NotTo(BeNil())

			// Verify Secret reference is configured
			containers, found, err := unstructured.NestedSlice(proxyDeployment.Object, "spec", "template", "spec", "containers")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(containers).ToNot(BeEmpty())

			// Verify Secret version annotation is stamped
			annotations, found, err := unstructured.NestedStringMap(proxyDeployment.Object, "spec", "template", "metadata", "annotations")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(annotations).To(HaveKey("openclaw.sandbox.redhat.com/gemini-secret-version"))
		})
	})
})
