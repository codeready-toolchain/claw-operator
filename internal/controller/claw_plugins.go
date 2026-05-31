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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

const (
	PluginsInitContainerName = "init-plugins"
)

func pluginsEnabled(instance *clawv1alpha1.Claw) bool {
	return len(instance.Spec.Plugins) > 0
}

// effectivePlugins returns the complete list of plugins to install: explicit
// spec.plugins plus any implicitly required by the configured credentials
// (e.g., Vertex AI SDK providers that need an external plugin).
// Duplicates are removed (spec declarations take precedence).
func effectivePlugins(instance *clawv1alpha1.Claw) []string {
	implicit := requiredProviderPlugins(instance)
	if len(implicit) == 0 {
		return instance.Spec.Plugins
	}
	seen := make(map[string]bool, len(instance.Spec.Plugins))
	for _, p := range instance.Spec.Plugins {
		seen[p] = true
	}
	merged := append([]string{}, instance.Spec.Plugins...)
	for _, p := range implicit {
		if !seen[p] {
			merged = append(merged, p)
			seen[p] = true
		}
	}
	return merged
}

// requiredProviderPlugins inspects credentials and returns plugin package specs
// that must be installed for the configured providers to work.
func requiredProviderPlugins(instance *clawv1alpha1.Claw) []string {
	var plugins []string
	seen := make(map[string]bool)
	for _, cred := range instance.Spec.Credentials {
		if !usesVertexSDK(cred) {
			continue
		}
		defaults, ok := knownProviders[cred.Provider]
		if !ok || defaults.VertexPlugin == "" {
			continue
		}
		if !seen[defaults.VertexPlugin] {
			plugins = append(plugins, defaults.VertexPlugin)
			seen[defaults.VertexPlugin] = true
		}
	}
	return plugins
}

func generatePluginInstallScript(plugins []string) string {
	var b strings.Builder
	b.WriteString(`set -e
EXT="/home/node/.openclaw/extensions"
MANIFEST="$EXT/.operator-managed"
if [ -f "$MANIFEST" ]; then
  while IFS= read -r dir; do
    [ -n "$dir" ] && rm -rf "$EXT/$dir"
  done < "$MANIFEST"
  rm -f "$MANIFEST"
fi
mkdir -p "$EXT"
ls "$EXT" 2>/dev/null | sort > /tmp/before-plugins.txt
`)
	for _, pkg := range plugins {
		escaped := "'" + strings.ReplaceAll(pkg, "'", "'\\''") + "'"
		fmt.Fprintf(&b, "openclaw plugins install clawhub:%s\n", escaped)
	}
	b.WriteString(`ls "$EXT" | sort | comm -13 /tmp/before-plugins.txt - > "$MANIFEST"
`)
	return b.String()
}

// configurePluginsInitContainer adds an init-plugins init container to the
// gateway Deployment when plugins need to be installed. The container runs the
// openclaw CLI to install each declared plugin on the shared PVC. It goes
// through the MITM proxy (appended after wait-for-proxy).
func configurePluginsInitContainer(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	plugins []string,
) error {
	if len(plugins) == 0 {
		return nil
	}

	gatewayName := getClawDeploymentName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind || obj.GetName() != gatewayName {
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return fmt.Errorf("failed to get containers from claw deployment: %w", err)
		}
		if !found {
			return fmt.Errorf("containers field not found in claw deployment")
		}

		var gatewayImage string
		for _, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name == ClawGatewayContainerName {
				gatewayImage, _, _ = unstructured.NestedString(cm, "image")
				break
			}
		}
		if gatewayImage == "" {
			return fmt.Errorf("gateway container image not found in claw deployment")
		}

		initContainers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")

		proxyHost := fmt.Sprintf("http://%s-proxy:8080", instance.Name)
		script := generatePluginInstallScript(plugins)

		initContainers = append(initContainers, map[string]any{
			"name":            PluginsInitContainerName,
			"image":           gatewayImage,
			"imagePullPolicy": "IfNotPresent",
			"command":         []any{"sh", "-c", script},
			"env": []any{
				map[string]any{"name": "HOME", "value": "/home/node"},
				map[string]any{"name": "NPM_CONFIG_CACHE", "value": "/home/node/.cache/npm"},
				map[string]any{"name": "HTTP_PROXY", "value": proxyHost},
				map[string]any{"name": "HTTPS_PROXY", "value": proxyHost},
				map[string]any{"name": "NO_PROXY", "value": "localhost,127.0.0.1,.svc,.svc.cluster.local"},
				map[string]any{"name": "NODE_EXTRA_CA_CERTS", "value": "/etc/proxy-ca/ca.crt"},
			},
			"resources": map[string]any{
				"requests": map[string]any{"memory": "128Mi", "cpu": "100m"},
				"limits":   map[string]any{"memory": "512Mi", "cpu": "500m"},
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false,
				"capabilities":             map[string]any{"drop": []any{"ALL"}},
			},
			"volumeMounts": []any{
				map[string]any{
					"name":      "claw-home",
					"mountPath": "/home/node/.openclaw",
					"subPath":   "home",
				},
				map[string]any{
					"name":      "claw-home",
					"mountPath": "/home/node/.local",
					"subPath":   "home/.local",
				},
				map[string]any{
					"name":      "claw-home",
					"mountPath": "/home/node/.cache",
					"subPath":   "home/.cache",
				},
				map[string]any{
					"name":      "proxy-ca",
					"mountPath": "/etc/proxy-ca",
					"readOnly":  true,
				},
				map[string]any{
					"name":      "tmp-volume",
					"mountPath": "/tmp",
				},
			},
		})

		if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
			return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}
