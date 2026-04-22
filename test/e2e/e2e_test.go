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

	"github.com/codeready-toolchain/claw-operator/test/utils"
)

const (
	operatorNamespace = "claw-operator"
	userNamespace     = "default"

	serviceAccountName     = "claw-operator-controller-manager"
	metricsServiceName     = "claw-operator-controller-manager-metrics-service"
	metricsRoleBindingName = "claw-operator-metrics-binding"

	defaultTimeout  = 2 * time.Minute
	pollInterval    = 1 * time.Second
	extendedTimeout = 5 * time.Minute

	podPhaseRunning   = "Running"
	podPhaseSucceeded = "Succeeded"
	conditionTrue     = "True"
)

// clawYAMLWithGemini returns a Claw CR YAML using spec.credentials[] with apiKey type.
func clawYAMLWithGemini(secretName, secretKey string) string {
	return fmt.Sprintf(`apiVersion: claw.sandbox.redhat.com/v1alpha1
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
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage),
		fmt.Sprintf("PROXY_IMG=%s", proxyImage))
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

		// skipping curl-metrics logs for now as it is verbose and not so useful for debugging
		// t.Log("Fetching curl-metrics logs")
		// cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", operatorNamespace)
		// metricsOutput, err := utils.Run(t, cmd)
		// if err == nil {
		// 	t.Logf("Metrics logs:\n %s", metricsOutput)
		// } else {
		// 	t.Logf("Failed to get curl-metrics logs: %s", err)
		// }

		t.Log("Fetching controller manager pod description")
		cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", operatorNamespace)
		podDescription, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Pod description:\n %s", podDescription)
		} else {
			t.Log("Failed to describe controller pod")
		}

		t.Log("Fetching Claw status in user namespace")
		cmd = exec.Command("kubectl", "get", "claw", "instance",
			"-o", "yaml", "-n", userNamespace)
		clawOutput, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Claw status:\n%s", clawOutput)
		}

		t.Log("Fetching events in user namespace")
		cmd = exec.Command("kubectl", "get", "events", "-n", userNamespace, "--sort-by=.lastTimestamp")
		userEvents, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("User namespace events:\n%s", userEvents)
		}

		t.Log("Fetching deployments in user namespace")
		cmd = exec.Command("kubectl", "get", "deployments", "-o", "wide", "-n", userNamespace)
		deploymentsOutput, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("User namespace deployments:\n%s", deploymentsOutput)
		}

		t.Log("Fetching pods in user namespace")
		cmd = exec.Command("kubectl", "get", "pods", "-o", "wide", "-n", userNamespace)
		podsOutput, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("User namespace pods:\n%s", podsOutput)
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
							if err == nil && output == podPhaseRunning {
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
				"--clusterrole=claw-operator-metrics-reader",
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
				if err == nil && output == podPhaseSucceeded {
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
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for Claw ProxyConfigured condition")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='ProxyConfigured')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw ProxyConfigured did not become True within %v", defaultTimeout)

			t.Log("verifying CRED_GEMINI env var references the user's Secret")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", "claw-proxy",
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "gemini-api-key", output,
				"CRED_GEMINI should reference gemini-api-key Secret")

			t.Log("verifying proxy-config ConfigMap was generated")
			cmd = exec.Command("kubectl", "get", "configmap", "claw-proxy-config",
				"-o", "jsonpath={.data.proxy-config\\.json}",
				"-n", userNamespace)
			configOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "claw-proxy-config ConfigMap should exist")
			assert.Contains(t, configOutput, ".googleapis.com",
				"proxy config should contain the credential domain")

			t.Log("verifying the proxy CA Secret was created")
			cmd = exec.Command("kubectl", "get", "secret", "claw-proxy-ca",
				"-o", "jsonpath={.data.ca\\.crt}",
				"-n", userNamespace)
			caOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "Proxy CA Secret should exist")
			assert.NotEmpty(t, caOutput, "CA cert should not be empty")

			t.Log("verifying the ingress NetworkPolicy exists")
			cmd = exec.Command("kubectl", "get", "networkpolicy", "claw-ingress",
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Ingress NetworkPolicy should exist")

			t.Log("verifying the gateway Secret was created with a token")
			cmd = exec.Command("kubectl", "get", "secret", "claw-gateway-token",
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
			assert.Equal(t, "claw-gateway-token", secretRefOutput)

			t.Log("verifying CredentialsResolved condition")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='CredentialsResolved')].status}",
				"-n", userNamespace)
			condOutput, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, conditionTrue, condOutput, "CredentialsResolved should be True")

			t.Log("verifying ProxyConfigured condition")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='ProxyConfigured')].status}",
				"-n", userNamespace)
			condOutput, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, conditionTrue, condOutput, "ProxyConfigured should be True")

			t.Log("verifying reconciliation success in metrics")
			metricsOutput := fetchFreshMetrics(t, "curl-metrics-reconcile")
			assert.Contains(t, metricsOutput,
				`controller_runtime_reconcile_total{controller="claw",result="success"}`)
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
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for claw-proxy deployment")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, 2*time.Minute, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "deployment", "claw-proxy",
						"-n", userNamespace)
					_, err := utils.Run(t, cmd)
					return err == nil, nil
				})
			require.NoError(t, err,
				"timed out waiting for claw-proxy deployment in namespace %s", userNamespace)

			t.Log("verifying CRED_GEMINI references the correct Secret name")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", "claw-proxy",
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "gemini-api-key", output)

			t.Log("verifying CRED_GEMINI references the correct Secret key")
			jp = "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.key}"
			cmd = exec.Command("kubectl", "get", "deployment", "claw-proxy",
				"-o", jp, "-n", userNamespace)
			output, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "api-key", output)

			t.Log("verifying the deployment uses the proxy container")
			cmd = exec.Command("kubectl", "get", "deployment", "claw-proxy",
				"-o", "jsonpath={.spec.template.spec.containers[0].name}",
				"-n", userNamespace)
			output, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "proxy", output, "First container should be named 'proxy'")

			t.Log("verifying pods are running")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, 3*time.Minute, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods", "-l", "app=claw-proxy",
						"-o", "jsonpath={.items[*].status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && strings.Contains(output, podPhaseRunning), nil
				})
			require.NoError(t, err,
				"claw-proxy pods in namespace %s never reached Running phase", userNamespace)
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

			t.Log("waiting for Claw ProxyConfigured condition")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='ProxyConfigured')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw ProxyConfigured did not become True within %v", defaultTimeout)

			t.Log("waiting for proxy pod to be running")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "app=claw-proxy",
						"-o", "jsonpath={.items[0].status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == podPhaseRunning, nil
				})
			require.NoError(t, err, "proxy pod did not reach Running phase")

			t.Log("capturing original pod UID")
			cmd = exec.Command("kubectl", "get", "pods", "-l", "app=claw-proxy",
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
					cmd := exec.Command("kubectl", "get", "deployment", "claw-proxy",
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
						"-l", "app=claw-proxy",
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
						"-l", "app=claw-proxy",
						"-o", "jsonpath={.items[0].status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == podPhaseRunning, nil
				})
			require.NoError(t, err, "new pod did not reach Running phase")
		})

		t.Run("should patch SSRF guard in claw gateway init container", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				waitForPVCDeletion(t, userNamespace)
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
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for claw pod to reach Running")
			ctx := context.Background()
			var podName string
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods", "-l", "app=claw",
						"-o", "go-template={{ range .items }}"+
							"{{ if not .metadata.deletionTimestamp }}"+
							"{{ .metadata.name }} {{ .status.phase }}"+
							"{{ \"\\n\" }}{{ end }}{{ end }}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					if err != nil {
						return false, nil
					}
					for _, line := range utils.GetNonEmptyLines(output) {
						parts := strings.Fields(line)
						if len(parts) == 2 && parts[1] == podPhaseRunning {
							podName = parts[0]
							return true, nil
						}
					}
					return false, nil
				})
			require.NoError(t, err, "claw pod did not reach Running — init containers may have failed")

			t.Log("verifying patch-proxy init container logs")
			cmd = exec.Command("kubectl", "logs", podName, "-c", "patch-proxy",
				"-n", userNamespace)
			logOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get patch-proxy logs")
			t.Logf("patch-proxy logs:\n%s", logOutput)

			assert.Contains(t, logOutput, "[proxy-patch] Patched SSRF guard:",
				"init container should report patching at least one file")
			assert.NotContains(t, logOutput, "ERROR",
				"init container should not report errors")
		})

		t.Run("should proxy kubectl requests with kubernetes credential type", func(t *testing.T) {
			const (
				kubeWorkspace = "e2e-kube-workspace"
				kubeSAName    = "claw-e2e-sa"
				kubeSecretNm  = "e2e-kubeconfig"
				curlPodName   = "curl-kube-proxy"
			)

			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", kubeSecretNm, "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "pod", curlPodName, "-n", userNamespace, "--ignore-not-found")
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "ns", kubeWorkspace, "--ignore-not-found")
				_, _ = utils.Run(t, cmd)
				waitForPVCDeletion(t, userNamespace)
			})

			// 1. Create workspace namespace
			t.Log("creating workspace namespace")
			cmd := exec.Command("kubectl", "create", "ns", kubeWorkspace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to create workspace namespace")

			// 2. Create ServiceAccount in workspace
			t.Log("creating ServiceAccount in workspace")
			cmd = exec.Command("kubectl", "create", "sa", kubeSAName, "-n", kubeWorkspace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "failed to create ServiceAccount")

			// 3. Grant edit role
			t.Log("granting edit role to ServiceAccount")
			cmd = exec.Command("kubectl", "create", "rolebinding", "claw-e2e-edit",
				"--clusterrole=edit",
				fmt.Sprintf("--serviceaccount=%s:%s", kubeWorkspace, kubeSAName),
				"-n", kubeWorkspace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "failed to create RoleBinding")

			// 4. Get SA token
			t.Log("requesting token for ServiceAccount")
			cmd = exec.Command("kubectl", "create", "token", kubeSAName,
				"-n", kubeWorkspace, "--duration=1h")
			saToken, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to create token")
			saToken = strings.TrimSpace(saToken)
			require.NotEmpty(t, saToken)

			// 5. Get cluster CA from host kubeconfig (--minify to get current context only)
			t.Log("extracting cluster CA from kubeconfig")
			cmd = exec.Command("kubectl", "config", "view", "--raw", "--minify",
				"-o", "jsonpath={.clusters[0].cluster.certificate-authority-data}")
			clusterCAB64, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get cluster CA")
			clusterCAB64 = strings.TrimSpace(clusterCAB64)
			require.NotEmpty(t, clusterCAB64, "cluster CA should not be empty")

			// 6. Build kubeconfig YAML
			kubeconfigYAML := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: kind-cluster
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority-data: %s
contexts:
  - name: workspace
    context:
      cluster: kind-cluster
      user: claw-sa
      namespace: %s
current-context: workspace
users:
  - name: claw-sa
    user:
      token: %s
`, clusterCAB64, kubeWorkspace, saToken)

			// 7. Create Secret with kubeconfig
			t.Log("creating kubeconfig Secret")
			f, err := os.CreateTemp("", "e2e-kubeconfig-*.yaml")
			require.NoError(t, err)
			kubeconfigFile := f.Name()
			t.Cleanup(func() { _ = os.Remove(kubeconfigFile) })
			_, err = f.Write([]byte(kubeconfigYAML))
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.NoError(t, os.Chmod(kubeconfigFile, 0o600))

			cmd = exec.Command("kubectl", "delete", "secret", kubeSecretNm,
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "secret", "generic", kubeSecretNm,
				fmt.Sprintf("--from-file=kubeconfig=%s", kubeconfigFile),
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "failed to create kubeconfig Secret")

			// 8. Apply Claw CR with kubernetes credential
			t.Log("applying Claw CR with kubernetes credential")
			clawYAML := fmt.Sprintf(`apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: k8s-test
      type: kubernetes
      secretRef:
        name: %s
        key: kubeconfig
`, kubeSecretNm)
			crFile := filepath.Join("/tmp", "claw-e2e-kube.yaml")
			err = os.WriteFile(crFile, []byte(clawYAML), os.FileMode(0o644))
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "failed to apply Claw CR")

			// 9. Wait for ProxyConfigured=True
			t.Log("waiting for Claw ProxyConfigured condition")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='ProxyConfigured')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw ProxyConfigured did not become True")

			// Wait for both deployments to be fully available (readiness probes passing)
			t.Log("waiting for claw-proxy deployment to be available")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "deployment", "claw-proxy",
						"-o", "jsonpath={.status.availableReplicas}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == "1", nil
				})
			require.NoError(t, err, "claw-proxy deployment did not become available")

			// 10. Extract MITM CA cert from claw-proxy-ca Secret
			t.Log("extracting MITM CA cert")
			var mitmCAB64 string
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "secret", "claw-proxy-ca",
						"-o", "jsonpath={.data.ca\\.crt}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					if err == nil && output != "" {
						mitmCAB64 = strings.TrimSpace(output)
						return true, nil
					}
					return false, nil
				})
			require.NoError(t, err, "failed to get MITM CA cert")
			require.NotEmpty(t, mitmCAB64)

			// 11. Run curl pod through proxy to hit the Kubernetes API
			t.Log("running curl pod through proxy to access Kubernetes API")
			cmd = exec.Command("kubectl", "delete", "pod", curlPodName,
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)

			curlScript := fmt.Sprintf(
				"echo '%s' | base64 -d > /tmp/mitm-ca.crt && "+
					"curl -s -o /tmp/response.json -w '%%{http_code}' "+
					"--proxy http://claw-proxy.%s.svc.cluster.local:8080 "+
					"--cacert /tmp/mitm-ca.crt "+
					"https://kubernetes.default.svc/api/v1/namespaces/%s/configmaps && "+
					"echo && cat /tmp/response.json",
				mitmCAB64, userNamespace, kubeWorkspace)

			cmd = exec.Command("kubectl", "run", curlPodName, "--restart=Never",
				"--namespace", userNamespace,
				"--image=curlimages/curl:latest",
				"--overrides", fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [%q],
							"securityContext": {
								"allowPrivilegeEscalation": false,
								"capabilities": {"drop": ["ALL"]},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {"type": "RuntimeDefault"}
							}
						}]
					}
				}`, curlScript))
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "failed to create curl pod")

			// 12. Wait for curl pod to complete and check results
			t.Log("waiting for curl pod to complete")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods", curlPodName,
						"-o", "jsonpath={.status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && (output == podPhaseSucceeded || output == "Failed"), nil
				})
			require.NoError(t, err, "curl pod did not complete")

			t.Log("checking curl pod logs")
			cmd = exec.Command("kubectl", "logs", curlPodName, "-n", userNamespace)
			curlOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get curl pod logs")
			t.Logf("curl output:\n%s", curlOutput)

			assert.Contains(t, curlOutput, "200",
				"curl through proxy to Kubernetes API should return 200")
			assert.Contains(t, curlOutput, "ConfigMapList",
				"response should contain ConfigMapList kind")
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
			return err == nil && output == podPhaseSucceeded, nil
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

func waitForPVCDeletion(t *testing.T, namespace string) {
	t.Helper()
	ctx := context.Background()
	_ = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pvc", "claw-home-pvc",
				"-n", namespace, "--no-headers")
			_, err := utils.Run(t, cmd)
			return err != nil, nil
		})
}
