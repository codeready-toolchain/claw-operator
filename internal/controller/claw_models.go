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

// providerModelCatalog returns the model catalog for a logical provider name.
// Returns nil if the provider has no catalog entries in knownProviders.
func providerModelCatalog(provider string) []modelEntry {
	if defaults, ok := knownProviders[provider]; ok {
		return defaults.Models
	}
	return nil
}
