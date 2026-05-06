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
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "PROXY_SETUP.md"), []byte(cmData["PROXY_SETUP.md"]), 0o644))

	if setup.withK8sSkill != "" {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "KUBERNETES.md"), []byte(setup.withK8sSkill), 0o644))
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
						"google/gemini-3-flash-preview": { "alias": "Gemini Flash" }
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

		agentsContent, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "AGENTS.md"))
		require.NoError(t, err, "AGENTS.md should be seeded to workspace")
		assert.Equal(t, cmData["AGENTS.md"], string(agentsContent))

		skillContent, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "proxy", "SKILL.md"))
		require.NoError(t, err, "PROXY_SETUP.md should be copied to skills/proxy/SKILL.md")
		assert.Equal(t, cmData["PROXY_SETUP.md"], string(skillContent))
	})

	t.Run("seed files not overwritten vs always copied", func(t *testing.T) {
		cmData := extractConfigMapData(t)
		customAgents := "custom user AGENTS.md content"
		oldSkill := "old skill content"

		result := runMergeJS(t, mergeTestSetup{
			pvcFiles: map[string]string{
				"workspace/AGENTS.md":             customAgents,
				"workspace/skills/proxy/SKILL.md": oldSkill,
			},
		})

		agentsContent, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "AGENTS.md"))
		require.NoError(t, err)
		assert.Equal(t, customAgents, string(agentsContent), "AGENTS.md should NOT be overwritten (seedIfMissing)")

		skillContent, err := os.ReadFile(filepath.Join(result.pvcDir, "workspace", "skills", "proxy", "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, cmData["PROXY_SETUP.md"], string(skillContent), "SKILL.md should be overwritten (copyAlways)")
		assert.NotEqual(t, oldSkill, string(skillContent))
	})

	t.Run("primary preserved on restart", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3-flash-preview"}, "models": {"google/gemini-3-flash-preview": {"alias": "Gemini 3 Flash"}}}}
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
			"agents": {"defaults": {"model": {"primary": "google/gemini-3-flash-preview"}, "models": {"google/gemini-3-flash-preview": {"alias": "Gemini 3 Flash"}}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary, "should have primary model on first run")
		assert.Equal(t, "google/gemini-3-flash-preview", primary, "operator's primary should be used on first run")
	})

	t.Run("primary preserved even when models change", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3-flash-preview"}, "models": {
				"google/gemini-3-flash-preview": {"alias": "Gemini 3 Flash"},
				"google/gemini-3.1-pro-preview": {"alias": "Gemini 3.1 Pro"}
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
		assert.Contains(t, modelsMap, "google/gemini-3-flash-preview", "new operator models should be merged in")
		assert.Contains(t, modelsMap, "google/gemini-3.1-pro-preview", "new operator models should be merged in")
	})

	t.Run("primary not preserved in overwrite mode", func(t *testing.T) {
		operatorJSON := `{
			"gateway": {"port": 18789},
			"agents": {"defaults": {"model": {"primary": "google/gemini-3-flash-preview"}}}
		}`
		pvcJSON := `{
			"agents": {"defaults": {"model": {"primary": "anthropic/claude-opus-4-7"}}}
		}`

		result := runMergeJS(t, mergeTestSetup{operatorJSON: operatorJSON, pvcJSON: pvcJSON, configMode: "overwrite"})

		primary, hasPrimary := nestedValue(result.config, "agents.defaults.model.primary")
		assert.True(t, hasPrimary)
		assert.Equal(t, "google/gemini-3-flash-preview", primary, "overwrite mode should reset to operator's primary")
	})
}
