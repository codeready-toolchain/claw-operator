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
	"path/filepath"
	"strings"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ConfigMap key prefixes for workspace files and skills.
const (
	workspaceKeyPrefix = "_ws_"
	skillKeyPrefix     = "_skill_"
	pathSeparatorCode  = "--"
)

// builtinSkillNames lists operator-managed skill directory names that cannot
// be used as user skill names.
var builtinSkillNames = map[string]bool{
	"platform":   true,
	"kubernetes": true,
}

// encodeWorkspacePath encodes a workspace-relative path for use as a
// ConfigMap key by replacing "/" with "--".
func encodeWorkspacePath(p string) string {
	return strings.ReplaceAll(p, "/", pathSeparatorCode)
}

// validateWorkspaceFiles checks that all workspace file paths are safe and do
// not conflict with operator-managed paths.
func validateWorkspaceFiles(files map[string]string) error {
	for p := range files {
		if p == "" {
			return fmt.Errorf("workspace file path must not be empty")
		}
		if filepath.IsAbs(p) {
			return fmt.Errorf("workspace file path %q is invalid: must not be absolute", p)
		}
		if strings.Contains(p, "..") {
			return fmt.Errorf("workspace file path %q is invalid: must not contain \"..\"", p)
		}
		if strings.Contains(p, pathSeparatorCode) {
			return fmt.Errorf("workspace file path %q is invalid: must not contain %q (reserved as path encoding delimiter)", p, pathSeparatorCode)
		}
		cleaned := filepath.Clean(p)
		if strings.HasPrefix(cleaned, "skills/platform/") || cleaned == "skills/platform" {
			return fmt.Errorf("workspace file path %q conflicts with operator-managed platform skill", p)
		}
		if strings.HasPrefix(cleaned, "skills/kubernetes/") || cleaned == "skills/kubernetes" {
			return fmt.Errorf("workspace file path %q conflicts with operator-managed kubernetes skill", p)
		}
	}
	return nil
}

// validateSkillNames checks that all skill names are valid directory components
// and do not conflict with builtin operator skills.
func validateSkillNames(skills map[string]string) error {
	for name := range skills {
		if name == "" {
			return fmt.Errorf("skill name must not be empty")
		}
		if name == "." || name == ".." {
			return fmt.Errorf("skill name %q is invalid: must not be %q", name, name)
		}
		if strings.Contains(name, "/") {
			return fmt.Errorf("skill name %q is invalid: must not contain \"/\"", name)
		}
		if strings.Contains(name, pathSeparatorCode) {
			return fmt.Errorf("skill name %q is invalid: must not contain %q (reserved as path encoding delimiter)", name, pathSeparatorCode)
		}
		if builtinSkillNames[name] {
			return fmt.Errorf("skill name %q conflicts with builtin operator skill", name)
		}
	}
	return nil
}

// injectWorkspaceFiles validates workspace file paths and writes _ws_ prefixed
// keys into the gateway ConfigMap.
func injectWorkspaceFiles(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Workspace == nil || len(instance.Spec.Workspace.Files) == 0 {
		return nil
	}

	if err := validateWorkspaceFiles(instance.Spec.Workspace.Files); err != nil {
		return err
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	for p, content := range instance.Spec.Workspace.Files {
		key := workspaceKeyPrefix + encodeWorkspacePath(p)
		if err := unstructured.SetNestedField(cmObj.Object, content, "data", key); err != nil {
			return fmt.Errorf("failed to set workspace file %q in ConfigMap: %w", p, err)
		}
	}
	return nil
}

// injectSkillFiles validates skill names and writes _skill_ prefixed keys
// into the gateway ConfigMap.
func injectSkillFiles(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if len(instance.Spec.Skills) == 0 {
		return nil
	}

	if err := validateSkillNames(instance.Spec.Skills); err != nil {
		return err
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	for name, content := range instance.Spec.Skills {
		key := skillKeyPrefix + name
		if err := unstructured.SetNestedField(cmObj.Object, content, "data", key); err != nil {
			return fmt.Errorf("failed to set skill %q in ConfigMap: %w", name, err)
		}
	}
	return nil
}

// injectSkipBootstrap sets agents.defaults.skipBootstrap in operator.json
// when spec.workspace.skipBootstrap is true.
func injectSkipBootstrap(config map[string]any, instance *clawv1alpha1.Claw) {
	if instance.Spec.Workspace != nil && instance.Spec.Workspace.SkipBootstrap {
		setNestedValue(config, true, "agents", "defaults", "skipBootstrap")
	}
}

// injectBootstrapHook configures the bootstrap-extra-files hook to load
// BOOTSTRAP.md from .operator/ instead of the workspace root. This avoids
// OpenClaw 6.1+'s reconciliation that deletes root BOOTSTRAP.md when it
// detects completion evidence (custom SOUL.md or skills).
func injectBootstrapHook(config map[string]any) {
	if instance, ok := config["hooks"]; ok {
		if hooks, ok := instance.(map[string]any); ok {
			if internal, ok := hooks["internal"]; ok {
				if internalMap, ok := internal.(map[string]any); ok {
					if entries, ok := internalMap["entries"]; ok {
						if entriesMap, ok := entries.(map[string]any); ok {
							if _, exists := entriesMap["bootstrap-extra-files"]; exists {
								return
							}
						}
					}
				}
			}
		}
	}
	setNestedValue(config, true, "hooks", "internal", "enabled")
	setNestedValue(config, map[string]any{
		"enabled": true,
		"paths":   []any{".operator/BOOTSTRAP.md"},
	}, "hooks", "internal", "entries", "bootstrap-extra-files")
}
