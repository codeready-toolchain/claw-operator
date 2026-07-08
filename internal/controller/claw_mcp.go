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
			if err := r.UserSecretReader.Get(ctx, client.ObjectKey{
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
//
// Also records which server names need full-entry Bucket-A reassertion under
// seedOnly mode (see docs/adr/0021-seed-only-config-mode.md) via a private
// "_seedOnlyMeta" key: servers using envFrom/credentialRef, or reached over a
// URL (proxy-routed, domain-allowlist-gated), can never be safely
// hand-authored by a user — merge.js can't tell this from the entry's JSON
// shape alone (a command+env entry looks the same whether or not env came
// from envFrom), so it's flagged here instead. merge.js strips this key
// before writing the final openclaw.json to the PVC; it must never reach the
// user-facing file. Command-based servers with only inline env are Bucket B
// — safe to hand-add/edit directly in the file, in any mode.
func injectMcpServers(config map[string]any, instance *clawv1alpha1.Claw) {
	if len(instance.Spec.McpServers) == 0 {
		return
	}

	servers := make(map[string]any, len(instance.Spec.McpServers))
	var bucketAServers []any
	for name, spec := range instance.Spec.McpServers {
		servers[name] = buildMcpServerConfig(spec)
		if mcpServerIsBucketA(spec) {
			bucketAServers = append(bucketAServers, name)
		}
	}

	config["mcp"] = map[string]any{"servers": servers}
	config["_seedOnlyMeta"] = map[string]any{"mcpBucketAServers": bucketAServers}
}

// mcpServerIsBucketA reports whether an MCP server entry needs full-entry
// reassertion under seedOnly mode: any server reached via URL (proxy-routed)
// or using envFrom/credentialRef (Secret- or proxy-backed) can never be a
// genuine hand-edit candidate, in any mode.
func mcpServerIsBucketA(spec clawv1alpha1.McpServerSpec) bool {
	return spec.Command == "" || len(spec.EnvFrom) > 0 || spec.CredentialRef != ""
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
