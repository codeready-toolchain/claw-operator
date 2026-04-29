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
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	testKubeSecretName = "kube-secret"
	testKubeCredVolume = "cred-k8s"
)

// --- Kubeconfig test helpers ---

func buildTestKubeconfig(t *testing.T, clusters map[string]string, users map[string]string, contexts map[string][2]string, currentContext string) []byte {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	for name, server := range clusters {
		cfg.Clusters[name] = &clientcmdapi.Cluster{Server: server}
	}
	for name, token := range users {
		cfg.AuthInfos[name] = &clientcmdapi.AuthInfo{Token: token}
	}
	for name, pair := range contexts {
		cfg.Contexts[name] = &clientcmdapi.Context{Cluster: pair[0], AuthInfo: pair[1]}
	}
	cfg.CurrentContext = currentContext
	data, err := clientcmd.Write(*cfg)
	require.NoError(t, err)
	return data
}

// --- parseAndValidateKubeconfig tests ---

func TestParseAndValidateKubeconfig(t *testing.T) {
	t.Run("valid single cluster kubeconfig", func(t *testing.T) {
		data := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		kd, err := parseAndValidateKubeconfig(data)
		require.NoError(t, err)
		require.Len(t, kd.Clusters, 1)
		assert.Equal(t, "api.example.com", kd.Clusters[0].Hostname)
		assert.Equal(t, "6443", kd.Clusters[0].Port)
		require.Len(t, kd.Contexts, 1)
		assert.True(t, kd.Contexts[0].Current)
	})

	t.Run("valid multi-cluster kubeconfig", func(t *testing.T) {
		data := buildTestKubeconfig(t,
			map[string]string{
				"prod":    "https://api.prod.example.com:6443",
				"staging": "https://api.staging.example.com",
			},
			map[string]string{"admin": "my-token"},
			map[string][2]string{
				"prod-ctx":    {"prod", "admin"},
				"staging-ctx": {"staging", "admin"},
			},
			"prod-ctx",
		)

		kd, err := parseAndValidateKubeconfig(data)
		require.NoError(t, err)
		require.Len(t, kd.Clusters, 2)
	})

	t.Run("reject client certificate auth", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{ClientCertificateData: []byte("cert")}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "client certificate auth")
	})

	t.Run("reject exec-based auth", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{Command: "/bin/get-token"}}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exec-based auth")
	})

	t.Run("reject auth provider", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "gcp"}}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth provider")
	})

	t.Run("reject user with no token", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no token configured")
	})

	t.Run("reject conflicting tokens for same server", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com:6443"}
		cfg.AuthInfos["u1"] = &clientcmdapi.AuthInfo{Token: "token1"}
		cfg.AuthInfos["u2"] = &clientcmdapi.AuthInfo{Token: "token2"}
		cfg.Contexts["ctx1"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u1"}
		cfg.Contexts["ctx2"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u2"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflicting tokens")
	})

	t.Run("default port 443 for https without explicit port", func(t *testing.T) {
		data := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"ctx": {"prod", "admin"}},
			"ctx",
		)

		kd, err := parseAndValidateKubeconfig(data)
		require.NoError(t, err)
		assert.Equal(t, "443", kd.Clusters[0].Port)
	})

	t.Run("reject client certificate file path auth", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{ClientCertificate: "/path/to/cert.pem"}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "client certificate auth")
	})

	t.Run("reject cluster with empty server URL", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: ""}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{Token: "t"}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no server URL")
	})

	t.Run("should preserve certificate-authority-data", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		caData := []byte("fake-ca-data")
		cfg.Clusters["c"] = &clientcmdapi.Cluster{
			Server:                   "https://api.example.com:6443",
			CertificateAuthorityData: caData,
		}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{Token: "my-token"}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		kd, err := parseAndValidateKubeconfig(data)
		require.NoError(t, err)
		require.Len(t, kd.Clusters, 1)
		assert.Equal(t, caData, kd.Clusters[0].CAData)
	})

	t.Run("should reject tokenFile-based auth", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com:6443"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{TokenFile: "/var/run/secrets/token"}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token-file auth")
	})

	t.Run("should reject basic auth (username/password)", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: "https://api.example.com:6443"}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{Username: "admin", Password: "secret"}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "basic auth")
	})

	t.Run("should reject cluster with certificate-authority file path", func(t *testing.T) {
		cfg := clientcmdapi.NewConfig()
		cfg.Clusters["c"] = &clientcmdapi.Cluster{
			Server:               "https://api.example.com:6443",
			CertificateAuthority: "/etc/kubernetes/pki/ca.crt",
		}
		cfg.AuthInfos["u"] = &clientcmdapi.AuthInfo{Token: "my-token"}
		cfg.Contexts["ctx"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
		data, err := clientcmd.Write(*cfg)
		require.NoError(t, err)

		_, err = parseAndValidateKubeconfig(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "certificate-authority file path")
	})
}

// --- sanitizeKubeconfig tests ---

func TestSanitizeKubeconfig(t *testing.T) {
	t.Run("should replace tokens with placeholder", func(t *testing.T) {
		data := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.example.com:6443"},
			map[string]string{"admin": "real-secret-token"},
			map[string][2]string{"ctx": {"prod", "admin"}},
			"ctx",
		)

		sanitized, err := sanitizeKubeconfig(data)
		require.NoError(t, err)

		cfg, err := clientcmd.Load(sanitized)
		require.NoError(t, err)
		assert.Equal(t, "proxy-managed-token", cfg.AuthInfos["admin"].Token)
		assert.NotContains(t, string(sanitized), "real-secret-token")

		// Verify cluster info preserved
		assert.Equal(t, "https://api.example.com:6443", cfg.Clusters["prod"].Server)
		assert.Equal(t, "ctx", cfg.CurrentContext)
	})

	t.Run("should sanitize all users in multi-user kubeconfig", func(t *testing.T) {
		data := buildTestKubeconfig(t,
			map[string]string{
				"prod":    "https://api.prod.example.com:6443",
				"staging": "https://api.staging.example.com:6443",
			},
			map[string]string{
				"prod-admin":    "prod-secret-token",
				"staging-admin": "staging-secret-token",
			},
			map[string][2]string{
				"prod-ctx":    {"prod", "prod-admin"},
				"staging-ctx": {"staging", "staging-admin"},
			},
			"prod-ctx",
		)

		sanitized, err := sanitizeKubeconfig(data)
		require.NoError(t, err)

		cfg, err := clientcmd.Load(sanitized)
		require.NoError(t, err)
		assert.Equal(t, "proxy-managed-token", cfg.AuthInfos["prod-admin"].Token)
		assert.Equal(t, "proxy-managed-token", cfg.AuthInfos["staging-admin"].Token)
		assert.NotContains(t, string(sanitized), "prod-secret-token")
		assert.NotContains(t, string(sanitized), "staging-secret-token")
	})
}

// --- generateProxyConfig kubernetes route tests ---

func TestGenerateProxyConfigKubernetes(t *testing.T) {
	t.Run("should generate routes per cluster from kubeconfig", func(t *testing.T) {
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Name: "prod", Hostname: "api.prod.example.com", Port: "6443"},
						{Name: "staging", Hostname: "api.staging.example.com", Port: "443"},
					},
				},
			},
		}

		data, err := generateProxyConfig(creds)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		prodRoute := findRouteByDomain(t, cfg.Routes, "api.prod.example.com:6443")
		assert.Equal(t, "kubernetes", prodRoute.Injector)
		assert.Equal(t, "/etc/proxy/credentials/k8s/kubeconfig", prodRoute.KubeconfigPath)

		stagingRoute := findRouteByDomain(t, cfg.Routes, "api.staging.example.com:443")
		assert.Equal(t, "kubernetes", stagingRoute.Injector)
	})

	t.Run("should skip kubernetes credential with nil kubeconfig", func(t *testing.T) {
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "k8s",
					Type: clawv1alpha1.CredentialTypeKubernetes,
				},
				KubeConfig: nil,
			},
		}

		data, err := generateProxyConfig(creds)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		// Only builtin passthrough routes
		assert.Len(t, cfg.Routes, len(builtinPassthroughDomains))
	})

	t.Run("should include caCert when cluster has CAData", func(t *testing.T) {
		caData := []byte("-----BEGIN CERTIFICATE-----\nMIIBfake\n-----END CERTIFICATE-----\n")
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Name: "prod", Hostname: "api.prod.example.com", Port: "6443", CAData: caData},
						{Name: "staging", Hostname: "api.staging.example.com", Port: "443"},
					},
				},
			},
		}

		data, err := generateProxyConfig(creds)
		require.NoError(t, err)

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal(data, &cfg))

		prodRoute := findRouteByDomain(t, cfg.Routes, "api.prod.example.com:6443")
		assert.NotEmpty(t, prodRoute.CACert, "route with CAData should have caCert set")

		decoded, err := base64.StdEncoding.DecodeString(prodRoute.CACert)
		require.NoError(t, err)
		assert.Equal(t, caData, decoded)

		stagingRoute := findRouteByDomain(t, cfg.Routes, "api.staging.example.com:443")
		assert.Empty(t, stagingRoute.CACert, "route without CAData should have empty caCert")
	})
}

// --- configureProxyForCredentials kubernetes volume mount tests ---

func TestConfigureProxyForKubernetesCredentials(t *testing.T) {
	buildObjects := func(t *testing.T) []*unstructured.Unstructured {
		t.Helper()
		reconciler := createClawReconciler()
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		objects, err := reconciler.buildKustomizedObjects(instance)
		require.NoError(t, err)
		return objects
	}

	t.Run("should add kubernetes kubeconfig volume mount", func(t *testing.T) {
		objects := buildObjects(t)
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
			},
		}

		require.NoError(t, configureProxyForCredentials(objects, creds))

		for _, obj := range objects {
			if obj.GetKind() != DeploymentKind || obj.GetName() != getProxyDeploymentName(testInstanceName) {
				continue
			}
			volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")
			var foundVol bool
			for _, v := range volumes {
				vol := v.(map[string]any)
				if vol["name"] == testKubeCredVolume {
					foundVol = true
					secret := vol["secret"].(map[string]any)
					assert.Equal(t, testKubeSecretName, secret["secretName"])
					items := secret["items"].([]any)
					item := items[0].(map[string]any)
					assert.Equal(t, "config", item["key"])
					assert.Equal(t, "kubeconfig", item["path"])
				}
			}
			assert.True(t, foundVol, "kubernetes credential volume should be present")
		}
	})
}

// --- configureClawDeploymentForKubernetes tests ---

func TestConfigureClawDeploymentForKubernetes(t *testing.T) {
	makeDeployment := func() []*unstructured.Unstructured {
		dep := &unstructured.Unstructured{}
		dep.SetKind(DeploymentKind)
		dep.SetName(getClawDeploymentName(testInstanceName))
		dep.Object["spec"] = map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":         ClawGatewayContainerName,
							"env":          []any{},
							"volumeMounts": []any{},
						},
					},
					"volumes": []any{},
				},
			},
		}
		return []*unstructured.Unstructured{dep}
	}

	t.Run("should add KUBECONFIG env, PATH, volumes, and init container", func(t *testing.T) {
		objects := makeDeployment()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:      "k8s",
					Type:      clawv1alpha1.CredentialTypeKubernetes,
					SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
				},
				KubeConfig: &kubeconfigData{},
			},
		}

		require.NoError(t, configureClawDeploymentForKubernetes(objects, creds, DefaultKubectlImage))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)

		envVars := container["env"].([]any)
		envMap := map[string]string{}
		for _, e := range envVars {
			env := e.(map[string]any)
			envMap[env["name"].(string)] = env["value"].(string)
		}
		assert.Equal(t, "/etc/kube/config", envMap["KUBECONFIG"])
		assert.Contains(t, envMap["PATH"], "/opt/kube-tools")

		volumeMounts := container["volumeMounts"].([]any)
		require.Len(t, volumeMounts, 2)
		vmNames := map[string]string{}
		for _, vm := range volumeMounts {
			m := vm.(map[string]any)
			vmNames[m["name"].(string)] = m["mountPath"].(string)
		}
		assert.Equal(t, "/etc/kube", vmNames["kube-config"])
		assert.Equal(t, "/opt/kube-tools", vmNames["kubectl-bin"])

		volumes, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "volumes")
		require.Len(t, volumes, 2)
		volNames := map[string]bool{}
		for _, v := range volumes {
			vol := v.(map[string]any)
			volNames[vol["name"].(string)] = true
		}
		assert.True(t, volNames["kube-config"])
		assert.True(t, volNames["kubectl-bin"])

		initContainers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "initContainers")
		require.Len(t, initContainers, 1)
		initC := initContainers[0].(map[string]any)
		assert.Equal(t, "init-kubectl", initC["name"])
		assert.Equal(t, DefaultKubectlImage, initC["image"])
	})

	t.Run("should be no-op when no kubernetes credentials exist", func(t *testing.T) {
		objects := makeDeployment()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name:   "gemini",
					Type:   clawv1alpha1.CredentialTypeAPIKey,
					Domain: "generativelanguage.googleapis.com",
				},
			},
		}

		require.NoError(t, configureClawDeploymentForKubernetes(objects, creds, DefaultKubectlImage))

		containers, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		envVars := container["env"].([]any)
		assert.Empty(t, envVars)
	})
}

// --- injectKubePortsIntoNetworkPolicy tests ---

func TestInjectKubePortsIntoNetworkPolicy(t *testing.T) {
	makeNP := func() []*unstructured.Unstructured {
		np := &unstructured.Unstructured{}
		np.SetKind(NetworkPolicyKind)
		np.SetName(ClawProxyEgressNetworkPolicyName)
		np.Object["spec"] = map[string]any{
			"egress": []any{
				map[string]any{
					"ports": []any{
						map[string]any{"port": int64(443), "protocol": "TCP"},
					},
				},
			},
		}
		return []*unstructured.Unstructured{np}
	}

	t.Run("should add non-443 port", func(t *testing.T) {
		objects := makeNP()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "k8s",
					Type: clawv1alpha1.CredentialTypeKubernetes,
				},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Hostname: "api.example.com", Port: "6443"},
						{Hostname: "api.other.com", Port: "443"},
					},
				},
			},
		}

		require.NoError(t, injectKubePortsIntoNetworkPolicy(objects, creds))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 2, "should have original 443 + new 6443")

		var found6443 bool
		for _, p := range ports {
			port := p.(map[string]any)
			if port["port"] == int64(6443) {
				found6443 = true
			}
		}
		assert.True(t, found6443, "port 6443 should be added")
	})

	t.Run("should be no-op when all ports are 443", func(t *testing.T) {
		objects := makeNP()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "k8s",
					Type: clawv1alpha1.CredentialTypeKubernetes,
				},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Hostname: "api.example.com", Port: "443"},
					},
				},
			},
		}

		require.NoError(t, injectKubePortsIntoNetworkPolicy(objects, creds))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 1, "should not add duplicate 443")
	})

	t.Run("should be no-op with no kubernetes credentials", func(t *testing.T) {
		objects := makeNP()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "gemini",
					Type: clawv1alpha1.CredentialTypeAPIKey,
				},
			},
		}

		require.NoError(t, injectKubePortsIntoNetworkPolicy(objects, creds))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 1)
	})

	t.Run("should deduplicate same port across multiple kubeconfigs", func(t *testing.T) {
		objects := makeNP()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{Name: "k8s-a", Type: clawv1alpha1.CredentialTypeKubernetes},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Hostname: "api.cluster-a.com", Port: "6443"},
					},
				},
			},
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{Name: "k8s-b", Type: clawv1alpha1.CredentialTypeKubernetes},
				KubeConfig: &kubeconfigData{
					Clusters: []kubeconfigCluster{
						{Hostname: "api.cluster-b.com", Port: "6443"},
						{Hostname: "api.cluster-c.com", Port: "8443"},
					},
				},
			},
		}

		require.NoError(t, injectKubePortsIntoNetworkPolicy(objects, creds))

		egress, _, _ := unstructured.NestedSlice(objects[0].Object, "spec", "egress")
		rule := egress[0].(map[string]any)
		ports := rule["ports"].([]any)
		assert.Len(t, ports, 3, "should have 443 + 6443 + 8443 (no duplicate 6443)")
	})
}

// --- injectKubernetesSkill tests ---

func TestInjectKubernetesSkill(t *testing.T) {
	makeCM := func() []*unstructured.Unstructured {
		cm := &unstructured.Unstructured{}
		cm.SetKind(ConfigMapKind)
		cm.SetName(getConfigMapName(testInstanceName))
		cm.Object["data"] = map[string]any{}
		return []*unstructured.Unstructured{cm}
	}

	t.Run("should write KUBERNETES.md key with skill frontmatter into ConfigMap", func(t *testing.T) {
		objects := makeCM()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "k8s",
					Type: clawv1alpha1.CredentialTypeKubernetes,
				},
				KubeConfig: &kubeconfigData{
					Contexts: []kubeconfigContext{
						{Name: "prod-ctx", Cluster: "prod", Namespace: "default", Current: true},
						{Name: "staging-ctx", Cluster: "staging", Namespace: "staging"},
					},
				},
			},
		}

		require.NoError(t, injectKubernetesSkill(objects, creds))

		kubeMd, found, _ := unstructured.NestedString(objects[0].Object, "data", "KUBERNETES.md")
		assert.True(t, found, "KUBERNETES.md should exist in ConfigMap data")
		assert.True(t, strings.HasPrefix(kubeMd, "---\n"), "should start with YAML frontmatter")
		assert.Contains(t, kubeMd, "name: kubernetes")
		assert.Contains(t, kubeMd, "description:")
		assert.Contains(t, kubeMd, "# Kubernetes Access")
		assert.Contains(t, kubeMd, "`prod-ctx`")
		assert.Contains(t, kubeMd, "[current]")
		assert.Contains(t, kubeMd, "namespace: staging")
		assert.Contains(t, kubeMd, "`oc`")
		assert.Contains(t, kubeMd, "`kubectl`")
	})

	t.Run("should be no-op with no kubernetes credentials", func(t *testing.T) {
		objects := makeCM()
		creds := []resolvedCredential{
			{
				CredentialSpec: clawv1alpha1.CredentialSpec{
					Name: "gemini",
					Type: clawv1alpha1.CredentialTypeAPIKey,
				},
			},
		}

		require.NoError(t, injectKubernetesSkill(objects, creds))

		_, found, _ := unstructured.NestedString(objects[0].Object, "data", "KUBERNETES.md")
		assert.False(t, found, "KUBERNETES.md should not exist when no kubernetes credentials")
	})
}

// --- resolveProviderDefaults kubernetes tests ---

func TestResolveProviderDefaultsKubernetes(t *testing.T) {
	t.Run("should return nil for kubernetes type (no domain required)", func(t *testing.T) {
		cred := clawv1alpha1.CredentialSpec{
			Name: "k8s",
			Type: clawv1alpha1.CredentialTypeKubernetes,
		}
		err := resolveProviderDefaults(&cred)
		require.NoError(t, err)
		assert.Empty(t, cred.Domain)
	})
}

// --- Integration test: full reconciliation with kubernetes credential ---

func TestKubernetesCredentialReconciliation(t *testing.T) {
	ctx := context.Background()

	t.Run("should create sanitized kubeconfig ConfigMap after reconciliation", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: testKubeSecretName, Namespace: namespace})
			_ = deleteAndWait(&corev1.ConfigMap{}, client.ObjectKey{Name: getKubeConfigMapName(testInstanceName), Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		kubeconfig := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.prod.example.com:6443"},
			map[string]string{"admin": "real-secret-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		secret := &corev1.Secret{}
		secret.Name = testKubeSecretName
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": kubeconfig}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify sanitized kubeconfig ConfigMap was created
		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getKubeConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "sanitized kubeconfig ConfigMap should be created")

		sanitizedConfig := cm.Data["config"]
		assert.NotEmpty(t, sanitizedConfig)
		assert.NotContains(t, sanitizedConfig, "real-secret-token", "real token should be stripped")
		assert.Contains(t, sanitizedConfig, "proxy-managed-token", "sanitized placeholder should be present")
		assert.Contains(t, sanitizedConfig, "api.prod.example.com", "cluster info should be preserved")
	})

	t.Run("should inject KUBERNETES.md with context info", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: testKubeSecretName, Namespace: namespace})
			_ = deleteAndWait(&corev1.ConfigMap{}, client.ObjectKey{Name: getKubeConfigMapName(testInstanceName), Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		kubeconfig := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.prod.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		secret := &corev1.Secret{}
		secret.Name = testKubeSecretName
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": kubeconfig}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "ConfigMap should be created")

		kubeMd, ok := cm.Data["KUBERNETES.md"]
		assert.True(t, ok, "KUBERNETES.md should exist in ConfigMap")
		assert.Contains(t, kubeMd, "Kubernetes Access")
		assert.Contains(t, kubeMd, "prod-ctx")
	})

	t.Run("should configure proxy deployment with kubernetes volume mount", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: testKubeSecretName, Namespace: namespace})
			_ = deleteAndWait(&corev1.ConfigMap{}, client.ObjectKey{Name: getKubeConfigMapName(testInstanceName), Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		kubeconfig := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.prod.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		secret := &corev1.Secret{}
		secret.Name = testKubeSecretName
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": kubeconfig}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "proxy deployment should be created")

		var foundVol bool
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == testKubeCredVolume && vol.Secret != nil {
				foundVol = true
				assert.Equal(t, testKubeSecretName, vol.Secret.SecretName)
				require.Len(t, vol.Secret.Items, 1)
				assert.Equal(t, "config", vol.Secret.Items[0].Key)
				assert.Equal(t, "kubeconfig", vol.Secret.Items[0].Path)
			}
		}
		assert.True(t, foundVol, "proxy deployment should have kubeconfig Secret volume")

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == ClawProxyContainerName {
				var foundMount bool
				for _, mount := range container.VolumeMounts {
					if mount.Name == testKubeCredVolume {
						foundMount = true
						assert.Equal(t, "/etc/proxy/credentials/k8s", mount.MountPath)
						assert.True(t, mount.ReadOnly)
					}
				}
				assert.True(t, foundMount, "proxy container should have kubeconfig volume mount")
			}
		}
	})

	t.Run("should configure gateway deployment with KUBECONFIG env and volume", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: testKubeSecretName, Namespace: namespace})
			_ = deleteAndWait(&corev1.ConfigMap{}, client.ObjectKey{Name: getKubeConfigMapName(testInstanceName), Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		kubeconfig := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.prod.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		secret := &corev1.Secret{}
		secret.Name = testKubeSecretName
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": kubeconfig}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		deployment := &appsv1.Deployment{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getClawDeploymentName(testInstanceName),
				Namespace: namespace,
			}, deployment) == nil
		}, "claw deployment should be created")

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == ClawGatewayContainerName {
				envMap := map[string]string{}
				for _, env := range container.Env {
					envMap[env.Name] = env.Value
				}
				assert.Equal(t, "/etc/kube/config", envMap["KUBECONFIG"], "KUBECONFIG env var")
				assert.Contains(t, envMap["PATH"], "/opt/kube-tools", "PATH should include kubectl dir")

				mountMap := map[string]string{}
				for _, mount := range container.VolumeMounts {
					mountMap[mount.Name] = mount.MountPath
				}
				assert.Equal(t, "/etc/kube", mountMap["kube-config"], "kube-config volume mount")
				assert.Equal(t, "/opt/kube-tools", mountMap["kubectl-bin"], "kubectl-bin volume mount")
			}
		}

		volNames := map[string]bool{}
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			volNames[vol.Name] = true
			if vol.Name == "kube-config" && vol.ConfigMap != nil {
				assert.Equal(t, ClawKubeConfigMapName, vol.ConfigMap.Name)
			}
		}
		assert.True(t, volNames["kube-config"], "should have kube-config volume")
		assert.True(t, volNames["kubectl-bin"], "should have kubectl-bin emptyDir volume")

		var foundInitKubectl bool
		for _, ic := range deployment.Spec.Template.Spec.InitContainers {
			if ic.Name == "init-kubectl" {
				foundInitKubectl = true
				assert.Equal(t, DefaultKubectlImage, ic.Image)
			}
		}
		assert.True(t, foundInitKubectl, "should have init-kubectl init container")
	})

	t.Run("should include kubernetes routes in proxy config ConfigMap", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: testKubeSecretName, Namespace: namespace})
			_ = deleteAndWait(&corev1.ConfigMap{}, client.ObjectKey{Name: getKubeConfigMapName(testInstanceName), Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		kubeconfig := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.prod.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		secret := &corev1.Secret{}
		secret.Name = testKubeSecretName
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": kubeconfig}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getProxyConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "proxy config ConfigMap should be created")

		var cfg proxyConfig
		require.NoError(t, json.Unmarshal([]byte(cm.Data["proxy-config.json"]), &cfg))

		route := findRouteByDomain(t, cfg.Routes, "api.prod.example.com:6443")
		assert.Equal(t, "kubernetes", route.Injector)
		assert.Equal(t, "/etc/proxy/credentials/k8s/kubeconfig", route.KubeconfigPath)
	})

	t.Run("should clean up kubeconfig ConfigMap when kubernetes credential is removed", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: testKubeSecretName, Namespace: namespace})
			_ = deleteAndWait(&corev1.ConfigMap{}, client.ObjectKey{Name: getKubeConfigMapName(testInstanceName), Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		kubeconfig := buildTestKubeconfig(t,
			map[string]string{"prod": "https://api.prod.example.com:6443"},
			map[string]string{"admin": "my-token"},
			map[string][2]string{"prod-ctx": {"prod", "admin"}},
			"prod-ctx",
		)

		secret := &corev1.Secret{}
		secret.Name = testKubeSecretName
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": kubeconfig}
		require.NoError(t, k8sClient.Create(ctx, secret))

		// Create Claw with kubernetes credential
		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: testKubeSecretName, Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify kubeconfig ConfigMap exists
		cm := &corev1.ConfigMap{}
		waitFor(t, timeout, interval, func() bool {
			return k8sClient.Get(ctx, client.ObjectKey{
				Name:      getKubeConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm) == nil
		}, "kubeconfig ConfigMap should be created")

		// Remove kubernetes credential from Claw spec
		require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: testInstanceName, Namespace: namespace}, instance))
		instance.Spec.Credentials = nil
		require.NoError(t, k8sClient.Update(ctx, instance))

		reconcileClaw(t, ctx, reconciler, testInstanceName, namespace)

		// Verify kubeconfig ConfigMap was deleted
		waitFor(t, timeout, interval, func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      getKubeConfigMapName(testInstanceName),
				Namespace: namespace,
			}, cm)
			return apierrors.IsNotFound(err)
		}, "kubeconfig ConfigMap should be deleted after credential removal")
	})

	t.Run("should fail reconciliation with invalid kubeconfig in Secret", func(t *testing.T) {
		t.Cleanup(func() {
			_ = deleteAndWait(&corev1.Secret{}, client.ObjectKey{Name: "bad-kube-secret", Namespace: namespace})
			deleteAndWaitAllResources(t, namespace)
		})

		secret := &corev1.Secret{}
		secret.Name = "bad-kube-secret"
		secret.Namespace = namespace
		secret.Data = map[string][]byte{"config": []byte("not-valid-kubeconfig")}
		require.NoError(t, k8sClient.Create(ctx, secret))

		instance := &clawv1alpha1.Claw{}
		instance.Name = testInstanceName
		instance.Namespace = namespace
		instance.Spec.Credentials = []clawv1alpha1.CredentialSpec{
			{
				Name:      "k8s",
				Type:      clawv1alpha1.CredentialTypeKubernetes,
				SecretRef: &clawv1alpha1.SecretRef{Name: "bad-kube-secret", Key: "config"},
			},
		}
		require.NoError(t, k8sClient.Create(ctx, instance))

		reconciler := createClawReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKey{Name: testInstanceName, Namespace: namespace},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential validation failed")
	})
}
