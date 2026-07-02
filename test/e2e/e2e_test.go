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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/codeready-toolchain/claw-operator/internal/controller"
	"github.com/codeready-toolchain/claw-operator/test/utils"
)

const (
	operatorNamespace = "claw-operator"
	userNamespace     = "test-e2e"

	serviceAccountName     = "claw-operator-controller-manager"
	metricsServiceName     = "claw-operator-controller-manager-metrics-service"
	metricsRoleBindingName = "claw-operator-metrics-binding"

	defaultTimeout  = 2 * time.Minute
	pollInterval    = 1 * time.Second
	extendedTimeout = 5 * time.Minute

	podPhaseRunning   = "Running"
	podPhaseSucceeded = "Succeeded"
	podPhaseFailed    = "Failed"
	conditionTrue     = "True"

	clawInstanceName    = "instance"
	proxyDeploymentName = clawInstanceName + "-proxy"
	configMapName       = clawInstanceName + "-config"
	proxyConfigMapName  = clawInstanceName + "-proxy-config"
	proxyCACertName     = clawInstanceName + "-proxy-ca"
	gatewaySecretName   = clawInstanceName + "-gateway-token"
	ingressNetPolName   = clawInstanceName + "-ingress"
	pvcName             = clawInstanceName + "-home-pvc"
	proxyServiceName    = clawInstanceName + "-proxy"
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
        - name: %s
          key: %s
      domain: ".googleapis.com"
      apiKey:
        header: x-goog-api-key
`, secretName, secretKey)
}

// clawYAMLWithImage returns a Claw CR YAML with an explicit spec.image and apiKey credential.
func clawYAMLWithImage(image, secretName, secretKey string) string {
	return fmt.Sprintf(`apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  image: %s
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: %s
          key: %s
      domain: ".googleapis.com"
      apiKey:
        header: x-goog-api-key
`, image, secretName, secretKey)
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

	t.Log("creating user namespace")
	cmd = exec.Command("kubectl", "create", "ns", userNamespace)
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create user namespace")

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

		t.Log("removing user namespace")
		cmd = exec.Command("kubectl", "delete", "ns", userNamespace, "--ignore-not-found")
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

		t.Log("Fetching Kubernetes events in operator namespace")
		cmd = exec.Command("kubectl", "get", "events", "-n", operatorNamespace, "--sort-by=.lastTimestamp")
		eventsOutput, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Kubernetes events in operator namespace:\n%s", eventsOutput)
		} else {
			t.Logf("Failed to get Kubernetes events in operator namespace: %s", err)
		}

		t.Log("Fetching Kubernetes events in user namespace")
		cmd = exec.Command("kubectl", "get", "events", "-n", userNamespace, "--sort-by=.lastTimestamp")
		eventsOutput, err = utils.Run(t, cmd)
		if err == nil {
			t.Logf("Kubernetes events in user namespace:\n%s", eventsOutput)
		} else {
			t.Logf("Failed to get Kubernetes events in user namespace: %s", err)
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

		t.Log("Fetching proxy pod logs")
		cmd = exec.Command("kubectl", "get", "pods", "-l", "app=claw-proxy", "-n", userNamespace, "-o", "jsonpath={.items[0].metadata.name}")
		proxyPodName, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Proxy pod name: %s", proxyPodName)
		} else {
			t.Logf("Failed to get Proxy pod name: %s", err)
		}
		cmd = exec.Command("kubectl", "logs", proxyPodName, "-n", userNamespace)
		proxyLogs, err := utils.Run(t, cmd)
		if err == nil {
			t.Logf("Proxy logs:\n %s", proxyLogs)
		} else {
			t.Logf("Failed to get Proxy logs: %s", err)
		}

	}

	t.Log("waiting for the controller-manager pod to be running")
	deadline := time.Now().Add(defaultTimeout)
	for time.Now().Before(deadline) {
		cmd = exec.Command("kubectl", "get",
			"pods", "-l", "control-plane=controller-manager",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", operatorNamespace,
		)
		podOutput, podErr := utils.Run(t, cmd)
		if podErr == nil {
			podNames := utils.GetNonEmptyLines(podOutput)
			if len(podNames) == 1 {
				controllerPodName = podNames[0]
				if strings.Contains(controllerPodName, "controller-manager") {
					cmd = exec.Command("kubectl", "get",
						"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
						"-n", operatorNamespace,
					)
					output, phaseErr := utils.Run(t, cmd)
					if phaseErr == nil && output == podPhaseRunning {
						break
					}
				}
			}
		}
		time.Sleep(pollInterval)
	}
	require.NotEmpty(t, controllerPodName,
		"timeout waiting for controller-manager pod to be running")

	t.Run("Manager", func(t *testing.T) {
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

			t.Log("creating credential Secret and Claw instance for operator metrics check")
			t.Cleanup(func() {
				cmd := exec.Command("kubectl", "delete", "claw", clawInstanceName,
					"-n", userNamespace, "--ignore-not-found")
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key",
					"-n", userNamespace, "--ignore-not-found")
				_, _ = utils.Run(t, cmd)
			})
			cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err)

			t.Log("waiting for Claw to become Ready")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", clawInstanceName,
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, runErr := utils.Run(t, cmd)
					return runErr == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw should become Ready")

			t.Log("verifying claw_instance_status metric for ready instance")
			metricsOutput = fetchFreshMetrics(t, "curl-metrics-status")
			t.Logf("metricsOutput: %s", metricsOutput)
			assert.Contains(t, metricsOutput, `claw_instance_status{name="instance",namespace="test-e2e",status="ready"} 1`)
			assert.Contains(t, metricsOutput, `claw_instance_status{name="instance",namespace="test-e2e",status="provisioning"} 0`)
			assert.Contains(t, metricsOutput, `claw_instance_status{name="instance",namespace="test-e2e",status="failed"} 0`)
			assert.Contains(t, metricsOutput, `claw_instance_status{name="instance",namespace="test-e2e",status="idled"} 0`)

			t.Log("verifying claw_instance_info metric")
			assert.Contains(t, metricsOutput, `claw_instance_info{auth_mode="token",idle="false",name="instance",namespace="test-e2e"} 1`)
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		t.Run("should reconcile Claw with credential-based proxy wiring", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

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
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "gemini-api-key", output,
				"CRED_GEMINI should reference gemini-api-key Secret")

			t.Log("verifying proxy-config ConfigMap was generated")
			cmd = exec.Command("kubectl", "get", "configmap", proxyConfigMapName,
				"-o", "jsonpath={.data.proxy-config\\.json}",
				"-n", userNamespace)
			configOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "proxy-config ConfigMap should exist")
			assert.Contains(t, configOutput, ".googleapis.com",
				"proxy config should contain the credential domain")

			t.Log("verifying the proxy CA Secret was created")
			cmd = exec.Command("kubectl", "get", "secret", proxyCACertName,
				"-o", "jsonpath={.data.ca\\.crt}",
				"-n", userNamespace)
			caOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "Proxy CA Secret should exist")
			assert.NotEmpty(t, caOutput, "CA cert should not be empty")

			t.Log("verifying the ingress NetworkPolicy exists")
			cmd = exec.Command("kubectl", "get", "networkpolicy", ingressNetPolName,
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Ingress NetworkPolicy should exist")

			t.Log("verifying the gateway Secret was created with a token")
			cmd = exec.Command("kubectl", "get", "secret", gatewaySecretName,
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
			assert.Equal(t, gatewaySecretName, secretRefOutput)

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

		t.Run("should create claw-config ConfigMap with deep-merge config", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("applying the Claw CR")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err := utils.Run(t, cmd)
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
			require.NoError(t, err, "Claw ProxyConfigured did not become True")

			t.Log("verifying operator.json has gateway config and providers")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data.operator\\.json}",
				"-n", userNamespace)
			operatorJSON, err := utils.Run(t, cmd)
			require.NoError(t, err, "config ConfigMap should exist with operator.json")
			assert.Contains(t, operatorJSON, `"gateway"`,
				"operator.json should contain gateway config")
			assert.Contains(t, operatorJSON, `"providers"`,
				"operator.json should contain providers section")
			assert.Contains(t, operatorJSON, `"agents"`,
				"operator.json should contain agents section (model catalog)")

			var operatorConfig map[string]any
			require.NoError(t, json.Unmarshal([]byte(operatorJSON), &operatorConfig),
				"operator.json should be valid JSON")
			models := operatorConfig["models"].(map[string]any)
			providers := models["providers"].(map[string]any)
			assert.NotEmpty(t, providers, "should have injected provider from credential")

			agents, okAgents := operatorConfig["agents"].(map[string]any)
			require.True(t, okAgents, "operator.json should contain agents section")
			defaults, okDefaults := agents["defaults"].(map[string]any)
			require.True(t, okDefaults, "agents should contain defaults section")
			catalogModels, hasModels := defaults["models"].(map[string]any)
			require.True(t, hasModels, "operator.json should contain agents.defaults.models")
			assert.NotEmpty(t, catalogModels, "model catalog should not be empty")
			model, okModel := defaults["model"].(map[string]any)
			require.True(t, okModel, "defaults should contain model section")
			assert.NotEmpty(t, model["primary"], "operator.json should have primary model")

			t.Log("verifying openclaw.json seed is user-owned (no $include, no hardcoded models)")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data.openclaw\\.json}",
				"-n", userNamespace)
			openclawJSON, err := utils.Run(t, cmd)
			require.NoError(t, err, "config ConfigMap should have openclaw.json")
			assert.NotContains(t, openclawJSON, `"$include"`,
				"openclaw.json should not contain $include (replaced by deep-merge)")
			assert.Contains(t, openclawJSON, `"agents"`,
				"openclaw.json seed should contain agents section")
			assert.NotContains(t, openclawJSON, `"models"`,
				"openclaw.json seed must not contain models (now operator-managed)")

			t.Log("verifying merge.js script is present in ConfigMap")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data.merge\\.js}",
				"-n", userNamespace)
			mergeJS, err := utils.Run(t, cmd)
			require.NoError(t, err, "config ConfigMap should have merge.js")
			assert.Contains(t, mergeJS, "deepMerge",
				"merge.js should contain the deep-merge function")

			t.Log("verifying init-config container uses gateway image and merge script")
			clawDeployName := clawInstanceName
			initJP := `jsonpath={.spec.template.spec.initContainers[?(@.name=="init-config")].command}`
			cmd = exec.Command("kubectl", "get", "deployment", clawDeployName,
				"-o", initJP, "-n", userNamespace)
			initCmd, err := utils.Run(t, cmd)
			require.NoError(t, err, "should be able to read init-config command")
			assert.Contains(t, initCmd, "node",
				"init-config should use node runtime")
			assert.Contains(t, initCmd, "/config/merge.js",
				"init-config should run merge.js script")

			t.Log("verifying CLAW_CONFIG_MODE env var defaults to merge")
			envJP := `jsonpath={.spec.template.spec.initContainers[?(@.name=="init-config")]` +
				`.env[?(@.name=="CLAW_CONFIG_MODE")].value}`
			cmd = exec.Command("kubectl", "get", "deployment", clawDeployName,
				"-o", envJP, "-n", userNamespace)
			configMode, err := utils.Run(t, cmd)
			require.NoError(t, err, "should be able to read CLAW_CONFIG_MODE")
			assert.Equal(t, "merge", configMode,
				"CLAW_CONFIG_MODE should default to merge")

			t.Log("verifying AGENTS.md seed is present")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data.AGENTS\\.md}",
				"-n", userNamespace)
			agentsMd, err := utils.Run(t, cmd)
			require.NoError(t, err, "config ConfigMap should have AGENTS.md")
			assert.Contains(t, agentsMd, "OpenClaw Assistant",
				"AGENTS.md should contain seed content")

			t.Log("verifying KUBERNETES.md is absent (no kubernetes credentials)")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data.KUBERNETES\\.md}",
				"-n", userNamespace)
			kubeMd, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Empty(t, kubeMd, "KUBERNETES.md should not exist without kubernetes credentials")
		})

		t.Run("should wire credential env var with correct Secret reference", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-gemini-key-value")

			t.Log("applying the Claw CR")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for proxy deployment")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, 2*time.Minute, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
						"-n", userNamespace)
					_, err := utils.Run(t, cmd)
					return err == nil, nil
				})
			require.NoError(t, err,
				"timed out waiting for proxy deployment in namespace %s", userNamespace)

			t.Log("verifying CRED_GEMINI references the correct Secret name")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.name}"
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "gemini-api-key", output)

			t.Log("verifying CRED_GEMINI references the correct Secret key")
			jp = "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_GEMINI')].valueFrom.secretKeyRef.key}"
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
				"-o", jp, "-n", userNamespace)
			output, err = utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "api-key", output)

			t.Log("verifying the deployment uses the proxy container")
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
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
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the first credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "llm-key-1",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "llm-key-1",
				"--from-literal=api-key=first-api-key")

			t.Log("creating Claw CR referencing first Secret")
			crFile := filepath.Join("/tmp", "claw-e2e-test.yaml")
			err := os.WriteFile(crFile, []byte(clawYAMLWithGemini("llm-key-1", "api-key")),
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
			createLabeledSecret(t, "llm-key-2",
				"--from-literal=api-key=second-api-key")

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
					cmd := exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
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

		t.Run("should set OPENCLAW_PROXY_ACTIVE env for managed proxy support", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("applying the Claw CR")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err := utils.Run(t, cmd)
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

			t.Log("verifying OPENCLAW_PROXY_ACTIVE env var is set on gateway container")
			jsonPath := `{.spec.containers[?(@.name=="gateway")]` +
				`.env[?(@.name=="OPENCLAW_PROXY_ACTIVE")].value}`
			cmd = exec.Command("kubectl", "get", "pod", podName,
				"-n", userNamespace, "-o", "jsonpath="+jsonPath)
			logOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get gateway env")
			assert.Equal(t, "1", logOutput,
				"OPENCLAW_PROXY_ACTIVE should be set to 1")
		})

		t.Run("should reject Claw CR with password mode but no passwordSecretRef", func(t *testing.T) {
			t.Cleanup(func() { collectDebugInfo(t) })

			crYAML := `apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  auth:
    mode: password
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      domain: ".googleapis.com"
      apiKey:
        header: x-goog-api-key
`
			crFile := filepath.Join("/tmp", "claw-e2e-invalid-auth.yaml")
			err := os.WriteFile(crFile, []byte(crYAML), os.FileMode(0o644))
			require.NoError(t, err)

			cmd := exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.Error(t, err, "CR with password mode but no passwordSecretRef should be rejected")
			assert.Contains(t, output+err.Error(), "passwordSecretRef is required when mode is password",
				"error should mention the missing passwordSecretRef")
		})

		t.Run("should configure password auth mode via env var, not ConfigMap", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "workshop-pw", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("creating the password Secret")
			cmd = exec.Command("kubectl", "delete", "secret", "workshop-pw",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "workshop-pw",
				"--from-literal=password=classroom-pass-e2e")

			t.Log("applying Claw CR with password auth mode")
			crYAML := `apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  auth:
    mode: password
    passwordSecretRef:
      name: workshop-pw
      key: password
  credentials:
    - name: gemini
      type: apiKey
      secretRef:
        - name: gemini-api-key
          key: api-key
      provider: google
`
			crFile := filepath.Join("/tmp", "claw-e2e-password-auth.yaml")
			err := os.WriteFile(crFile, []byte(crYAML), os.FileMode(0o644))
			require.NoError(t, err)

			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR with password auth")

			t.Log("waiting for operator.json auth mode to be set")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "configmap", configMapName,
						"-o", "jsonpath={.data.operator\\.json}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					if err != nil {
						return false, nil
					}
					var config map[string]any
					if json.Unmarshal([]byte(output), &config) != nil {
						return false, nil
					}
					gw, _ := config["gateway"].(map[string]any)
					if gw == nil {
						return false, nil
					}
					auth, _ := gw["auth"].(map[string]any)
					return auth != nil && auth["mode"] == "password", nil
				})
			require.NoError(t, err, "operator.json auth mode did not become password")

			t.Log("verifying operator.json has no plaintext password")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data.operator\\.json}",
				"-n", userNamespace)
			operatorJSON, err := utils.Run(t, cmd)
			require.NoError(t, err, "config ConfigMap should exist with operator.json")

			var operatorConfig map[string]any
			require.NoError(t, json.Unmarshal([]byte(operatorJSON), &operatorConfig))

			gateway := operatorConfig["gateway"].(map[string]any)
			auth := gateway["auth"].(map[string]any)
			assert.Equal(t, "password", auth["mode"])
			_, hasPassword := auth["password"]
			assert.False(t, hasPassword, "password must not be in ConfigMap")

			controlUI, ok := gateway["controlUi"].(map[string]any)
			require.True(t, ok, "gateway should contain controlUi section")
			assert.Equal(t, true, controlUI["dangerouslyDisableDeviceAuth"])

			t.Log("verifying gateway deployment has OPENCLAW_GATEWAY_PASSWORD env var from Secret")
			gwEnvPath := ".spec.template.spec.containers[?(@.name=='gateway')]" +
				".env[?(@.name=='OPENCLAW_GATEWAY_PASSWORD')]"
			cmd = exec.Command("kubectl", "get", "deployment", "instance",
				"-o", "jsonpath={"+gwEnvPath+".valueFrom.secretKeyRef.name}",
				"-n", userNamespace)
			secretName, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "workshop-pw", secretName,
				"OPENCLAW_GATEWAY_PASSWORD should reference the password Secret")

			cmd = exec.Command("kubectl", "get", "deployment", "instance",
				"-o", "jsonpath={"+gwEnvPath+".valueFrom.secretKeyRef.key}",
				"-n", userNamespace)
			secretKey, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "password", secretKey,
				"OPENCLAW_GATEWAY_PASSWORD should reference the correct key")
		})

		t.Run("should idle and unidle a Claw instance", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("applying the Claw CR")
			cmd = exec.Command("kubectl", "apply", "-f",
				"config/samples/claw_v1alpha1_claw.yaml", "-n", userNamespace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for Claw Ready=True")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw Ready did not become True within %v", extendedTimeout)

			t.Log("idling the instance via spec.idle patch")
			cmd = exec.Command("kubectl", "patch", "claw", "instance",
				"--type=merge", "-p", `{"spec":{"idle":true}}`,
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to patch spec.idle to true")

			t.Log("waiting for Idle=True condition")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Idle')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Idle condition did not become True")

			t.Log("verifying Ready=False with reason Idle")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
				"-n", userNamespace)
			readyStatus, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "False", readyStatus, "Ready should be False when idled")

			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].reason}",
				"-n", userNamespace)
			readyReason, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "Idle", readyReason, "Ready reason should be Idle")

			t.Log("verifying status URLs are cleared when idled")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.url}",
				"-n", userNamespace)
			urlOutput, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Empty(t, urlOutput, "status.url should be empty when idled")

			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.gatewayURL}",
				"-n", userNamespace)
			gwURLOutput, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Empty(t, gwURLOutput, "status.gatewayURL should be empty when idled")

			t.Log("verifying all pods are terminated")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "claw.sandbox.redhat.com/instance=instance",
						"-o", "jsonpath={.items}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && (output == "[]" || output == ""), nil
				})
			require.NoError(t, err, "Pods should be terminated after idling")

			t.Log("verifying claw_instance_status metric for idled instance")
			idleMetrics := fetchFreshMetrics(t, "curl-metrics-idle")
			assert.Contains(t, idleMetrics, `claw_instance_status{name="instance",namespace="test-e2e",status="idled"} 1`)
			assert.Contains(t, idleMetrics, `claw_instance_status{name="instance",namespace="test-e2e",status="ready"} 0`)
			assert.Contains(t, idleMetrics, `claw_instance_status{name="instance",namespace="test-e2e",status="provisioning"} 0`)
			assert.Contains(t, idleMetrics, `claw_instance_status{name="instance",namespace="test-e2e",status="failed"} 0`)

			t.Log("unidling the instance")
			cmd = exec.Command("kubectl", "patch", "claw", "instance",
				"--type=merge", "-p", `{"spec":{"idle":false}}`,
				"-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to patch spec.idle to false")

			t.Log("waiting for Claw Ready=True after unidle")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw Ready did not become True after unidle")

			t.Log("verifying Idle condition is removed")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.conditions[?(@.type=='Idle')].status}",
				"-n", userNamespace)
			idleStatus, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Empty(t, idleStatus, "Idle condition should be absent after unidle")
		})

		t.Run("should reconcile unlabeled user Secret and detect rotation via metadata-only watch", func(t *testing.T) {
			const unlabeledSecretName = "e2e-unlabeled-key"

			require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")

			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", unlabeledSecretName, "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating a user Secret WITHOUT the claw managed label")
			cmd := exec.Command("kubectl", "delete", "secret", unlabeledSecretName,
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			cmd = exec.Command("kubectl", "create", "secret", "generic", unlabeledSecretName,
				"--from-literal=api-key=initial-key-value", "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create unlabeled Secret")

			t.Log("applying Claw CR referencing the unlabeled Secret")
			crFile := filepath.Join("/tmp", "claw-e2e-unlabeled.yaml")
			err = os.WriteFile(crFile, []byte(clawYAMLWithGemini(unlabeledSecretName, "api-key")),
				os.FileMode(0o644))
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for Claw CredentialsResolved condition")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='CredentialsResolved')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw CredentialsResolved did not become True — "+
				"UserSecretReader should read unlabeled Secrets")

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

			t.Log("capturing original proxy pod UID before Secret rotation")
			cmd = exec.Command("kubectl", "get", "pods", "-l", "app=claw-proxy",
				"-o", "jsonpath={.items[0].metadata.uid}",
				"-n", userNamespace)
			originalPodUID, err := utils.Run(t, cmd)
			require.NoError(t, err)
			require.NotEmpty(t, originalPodUID)

			t.Log("rotating user Secret data (no label change)")
			cmd = exec.Command("kubectl", "create", "secret", "generic", unlabeledSecretName,
				"--from-literal=api-key=rotated-key-value",
				"-n", userNamespace, "--dry-run=client", "-o", "yaml")
			yamlOut, err := utils.Run(t, cmd)
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", "-", "-n", userNamespace)
			cmd.Stdin = strings.NewReader(yamlOut)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to rotate Secret data")

			t.Log("verifying proxy pod was restarted after Secret rotation")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "app=claw-proxy",
						"-o", "jsonpath={.items[0].metadata.uid}",
						"-n", userNamespace)
					uid, err := utils.Run(t, cmd)
					return err == nil && uid != "" && uid != originalPodUID, nil
				})
			require.NoError(t, err, "proxy pod was not restarted after Secret rotation — "+
				"Watches should detect changes to unlabeled Secrets")

			t.Log("verifying operator-created Secrets have the instance label")
			for _, secretName := range []string{gatewaySecretName, proxyCACertName} {
				cmd = exec.Command("kubectl", "get", "secret", secretName,
					"-o", "jsonpath={.metadata.labels}", "-n", userNamespace)
				labelsOut, err := utils.Run(t, cmd)
				require.NoError(t, err, "Secret %s should exist", secretName)
				assert.Contains(t, labelsOut, controller.InstanceLabelKey,
					"Secret %s should have instance label", secretName)
			}

			t.Log("verifying operator-created ConfigMaps have the instance label")
			for _, cmName := range []string{proxyConfigMapName, configMapName} {
				cmd = exec.Command("kubectl", "get", "configmap", cmName,
					"-o", "jsonpath={.metadata.labels}", "-n", userNamespace)
				labelsOut, err := utils.Run(t, cmd)
				require.NoError(t, err, "ConfigMap %s should exist", cmName)
				assert.Contains(t, labelsOut, controller.InstanceLabelKey,
					"ConfigMap %s should have instance label", cmName)
			}
		})

		t.Run("should inject init-plugins container for anthropic vertex credential", func(t *testing.T) {
			const gcpSecretName = "e2e-gcp-sa"

			require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")

			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", gcpSecretName, "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating a dummy GCP service account Secret")
			cmd := exec.Command("kubectl", "delete", "secret", gcpSecretName,
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			dummySA := `{"type":"service_account",` +
				`"project_id":"fake-project",` +
				`"private_key_id":"k",` +
				`"private_key":"-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----\n",` +
				`"client_email":"sa@fake.iam.gserviceaccount.com",` +
				`"client_id":"1",` +
				`"auth_uri":"https://accounts.google.com/o/oauth2/auth",` +
				`"token_uri":"https://oauth2.googleapis.com/token"}`
			createLabeledSecret(t, gcpSecretName,
				"--from-literal=sa.json="+dummySA)

			t.Log("applying Claw CR with anthropic vertex credential (type=gcp, provider=anthropic)")
			crYAML := fmt.Sprintf(`apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: anthropic-vertex
      type: gcp
      provider: anthropic
      secretRef:
        - name: %s
          key: sa.json
      gcp:
        project: fake-project
        location: us-east5
`, gcpSecretName)
			crFile := filepath.Join("/tmp", "claw-e2e-vertex-plugins.yaml")
			err := os.WriteFile(crFile, []byte(crYAML), os.FileMode(0o644))
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR with anthropic vertex credential")

			t.Log("waiting for claw gateway Deployment to be available (init containers must complete)")
			clawDeployName := clawInstanceName
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true,
				func(ctx context.Context) (bool, error) {
					availableJP := `jsonpath={.status.conditions[?(@.type=="Available")].status}`
					cmd := exec.Command("kubectl", "get", "deployment", clawDeployName,
						"-o", availableJP, "-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			if err != nil {
				t.Log("deployment did not become Available — collecting init container diagnostics")
				cmd = exec.Command("kubectl", "get", "pods", "-l", "app=claw",
					"-o", "jsonpath={.items[0].metadata.name}", "-n", userNamespace)
				if podName, podErr := utils.Run(t, cmd); podErr == nil && podName != "" {
					cmd = exec.Command("kubectl", "get", "pod", podName,
						"-o", `jsonpath={range .status.initContainerStatuses[*]}{.name}{"\t"}{.state}{"\n"}{end}`,
						"-n", userNamespace)
					if statuses, sErr := utils.Run(t, cmd); sErr == nil {
						t.Logf("Init container statuses:\n%s", statuses)
					}
					for _, ic := range []string{controller.PluginsInitContainerName, "wait-for-proxy", "init-config", "init-volume"} {
						cmd = exec.Command("kubectl", "logs", podName, "-c", ic, "-n", userNamespace)
						if logs, lErr := utils.Run(t, cmd); lErr == nil && logs != "" {
							t.Logf("Logs from init container %q:\n%s", ic, logs)
						}
					}
					cmd = exec.Command("kubectl", "describe", "pod", podName, "-n", userNamespace)
					if desc, dErr := utils.Run(t, cmd); dErr == nil {
						t.Logf("Pod describe:\n%s", desc)
					}
				}
			}
			require.NoError(t, err,
				"timed out waiting for claw deployment to become Available — "+
					"init-plugins container likely failed (see diagnostics above)")

			t.Log("verifying init-plugins logs mention the anthropic-vertex-provider plugin")
			cmd = exec.Command("kubectl", "get", "pods", "-l", "app=claw",
				"-o", "jsonpath={.items[0].metadata.name}", "-n", userNamespace)
			podName, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get claw pod name")
			cmd = exec.Command("kubectl", "logs", podName, "-c", controller.PluginsInitContainerName,
				"-n", userNamespace)
			pluginLogs, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get init-plugins logs")
			assert.Contains(t, pluginLogs, "anthropic-vertex-provider",
				"init-plugins logs should mention the anthropic-vertex-provider plugin")
		})

		t.Run("should wire Slack dual-token credential with separate env vars per role", func(t *testing.T) {
			const (
				slackSecretName = "e2e-slack-secret"
				slackEchoPod    = "slack-echo"
				curlSlackPod    = "curl-slack-proxy"
			)

			require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")

			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", slackSecretName, "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				for _, suffix := range []string{"-app", "-bot", "-form", "-json"} {
					cmd = exec.Command("kubectl", "delete", "pod", curlSlackPod+suffix, "-n", userNamespace, "--ignore-not-found")
					_, _ = utils.Run(t, cmd)
				}
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			// --- Deploy TLS echo server ---
			certDir, echoPodIP := deployTLSEchoServer(t, "slack.com", slackEchoPod, "e2e-slack-tls")

			ctx := context.Background()

			// --- Create Claw CR and verify operator wiring ---

			t.Log("creating the Slack credential Secret with app-token and bot-token keys")
			cmd = exec.Command("kubectl", "delete", "secret", slackSecretName,
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, slackSecretName,
				"--from-literal=app-token=xapp-test-e2e-app-token",
				"--from-literal=bot-token=xoxb-test-e2e-bot-token")

			t.Log("applying Claw CR with Slack channel credential")
			crYAML := fmt.Sprintf(`apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: slack
      channel: slack
      secretRef:
        - name: %s
          key: app-token
          role: appToken
        - name: %s
          key: bot-token
          role: botToken
`, slackSecretName, slackSecretName)
			crFile := filepath.Join("/tmp", "claw-e2e-slack.yaml")
			err = os.WriteFile(crFile, []byte(crYAML), os.FileMode(0o644))
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR with Slack credential")

			t.Log("waiting for Claw ProxyConfigured condition")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='ProxyConfigured')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw ProxyConfigured did not become True")

			t.Log("verifying CRED_SLACK_APP env var references the app-token key")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_SLACK_APP')].valueFrom.secretKeyRef}"
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
				"-o", jp, "-n", userNamespace)
			output, err := utils.Run(t, cmd)
			require.NoError(t, err, "CRED_SLACK_APP env var should exist")
			assert.Contains(t, output, slackSecretName,
				"CRED_SLACK_APP should reference the Slack Secret")
			assert.Contains(t, output, "app-token",
				"CRED_SLACK_APP should reference the app-token key")

			t.Log("verifying CRED_SLACK_BOT env var references the bot-token key")
			jp = "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')]" +
				".env[?(@.name=='CRED_SLACK_BOT')].valueFrom.secretKeyRef}"
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
				"-o", jp, "-n", userNamespace)
			output, err = utils.Run(t, cmd)
			require.NoError(t, err, "CRED_SLACK_BOT env var should exist")
			assert.Contains(t, output, slackSecretName,
				"CRED_SLACK_BOT should reference the Slack Secret")
			assert.Contains(t, output, "bot-token",
				"CRED_SLACK_BOT should reference the bot-token key")

			t.Log("verifying no single CRED_SLACK env var exists (should be split)")
			allEnvJP := "jsonpath={.spec.template.spec.containers[?(@.name=='proxy')].env[*].name}"
			cmd = exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
				"-o", allEnvJP, "-n", userNamespace)
			allEnvNames, err := utils.Run(t, cmd)
			require.NoError(t, err)
			for _, name := range strings.Fields(allEnvNames) {
				assert.NotEqual(t, "CRED_SLACK", name,
					"should not have a single CRED_SLACK env var — tokens must be split")
			}

			t.Log("verifying proxy-config.json has two routes for slack.com")
			cmd = exec.Command("kubectl", "get", "configmap", proxyConfigMapName,
				"-o", "jsonpath={.data.proxy-config\\.json}",
				"-n", userNamespace)
			configOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "proxy-config ConfigMap should exist")

			var proxyCfg struct {
				Routes []struct {
					Domain       string   `json:"domain"`
					Injector     string   `json:"injector"`
					EnvVar       string   `json:"envVar"`
					AllowedPaths []string `json:"allowedPaths"`
				} `json:"routes"`
			}
			require.NoError(t, json.Unmarshal([]byte(configOutput), &proxyCfg),
				"proxy-config.json should be valid JSON")

			var slackRoutes []struct {
				EnvVar       string
				AllowedPaths []string
			}
			for _, r := range proxyCfg.Routes {
				if r.Domain == "slack.com" {
					slackRoutes = append(slackRoutes, struct {
						EnvVar       string
						AllowedPaths []string
					}{r.EnvVar, r.AllowedPaths})
				}
			}
			require.Len(t, slackRoutes, 2,
				"should have two routes for slack.com (app + bot)")

			foundApp := false
			foundBot := false
			for _, sr := range slackRoutes {
				switch sr.EnvVar {
				case "CRED_SLACK_APP":
					foundApp = true
					assert.Equal(t, []string{"/api/apps.connections.open"}, sr.AllowedPaths,
						"app-token route should restrict to Socket Mode handshake path")
				case "CRED_SLACK_BOT":
					foundBot = true
					assert.Empty(t, sr.AllowedPaths,
						"bot-token route should be a catch-all with no path restriction")
				}
			}
			assert.True(t, foundApp, "should have a CRED_SLACK_APP route for slack.com")
			assert.True(t, foundBot, "should have a CRED_SLACK_BOT route for slack.com")

			t.Log("verifying companion .slack.com route exists with injector=none")
			hasCompanion := false
			for _, r := range proxyCfg.Routes {
				if r.Domain == ".slack.com" && r.Injector == "none" {
					hasCompanion = true
					break
				}
			}
			assert.True(t, hasCompanion,
				"should have a .slack.com companion route with injector=none for WebSocket traffic")

			// --- Patch proxy with echo server CA and hostAliases ---
			patchProxyForEchoServer(t, certDir, "slack.com", echoPodIP)

			// --- Send HTTPS requests through the proxy (CONNECT/MITM) ---

			t.Log("extracting MITM CA cert for curl")
			mitmCAB64 := extractMITMCA(t)

			t.Log("verifying app token injection on /api/apps.connections.open")
			appOutput := curlThroughProxy(t, curlSlackPod+"-app", mitmCAB64, curlThroughProxyRequest{
				URL: "https://slack.com/api/apps.connections.open",
			})
			assert.Contains(t, appOutput, "Bearer xapp-test-e2e-app-token",
				"/api/apps.connections.open should receive the app token")
			assert.NotContains(t, appOutput, "xoxb-test-e2e-bot-token",
				"/api/apps.connections.open should NOT receive the bot token")

			t.Log("verifying bot token injection on /api/chat.postMessage")
			botOutput := curlThroughProxy(t, curlSlackPod+"-bot", mitmCAB64, curlThroughProxyRequest{
				URL: "https://slack.com/api/chat.postMessage",
			})
			assert.Contains(t, botOutput, "Bearer xoxb-test-e2e-bot-token",
				"/api/chat.postMessage should receive the bot token")
			assert.NotContains(t, botOutput, "xapp-test-e2e-app-token",
				"/api/chat.postMessage should NOT receive the app token")

			// --- Body token replacement tests ---

			t.Log("verifying form-encoded body token replacement (only token field, not text)")
			formOutput := curlThroughProxy(t, curlSlackPod+"-form", mitmCAB64, curlThroughProxyRequest{
				Method:      "POST",
				URL:         "https://slack.com/api/chat.postMessage",
				Body:        "token=xoxb-placeholder&text=hello+xoxb-placeholder&channel=C1234",
				ContentType: "application/x-www-form-urlencoded",
			})
			assert.Contains(t, formOutput, "xoxb-test-e2e-bot-token",
				"token field should contain the real bot token")
			assert.Contains(t, formOutput, "xoxb-placeholder",
				"text field should still contain the placeholder (proves non-global replacement)")

			t.Log("verifying JSON body token replacement on /api/chat.postMessage")
			jsonOutput := curlThroughProxy(t, curlSlackPod+"-json", mitmCAB64, curlThroughProxyRequest{
				Method:      "POST",
				URL:         "https://slack.com/api/chat.postMessage",
				Body:        `{"token":"xoxb-placeholder","channel":"C1234"}`,
				ContentType: "application/json",
			})
			assert.Contains(t, jsonOutput, "xoxb-test-e2e-bot-token",
				"token field should contain the real bot token")
			assert.NotContains(t, jsonOutput, "xoxb-placeholder",
				"placeholder should be fully replaced when no other field contains it")
		})

		t.Run("should proxy kubectl requests with kubernetes credential type", func(t *testing.T) {
			const (
				kubeWorkspace = "e2e-kube-workspace"
				kubeSAName    = "claw-e2e-sa"
				kubeSecretNm  = "e2e-kubeconfig"
				curlPodName   = "curl-kube-proxy"
			)

			// Ensure clean state from previous tests before creating resources
			require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")

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
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
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
			createLabeledSecret(t, kubeSecretNm,
				fmt.Sprintf("--from-file=kubeconfig=%s", kubeconfigFile))

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
        - name: %s
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
			t.Log("waiting for proxy deployment to be available")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "deployment", proxyDeploymentName,
						"-o", "jsonpath={.status.availableReplicas}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == "1", nil
				})
			require.NoError(t, err, "proxy deployment did not become available")

			// 10. Extract MITM CA cert from proxy CA Secret
			t.Log("extracting MITM CA cert")
			var mitmCAB64 string
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "secret", proxyCACertName,
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
					"--connect-timeout 10 --max-time 30 "+
					"--proxy http://%s.%s.svc.cluster.local:8080 "+
					"--cacert /tmp/mitm-ca.crt "+
					"https://kubernetes.default.svc/api/v1/namespaces/%s/configmaps && "+
					"echo && cat /tmp/response.json",
				mitmCAB64, proxyServiceName, userNamespace, kubeWorkspace)

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
					return err == nil && (output == podPhaseSucceeded || output == podPhaseFailed), nil
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

		t.Run("should deploy with custom image and report it in status", func(t *testing.T) {
			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("applying the Claw CR with explicit image")
			crYAML := clawYAMLWithImage(gatewayImage, "gemini-api-key", "api-key")
			crFile := filepath.Join(t.TempDir(), "claw.yaml")
			require.NoError(t, os.WriteFile(crFile, []byte(crYAML), 0o644))
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR")

			t.Log("waiting for Claw Ready condition")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw Ready did not become True within %v", extendedTimeout)

			t.Log("verifying status.image matches the resolved image")
			cmd = exec.Command("kubectl", "get", "claw", "instance",
				"-o", "jsonpath={.status.image}",
				"-n", userNamespace)
			statusImage, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, gatewayImage, statusImage,
				"status.image should reflect the resolved image")

			t.Log("verifying gateway container uses the custom image")
			jp := "jsonpath={.spec.template.spec.containers[?(@.name=='gateway')].image}"
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", jp, "-n", userNamespace)
			deployImage, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, gatewayImage, deployImage,
				"gateway container should use the custom image")

			t.Log("verifying init-config container uses the custom image")
			jp = "jsonpath={.spec.template.spec.initContainers[?(@.name=='init-config')].image}"
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", jp, "-n", userNamespace)
			initImage, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, gatewayImage, initImage,
				"init-config container should use the custom image")

			t.Log("verifying gateway pod is Running")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "app=claw",
						"-o", "jsonpath={.items[0].status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == podPhaseRunning, nil
				})
			require.NoError(t, err, "Gateway pod did not reach Running state")
		})

		t.Run("should seed workspace files from ConfigMap sources", func(t *testing.T) {
			const wsConfigMapName = "e2e-workspace-config"

			require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")

			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "configmap", wsConfigMapName, "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating workspace source ConfigMap")
			cmd := exec.Command("kubectl", "create", "configmap", wsConfigMapName,
				"--from-literal=soul.md=# Enterprise Soul\nYou are an enterprise assistant.",
				"--from-literal=tools.md=# Tools\nUse approved tools only.",
				"-n", userNamespace)
			_, err := utils.Run(t, cmd)
			require.NoError(t, err, "Failed to create workspace source ConfigMap")

			t.Log("creating the credential Secret")
			cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("applying Claw CR with configMapSources")
			crYAML := fmt.Sprintf(`apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      provider: google
      secretRef:
        - name: gemini-api-key
          key: api-key
  workspace:
    skipBootstrap: true
    configMapSources:
      - configMapRef:
          name: %s
        items:
          - key: soul.md
            path: "SOUL.md"
          - key: tools.md
            path: "TOOLS.md"
            mode: seedIfMissing
`, wsConfigMapName)
			crFile := filepath.Join("/tmp", "claw-e2e-cm-sources.yaml")
			err = os.WriteFile(crFile, []byte(crYAML), os.FileMode(0o644))
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR with configMapSources")

			t.Log("waiting for Claw Ready condition")
			ctx := context.Background()
			err = wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "claw", "instance",
						"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == conditionTrue, nil
				})
			require.NoError(t, err, "Claw Ready did not become True")

			t.Log("verifying ConfigMap source volume on gateway Deployment")
			volJP := fmt.Sprintf(
				"jsonpath={.spec.template.spec.volumes[?(@.name=='ws-cm-%s')].configMap.name}",
				wsConfigMapName)
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", volJP, "-n", userNamespace)
			volOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "should find ws-cm volume")
			assert.Equal(t, wsConfigMapName, volOutput,
				"ConfigMap source volume should reference the workspace ConfigMap")

			t.Log("verifying seed manifest has ConfigMap source entries")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data._seed_manifest\\.json}",
				"-n", userNamespace)
			manifestJSON, err := utils.Run(t, cmd)
			require.NoError(t, err, "seed manifest should exist in gateway ConfigMap")
			assert.Contains(t, manifestJSON, wsConfigMapName,
				"seed manifest should reference the ConfigMap source")
			assert.Contains(t, manifestJSON, "SOUL.md",
				"seed manifest should include SOUL.md target")
			assert.Contains(t, manifestJSON, "TOOLS.md",
				"seed manifest should include TOOLS.md target")

			t.Log("verifying init-seed container is present")
			initSeedJP := fmt.Sprintf(
				"jsonpath={.spec.template.spec.initContainers[?(@.name=='%s')].name}",
				controller.ClawSeedContainerName)
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", initSeedJP, "-n", userNamespace)
			initSeedName, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, controller.ClawSeedContainerName, initSeedName,
				"init-seed container should be present in the Deployment")

			t.Log("verifying gateway pod is Running (init-seed completed successfully)")
			err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
				func(ctx context.Context) (bool, error) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "app=claw",
						"-o", "jsonpath={.items[0].status.phase}",
						"-n", userNamespace)
					output, err := utils.Run(t, cmd)
					return err == nil && output == podPhaseRunning, nil
				})
			require.NoError(t, err, "Gateway pod did not reach Running — init-seed may have failed")

			t.Log("verifying init-seed logs show seeded files")
			cmd = exec.Command("kubectl", "get", "pods", "-l", "app=claw",
				"-o", "jsonpath={.items[0].metadata.name}", "-n", userNamespace)
			podName, err := utils.Run(t, cmd)
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "logs", podName, "-c",
				controller.ClawSeedContainerName, "-n", userNamespace)
			seedLogs, err := utils.Run(t, cmd)
			require.NoError(t, err, "failed to get init-seed logs")
			assert.Contains(t, seedLogs, "SOUL.md",
				"init-seed logs should mention SOUL.md")
		})

		t.Run("should wire git source init container and volumes", func(t *testing.T) {
			require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")

			t.Cleanup(func() {
				collectDebugInfo(t)
				cmd := exec.Command("kubectl", "delete", "claw", "instance", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "gemini-api-key", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				cmd = exec.Command("kubectl", "delete", "secret", "e2e-git-token", "-n", userNamespace)
				_, _ = utils.Run(t, cmd)
				require.NoError(t, waitForPVCDeletion(t), "PVC deletion timed out")
			})

			t.Log("creating the credential Secret")
			cmd := exec.Command("kubectl", "delete", "secret", "gemini-api-key",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "gemini-api-key",
				"--from-literal=api-key=test-api-key-value")

			t.Log("creating the git token Secret")
			cmd = exec.Command("kubectl", "delete", "secret", "e2e-git-token",
				"-n", userNamespace, "--ignore-not-found")
			_, _ = utils.Run(t, cmd)
			createLabeledSecret(t, "e2e-git-token",
				"--from-literal=token=ghp_fake_e2e_token")

			t.Log("applying Claw CR with gitSources")
			crYAML := `apiVersion: claw.sandbox.redhat.com/v1alpha1
kind: Claw
metadata:
  name: instance
spec:
  credentials:
    - name: gemini
      provider: google
      secretRef:
        - name: gemini-api-key
          key: api-key
  workspace:
    skipBootstrap: true
    gitSources:
      - url: https://git.example.com/team/agent-config.git
        ref: main
        secretRef:
          name: e2e-git-token
          key: token
        items:
          - repoPath: "configs/SOUL.md"
            path: "SOUL.md"
          - repoPath: "configs/AGENTS.md"
            path: "AGENTS.md"
`
			crFile := filepath.Join("/tmp", "claw-e2e-git-sources.yaml")
			err := os.WriteFile(crFile, []byte(crYAML), os.FileMode(0o644))
			require.NoError(t, err)
			cmd = exec.Command("kubectl", "apply", "-f", crFile, "-n", userNamespace)
			_, err = utils.Run(t, cmd)
			require.NoError(t, err, "Failed to apply Claw CR with gitSources")

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

			t.Log("verifying init-git-sync container exists with correct image")
			gitSyncJP := fmt.Sprintf(
				"jsonpath={.spec.template.spec.initContainers[?(@.name=='%s')].image}",
				controller.ClawGitSyncContainerName)
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", gitSyncJP, "-n", userNamespace)
			gitSyncImage, err := utils.Run(t, cmd)
			require.NoError(t, err, "init-git-sync container should exist")
			assert.Contains(t, gitSyncImage, "alpine/git",
				"init-git-sync should use alpine/git image")

			t.Log("verifying init-git-sync has proxy env vars")
			envJP := fmt.Sprintf(
				"jsonpath={.spec.template.spec.initContainers[?(@.name=='%s')].env[?(@.name=='HTTPS_PROXY')].value}",
				controller.ClawGitSyncContainerName)
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", envJP, "-n", userNamespace)
			proxyEnv, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Contains(t, proxyEnv, "proxy:8080",
				"init-git-sync should have HTTPS_PROXY set to the proxy service")

			t.Log("verifying init-git-sync has GIT_TOKEN_0 env from Secret")
			tokenJP := fmt.Sprintf(
				"jsonpath={.spec.template.spec.initContainers[?(@.name=='%s')].env[?(@.name=='GIT_TOKEN_0')].valueFrom.secretKeyRef.name}",
				controller.ClawGitSyncContainerName)
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", tokenJP, "-n", userNamespace)
			tokenSecretRef, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Equal(t, "e2e-git-token", tokenSecretRef,
				"GIT_TOKEN_0 should reference the git token Secret")

			t.Log("verifying emptyDir volume for git source")
			gitVolJP := "jsonpath={.spec.template.spec.volumes[?(@.name=='ws-git-0')].emptyDir}"
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", gitVolJP, "-n", userNamespace)
			gitVolOutput, err := utils.Run(t, cmd)
			require.NoError(t, err, "ws-git-0 volume should exist")
			assert.Equal(t, "{}", gitVolOutput,
				"ws-git-0 should be an emptyDir volume")

			t.Log("verifying seed manifest has git source entries")
			cmd = exec.Command("kubectl", "get", "configmap", configMapName,
				"-o", "jsonpath={.data._seed_manifest\\.json}",
				"-n", userNamespace)
			manifestJSON, err := utils.Run(t, cmd)
			require.NoError(t, err, "seed manifest should exist")
			assert.Contains(t, manifestJSON, "/git-sources/0/configs/SOUL.md",
				"seed manifest should contain git source path for SOUL.md")
			assert.Contains(t, manifestJSON, "/git-sources/0/configs/AGENTS.md",
				"seed manifest should contain git source path for AGENTS.md")

			t.Log("verifying init-git-sync command contains clone script")
			cmdJP := fmt.Sprintf(
				"jsonpath={.spec.template.spec.initContainers[?(@.name=='%s')].command}",
				controller.ClawGitSyncContainerName)
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", cmdJP, "-n", userNamespace)
			cmdOutput, err := utils.Run(t, cmd)
			require.NoError(t, err)
			assert.Contains(t, cmdOutput, "git clone --depth 1",
				"init-git-sync command should contain shallow clone")
			assert.Contains(t, cmdOutput, "oauth2",
				"clone script should use oauth2 token injection for private repos")

			t.Log("verifying init container ordering")
			allInitJP := "jsonpath={.spec.template.spec.initContainers[*].name}"
			cmd = exec.Command("kubectl", "get", "deployment", clawInstanceName,
				"-o", allInitJP, "-n", userNamespace)
			allInitNames, err := utils.Run(t, cmd)
			require.NoError(t, err)
			initNames := strings.Fields(allInitNames)
			require.True(t, len(initNames) >= 5,
				"should have at least 5 init containers (init-volume, init-config, wait-for-proxy, init-git-sync, init-seed)")

			gitSyncIdx := -1
			seedIdx := -1
			waitProxyIdx := -1
			for i, name := range initNames {
				switch name {
				case "wait-for-proxy":
					waitProxyIdx = i
				case controller.ClawGitSyncContainerName:
					gitSyncIdx = i
				case controller.ClawSeedContainerName:
					seedIdx = i
				}
			}
			assert.Greater(t, gitSyncIdx, waitProxyIdx,
				"init-git-sync should come after wait-for-proxy")
			assert.Greater(t, seedIdx, gitSyncIdx,
				"init-seed should come after init-git-sync")
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

// createLabeledSecret creates a Secret via kubectl and applies the instance
// label so it is visible to the operator's label-filtered informer cache.
// extraArgs are passed to `kubectl create secret generic` (e.g. --from-literal, --from-file).
func createLabeledSecret(t *testing.T, name string, extraArgs ...string) {
	t.Helper()
	args := append([]string{"create", "secret", "generic", name, "-n", userNamespace}, extraArgs...)
	cmd := exec.Command("kubectl", args...)
	_, err := utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create Secret %s", name)

	cmd = exec.Command("kubectl", "label", "secret", name,
		controller.InstanceLabelKey+"="+clawInstanceName,
		"-n", userNamespace, "--overwrite")
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to label Secret %s", name)
}

// deployTLSEchoServer deploys a TLS echo server (traefik/whoami) in the user
// namespace with a certificate valid for the given domain. Returns the local
// certDir (containing ca.crt for proxy config patching) and the echo pod IP.
// Registers cleanup of the echo pod and TLS secret.
func deployTLSEchoServer(t *testing.T, domain, echoPod, tlsSecret string) (string, string) {
	t.Helper()

	t.Log("generating TLS cert for echo server")
	certDir := t.TempDir()
	cmd := exec.Command("openssl", "req", "-x509", "-newkey", "ec",
		"-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
		"-keyout", filepath.Join(certDir, "ca.key"),
		"-out", filepath.Join(certDir, "ca.crt"),
		"-days", "1", "-subj", "/CN=E2E Echo CA")
	_, err := utils.Run(t, cmd)
	require.NoError(t, err, "Failed to generate echo CA")

	extFile := filepath.Join(certDir, "ext.cnf")
	require.NoError(t, os.WriteFile(extFile,
		[]byte(fmt.Sprintf("subjectAltName=DNS:%s", domain)), 0o644))
	cmd = exec.Command("openssl", "req", "-newkey", "ec",
		"-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
		"-keyout", filepath.Join(certDir, "server.key"),
		"-out", filepath.Join(certDir, "server.csr"),
		"-subj", fmt.Sprintf("/CN=%s", domain))
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to generate echo server CSR")

	cmd = exec.Command("openssl", "x509", "-req",
		"-in", filepath.Join(certDir, "server.csr"),
		"-CA", filepath.Join(certDir, "ca.crt"),
		"-CAkey", filepath.Join(certDir, "ca.key"),
		"-CAcreateserial",
		"-out", filepath.Join(certDir, "server.crt"),
		"-days", "1", "-extfile", extFile)
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to sign echo server cert")

	cmd = exec.Command("kubectl", "delete", "secret", tlsSecret,
		"-n", userNamespace, "--ignore-not-found")
	_, _ = utils.Run(t, cmd)
	cmd = exec.Command("kubectl", "create", "secret", "tls", tlsSecret,
		"--cert="+filepath.Join(certDir, "server.crt"),
		"--key="+filepath.Join(certDir, "server.key"),
		"-n", userNamespace)
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create echo TLS Secret")

	t.Log("deploying echo server with TLS")
	cmd = exec.Command("kubectl", "delete", "pod", echoPod,
		"-n", userNamespace, "--ignore-not-found")
	_, _ = utils.Run(t, cmd)

	cmd = exec.Command("kubectl", "run", echoPod, "--restart=Never",
		"--namespace", userNamespace,
		"--image=traefik/whoami:latest",
		"--overrides", fmt.Sprintf(`{
			"spec": {
				"containers": [{
					"name": "echo",
					"image": "traefik/whoami:latest",
					"args": ["--port=443", "--cert=/tls/tls.crt", "--key=/tls/tls.key"],
					"ports": [{"containerPort": 443}],
					"volumeMounts": [{"name": "tls", "mountPath": "/tls", "readOnly": true}],
					"securityContext": {
						"allowPrivilegeEscalation": false,
						"seccompProfile": {"type": "RuntimeDefault"}
					}
				}],
				"volumes": [{"name": "tls", "secret": {"secretName": %q}}]
			}
		}`, tlsSecret))
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create TLS echo server pod")

	t.Cleanup(func() {
		cmd := exec.Command("kubectl", "delete", "pod", echoPod, "-n", userNamespace, "--ignore-not-found")
		_, _ = utils.Run(t, cmd)
		cmd = exec.Command("kubectl", "delete", "secret", tlsSecret, "-n", userNamespace, "--ignore-not-found")
		_, _ = utils.Run(t, cmd)
	})

	ctx := context.Background()
	t.Log("waiting for echo server pod IP")
	var echoPodIP string
	err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pods", echoPod,
				"-o", "jsonpath={.status.podIP}",
				"-n", userNamespace)
			output, err := utils.Run(t, cmd)
			if err == nil && output != "" {
				echoPodIP = strings.TrimSpace(output)
				return true, nil
			}
			return false, nil
		})
	require.NoError(t, err, "echo server pod did not get an IP")
	t.Logf("echo pod IP: %s", echoPodIP)

	t.Log("verifying echo server accepts TLS connections")
	diagPod := echoPod + "-diag"
	cmd = exec.Command("kubectl", "delete", "pod", diagPod,
		"-n", userNamespace, "--ignore-not-found")
	_, _ = utils.Run(t, cmd)
	diagScript := fmt.Sprintf(`curl -sk --max-time 10 https://%s:443/healthz`, echoPodIP)
	cmd = exec.Command("kubectl", "run", diagPod, "--restart=Never",
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
		}`, diagScript))
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to create diag pod")
	err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pods", diagPod,
				"-o", "jsonpath={.status.phase}",
				"-n", userNamespace)
			output, err := utils.Run(t, cmd)
			return err == nil && (output == podPhaseSucceeded || output == podPhaseFailed), nil
		})
	require.NoError(t, err, "diag pod did not complete")
	cmd = exec.Command("kubectl", "logs", diagPod, "-n", userNamespace)
	diagOutput, err := utils.Run(t, cmd)
	require.NoError(t, err)
	t.Logf("echo server diag output:\n%s", diagOutput)
	require.Contains(t, diagOutput, "/healthz",
		"echo server should echo back the request path — TLS is not working")
	cmd = exec.Command("kubectl", "delete", "pod", diagPod,
		"-n", userNamespace, "--ignore-not-found")
	_, _ = utils.Run(t, cmd)

	return certDir, echoPodIP
}

// patchProxyForEchoServer patches the proxy deployment to trust the echo server's
// CA cert and resolve the domain to the echo server's IP via hostAliases. It scales
// the operator to 0 to prevent overwrites, patches the proxy-config ConfigMap,
// adds a hostAliases entry, and waits for the proxy to be available. Registers
// cleanup to scale the operator back to 1.
func patchProxyForEchoServer(t *testing.T, certDir, domain, echoPodIP string) {
	t.Helper()
	ctx := context.Background()
	t.Log("scaling operator to 0 to prevent config overwrite")
	cmd := exec.Command("kubectl", "scale", "deployment",
		"claw-operator-controller-manager",
		"--replicas=0", "-n", operatorNamespace)
	_, err := utils.Run(t, cmd)
	require.NoError(t, err)
	t.Cleanup(func() {
		cmd := exec.Command("kubectl", "scale", "deployment",
			"claw-operator-controller-manager",
			"--replicas=1", "-n", operatorNamespace)
		_, _ = utils.Run(t, cmd)
	})

	// wait for the operator to be scaled to 0
	err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "deployment", "claw-operator-controller-manager",
				"-o", "jsonpath={.status.availableReplicas}",
				"-n", operatorNamespace)
			output, err := utils.Run(t, cmd)
			t.Logf("operator availableReplicas: '%s'", output)
			return err == nil && (output == "0" || output == ""), nil
		})
	require.NoError(t, err, "operator not scaled to 0")

	echoCACert, err := os.ReadFile(filepath.Join(certDir, "ca.crt"))
	require.NoError(t, err)
	echoCACertB64 := base64.StdEncoding.EncodeToString(echoCACert)

	t.Logf("patching proxy-config to add echo server CA to %s routes", domain)
	cmd = exec.Command("kubectl", "get", "configmap", proxyConfigMapName,
		"-o", "jsonpath={.data.proxy-config\\.json}",
		"-n", userNamespace)
	rawProxyCfg, err := utils.Run(t, cmd)
	require.NoError(t, err)

	var cfgForPatch struct {
		Routes []map[string]any `json:"routes"`
	}
	require.NoError(t, json.Unmarshal([]byte(rawProxyCfg), &cfgForPatch))
	for i, r := range cfgForPatch.Routes {
		if d, _ := r["domain"].(string); d == domain {
			cfgForPatch.Routes[i]["caCert"] = echoCACertB64
		}
	}
	patchedCfgJSON, err := json.Marshal(cfgForPatch)
	require.NoError(t, err)

	cmd = exec.Command("kubectl", "create", "configmap", proxyConfigMapName,
		"-n", userNamespace,
		"--from-literal=proxy-config.json="+string(patchedCfgJSON),
		"--dry-run=client", "-o", "yaml")
	yamlOut, err := utils.Run(t, cmd)
	require.NoError(t, err)
	cmd = exec.Command("kubectl", "apply", "-f", "-", "-n", userNamespace)
	cmd.Stdin = strings.NewReader(yamlOut)
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to patch proxy-config with echo CA")

	t.Logf("adding hostAliases to proxy deployment: %s → %s", domain, echoPodIP)
	hostAliasJSON := fmt.Sprintf(
		`{"spec":{"template":{"spec":{"hostAliases":[{"ip":%q,"hostnames":[%q]}]}}}}`,
		echoPodIP, domain)
	cmd = exec.Command("kubectl", "patch", "deployment", proxyDeploymentName,
		"-n", userNamespace, "--type=strategic", "-p", hostAliasJSON)
	_, err = utils.Run(t, cmd)
	require.NoError(t, err, "Failed to patch proxy deployment with hostAliases")

	t.Log("waiting for new proxy pod with hostAliases to be available")
	err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pods", "-l", "app=claw-proxy",
				"-o", "json",
				"-n", userNamespace)
			output, err := utils.Run(t, cmd)
			// t.Logf("proxy pods: '%s'", output)
			if err == nil {
				podList := corev1.PodList{}
				err = json.Unmarshal([]byte(output), &podList)
				if err == nil {
					for _, pod := range podList.Items {
						if pod.DeletionTimestamp != nil {
							continue
						}
						if hasStatusCondition(&pod, corev1.PodReady, conditionTrue) {
							hostAliases := pod.Spec.HostAliases
							if len(hostAliases) == 1 {
								if hostAliases[0].IP == echoPodIP && hostAliases[0].Hostnames[0] == domain {
									return true, nil
								}
							}
						}
					}
				}
			}
			return false, nil
		})
	require.NoError(t, err, "proxy deployment not available after patching")
}

func hasStatusCondition(pod *corev1.Pod, conditionType corev1.PodConditionType, conditionStatus corev1.ConditionStatus) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status == conditionStatus
		}
	}
	return false
}

// extractMITMCA retrieves the base64-encoded proxy MITM CA certificate from the cluster.
func extractMITMCA(t *testing.T) string {
	t.Helper()
	var mitmCAB64 string
	ctx := context.Background()
	err := wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "secret", proxyCACertName,
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
	return mitmCAB64
}

// curlThroughProxyRequest describes a single curl request to send through the proxy.
type curlThroughProxyRequest struct {
	Method      string
	URL         string
	Body        string
	ContentType string
}

// curlThroughProxy runs a curl request through the MITM proxy in a temporary pod
// and returns the response output. The mitmCAB64 is the base64-encoded proxy CA
// certificate used to trust the proxy's MITM-generated TLS certs.
func curlThroughProxy(t *testing.T, podName, mitmCAB64 string, req curlThroughProxyRequest) string {
	t.Helper()

	cmd := exec.Command("kubectl", "delete", "pod", podName,
		"-n", userNamespace, "--ignore-not-found")
	_, _ = utils.Run(t, cmd)

	curlArgs := fmt.Sprintf(
		`-s -v --max-time 30 --connect-timeout 10 `+
			`--proxy http://%s.%s.svc.cluster.local:8080 `+
			`--cacert /tmp/mitm-ca.crt`,
		proxyServiceName, userNamespace)
	if req.Method != "" && req.Method != http.MethodGet {
		curlArgs += fmt.Sprintf(` -X %s`, req.Method)
	}
	if req.Body != "" {
		curlArgs += fmt.Sprintf(` -d %q`, req.Body)
	}
	if req.ContentType != "" {
		curlArgs += fmt.Sprintf(` -H "Content-Type: %s"`, req.ContentType)
	}

	script := fmt.Sprintf(
		`echo '%s' | base64 -d > /tmp/mitm-ca.crt && curl %s %s`,
		mitmCAB64, curlArgs, req.URL)

	cmd = exec.Command("kubectl", "run", podName, "--restart=Never",
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
		}`, script))
	_, err := utils.Run(t, cmd)
	require.NoError(t, err, "failed to create curl pod %s", podName)

	ctx := context.Background()
	err = wait.PollUntilContextTimeout(ctx, pollInterval, defaultTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pods", podName,
				"-o", "jsonpath={.status.phase}",
				"-n", userNamespace)
			output, err := utils.Run(t, cmd)
			return err == nil && (output == podPhaseSucceeded || output == podPhaseFailed), nil
		})
	require.NoError(t, err, "curl pod %s did not complete", podName)

	cmd = exec.Command("kubectl", "logs", podName, "-n", userNamespace)
	output, err := utils.Run(t, cmd)
	require.NoError(t, err, "failed to get logs from curl pod %s", podName)
	t.Logf("curl pod %s output:\n%s", podName, output)
	return output
}

func waitForPVCDeletion(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	return wait.PollUntilContextTimeout(ctx, pollInterval, extendedTimeout, true,
		func(ctx context.Context) (bool, error) {
			cmd := exec.Command("kubectl", "get", "pvc", pvcName,
				"-n", userNamespace, "--no-headers")
			output, err := utils.Run(t, cmd)
			if err != nil {
				if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
					return true, nil
				}
				return false, nil
			}
			if strings.TrimSpace(output) == "" {
				return true, nil
			}
			return false, nil
		})
}
