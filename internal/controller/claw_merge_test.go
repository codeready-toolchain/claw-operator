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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// operatorJSONWithPrimaryAndFallback is a small operator.json fixture
// declaring a primary model and a single fallback, shared by several tests
// covering agents.defaults.model.primary/.fallbacks fill-if-empty semantics.
const operatorJSONWithPrimaryAndFallback = `{
	"gateway": {"port": 18789},
	"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["google/gemini-3-flash-preview"]}}}
}`

type configMapYAML struct {
	Data map[string]string `yaml:"data"`
}

func extractConfigMapData(t *testing.T) map[string]string {
	t.Helper()
	raw := readEmbeddedFile("manifests/claw/configmap.yaml")
	require.NotEmpty(t, raw, "embedded configmap.yaml must not be empty")

	var cm configMapYAML
	require.NoError(t, yaml.Unmarshal(raw, &cm))
	require.NotEmpty(t, cm.Data, "configmap data must not be empty")
	return cm.Data
}

type mergeTestSetup struct {
	operatorJSON string            // override operator.json (empty = use embedded default)
	seedJSON     string            // override openclaw.json seed (empty = use embedded default)
	pvcJSON      string            // existing PVC openclaw.json (empty = no existing file)
	configMode   string            // CLAW_CONFIG_MODE env (empty = unset, defaults to "merge" in script)
	withK8sSkill string            // KUBERNETES.md content (empty = not present)
	pvcFiles     map[string]string // pre-existing files on PVC (relative path -> content)
	extraConfigs map[string]string // extra files in config dir (e.g., _ws_*, _skill_* keys)
}

type mergeTestResult struct {
	config map[string]any // parsed PVC openclaw.json
	stdout string
	stderr string
	pvcDir string // temp PVC directory for filesystem assertions
}

func runMergeJS(t *testing.T, setup mergeTestSetup) mergeTestResult {
	t.Helper()

	cmData := extractConfigMapData(t)

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	pvcDir := filepath.Join(tmpDir, "pvc")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.MkdirAll(pvcDir, 0o755))

	mergeScript := cmData["merge.js"]
	require.NotEmpty(t, mergeScript, "merge.js must exist in configmap")
	require.Contains(t, mergeScript, `const configDir = "/config"`, "merge.js configDir anchor changed")
	require.Contains(t, mergeScript, `const pvcDir = "/home/node/.openclaw"`, "merge.js pvcDir anchor changed")

	mergeScript = strings.Replace(mergeScript, `const configDir = "/config"`, fmt.Sprintf(`const configDir = %q`, configDir), 1)
	mergeScript = strings.Replace(mergeScript, `const pvcDir = "/home/node/.openclaw"`, fmt.Sprintf(`const pvcDir = %q`, pvcDir), 1)

	scriptPath := filepath.Join(configDir, "merge.js")
	require.NoError(t, os.WriteFile(scriptPath, []byte(mergeScript), 0o644))

	operatorJSON := setup.operatorJSON
	if operatorJSON == "" {
		operatorJSON = cmData["operator.json"]
	}
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "operator.json"), []byte(operatorJSON), 0o644))

	seedJSON := setup.seedJSON
	if seedJSON == "" {
		seedJSON = cmData["openclaw.json"]
	}
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "openclaw.json"), []byte(seedJSON), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "AGENTS.md"), []byte(cmData["AGENTS.md"]), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "SOUL.md"), []byte(cmData["SOUL.md"]), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "BOOTSTRAP.md"), []byte(cmData["BOOTSTRAP.md"]), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "PLATFORM.md"), []byte(cmData["PLATFORM.md"]), 0o644))

	if setup.withK8sSkill != "" {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "KUBERNETES.md"), []byte(setup.withK8sSkill), 0o644))
	}

	for name, content := range setup.extraConfigs {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, name), []byte(content), 0o644))
	}

	if setup.pvcJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(pvcDir, "openclaw.json"), []byte(setup.pvcJSON), 0o644))
	}

	for relPath, content := range setup.pvcFiles {
		absPath := filepath.Join(pvcDir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
		require.NoError(t, os.WriteFile(absPath, []byte(content), 0o644))
	}

	cmd := exec.Command("node", scriptPath) //nolint:gosec
	if setup.configMode != "" {
		cmd.Env = append(os.Environ(), "CLAW_CONFIG_MODE="+setup.configMode)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, err, "merge.js failed: stdout=%s stderr=%s", stdout.String(), stderr.String())

	resultPath := filepath.Join(pvcDir, "openclaw.json")
	resultBytes, err := os.ReadFile(resultPath)
	require.NoError(t, err, "merged openclaw.json must exist")

	var config map[string]any
	require.NoError(t, json.Unmarshal(resultBytes, &config), "merged openclaw.json must be valid JSON")

	return mergeTestResult{
		config: config,
		stdout: stdout.String(),
		stderr: stderr.String(),
		pvcDir: pvcDir,
	}
}

// nestedValue traverses a map[string]any by dot-separated keys.
func nestedValue(m map[string]any, path string) (any, bool) {
	keys := strings.Split(path, ".")
	var current any = m
	for _, k := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[k]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func TestMergeJS(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping merge.js tests")
	}

	t.Run("first run merge mode", func(t *testing.T) {
		result := runMergeJS(t, mergeTestSetup{})

		_, hasGateway := nestedValue(result.config, "gateway")
		assert.True(t, hasGateway, "result should have gateway section from operator.json")

		agentsList, hasAgentsList := nestedValue(result.config, "agents.list")
		assert.True(t, hasAgentsList, "result should have agents.list from seed")
		list, ok := agentsList.([]any)
		assert.True(t, ok && len(list) > 0, "agents.list should be a non-empty array")

		_, hasModels := nestedValue(result.config, "agents.defaults.models")
		assert.False(t, hasModels, "embedded seed should not have hardcoded models (dynamically injected at reconcile time)")

		assert.Contains(t, result.stdout, "[init-config]")
	})

	t.Run("restart with existing PVC", func(t *testing.T) {
		pvcJSON := `{
			"agents": {
				"defaults": {
					"model": { "primary": "anthropic-vertex/claude-sonnet-4-6" },
					"models": {
						"google/gemini-3.5-flash": { "alias": "Gemini Flash" }
					},
					"workspace": "~/.openclaw/workspace"
				},
				"list": [{"id": "default", "name": "OpenClaw Assistant", "workspace": "~/.openclaw/workspace"}]
			},
			"plugins": { "foo": "bar" }
		}`

		result := runMergeJS(t, mergeTestSetup{pvcJSON: pvcJSON})

		_, hasGateway := nestedValue(result.config, "gateway")
		assert.True(t, hasGateway, "operator gateway should be merged into result")

		pluginsFoo, hasPlugins := nestedValue(result.config, "plugins.foo")
		assert.True(t, hasPlugins, "user plugins.foo should be preserved")
		assert.Equal(t, "bar", pluginsFoo)

		assert.Contains(t, result.stdout, "merged operator.json into existing openclaw.json")
	})

	t.Run("operator keys win on conflict", func(t *testing.T) {
		pvcJSON := `{
			"gateway": { "port": 9999, "mode": "local" },
			"agents": { "defaults": { "workspace": "~/.openclaw/workspace" } }
		}`

		result := runMergeJS(t, mergeTestSetup{pvcJSON: pvcJSON})

		port, hasPort := nestedValue(result.config, "gateway.port")
		assert.True(t, hasPort)
		assert.Equal(t, float64(18789), port, "operator's port should win over PVC's port")
	})

	t.Run("user keys preserved no collision", func(t *testing.T) {
		pvcJSON := `{
			"plugins": { "entries": { "slack": { "enabled": true } } },
			"agents": { "defaults": { "workspace": "~/.openclaw/workspace" } }
		}`

		result := runMergeJS(t, mergeTestSetup{pvcJSON: pvcJSON})

		enabled, hasEnabled := nestedValue(result.config, "plugins.entries.slack.enabled")
		assert.True(t, hasEnabled, "user plugin config should be preserved")
		assert.Equal(t, true, enabled)
	})

	t.Run("arrays replaced not merged", func(t *testing.T) {
		pvcJSON := `{
			"gateway": { "trustedProxies": ["1.1.1.1"] },
			"agents": { "defaults": { "workspace": "~/.openclaw/workspace" } }
		}`

		result := runMergeJS(t, mergeTestSetup{pvcJSON: pvcJSON})

		proxies, hasProxies := nestedValue(result.config, "gateway.trustedProxies")
		assert.True(t, hasProxies)
		arr, ok := proxies.([]any)
		require.True(t, ok, "trustedProxies should be an array")
		assert.Len(t, arr, 2, "operator's array should replace PVC's array entirely")
		assert.Equal(t, "10.0.0.0/8", arr[0])
		assert.Equal(t, "172.16.0.0/12", arr[1])
	})

	t.Run("overwrite mode ignores PVC", func(t *testing.T) {
		pvcJSON := `{
			"agents": { "defaults": { "workspace": "~/.openclaw/workspace" } },
			"plugins": { "custom": "user-data" }
		}`

		result := runMergeJS(t, mergeTestSetup{
			pvcJSON:    pvcJSON,
			configMode: "overwrite",
		})

		_, hasPlugins := nestedValue(result.config, "plugins.custom")
		assert.False(t, hasPlugins, "PVC user data should be gone in overwrite mode")

		_, hasGateway := nestedValue(result.config, "gateway")
		assert.True(t, hasGateway, "operator gateway should be present")

		_, hasAgentsList := nestedValue(result.config, "agents.list")
		assert.True(t, hasAgentsList, "seed agents.list should be present")
	})

	t.Run("invalid PVC JSON falls back to seed", func(t *testing.T) {
		result := runMergeJS(t, mergeTestSetup{
			pvcJSON: `{invalid json`,
		})

		_, hasGateway := nestedValue(result.config, "gateway")
		assert.True(t, hasGateway, "result should have gateway from operator")

		_, hasAgentsList := nestedValue(result.config, "agents.list")
		assert.True(t, hasAgentsList, "result should have agents.list from seed (fallback)")

		assert.Contains(t, result.stderr, "invalid JSON")
	})

	t.Run("seed files seeded correctly", func(t *testing.T) {
		cmData := extractConfigMapData(t)
		result := runMergeJS(t, mergeTestSetup{})

		// AGENTS.md, SOUL.md, BOOTSTRAP.md are now handled by init-seed, not merge.js.
		// Only operator-managed skills are still seeded by merge.js.
		skillContent, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "platform", "SKILL.md"))
		require.NoError(t, err, "PLATFORM.md should be copied to skills/platform/SKILL.md")
		assert.Equal(t, cmData["PLATFORM.md"], string(skillContent))
	})

	t.Run("skill files always overwritten by merge.js", func(t *testing.T) {
		cmData := extractConfigMapData(t)
		oldSkill := "old skill content"

		result := runMergeJS(t, mergeTestSetup{
			pvcFiles: map[string]string{
				"workspace/skills/platform/SKILL.md": oldSkill,
			},
		})

		skillContent, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "platform", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, cmData["PLATFORM.md"], string(skillContent), "SKILL.md should be overwritten (copyAlways)")
		assert.NotEqual(t, oldSkill, string(skillContent))
	})

	t.Run("primary preserved on restart", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["google/gemini-3-flash-preview"]}, "models": {"google/gemini-3.1-pro-preview": {"alias": "Gemini 3.1 Pro"}}}}
		}`
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "anthropic/claude-opus-4-7"}, "workspace": "~/.openclaw/workspace"}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary, "should have primary model")
		assert.Equal(t, "anthropic/claude-opus-4-7", primary, "user's primary should be preserved on restart")
	})

	t.Run("primary set on first run", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["google/gemini-3-flash-preview"]}, "models": {"google/gemini-3.1-pro-preview": {"alias": "Gemini 3.1 Pro"}}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary, "should have primary model on first run")
		assert.Equal(t, "google/gemini-3.1-pro-preview", primary, "operator's primary should be used on first run")

		fallbacks, hasFallbacks := nestedValue(result.config, "agents.defaults.model.fallbacks")
		assert.True(t, hasFallbacks, "should have fallbacks on first run")
		fbSlice := fallbacks.([]any)
		require.Len(t, fbSlice, 1)
		assert.Equal(t, "google/gemini-3-flash-preview", fbSlice[0], "operator's fallbacks should be used on first run")
	})

	t.Run("primary preserved even when models change", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["google/gemini-3-flash-preview"]}, "models": {
				"google/gemini-3.1-pro-preview": {"alias": "Gemini 3.1 Pro"},
				"google/gemini-3-flash-preview": {"alias": "Gemini 3 Flash"}
			}}}
		}`
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "anthropic/claude-opus-4-7"}, "models": {
				"anthropic/claude-opus-4-7": {"alias": "Claude Opus"}
			}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary)
		assert.Equal(t, "anthropic/claude-opus-4-7", primary, "user's primary should survive model catalog changes")

		models, hasModels := nestedValue(result.config, "agents.defaults.models")
		assert.True(t, hasModels)
		modelsMap := models.(map[string]any)
		assert.Contains(t, modelsMap, "google/gemini-3.1-pro-preview", "new operator models should be merged in")
		assert.Contains(t, modelsMap, "google/gemini-3-flash-preview", "new operator models should be merged in")
	})

	t.Run("primary not preserved in overwrite mode", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview"}}}
		}`
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "anthropic/claude-opus-4-7"}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "overwrite"})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary)
		assert.Equal(t, "google/gemini-3.1-pro-preview", primary, "overwrite mode should reset to operator's primary")
	})

	t.Run("fallbacks preserved on restart", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["google/gemini-3-flash-preview", "google/gemini-3.5-flash"]}}}
		}`
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["anthropic/claude-sonnet-4-6"]}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON})

		fallbacks, hasFallbacks := nestedValue(result.config, "agents.defaults.model.fallbacks")
		assert.True(t, hasFallbacks, "should have fallbacks")
		fbSlice := fallbacks.([]any)
		require.Len(t, fbSlice, 1)
		assert.Equal(t, "anthropic/claude-sonnet-4-6", fbSlice[0], "user's fallbacks should be preserved on restart")
	})

	t.Run("fallbacks not preserved in overwrite mode", func(t *testing.T) {
		operatorJSON := operatorJSONWithPrimaryAndFallback
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "google/gemini-3.1-pro-preview", "fallbacks": ["anthropic/claude-sonnet-4-6"]}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "overwrite"})

		fallbacks, hasFallbacks := nestedValue(result.config, "agents.defaults.model.fallbacks")
		assert.True(t, hasFallbacks)
		fbSlice := fallbacks.([]any)
		require.Len(t, fbSlice, 1)
		assert.Equal(t, "google/gemini-3-flash-preview", fbSlice[0], "overwrite mode should reset to operator's fallbacks")
	})

	t.Run("skill file copied on first run", func(t *testing.T) {
		result := runMergeJS(t, mergeTestSetup{
			extraConfigs: map[string]string{
				"_skill_quote-builder": "# Quote Builder\nBuild quotes...",
			},
		})

		content, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "quote-builder", "SKILL.md"))
		require.NoError(t, err, "skill should be copied to skills/<name>/SKILL.md")
		assert.Equal(t, "# Quote Builder\nBuild quotes...", string(content))
	})

	t.Run("skill file overwritten on restart", func(t *testing.T) {
		oldContent := "old skill content"
		newContent := "# Updated Skill\nNew version..."
		result := runMergeJS(t, mergeTestSetup{
			extraConfigs: map[string]string{
				"_skill_quote-builder": newContent,
			},
			pvcFiles: map[string]string{
				"workspace/skills/quote-builder/SKILL.md": oldContent,
			},
		})

		content, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "quote-builder", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, newContent, string(content), "copyAlways should overwrite existing skill")
	})

	t.Run("multiple skills copied together", func(t *testing.T) {
		result := runMergeJS(t, mergeTestSetup{
			extraConfigs: map[string]string{
				"_skill_compliance": "# Compliance\nFollow rules...",
				"_skill_quotes":     "# Quotes\nBuild quotes...",
			},
		})

		compliance, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "compliance", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, "# Compliance\nFollow rules...", string(compliance))

		quotes, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "quotes", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, "# Quotes\nBuild quotes...", string(quotes))
	})

	t.Run("_seedOnlyMeta never leaks into written openclaw.json in any mode", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"mcp": {"servers": {"db": {"command": "node"}}},
			"_seedOnlyMeta": {"mcpBucketAServers": ["db"]}
		}`
		for _, mode := range []string{"", "merge", "overwrite", "seedOnly"} {
			result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, configMode: mode})
			assert.NotContains(t, result.config, "_seedOnlyMeta", "mode=%q should not leak _seedOnlyMeta", mode)
		}
	})
}

// TestMergeJSSeedOnly exhaustively covers the seedOnly mode test matrix from
// docs/proposals/user-owned-config-design.md's Implementation Plan. Failure
// modes here are silent and asymmetric (under-reasserting leaves a
// security/auth field unprotected; over-reasserting silently clobbers a
// user/agent's customization) and only surface on the second-or-later
// restart of an already-seeded instance — so this suite intentionally covers
// every row of the design doc's matrix, not just one representative case.
func TestMergeJSSeedOnly(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping merge.js tests")
	}

	t.Run("first boot with no existing PVC file seeds correctly", func(t *testing.T) {
		result := runMergeJS(t, mergeTestSetup{configMode: "seedOnly"})

		_, hasGateway := nestedValue(result.config, "gateway")
		assert.True(t, hasGateway, "result should have gateway section from operator.json")

		agentsList, hasAgentsList := nestedValue(result.config, "agents.list")
		assert.True(t, hasAgentsList, "result should have agents.list from seed")
		list, ok := agentsList.([]any)
		assert.True(t, ok && len(list) > 0, "agents.list should be a non-empty array")
	})

	t.Run("declared provider's baseUrl/apiKey/api corrected, local models preserved", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"models": {"providers": {"google": {"baseUrl": "https://real.example.com", "apiKey": "ah-ah-ah-you-didnt-say-the-magic-word", "api": "openai-completions"}}}
		}`
		pvcJSON := `{
			"models": {"providers": {"google": {
				"baseUrl": "https://hacked.example.com",
				"apiKey": "stolen-key",
				"api": "anthropic-messages",
				"models": {"custom-model": {"alias": "Custom"}}
			}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		baseURL, _ := nestedValue(result.config, "models.providers.google.baseUrl")
		assert.Equal(t, "https://real.example.com", baseURL, "baseUrl should be corrected to operator's value")
		apiKey, _ := nestedValue(result.config, "models.providers.google.apiKey")
		assert.Equal(t, "ah-ah-ah-you-didnt-say-the-magic-word", apiKey, "apiKey should be corrected")
		api, _ := nestedValue(result.config, "models.providers.google.api")
		assert.Equal(t, "openai-completions", api, "api should be corrected")

		customModel, hasCustomModel := nestedValue(result.config, "models.providers.google.models.custom-model")
		assert.True(t, hasCustomModel, "hand-populated local .models array should be preserved (Bucket B)")
		assert.Equal(t, map[string]any{"alias": "Custom"}, customModel)
	})

	t.Run("declared channel's botToken/enabled corrected, dmPolicy/allowFrom preserved", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"channels": {"telegram": {"enabled": true, "botToken": "placeholder", "dmPolicy": "open", "allowFrom": ["*"]}}
		}`
		pvcJSON := `{
			"channels": {"telegram": {"enabled": false, "botToken": "hijacked", "dmPolicy": "allowlist", "allowFrom": [12345]}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		enabled, _ := nestedValue(result.config, "channels.telegram.enabled")
		assert.Equal(t, true, enabled, "enabled should be corrected back to operator's value")
		botToken, _ := nestedValue(result.config, "channels.telegram.botToken")
		assert.Equal(t, "placeholder", botToken, "botToken should be corrected back")

		dmPolicy, _ := nestedValue(result.config, "channels.telegram.dmPolicy")
		assert.Equal(t, "allowlist", dmPolicy, "hand-edited dmPolicy should be preserved (Bucket B)")
		allowFrom, _ := nestedValue(result.config, "channels.telegram.allowFrom")
		assert.Equal(t, []any{float64(12345)}, allowFrom, "hand-edited allowFrom should be preserved (Bucket B)")
	})

	t.Run("declared credentialed MCP server (envFrom) corrected on restart", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"mcp": {"servers": {"db": {"command": "node", "args": ["db-mcp-server.js"], "env": {"DB_HOST": "postgres.internal", "DB_PASSWORD": "DB_PASSWORD"}}}},
			"_seedOnlyMeta": {"mcpBucketAServers": ["db"]}
		}`
		pvcJSON := `{
			"mcp": {"servers": {"db": {"command": "malicious", "args": ["evil.js"], "env": {"DB_PASSWORD": "hacked"}}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		server, _ := nestedValue(result.config, "mcp.servers.db")
		serverMap, ok := server.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "node", serverMap["command"], "envFrom-backed MCP server should be fully reasserted")
		assert.Equal(t, []any{"db-mcp-server.js"}, serverMap["args"])
	})

	t.Run("hand-added non-declared MCP server is preserved untouched", func(t *testing.T) {
		operatorJSON := `{"gateway": {"port": 18789}}`
		pvcJSON := `{
			"mcp": {"servers": {"custom": {"command": "node", "args": ["my-own-server.js"]}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		server, hasServer := nestedValue(result.config, "mcp.servers.custom")
		assert.True(t, hasServer, "hand-added, non-CR-declared MCP server should be preserved")
		assert.Equal(t, map[string]any{"command": "node", "args": []any{"my-own-server.js"}}, server)
	})

	t.Run("new CR-declared provider gap-fills automatically", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"models": {"providers": {
				"google": {"baseUrl": "https://google.example.com", "apiKey": "placeholder", "api": "openai-completions"},
				"openai": {"baseUrl": "https://openai.example.com", "apiKey": "placeholder", "api": "openai-completions"}
			}}
		}`
		pvcJSON := `{
			"models": {"providers": {"google": {"baseUrl": "https://google.example.com", "apiKey": "placeholder", "api": "openai-completions"}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		openai, hasOpenai := nestedValue(result.config, "models.providers.openai")
		assert.True(t, hasOpenai, "new CR-declared provider should be gap-filled automatically")
		assert.Equal(t, map[string]any{"baseUrl": "https://openai.example.com", "apiKey": "placeholder", "api": "openai-completions"}, openai)
	})

	t.Run("newly catalog-eligible agents.defaults.models entry gap-fills automatically", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"models": {
				"anthropic/claude-opus-4-7": {"alias": "Claude Opus"},
				"openai/gpt-5": {"alias": "GPT-5"}
			}}}
		}`
		pvcJSON := `{
			"agents": {"defaults": {"models": {"anthropic/claude-opus-4-7": {"alias": "Claude Opus"}}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		gpt5, hasGpt5 := nestedValue(result.config, "agents.defaults.models.openai/gpt-5")
		assert.True(t, hasGpt5, "newly catalog-eligible model should be gap-filled automatically")
		assert.Equal(t, map[string]any{"alias": "GPT-5"}, gpt5)
	})

	t.Run("existing entry's CR-side content change does not propagate", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"models": {"providers": {"acme": {"baseUrl": "https://acme.example.com", "apiKey": "placeholder", "api": "openai-completions"}}}
		}`
		pvcJSON := `{
			"models": {"providers": {"acme": {
				"baseUrl": "https://acme.example.com", "apiKey": "placeholder", "api": "openai-completions",
				"models": {"acme-model-v1": {"alias": "Acme V1"}}
			}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		models, _ := nestedValue(result.config, "models.providers.acme.models")
		assert.Equal(t, map[string]any{"acme-model-v1": map[string]any{"alias": "Acme V1"}}, models,
			"gap-fill must not touch an already-existing entry's Bucket-B content")
	})

	t.Run("gap-fill and reassertion do not cross-contaminate sibling entries", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"models": {"providers": {
				"google": {"baseUrl": "https://google.example.com", "apiKey": "placeholder", "api": "openai-completions"},
				"newprov": {"baseUrl": "https://newprov.example.com", "apiKey": "placeholder", "api": "openai-completions"}
			}}
		}`
		pvcJSON := `{
			"models": {"providers": {"google": {
				"baseUrl": "https://google.example.com", "apiKey": "placeholder", "api": "openai-completions",
				"models": {"custom-model": {"alias": "Custom"}}
			}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		newprov, hasNewprov := nestedValue(result.config, "models.providers.newprov")
		assert.True(t, hasNewprov, "new sibling entry should appear via gap-fill")
		assert.Equal(t, "https://newprov.example.com", newprov.(map[string]any)["baseUrl"])

		googleModels, _ := nestedValue(result.config, "models.providers.google.models")
		assert.Equal(t, map[string]any{"custom-model": map[string]any{"alias": "Custom"}}, googleModels,
			"existing sibling's customization must remain untouched")
	})

	t.Run("hand-edited primary/fallbacks preserved", func(t *testing.T) {
		operatorJSON := operatorJSONWithPrimaryAndFallback
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "anthropic/claude-opus-4-7", "fallbacks": ["anthropic/claude-sonnet-4-6"]}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		primary, _ := nestedValue(result.config, "agents.defaults.model.primary")
		assert.Equal(t, "anthropic/claude-opus-4-7", primary, "hand-edited primary should be preserved")
		fallbacks, _ := nestedValue(result.config, "agents.defaults.model.fallbacks")
		assert.Equal(t, []any{"anthropic/claude-sonnet-4-6"}, fallbacks, "hand-edited fallbacks should be preserved")
	})

	t.Run("primary/fallbacks gap-filled when absent/empty and a catalog-eligible credential is added", func(t *testing.T) {
		operatorJSON := operatorJSONWithPrimaryAndFallback
		pvcJSON := `{
			"agents": {"defaults": {"workspace": "~/.openclaw/workspace"}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary, "absent primary should be gap-filled")
		assert.Equal(t, "google/gemini-3.1-pro-preview", primary)
		fallbacks, hasFallbacks := nestedValue(result.config, "agents.defaults.model.fallbacks")
		assert.True(t, hasFallbacks, "absent fallbacks should be gap-filled")
		assert.Equal(t, []any{"google/gemini-3-flash-preview"}, fallbacks)
	})

	t.Run("only the corrupted entry changes among multiple declared providers", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"models": {"providers": {
				"google": {"baseUrl": "https://google.example.com", "apiKey": "placeholder-g", "api": "openai-completions"},
				"openai": {"baseUrl": "https://openai.example.com", "apiKey": "placeholder-o", "api": "openai-completions"}
			}}
		}`
		pvcJSON := `{
			"models": {"providers": {
				"google": {"baseUrl": "https://hacked.example.com", "apiKey": "wrong", "api": "wrong"},
				"openai": {"baseUrl": "https://openai.example.com", "apiKey": "placeholder-o", "api": "openai-completions", "models": {"gpt-x": {"alias": "GPT-X"}}}
			}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		googleBaseURL, _ := nestedValue(result.config, "models.providers.google.baseUrl")
		assert.Equal(t, "https://google.example.com", googleBaseURL, "corrupted google provider should be corrected")

		openaiModels, _ := nestedValue(result.config, "models.providers.openai.models")
		assert.Equal(t, map[string]any{"gpt-x": map[string]any{"alias": "GPT-X"}}, openaiModels,
			"unaffected sibling's Bucket-B content should not be touched")
	})

	t.Run("infra keys and route host corrected, non-route origins preserved", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {
				"mode": "local", "bind": "lan", "port": 18789,
				"auth": {"mode": "token"},
				"controlUi": {"enabled": true, "allowedOrigins": ["https://route.example.com"], "dangerouslyDisableDeviceAuth": false},
				"trustedProxies": ["10.0.0.0/8", "172.16.0.0/12"]
			},
			"tools": {"web": {"search": {"enabled": true, "provider": "tavily"}, "fetch": {"enabled": true}}},
			"agents": {"defaults": {"memorySearch": {"provider": "openai", "enabled": true}}},
			"diagnostics": {"otel": {"metrics": true, "metricsEndpoint": "http://otel.example.com:4318"}}
		}`
		pvcJSON := `{
			"gateway": {
				"mode": "hacked", "bind": "0.0.0.0", "port": 9999,
				"auth": {"mode": "password"},
				"controlUi": {"enabled": false, "allowedOrigins": ["https://user-custom.example.com"], "dangerouslyDisableDeviceAuth": true},
				"trustedProxies": ["1.2.3.4/32"]
			},
			"tools": {"web": {"search": {"enabled": false, "provider": "hacked"}, "fetch": {"enabled": false}}},
			"agents": {"defaults": {"memorySearch": {"provider": "hacked", "enabled": false}}},
			"diagnostics": {"otel": {"metrics": false, "metricsEndpoint": "http://hacked.example.com"}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		mode, _ := nestedValue(result.config, "gateway.mode")
		assert.Equal(t, "local", mode)
		bind, _ := nestedValue(result.config, "gateway.bind")
		assert.Equal(t, "lan", bind)
		port, _ := nestedValue(result.config, "gateway.port")
		assert.Equal(t, float64(18789), port)
		authMode, _ := nestedValue(result.config, "gateway.auth.mode")
		assert.Equal(t, "token", authMode)
		enabled, _ := nestedValue(result.config, "gateway.controlUi.enabled")
		assert.Equal(t, true, enabled)
		deviceAuth, _ := nestedValue(result.config, "gateway.controlUi.dangerouslyDisableDeviceAuth")
		assert.Equal(t, false, deviceAuth)
		trustedProxies, _ := nestedValue(result.config, "gateway.trustedProxies")
		assert.Equal(t, []any{"10.0.0.0/8", "172.16.0.0/12"}, trustedProxies)

		origins, _ := nestedValue(result.config, "gateway.controlUi.allowedOrigins")
		assert.ElementsMatch(t, []any{"https://user-custom.example.com", "https://route.example.com"}, origins,
			"route host should be appended; hand-added origin should be preserved")

		searchEnabled, _ := nestedValue(result.config, "tools.web.search.enabled")
		assert.Equal(t, true, searchEnabled)
		searchProvider, _ := nestedValue(result.config, "tools.web.search.provider")
		assert.Equal(t, "tavily", searchProvider)
		fetchEnabled, _ := nestedValue(result.config, "tools.web.fetch.enabled")
		assert.Equal(t, true, fetchEnabled)
		memProvider, _ := nestedValue(result.config, "agents.defaults.memorySearch.provider")
		assert.Equal(t, "openai", memProvider)
		memEnabled, _ := nestedValue(result.config, "agents.defaults.memorySearch.enabled")
		assert.Equal(t, true, memEnabled)
		otelMetrics, _ := nestedValue(result.config, "diagnostics.otel.metrics")
		assert.Equal(t, true, otelMetrics)
		otelEndpoint, _ := nestedValue(result.config, "diagnostics.otel.metricsEndpoint")
		assert.Equal(t, "http://otel.example.com:4318", otelEndpoint)
	})

	t.Run("orphaned entry removed from CR is left untouched without erroring", func(t *testing.T) {
		operatorJSON := `{"gateway": {"port": 18789}, "models": {"providers": {}}}`
		pvcJSON := `{
			"models": {"providers": {"google": {"baseUrl": "https://google.example.com", "apiKey": "placeholder", "api": "openai-completions"}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		google, hasGoogle := nestedValue(result.config, "models.providers.google")
		assert.True(t, hasGoogle, "orphaned entry should remain in the file, untouched")
		assert.Equal(t, "https://google.example.com", google.(map[string]any)["baseUrl"])
	})

	t.Run("declared entry missing entirely from PVC file is added without erroring", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"channels": {"telegram": {"enabled": true, "botToken": "placeholder", "dmPolicy": "open", "allowFrom": ["*"]}}
		}`
		pvcJSON := `{"agents": {"defaults": {"workspace": "~/.openclaw/workspace"}}}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "seedOnly"})

		telegram, hasTelegram := nestedValue(result.config, "channels.telegram")
		assert.True(t, hasTelegram, "entry missing entirely from the PVC file should be added via gap-fill")
		assert.Equal(t, true, telegram.(map[string]any)["enabled"])
	})

	t.Run("skill docs are seeded once, user edits persist", func(t *testing.T) {
		cmData := extractConfigMapData(t)
		customSkill := "# My Custom Platform Notes\nEdited by the agent."

		result := runMergeJS(t, mergeTestSetup{
			configMode: "seedOnly",
			pvcJSON:    `{"agents": {"defaults": {"workspace": "~/.openclaw/workspace"}}}`,
			pvcFiles: map[string]string{
				"workspace/skills/platform/SKILL.md": customSkill,
			},
		})

		content, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "platform", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, customSkill, string(content), "seedOnly should not overwrite an existing skill doc")
		assert.NotEqual(t, cmData["PLATFORM.md"], string(content))
	})

	t.Run("skill docs are seeded on first boot under seedOnly", func(t *testing.T) {
		cmData := extractConfigMapData(t)

		result := runMergeJS(t, mergeTestSetup{configMode: "seedOnly"})

		content, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "platform", "SKILL.md"))
		require.NoError(t, err, "skill doc should be seeded on first boot even under seedOnly")
		assert.Equal(t, cmData["PLATFORM.md"], string(content))
	})
}
