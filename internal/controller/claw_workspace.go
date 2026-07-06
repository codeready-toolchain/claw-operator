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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ConfigMap key prefixes for workspace files and skills.
const (
	workspaceKeyPrefix     = "_ws_"
	skillKeyPrefix         = "_skill_"
	pathSeparatorCode      = "--"
	seedManifestKey        = "_seed_manifest.json"
	configMapSourceMountFn = "/configmap-sources/"
	gitSourceMountFn       = "/git-sources/"
	commitSHALength        = 40
	configMountPath        = "/config/"
)

// seedManifestEntry describes a single file to seed into the workspace.
type seedManifestEntry struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
}

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

// normalizeWorkspaceFiles converts deprecated spec.workspace.files entries
// into InlineSources with mode seedIfMissing, preserving original behavior.
// It is a no-op when InlineSources is already populated.
// Returns true if migration was performed (for deprecation logging).
func normalizeWorkspaceFiles(instance *clawv1alpha1.Claw) bool {
	if instance.Spec.Workspace == nil {
		return false
	}
	if len(instance.Spec.Workspace.Files) == 0 { //nolint:staticcheck // reading deprecated field for migration
		return false
	}
	if len(instance.Spec.Workspace.InlineSources) > 0 {
		return false
	}
	for p, content := range instance.Spec.Workspace.Files { //nolint:staticcheck // reading deprecated field for migration
		instance.Spec.Workspace.InlineSources = append(instance.Spec.Workspace.InlineSources,
			clawv1alpha1.InlineSource{
				Path:    p,
				Content: content,
				Mode:    clawv1alpha1.SeedModeSeedIfMissing,
			})
	}
	return true
}

// validateAllWorkspacePaths collects all target paths across inline, ConfigMap,
// and Git sources, validates each path, and rejects duplicate targets.
func validateAllWorkspacePaths(instance *clawv1alpha1.Claw) error {
	if instance.Spec.Workspace == nil {
		return nil
	}
	ws := instance.Spec.Workspace
	seen := map[string]string{} // path → source description

	for _, src := range ws.InlineSources {
		if err := validateWorkspaceFiles(map[string]string{src.Path: ""}); err != nil {
			return err
		}
		key := filepath.Clean(src.Path)
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("workspace path %q is declared by both %s and inline source", key, prev)
		}
		seen[key] = "inline source"
	}
	seenCM := map[string]bool{}
	for _, cms := range ws.ConfigMapSources {
		if seenCM[cms.ConfigMapRef.Name] {
			return fmt.Errorf("configMapSource %q is referenced more than once; combine items into a single entry",
				cms.ConfigMapRef.Name)
		}
		seenCM[cms.ConfigMapRef.Name] = true
		for _, item := range cms.Items {
			if err := validateWorkspaceFiles(map[string]string{item.Path: ""}); err != nil {
				return err
			}
			key := filepath.Clean(item.Path)
			if prev, ok := seen[key]; ok {
				return fmt.Errorf("workspace path %q is declared by both %s and configMapSource %q",
					key, prev, cms.ConfigMapRef.Name)
			}
			seen[key] = fmt.Sprintf("configMapSource %q", cms.ConfigMapRef.Name)
		}
	}
	for _, gs := range ws.GitSources {
		for _, item := range gs.Items {
			if err := validateWorkspaceFiles(map[string]string{item.Path: ""}); err != nil {
				return err
			}
			key := filepath.Clean(item.Path)
			if prev, ok := seen[key]; ok {
				return fmt.Errorf("workspace path %q is declared by both %s and gitSource %q",
					key, prev, gs.URL)
			}
			seen[key] = fmt.Sprintf("gitSource %q", gs.URL)
		}
	}
	return nil
}

// resolveSeedMode returns the effective mode using the three-tier cascade:
// item → source → overwrite (global default).
func resolveSeedMode(itemMode, sourceMode clawv1alpha1.SeedMode) string {
	if itemMode != "" {
		return string(itemMode)
	}
	if sourceMode != "" {
		return string(sourceMode)
	}
	return string(clawv1alpha1.SeedModeOverwrite)
}

// builtinWorkspaceFiles are default files seeded into the workspace on first run.
// They use seedIfMissing so user edits are preserved.
var builtinWorkspaceFiles = []seedManifestEntry{
	{Source: configMountPath + "AGENTS.md", Target: "AGENTS.md", Mode: string(clawv1alpha1.SeedModeSeedIfMissing)},
	{Source: configMountPath + "SOUL.md", Target: "SOUL.md", Mode: string(clawv1alpha1.SeedModeSeedIfMissing)},
	{Source: configMountPath + "BOOTSTRAP.md", Target: ".operator/BOOTSTRAP.md", Mode: string(clawv1alpha1.SeedModeSeedIfMissing)},
}

// generateSeedManifest builds the seeding manifest from all workspace source types,
// including builtin workspace files (AGENTS.md, SOUL.md, BOOTSTRAP.md).
func generateSeedManifest(ws *clawv1alpha1.WorkspaceSpec) []seedManifestEntry {
	var entries []seedManifestEntry

	// Always include builtin files (seedIfMissing)
	entries = append(entries, builtinWorkspaceFiles...)

	if ws == nil {
		return entries
	}

	for _, src := range ws.InlineSources {
		entries = append(entries, seedManifestEntry{
			Source: configMountPath + workspaceKeyPrefix + encodeWorkspacePath(src.Path),
			Target: src.Path,
			Mode:   resolveSeedMode(src.Mode, ""),
		})
	}

	for _, cms := range ws.ConfigMapSources {
		volName := configMapSourceVolumeName(cms.ConfigMapRef.Name)
		for _, item := range cms.Items {
			entries = append(entries, seedManifestEntry{
				Source: configMapSourceMountFn + volName + "/" + item.Key,
				Target: item.Path,
				Mode:   resolveSeedMode(item.Mode, cms.Mode),
			})
		}
	}

	for i, gs := range ws.GitSources {
		for _, item := range gs.Items {
			entries = append(entries, seedManifestEntry{
				Source: fmt.Sprintf("%s%d/%s", gitSourceMountFn, i, item.RepoPath),
				Target: item.Path,
				Mode:   resolveSeedMode(item.Mode, gs.Mode),
			})
		}
	}

	return entries
}

// injectSeedManifest generates the seeding manifest JSON and writes it into
// the gateway ConfigMap.
func injectSeedManifest(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	manifest := generateSeedManifest(instance.Spec.Workspace)

	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal seed manifest: %w", err)
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	if err := unstructured.SetNestedField(cmObj.Object, string(data), "data", seedManifestKey); err != nil {
		return fmt.Errorf("failed to set seed manifest in ConfigMap: %w", err)
	}
	return nil
}

// injectWorkspaceFiles validates workspace file paths and writes _ws_ prefixed
// keys into the gateway ConfigMap.
func injectWorkspaceFiles(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Workspace == nil || len(instance.Spec.Workspace.InlineSources) == 0 {
		return nil
	}

	paths := make(map[string]string, len(instance.Spec.Workspace.InlineSources))
	for _, src := range instance.Spec.Workspace.InlineSources {
		paths[src.Path] = src.Content
	}
	if err := validateWorkspaceFiles(paths); err != nil {
		return err
	}

	configMapName := getConfigMapName(instance.Name)
	cmObj, err := findObject(objects, ConfigMapKind, configMapName)
	if err != nil {
		return fmt.Errorf("ConfigMap %q not found in manifests", configMapName)
	}

	for _, src := range instance.Spec.Workspace.InlineSources {
		key := workspaceKeyPrefix + encodeWorkspacePath(src.Path)
		if err := unstructured.SetNestedField(cmObj.Object, src.Content, "data", key); err != nil {
			return fmt.Errorf("failed to set workspace file %q in ConfigMap: %w", src.Path, err)
		}
	}
	return nil
}

// validateConfigMapSources checks that all referenced ConfigMaps exist and
// contain the specified keys.
func validateConfigMapSources(ctx context.Context, c client.Reader, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Workspace == nil {
		return nil
	}
	logger := log.FromContext(ctx)
	var errs []string
	for _, cms := range instance.Spec.Workspace.ConfigMapSources {
		cm := &corev1.ConfigMap{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: instance.Namespace, Name: cms.ConfigMapRef.Name}, cm); err != nil {
			logger.Error(err, "failed to get ConfigMap", "configMapName", cms.ConfigMapRef.Name, "namespace", instance.Namespace)
			errs = append(errs, fmt.Sprintf("configMapSource: ConfigMap %q not found in namespace %q", cms.ConfigMapRef.Name, instance.Namespace))
			continue
		}
		for _, item := range cms.Items {
			if _, ok := cm.Data[item.Key]; !ok {
				if _, ok := cm.BinaryData[item.Key]; !ok {
					errs = append(errs, fmt.Sprintf("configMapSource: key %q not found in ConfigMap %q",
						item.Key, cms.ConfigMapRef.Name))
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// configMapSourceVolumeName returns a DNS-1123 volume name for a ConfigMap
// source. When truncation is needed, a short hash suffix is appended to
// avoid collisions between names that share a long prefix.
func configMapSourceVolumeName(cmName string) string {
	const prefix = "ws-cm-"
	const maxLen = 63
	const hashLen = 8 // 8 hex chars from SHA-256
	name := strings.ReplaceAll(cmName, ".", "-")
	if len(prefix)+len(name) <= maxLen {
		return prefix + name
	}
	h := sha256.Sum256([]byte(cmName))
	suffix := hex.EncodeToString(h[:4])
	truncLen := maxLen - len(prefix) - 1 - hashLen // 1 for the dash separator
	name = strings.TrimRight(name[:truncLen], "-")
	return prefix + name + "-" + suffix
}

// injectConfigMapSourceVolumes adds a ConfigMap volume and volumeMount per
// ConfigMap source to the gateway Deployment. The volumes are mounted on the
// init-seed container (added separately).
func injectConfigMapSourceVolumes(objects []*unstructured.Unstructured, instance *clawv1alpha1.Claw) error {
	if instance.Spec.Workspace == nil || len(instance.Spec.Workspace.ConfigMapSources) == 0 {
		return nil
	}

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind {
			continue
		}

		volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")

		for _, cms := range instance.Spec.Workspace.ConfigMapSources {
			volName := configMapSourceVolumeName(cms.ConfigMapRef.Name)
			volumes = append(volumes, map[string]any{
				"name": volName,
				"configMap": map[string]any{
					"name": cms.ConfigMapRef.Name,
				},
			})
		}

		if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
			return fmt.Errorf("failed to set volumes on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes
// using the standard '\” technique so the result is safe to interpolate
// into a shell script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// gitSourceVolumeName returns the emptyDir volume name for a git source by index.
func gitSourceVolumeName(index int) string {
	return fmt.Sprintf("ws-git-%d", index)
}

// isCommitSHA returns true when ref is a full 40-character hex SHA-1.
func isCommitSHA(ref string) bool {
	if len(ref) != commitSHALength {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

// generateGitSyncScript builds a shell script that clones each git source
// into its emptyDir volume. For private repos, credentials are passed via
// GIT_ASKPASS so the token never appears in process arguments.
func generateGitSyncScript(gitSources []clawv1alpha1.GitSource) string {
	var sb strings.Builder
	sb.WriteString("set -e\n")
	for i, gs := range gitSources {
		dest := fmt.Sprintf("%s%d", gitSourceMountFn, i)
		quotedDest := shellQuote(dest)
		var clonePrefix string
		if gs.SecretRef != nil {
			// Write a temporary GIT_ASKPASS script that echoes the token.
			// The username oauth2 is embedded in the URL so git only
			// prompts for the password.
			fmt.Fprintf(&sb, "ASKPASS_%d=$(mktemp)\n", i)
			fmt.Fprintf(&sb, "printf '#!/bin/sh\\necho \"${GIT_TOKEN_%d}\"\\n' > \"$ASKPASS_%d\"\n", i, i)
			fmt.Fprintf(&sb, "chmod +x \"$ASKPASS_%d\"\n", i)
			authURL := strings.Replace(gs.URL, "https://", "https://oauth2@", 1)
			fmt.Fprintf(&sb, "CLONE_URL=%s\n", shellQuote(authURL))
			clonePrefix = fmt.Sprintf("GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=\"$ASKPASS_%d\" ", i)
		} else {
			fmt.Fprintf(&sb, "CLONE_URL=%s\n", shellQuote(gs.URL))
		}
		if gs.Ref != "" && isCommitSHA(gs.Ref) {
			// SHA refs cannot use --branch; fetch the exact commit instead.
			fmt.Fprintf(&sb, "%sgit init %s\n", clonePrefix, quotedDest)
			fmt.Fprintf(&sb, "%sgit -C %s fetch --depth 1 \"${CLONE_URL}\" %s\n",
				clonePrefix, quotedDest, shellQuote(gs.Ref))
			fmt.Fprintf(&sb, "%sgit -C %s checkout FETCH_HEAD\n",
				clonePrefix, quotedDest)
		} else if gs.Ref != "" {
			fmt.Fprintf(&sb, "%sgit clone --depth 1 --branch %s \"${CLONE_URL}\" %s\n",
				clonePrefix, shellQuote(gs.Ref), quotedDest)
		} else {
			fmt.Fprintf(&sb, "%sgit clone --depth 1 \"${CLONE_URL}\" %s\n",
				clonePrefix, quotedDest)
		}
		if gs.SecretRef != nil {
			fmt.Fprintf(&sb, "rm -f \"$ASKPASS_%d\"\n", i)
		}
	}
	return sb.String()
}

// injectGitSyncInitContainer adds the init-git-sync container and emptyDir
// volumes to the gateway Deployment. Only called when gitSources is non-empty.
func injectGitSyncInitContainer(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	gitSyncImage string,
	proxyHost string,
) error {
	if instance.Spec.Workspace == nil || len(instance.Spec.Workspace.GitSources) == 0 {
		return nil
	}

	gitSources := instance.Spec.Workspace.GitSources

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind {
			continue
		}

		initContainers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")
		volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "volumes")

		// Build volumeMounts and env vars for the init container
		var volumeMounts []any
		var envVars []any

		// Proxy env vars
		envVars = append(envVars,
			map[string]any{"name": "HTTP_PROXY", "value": proxyHost},
			map[string]any{"name": "HTTPS_PROXY", "value": proxyHost},
			map[string]any{"name": "NO_PROXY", "value": "localhost,127.0.0.1"},
		)

		// Proxy CA cert mount
		volumeMounts = append(volumeMounts, map[string]any{
			"name":      "proxy-ca",
			"mountPath": "/etc/proxy-ca",
			"readOnly":  true,
		})

		// Git SSL env var for CA cert
		envVars = append(envVars,
			map[string]any{"name": "GIT_SSL_CAINFO", "value": "/etc/proxy-ca/ca.crt"},
		)

		for i, gs := range gitSources {
			volName := gitSourceVolumeName(i)

			// Add emptyDir volume
			volumes = append(volumes, map[string]any{
				"name":     volName,
				"emptyDir": map[string]any{},
			})

			// Mount into init container
			volumeMounts = append(volumeMounts, map[string]any{
				"name":      volName,
				"mountPath": fmt.Sprintf("%s%d", gitSourceMountFn, i),
			})

			// Token env var from secret
			if gs.SecretRef != nil {
				envVars = append(envVars, map[string]any{
					"name": fmt.Sprintf("GIT_TOKEN_%d", i),
					"valueFrom": map[string]any{
						"secretKeyRef": map[string]any{
							"name": gs.SecretRef.Name,
							"key":  gs.SecretRef.Key,
						},
					},
				})
			}
		}

		script := generateGitSyncScript(gitSources)

		container := map[string]any{
			"name":            ClawGitSyncContainerName,
			"image":           gitSyncImage,
			"imagePullPolicy": "IfNotPresent",
			"command":         []any{"sh", "-c", script},
			"env":             envVars,
			"resources": map[string]any{
				"requests": map[string]any{"memory": "64Mi", "cpu": "50m"},
				"limits":   map[string]any{"memory": "256Mi", "cpu": "200m"},
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false,
				"capabilities":             map[string]any{"drop": []any{"ALL"}},
			},
			"volumeMounts": volumeMounts,
		}

		// Insert after wait-for-proxy
		insertIdx := len(initContainers)
		for i, c := range initContainers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if name, _, _ := unstructured.NestedString(cm, "name"); name == "wait-for-proxy" {
				insertIdx = i + 1
				break
			}
		}
		// Insert at position
		initContainers = append(initContainers, nil)
		copy(initContainers[insertIdx+1:], initContainers[insertIdx:])
		initContainers[insertIdx] = container

		if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
			return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
		}
		if err := unstructured.SetNestedSlice(obj.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
			return fmt.Errorf("failed to set volumes on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
}

// seedScript is the shell script that reads the seed manifest and copies
// files to the workspace with mode-aware logic. It uses basic JSON parsing
// via sed since the init container runs in a minimal image.
// seedScript uses node (available in the gateway image) for robust JSON
// parsing, then iterates the entries in shell to copy files with
// mode-aware logic.
const seedScript = `set -e
MANIFEST="/config/_seed_manifest.json"
WORKSPACE="/home/node/.openclaw/workspace"
if [ ! -f "$MANIFEST" ]; then
  echo "[init-seed] no seed manifest found, skipping"
  exit 0
fi
node -e '
  const entries = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
  entries.forEach(e => console.log(e.source + "\t" + e.target + "\t" + e.mode));
' "$MANIFEST" | while IFS="$(printf '\t')" read -r src tgt mode; do
  dest="$WORKSPACE/$tgt"
  mkdir -p "$(dirname "$dest")"
  if [ "$mode" = "seedIfMissing" ] && [ -f "$dest" ]; then
    echo "[init-seed] skip (exists): $tgt"
    continue
  fi
  if [ ! -f "$src" ]; then
    echo "[init-seed] WARN: source not found: $src"
    continue
  fi
  cp "$src" "$dest"
  echo "[init-seed] seeded: $tgt (mode=$mode)"
done
echo "[init-seed] done"
`

// injectSeedInitContainer adds the init-seed container to the gateway Deployment.
// It mounts the gateway ConfigMap, all ConfigMap source volumes, all git emptyDirs,
// and the PVC workspace. It is always injected (after init-git-sync or wait-for-proxy).
func injectSeedInitContainer(
	objects []*unstructured.Unstructured,
	instance *clawv1alpha1.Claw,
	gatewayImage string,
) error {
	ws := instance.Spec.Workspace

	for _, obj := range objects {
		if obj.GetKind() != DeploymentKind {
			continue
		}

		initContainers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")

		var volumeMounts []any

		// Config volume (has _seed_manifest.json and _ws_* keys)
		volumeMounts = append(volumeMounts, map[string]any{
			"name":      "config",
			"mountPath": configMountPath,
		})

		// PVC workspace
		volumeMounts = append(volumeMounts, map[string]any{
			"name":      "claw-home",
			"mountPath": "/home/node/.openclaw",
			"subPath":   "home",
		})

		// ConfigMap source volumes
		if ws != nil {
			for _, cms := range ws.ConfigMapSources {
				volName := configMapSourceVolumeName(cms.ConfigMapRef.Name)
				volumeMounts = append(volumeMounts, map[string]any{
					"name":      volName,
					"mountPath": configMapSourceMountFn + volName + "/",
					"readOnly":  true,
				})
			}

			// Git emptyDir volumes
			for i := range ws.GitSources {
				volName := gitSourceVolumeName(i)
				volumeMounts = append(volumeMounts, map[string]any{
					"name":      volName,
					"mountPath": fmt.Sprintf("%s%d", gitSourceMountFn, i),
					"readOnly":  true,
				})
			}
		}

		container := map[string]any{
			"name":            ClawSeedContainerName,
			"image":           gatewayImage,
			"imagePullPolicy": "IfNotPresent",
			"command":         []any{"sh", "-c", seedScript},
			"resources": map[string]any{
				"requests": map[string]any{"memory": "32Mi", "cpu": "10m"},
				"limits":   map[string]any{"memory": "64Mi", "cpu": "100m"},
			},
			"securityContext": map[string]any{
				"allowPrivilegeEscalation": false,
				"capabilities":             map[string]any{"drop": []any{"ALL"}},
			},
			"volumeMounts": volumeMounts,
		}

		// Insert after init-git-sync if present, otherwise after wait-for-proxy
		insertIdx := len(initContainers)
		for i, c := range initContainers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(cm, "name")
			if name == ClawGitSyncContainerName {
				insertIdx = i + 1
				break
			}
			if name == "wait-for-proxy" {
				insertIdx = i + 1
			}
		}

		initContainers = append(initContainers, nil)
		copy(initContainers[insertIdx+1:], initContainers[insertIdx:])
		initContainers[insertIdx] = container

		if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
			return fmt.Errorf("failed to set init containers on claw deployment: %w", err)
		}
		return nil
	}
	return fmt.Errorf("claw deployment not found in manifests")
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
