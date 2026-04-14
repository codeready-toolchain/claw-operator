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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/codeready-toolchain/openclaw-operator/test/utils"
)

const (
	operatorNamespace = "openclaw-operator"
	userNamespace     = "default"

	serviceAccountName     = "openclaw-operator-controller-manager"
	metricsServiceName     = "openclaw-operator-controller-manager-metrics-service"
	metricsRoleBindingName = "openclaw-operator-metrics-binding"

	defaultTimeout  = 2 * time.Minute
	pollInterval    = 1 * time.Second
	extendedTimeout = 5 * time.Minute
)

// clawYAMLWithGemini returns a Claw CR YAML using spec.credentials[] with apiKey type.
func clawYAMLWithGemini(secretName, secretKey string) string {
	return fmt.Sprintf(`apiVersion: openclaw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        name: %s
        key: %s
      domain: ".googleapis.com"
      apiKey:
        header: x-goog-api-key
`, secretName, secretKey)
}

func TestManager(t *testing.T) { //nolint:gocyclo
	var controllerPodName string

	t.Log("creating manager namespace")
	cmd := exec.Command("kubectl", "create", "ns", operatorNamespace)
	_, err := utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create namespace")

	t.Log("labeling the namespace to enforce the restricted security policy")
	cmd = exec.Command("kubectl", "label", "--overwrite", "ns", operatorNamespace,
		"pod-security.kubernetes.io/enforce=restricted")
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to label namespace with restricted policy")

	t.Log("installing CRDs")
	cmd = exec.Command("make", "install")
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to install CRDs")

	t.Log("deploying the controller-manager")
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to deploy the controller-manager")

	t.Cleanup(func() {
		t.Log("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", operatorNamespace)
		_, _ = utils.Run(t, cmd)

		t.Log("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(t, cmd)

		t.Log("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(t, cmd)

		t.Log("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", operatorNamespace)
		_, _ = utils.Run(t, cmd)
	})

	collectDebugInfo := func(t *testing.T) {
		t.Helper()
		if !t.Failed() {
			return
		}

		t.Log("Fetching controller manager pod logs")
		cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", operatorNamespace)
		controllerLogs, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Controller logs:\n %s", controllerLogs)
		} else {
			t.Logf("Failed to get Controller logs: %s", err)
		}

		t.Log("Fetching Kubernetes events")
		cmd = exec.Command("kubectl", "get", "events", "-n", operatorNamespace, "--sort-by=.lastTimestamp")
		eventsOutput, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Kubernetes events:\n%s", eventsOutput)
		} else {
			t.Logf("Failed to get Kubernetes events: %s", err)
		}

		t.Log("Fetching curl-metrics logs")
		cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", operatorNamespace)
		metricsOutput, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Metrics logs:\n %s", metricsOutput)
		} else {
			t.Logf("Failed to get curl-metrics logs: %s", err)
		}

		t.Log("Fetching controller manager pod description")
		cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", operatorNamespace)
		podDescription, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Pod description:\n %s", podDescription)
		} else {
			t.Log("Failed to describe controller pod")
		}
	}

	t.Run("Manager", func(t *testing.T) {
		t.Run("should run successfully", func(t *testing.T) {
			t.Cleanup(func() { collectDebugInfo(t) })

			t.Log("validating that the controller-manager pod is running as expected")
			deadline := time.Now().Add(defaultTimeout)
			var podOutput string
			for time.Now().Before(deadline) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", operatorNamespace,
				)

				var err error
				podOutput, err = utils.Run(t, cmd)
				if err == nil {
					podNames := utils.GetNonEmptyLines(podOutput)
					if len(podNames) == 1 {
						controllerPodName = podNames[0]
						if strings.Contains(controllerPodName, "controller-manager") {
							cmd = exec.Command("kubectl", "get",
								"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
								"-n", operatorNamespace,
							)
							output, err := utils.Run(t, cmd)
							if err == nil && output == "Running" {
								return
							}
						}
					}
				}
				time.Sleep(pollInterval)
			}
			require.Fail(t, "timeout waiting for controller-manager pod to be running")
		})

		t.Run("should ensure the metrics endpoint is serving metrics", func(t *testing.T) {
			t.Cleanup(func() { collectDebugInfo(t) })

			t.Log("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=openclaw-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", operatorNamespace, serviceAccountName),
			)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create ClusterRoleBinding")

			t.Log("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", operatorNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Metrics service should exist")

			t.Log("getting the service account token")
			token, err := serviceAccountToken(t)
			require.NoError(t, err)
			require.NotEmpty(t, token)

			t.Log("waiting for the metrics endpoint to be ready")
			deadline := time.Now().Add(defaultTimeout)
			for time.Now().Before(deadline) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", operatorNamespace)
				output, err := utils.Run(t, cmd)
				if err == nil && strings.Contains(output, "8443") {
					break
				}
				time.Sleep(pollInterval)
			}

			t.Log("verifying that the controller manager is serving the metrics server")
			deadline = time.Now().Add(defaultTimeout)
			for time.Now().Before(deadline) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", operatorNamespace)
				output, err := utils.Run(t, cmd)
				if err == nil && strings.Contains(output, "controller-runtime.metrics\tServing metrics server") {
					break
				}
				time.Sleep(pollInterval)
			}

			t.Log("creating the curl-metrics pod to access the metrics endpoint")
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
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create curl-metrics pod")

			t.Log("waiting for the curl-metrics pod to complete")
			deadline = time.Now().Add(extendedTimeout)
			for time.Now().Before(deadline) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", operatorNamespace)
				output, err := utils.Run(t, cmd)
				if err == nil && output == "Succeeded" {
					break
				}
				time.Sleep(pollInterval)
			}

			t.Log("getting the metrics by checking curl-metrics logs")
			metricsOutput := getMetricsOutput(t)
			assert.Contains(t, metricsOutput, "controller_runtime_reconcile_total")
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		t.Run("should reconcile Claw with credential-based proxy wiring", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "secret", "generic", "gemini-api-key",
				"--from-literal=api-key=test-api-key-value",
				"-n", userNamespace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create Secret")

			t.Log("applying the Claw CR")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/openclaw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for Claw to become Ready")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == "True", nil
				})
			require.NoError(t, err, "Claw did not become Ready within %v", extendedTimeout)

			t.Log("verifying CRED_GEMINI env var references the user's Secret")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "gemini-api-key", output,
				"CRED_GEMINI should reference gemini-api-key Secret")

			t.Log("verifying proxy-config ConfigMap was generated")
			cmd = exec.Command("kubectl", "get", "configmap", "openclaw-proxy-config",
				"-o", "jsonpath={.data.proxy-config\\.json}",
				"-n", userNamespace)
			configOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "openclaw-proxy-config ConfigMap should exist")
			assert.Contains(t, configOutput, ".googleapis.com",
				"proxy config should contain the credential domain")

			t.Log("verifying the proxy CA Secret was created")
			cmd = exec.Command("kubectl", "get", "secret", "openclaw-proxy-ca",
				"-o", "jsonpath={.data.ca\\.crt}",
				"-n", userNamespace)
			caOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "Proxy CA Secret should exist")
			assert.NotEmpty(t, caOutput, "CA cert should not be empty")

			t.Log("verifying the ingress NetworkPolicy exists")
			cmd = exec.Command("kubectl", "get", "networkpolicy", "openclaw-ingress",
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Ingress NetworkPolicy should exist")

			t.Log("verifying the gateway Secret was created with a token")
			cmd = exec.Command("kubectl", "get", "secret", "openclaw-gateway-token",
				"-o", "jsonpath={.data.token}",
				"-n", userNamespace)
			tokenOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "Gateway Secret should exist")
			assert.NotEmpty(t, tokenOutput, "Gateway token should not be empty")

			t.Log("verifying gatewayTokenSecretRef in status")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.gatewayTokenSecretRef}",
				"-n", userNamespace)
			secretRefOutput, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "openclaw-gateway-token", secretRefOutput)

			t.Log("verifying CredentialsResolved condition")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='CredentialsResolved')].status}",
				"-n", userNamespace)
			condOutput, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "True", condOutput, "CredentialsResolved should be True")

			t.Log("verifying ProxyConfigured condition")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='ProxyConfigured')].status}",
				"-n", userNamespace)
			condOutput, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "True", condOutput, "ProxyConfigured should be True")

			t.Log("verifying reconciliation success in metrics")
			metricsOutput := fetchFreshMetrics(t, "curl-metrics-reconcile")
			assert.Contains(t, metricsOutput,
				`controller_runtime_reconcile_total{controller="openclaw",result="success"}`)
		})

		t.Run("should wire credential env var with correct Secret reference", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "secret", "generic", "gemini-api-key",
				"--from-literal=api-key=test-gemini-key-value",
				"-n", userNamespace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create Secret")

			t.Log("applying the Claw CR")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/openclaw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for openclaw-proxy deployment")
			deadline := time.Now().Add(2 * time.Minute)
			for time.Now().Before(deadline) {
				cmd := exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
					"-n", userNamespace)
				_, err := utils.Run(t, cmd)
				if err == nil {
					break
				}
				time.Sleep(pollInterval)
			}

			t.Log("verifying CRED_GEMINI references the correct Secret name")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "gemini-api-key", output)

			t.Log("verifying CRED_GEMINI references the correct Secret key")
			jp = "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.key}"
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", jp, "-n", userNamespace)
			output, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "api-key", output)

			t.Log("verifying the deployment uses the proxy container")
			cmd = exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
				"-o", "jsonpath={.spec.template.spec.containers[0].name}",
				"-n", userNamespace)
			output, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "proxy", output, "First container should be named 'proxy'")

			t.Log("verifying pods are running")
			deadline = time.Now().Add(3 * time.Minute)
			for time.Now().Before(deadline) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=openclaw-proxy",
					"-o", "jsonpath={.items[*].status.phase}",
					"-n", userNamespace)
				output, err := utils.Run(t, cmd)
				if err == nil && strings.Contains(output, "Running") {
					break
				}
				time.Sleep(pollInterval)
			}
		})

		t.Run("should trigger pod restart when credential Secret reference changes", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "llm-key-1", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "llm-key-2", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
			})

			t.Log("creating the first credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "llm-key-1",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "secret", "generic", "llm-key-1",
				"--from-literal=api-key=first-api-key",
				"-n", userNamespace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create first Secret")

			t.Log("creating Claw CR referencing first Secret")
			crFile := filepath.Join("/tmp", "claw-e2e-test.yaml")
			err = os.WriteFile(crFile, []byte(clawYAMLWithGemini("llm-key-1", "api-key")),
				os.FileMode(0o644))
			require.NoError(t, err)

			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for Claw to become Ready")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == "True", nil
				})
			require.NoError(t, err, "Claw did not become Ready within %v", extendedTimeout)

			t.Log("capturing original pod UID")
			cmd = exec.Command("kubectl", "get", "pods", "-l", "app=openclaw-proxy",
				"-o", "jsonpath={.items[0].metadata.uid}",
				"-n", userNamespace)
			originalPodUID, err := utils.Run(t, cmd)
			require.NoError(t, err)
			require.NotEmpty(t, originalPodUID)

			t.Log("creating the second credential Secret")
			cmd = exec.Command("kubectl", "delete", "secret", "llm-key-2",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "secret", "generic", "llm-key-2",
				"--from-literal=api-key=second-api-key",
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create second Secret")

			t.Log("updating Claw CR to reference the second Secret")
			err = os.WriteFile(crFile, []byte(clawYAMLWithGemini("llm-key-2", "api-key")),
				os.FileMode(0o644))
			require.NoError(t, err)

			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to update Claw CR")

			t.Log("verifying the deployment references the new Secret")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
						".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.name}"
					cmd := exec.Command("kubectl", "get", "deployment", "openclaw-proxy",
						"-o", jp, "-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == "llm-key-2", nil
				})
			require.NoError(t, err, "deployment did not reference new Secret")

			t.Log("verifying pod was restarted (different UID)")
			var newPodUID string
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "app=openclaw-proxy",
						"-o", "jsonpath={.items[0].metadata.uid}",
						"-n", userNamespace)
					uid, err := utils.Run(t, cmd)
					if err == nil && uid != "" && uid != originalPodUID {
						newPodUID = uid
						return true, nil
					}
					return false, nil
				})
			require.NoError(t, err, "pod was not recreated with new UID")
			require.NotEqual(t, originalPodUID, newPodUID,
				"Pod should have been recreated with new UID")

			t.Log("verifying new pod is running")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "app=openclaw-proxy",
						"-o", "jsonpath={.items[0].status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == "Running", nil
				})
			require.NoError(t, err, "new pod did not reach Running phase")
		})
	})
}

func serviceAccountToken(t *testing.T) (string, error) {
	t.Helper()
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	deadline := time.Now().Add(defaultTimeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			operatorNamespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		if err == nil {
			var token tokenRequest
			err = json.Unmarshal(output, &token)
			if err == nil {
				return token.Status.Token, nil
			}
		}
		time.Sleep(pollInterval)
	}

	return "", fmt.Errorf("timeout waiting for service account token creation")
}

func getMetricsOutput(t *testing.T) string {
	t.Helper()
	t.Log("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", operatorNamespace)
	metricsOutput, err := utils.Run(t, cmd)
	require.NoError(t, err, "Failed to retrieve logs from curl pod")
	require.Contains(t, metricsOutput, "< HTTP/1.1 200 OK")
	return metricsOutput
}

func fetchFreshMetrics(t *testing.T, podName string) string {
	t.Helper()

	token, err := serviceAccountToken(t)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	cmd := exec.Command("kubectl", "delete", "pod", podName, "-n", operatorNamespace, "--ignore-not-found")
	_, _ = utils.Run(t, cmd)

	t.Cleanup(func() {
		cmd := exec.Command("kubectl", "delete", "pod", podName, "-n", operatorNamespace, "--ignore-not-found")
		_, _ = utils.Run(t, cmd)
	})

	cmd = exec.Command("kubectl", "run", podName, "--restart=Never",
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
						"capabilities": {"drop": ["ALL"]},
						"runAsNonRoot": true,
						"runAsUser": 1000,
						"seccompProfile": {"type": "RuntimeDefault"}
					}
				}],
				"serviceAccount": "%s"
			}
		}`, token, metricsServiceName, operatorNamespace, serviceAccountName))
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create metrics pod")

	ctx := context.Background()
	err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pods", podName,
				"-o", "jsonpath={.status.phase}",
				"-n", operatorNamespace)
			output, err := utils.Run(t, cmd)
			return err == nil && output == "Succeeded", nil
		})
	require.NoError(t, err, "pod %s did not reach Succeeded phase within %v", podName, defaultTimeout)

	cmd = exec.Command("kubectl", "logs", podName, "-n", operatorNamespace)
	metricsOutput, err := utils.Run(t, cmd)
	require.NoError(t, err, "Failed to retrieve metrics logs")
	require.Contains(t, metricsOutput, "< HTTP/1.1 200 OK")
	return metricsOutput
}

type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
