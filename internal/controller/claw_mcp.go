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
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// validateMcpServerSecrets validates that all envFrom-referenced Secrets exist and
// contain the specified keys. Returns a joined error describing all failures.
func (r *ClawResourceReconciler) validateMcpServerSecrets(ctx context.Context, instance *clawv1alpha1.Claw) error {
	var errs []error
	for serverName, spec := range instance.Spec.McpServers {
		for _, ef := range spec.EnvFrom {
			secret := &corev1.Secret{}
			if err := r.Get(ctx, client.ObjectKey{
				Namespace: instance.Namespace,
				Name:      ef.SecretRef.Name,
			}, secret); err != nil {
				if apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("MCP server %q envFrom %q: Secret %q not found",
						serverName, ef.Name, ef.SecretRef.Name))
				} else {
					errs = append(errs, fmt.Errorf("MCP server %q envFrom %q: failed to get Secret %q: %w",
						serverName, ef.Name, ef.SecretRef.Name, err))
				}
				continue
			}
			if _, ok := secret.Data[ef.SecretRef.Key]; !ok {
				errs = append(errs, fmt.Errorf("MCP server %q envFrom %q: key %q not found in Secret %q",
					serverName, ef.Name, ef.SecretRef.Key, ef.SecretRef.Name))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("MCP server secret validation failed: %w", errors.Join(errs...))
	}
	return nil
}

// injectMcpServers injects MCP server configuration for all entries in
// spec.mcpServers. Always-win: operator overwrites mcp.servers unconditionally.
func injectMcpServers(config map[string]any, instance *clawv1alpha1.Claw) {
	if len(instance.Spec.McpServers) == 0 {
		return
	}

	servers := make(map[string]any, len(instance.Spec.McpServers))
	for name, spec := range instance.Spec.McpServers {
		servers[name] = buildMcpServerConfig(spec)
	}

	config["mcp"] = map[string]any{"servers": servers}
}

// buildMcpServerConfig builds the JSON-ready config for a single MCP server entry.
// For envFrom entries, the env var name is included in the env map with the env var
// name as a placeholder value — the real value comes from the container environment.
func buildMcpServerConfig(spec clawv1alpha1.McpServerSpec) map[string]any {
	entry := map[string]any{}

	if spec.Command != "" {
		entry["command"] = spec.Command
		if len(spec.Args) > 0 {
			entry["args"] = spec.Args
		}

		envMap := make(map[string]string, len(spec.Env)+len(spec.EnvFrom))
		for k, v := range spec.Env {
			envMap[k] = v
		}
		for _, ef := range spec.EnvFrom {
			envMap[ef.Name] = ef.Name
		}
		if len(envMap) > 0 {
			entry["env"] = envMap
		}
	} else {
		entry["url"] = spec.URL
		if spec.Transport != "" {
			entry["transport"] = string(spec.Transport)
		}
	}

	return entry
}
