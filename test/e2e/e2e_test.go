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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/codeready-toolchain/openclaw-operator/test/utils"
)

// operatorNamespace is the namespace where the operator is deployed in
const operatorNamespace = "openclaw-operator"

// userNamespace is the namespace where the user will create the OpenClaw CR in
const userNamespace = "default"

// serviceAccountName created for the project
const serviceAccountName = "openclaw-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "openclaw-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "openclaw-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", operatorNamespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", operatorNamespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", operatorNamespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", operatorNamespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", operatorNamespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", operatorNamespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", operatorNamespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", operatorNamespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", operatorNamespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", operatorNamespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=openclaw-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", operatorNamespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", operatorNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", operatorNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", operatorNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", operatorNamespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccount": "%s"
					}
				}`, token, metricsServiceName, operatorNamespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", operatorNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring(
				"controller_runtime_reconcile_total",
			))
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		It("should successfully reconcile OpenClaw instance with Secret reference", func() {
			By("creating the Gemini API key Secret")
			cmd := exec.Command("kubectl", "create", "secret", "generic", "gemini-api-key",
				"--from-literal=api-key=test-api-key-value",
				"-n", userNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Secret")

			By("applying the OpenClaw CR")
			cmd = exec.Command("kubectl", "apply", "-f", "config/samples/openclaw_v1alpha1_openclaw.yaml",
				"-n", userNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply OpenClaw CR")

			By("verifying the OpenClaw instance becomes Available")
			verifyOpenClawAvailable := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "openclaw", "instance",
					"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}",
					"-n", userNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "OpenClaw instance should be Available")
			}
			Eventually(verifyOpenClawAvailable, 3*time.Minute).Should(Succeed())

			By("verifying the openclaw-proxy deployment references the user's Secret")
			jsonPathSecretName := "jsonpath={.spec.template.spec.containers[0]" +
				".env[?(@.name=='GEMINI_API_KEY')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jsonPathSecretName,
				"-n", userNamespace)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("gemini-api-key"), "Proxy deployment should reference user's Secret")

			By("verifying reconciliation success in metrics")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring(
				`controller_runtime_reconcile_total{controller="openclaw",result="success"}`,
			))

			By("cleaning up the OpenClaw CR")
			cmd = exec.Command("kubectl", "delete", "openclaw", "instance", "-n", userNamespace)
			_, _ = utils.Run(cmd)

			By("cleaning up the Secret")
			cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
			_, _ = utils.Run(cmd)
		})

		It("should configure openclaw-proxy GEMINI_API_KEY env var with correct Secret reference", func() {
			By("creating the Gemini API key Secret")
			cmd := exec.Command("kubectl", "create", "secret", "generic", "gemini-api-key",
				"--from-literal=api-key=test-gemini-key-value",
				"-n", userNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Secret")

			By("applying the OpenClaw CR")
			cmd = exec.Command("kubectl", "apply", "-f", "config/samples/openclaw_v1alpha1_openclaw.yaml",
				"-n", userNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply OpenClaw CR")

			By("waiting for openclaw-proxy deployment to be created")
			verifyProxyDeploymentExists := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
					"-n", userNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}
			Eventually(verifyProxyDeploymentExists, 2*time.Minute).Should(Succeed())

			By("verifying GEMINI_API_KEY env var references the correct Secret name")
			jsonPathSecretName := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='GEMINI_API_KEY')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jsonPathSecretName,
				"-n", userNamespace)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("gemini-api-key"), "GEMINI_API_KEY should reference gemini-api-key Secret")

			By("verifying GEMINI_API_KEY env var references the correct Secret key")
			jsonPathSecretKey := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='GEMINI_API_KEY')].valueFrom.secretKeyRef.key}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jsonPathSecretKey,
				"-n", userNamespace)
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("api-key"), "GEMINI_API_KEY should reference 'api-key' key in Secret")

			By("verifying GEMINI_API_KEY env var is not optional")
			jsonPathOptional := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='GEMINI_API_KEY')].valueFrom.secretKeyRef.optional}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jsonPathOptional,
				"-n", userNamespace)
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("false"), "GEMINI_API_KEY should be required (optional=false)")

			By("verifying the deployment uses the proxy container")
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", "jsonpath={.spec.template.spec.containers[0].name}",
				"-n", userNamespace)
			output, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(Equal("proxy"), "First container should be named 'proxy'")

			By("verifying pods are running with the Secret reference")
			verifyProxyPodsRunning := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=openclaw-proxy",
					"-o", "jsonpath={.items[*].status.phase}",
					"-n", userNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Running"), "Proxy pods should be running")
			}
			Eventually(verifyProxyPodsRunning, 3*time.Minute).Should(Succeed())

			By("cleaning up the OpenClaw CR")
			cmd = exec.Command("kubectl", "delete", "openclaw", "instance", "-n", userNamespace)
			_, _ = utils.Run(cmd)

			By("cleaning up the Secret")
			cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
			_, _ = utils.Run(cmd)
		})

		It("should trigger pod restart when Secret reference changes", func() {
			By("creating the first Gemini API key Secret")
			cmd := exec.Command("kubectl", "create", "secret", "generic", "gemini-api-key-1",
				"--from-literal=api-key=first-api-key",
				"-n", userNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create first Secret")

			By("creating a custom OpenClaw CR with first Secret")
			openclawYAML := `apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: OpenClaw
metadata:
  name: instance
spec:
  geminiAPIKey:
    name: gemini-api-key-1
    key: api-key
`
			crFile := filepath.Join("/tmp", "openclaw-e2e-test.yaml")
			err = os.WriteFile(crFile, []byte(openclawYAML), os.FileMode(0o644))
			Expect(err).NotTo(HaveOccurred(), "Failed to write CR file")

			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply OpenClaw CR")

			By("waiting for OpenClaw to become Available")
			verifyOpenClawAvailable := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "openclaw", "instance",
					"-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}",
					"-n", userNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}
			Eventually(verifyOpenClawAvailable, 3*time.Minute).Should(Succeed())

			By("capturing original pod UID")
			cmd = exec.Command("kubectl", "get", "pods", "-l", "app=openclaw-proxy",
				"-o", "jsonpath={.items[0].metadata.uid}",
				"-n", userNamespace)
			originalPodUID, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(originalPodUID).NotTo(BeEmpty())

			By("creating the second Gemini API key Secret")
			cmd = exec.Command("kubectl", "create", "secret", "generic", "gemini-api-key-2",
				"--from-literal=api-key=second-api-key",
				"-n", userNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create second Secret")

			By("updating OpenClaw CR to reference the second Secret")
			openclawYAML2 := `apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: OpenClaw
metadata:
  name: instance
spec:
  geminiAPIKey:
    name: gemini-api-key-2
    key: api-key
`
			err = os.WriteFile(crFile, []byte(openclawYAML2), os.FileMode(0o644))
			Expect(err).NotTo(HaveOccurred(), "Failed to write updated CR file")

			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to update OpenClaw CR")

			By("verifying the deployment references the new Secret")
			verifyNewSecretReference := func(g Gomega) {
				jsonPathSecretName := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
					".env[?(@.name=='GEMINI_API_KEY')].valueFrom.secretKeyRef.name}"
				cmd := exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
					"-o", jsonPathSecretName,
					"-n", userNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("gemini-api-key-2"), "Deployment should reference new Secret")
			}
			Eventually(verifyNewSecretReference, 1*time.Minute).Should(Succeed())

			By("verifying pod was restarted (different UID)")
			verifyPodRestarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=openclaw-proxy",
					"-o", "jsonpath={.items[0].metadata.uid}",
					"-n", userNamespace)
				newPodUID, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(newPodUID).NotTo(BeEmpty())
				g.Expect(newPodUID).NotTo(Equal(originalPodUID), "Pod should have been recreated with new UID")
			}
			Eventually(verifyPodRestarted, 2*time.Minute).Should(Succeed())

			By("verifying new pod is running")
			verifyNewPodRunning := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=openclaw-proxy",
					"-o", "jsonpath={.items[0].status.phase}",
					"-n", userNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "New pod should be running")
			}
			Eventually(verifyNewPodRunning, 2*time.Minute).Should(Succeed())

			By("cleaning up the OpenClaw CR")
			cmd = exec.Command("kubectl", "delete", "openclaw", "instance", "-n", userNamespace)
			_, _ = utils.Run(cmd)

			By("cleaning up the Secrets")
			cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key-1", "-n", userNamespace)
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key-2", "-n", userNamespace)
			_, _ = utils.Run(cmd)
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			operatorNamespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", operatorNamespace)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
