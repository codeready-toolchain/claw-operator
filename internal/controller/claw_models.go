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

import "slices"

// providerModelCatalog returns the model catalog for a logical provider name.
// Returns nil if the provider has no catalog entries in knownProviders.
func providerModelCatalog(provider string) []modelEntry {
	if defaults, ok := knownProviders[provider]; ok {
		return defaults.Models
	}
	return nil
}

// anthropicVertexPreferredPrimary matches ANTHROPIC_VERTEX_DEFAULT_MODEL_ID in
// @openclaw/anthropic-vertex-provider. Claude Sonnet 5 remains in the shared
// catalog (and Vertex fallbacks); Vertex only reorders primary to this model.
const anthropicVertexPreferredPrimary = "claude-sonnet-4-6"

// preferVertexCatalogPrimary reorders a provider catalog for Vertex so the
// first entry (used as agents.defaults.model.primary) matches the upstream
// Vertex plugin default when it differs from the API-key catalog order.
func preferVertexCatalogPrimary(logicalProvider string, catalog []modelEntry) []modelEntry {
	if logicalProvider != "anthropic" || len(catalog) == 0 {
		return catalog
	}
	idx := slices.IndexFunc(catalog, func(m modelEntry) bool {
		return m.Name == anthropicVertexPreferredPrimary
	})
	if idx <= 0 {
		return catalog
	}
	out := make([]modelEntry, 0, len(catalog))
	out = append(out, catalog[idx])
	out = append(out, catalog[:idx]...)
	out = append(out, catalog[idx+1:]...)
	return out
}
