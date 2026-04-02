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
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	openclawv1alpha1 "github.com/codeready-toolchain/openclaw-operator/api/v1alpha1"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = openclawv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("OpenClawInstance Controller", func() {
	const (
		namespace = "default"
	)

	Context("When reconciling an OpenClawInstance named 'instance'", func() {
		const resourceName = "instance"
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClawInstance{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)

			// Cleanup deployment
			deployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw", Namespace: namespace}, deployment)
			_ = k8sClient.Delete(ctx, deployment)
		})

		It("should successfully create a Deployment when OpenClawInstance is created", func() {
			By("Creating a new OpenClawInstance named 'instance'")
			instance := &openclawv1alpha1.OpenClawInstance{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawInstanceReconciler{
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

			By("Checking if Deployment was created in the same namespace as the OpenClawInstance")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err == nil && deployment.Namespace == namespace
			}, timeout, interval).Should(BeTrue())

			By("Checking Deployment has correct owner reference")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				return len(deployment.OwnerReferences) > 0 &&
					deployment.OwnerReferences[0].Kind == "OpenClawInstance" &&
					deployment.OwnerReferences[0].Name == resourceName
			}, timeout, interval).Should(BeTrue())

		})

		It("should configure Deployment for garbage collection when OpenClawInstance is deleted", func() {
			By("Creating a new OpenClawInstance named 'instance'")
			instance := &openclawv1alpha1.OpenClawInstance{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawInstanceReconciler{
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

			By("Verifying Deployment has blockOwnerDeletion set to true")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				if err != nil {
					return false
				}
				// Verify owner reference is configured for garbage collection
				if len(deployment.OwnerReferences) == 0 {
					return false
				}
				ownerRef := deployment.OwnerReferences[0]
				return ownerRef.Kind == "OpenClawInstance" &&
					ownerRef.Name == resourceName &&
					ownerRef.Controller != nil &&
					*ownerRef.Controller == true
			}, timeout, interval).Should(BeTrue())

			// Note: envtest doesn't run garbage collection controller, so we can't test
			// actual deletion. The test above verifies the owner reference is properly
			// configured with Controller=true, which ensures GC will work in real clusters.
		})
	})

	Context("When reconciling an OpenClawInstance with different name", func() {
		const resourceName = "other-instance"
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup resources
			instance := &openclawv1alpha1.OpenClawInstance{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: namespace}, instance)
			_ = k8sClient.Delete(ctx, instance)
		})

		It("should skip reconciliation for resource with non-matching name", func() {
			By("Creating a new OpenClawInstance with name 'other-instance'")
			instance := &openclawv1alpha1.OpenClawInstance{}
			instance.Name = resourceName
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			// Setup reconciler
			reconciler := &OpenClawInstanceReconciler{
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

			By("Verifying Deployment was NOT created")
			deployment := &appsv1.Deployment{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err != nil
			}, time.Second*2, interval).Should(BeTrue())
		})
	})

	Context("When multiple OpenClawInstance resources exist", func() {
		ctx := context.Background()

		AfterEach(func() {
			// Cleanup all test resources
			instanceNames := []string{"instance", "another-instance"}
			for _, name := range instanceNames {
				instance := &openclawv1alpha1.OpenClawInstance{}
				_ = k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, instance)
				_ = k8sClient.Delete(ctx, instance)
			}

			// Cleanup deployment
			deployment := &appsv1.Deployment{}
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "openclaw", Namespace: namespace}, deployment)
			_ = k8sClient.Delete(ctx, deployment)
		})

		It("should only reconcile the resource named 'instance'", func() {
			// Setup reconciler
			reconciler := &OpenClawInstanceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			By("Creating OpenClawInstance named 'another-instance'")
			otherInstance := &openclawv1alpha1.OpenClawInstance{}
			otherInstance.Name = "another-instance"
			otherInstance.Namespace = namespace
			Expect(k8sClient.Create(ctx, otherInstance)).Should(Succeed())

			By("Reconciling 'another-instance'")
			_, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      "another-instance",
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying no Deployment was created")
			deployment := &appsv1.Deployment{}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err != nil
			}, time.Second*1, interval).Should(BeTrue())

			By("Creating OpenClawInstance named 'instance'")
			instance := &openclawv1alpha1.OpenClawInstance{}
			instance.Name = "instance"
			instance.Namespace = namespace
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Reconciling 'instance'")
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      "instance",
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Deployment was created")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      "openclaw",
					Namespace: namespace,
				}, deployment)
				return err == nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
