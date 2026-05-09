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
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// injectMcpServersIntoConfigMap injects MCP server configuration into operator.json
// for all entries in spec.mcpServers. Stdio servers get command/args/env; HTTP servers
// get url/transport.
func injectMcpServersIntoConfigMap(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if len(instance.Spec.McpServers) == 0 {
		return nil
	}

	servers := make(map[string]any, len(instance.Spec.McpServers))
	for name, spec := range instance.Spec.McpServers {
		servers[name] = buildMcpServerConfig(spec)
	}

	configMapName := getConfigMapName(instance.Name)
	for _, obj := range objects {
		if obj.GetKind() != ConfigMapKind || obj.GetName() != configMapName {
			continue
		}

		operatorJSON, found, err := unstructured.NestedString(obj.Object, "data", "operator.json")
		if err != nil {
			return fmt.Errorf("failed to extract operator.json from ConfigMap: %w", err)
		}
		if !found {
			return fmt.Errorf("operator.json not found in ConfigMap data")
		}

		var config map[string]any
		if err := json.Unmarshal([]byte(operatorJSON), &config); err != nil {
			return fmt.Errorf("failed to parse operator.json: %w", err)
		}

		config["mcp"] = map[string]any{"servers": servers}

		updatedJSON, err := json.MarshalIndent(config, "    ", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal operator.json: %w", err)
		}

		if err := unstructured.SetNestedField(obj.Object, string(updatedJSON), "data", "operator.json"); err != nil {
			return fmt.Errorf("failed to set updated operator.json in ConfigMap: %w", err)
		}
		return nil
	}

	return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
}

// buildMcpServerConfig builds the JSON-ready config for a single MCP server entry.
func buildMcpServerConfig(spec clawv1alpha1.McpServerSpec) map[string]any {
	entry := map[string]any{}

	if spec.Command != "" {
		entry["command"] = spec.Command
		if len(spec.Args) > 0 {
			entry["args"] = spec.Args
		}
		if len(spec.Env) > 0 {
			entry["env"] = spec.Env
		}
	} else {
		entry["url"] = spec.URL
		if spec.Transport != "" {
			entry["transport"] = spec.Transport
		}
	}

	return entry
}
