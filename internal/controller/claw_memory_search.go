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
	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

// userHasMemorySearchConfig returns true when the merged config already
// contains agents.defaults.memorySearch. Since the operator's base template
// (operator.json) has no memorySearch key, its presence means the user set
// it via spec.config.raw — the operator should not override it.
func userHasMemorySearchConfig(config map[string]any) bool {
	agents, ok := config["agents"].(map[string]any)
	if !ok {
		return false
	}
	defaults, ok := agents["defaults"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = defaults["memorySearch"]
	return ok
}

// injectMemorySearch auto-configures agents.defaults.memorySearch based on
// the first embedding-capable credential. GCP credentials (Vertex AI) are
// skipped because the gemini adapter expects API key auth, not OAuth2 tokens.
// If no eligible provider is found, memory search is explicitly disabled to
// suppress noisy runtime errors. User-provided memorySearch config in
// spec.config.raw takes full precedence — the operator never overrides it.
func injectMemorySearch(config map[string]any, instance *clawv1alpha1.Claw) {
	if userHasMemorySearchConfig(config) {
		return
	}

	for _, cred := range instance.Spec.Credentials {
		if cred.Type == clawv1alpha1.CredentialTypeGCP {
			continue
		}
		if defaults, ok := knownProviders[cred.Provider]; ok && defaults.EmbeddingAdapter != "" {
			setNestedValue(config, defaults.EmbeddingAdapter,
				"agents", "defaults", "memorySearch", "provider")
			return
		}
	}

	setNestedValue(config, false, "agents", "defaults", "memorySearch", "enabled")
}
