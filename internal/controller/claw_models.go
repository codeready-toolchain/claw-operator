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

// modelEntry defines a single model with its API name and human-readable alias.
type modelEntry struct {
	Name  string
	Alias string
}

// modelCatalog maps logical provider names to their known models.
// Order matters: the first model becomes the default primary when this provider
// is the first configured credential in the Claw CR. Lead with the best
// cost/performance model, not the most expensive.
// Providers not in this map (e.g., "openrouter") are silently skipped.
var modelCatalog = map[string][]modelEntry{
	"google": {
		{Name: "gemini-3-flash-preview", Alias: "Gemini 3 Flash"},
		{Name: "gemini-3.1-pro-preview", Alias: "Gemini 3.1 Pro"},
		{Name: "gemini-3.1-flash-lite-preview", Alias: "Gemini 3.1 Flash Lite"},
	},
	"anthropic": {
		{Name: "claude-sonnet-4-6", Alias: "Claude Sonnet 4.6"},
		{Name: "claude-opus-4-7", Alias: "Claude Opus 4.7"},
		{Name: "claude-opus-4-6", Alias: "Claude Opus 4.6"},
		{Name: "claude-haiku-4-5", Alias: "Claude Haiku 4.5"},
	},
	"openai": {
		{Name: "gpt-5.4", Alias: "GPT-5.4"},
		{Name: "gpt-5.5", Alias: "GPT-5.5"},
		{Name: "gpt-5.4-mini", Alias: "GPT-5.4 Mini"},
		{Name: "o3", Alias: "o3"},
		{Name: "o4-mini", Alias: "o4 Mini"},
	},
	"xai": {
		{Name: "grok-4.20-beta-latest-reasoning", Alias: "Grok 4.20"},
		{Name: "grok-4.3", Alias: "Grok 4.3"},
	},
}
