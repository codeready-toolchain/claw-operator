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

// TestGeneratePluginInstallScript (claw_plugins_test.go) only ever
// string-matches the generated script; it can prove the right substrings are
// present but not that the shell control flow — manifest-based cleanup, the
// before/after directory diff, or the shell-quoting of untrusted plugin
// names — actually behaves correctly when a real shell runs it. That gap is
// exactly how the npm/projects cache idempotency bug (see
// generatePluginInstallScript's own comments) slipped through once already.
//
// These tests execute the real generated script under sh, redirecting its
// two hardcoded absolute paths (extensions dir, npm project cache) into a
// temp directory via string replacement — the same technique
// claw_merge_test.go and claw_seed_script_test.go use. The real `openclaw`
// CLI is replaced by a tiny fake on $PATH that just materializes a directory
// per "install" call, which is all the script's own logic depends on.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOpenclawScript stands in for the real openclaw CLI's
// `plugins install <pkg>` subcommand: it materializes a directory under
// $FAKE_EXT named after a filesystem-safe encoding of the package spec it
// was given, so tests can assert on exactly what the real script asked to
// have installed.
//
// It also stands in for `plugins registry --json` and `plugins uninstall
// <id>`, backed by a flat "id\tpackage" file at $FAKE_REGISTRY: `install`
// appends a record keyed by a fake id derived from the package name,
// `registry --json` renders those records as the real CLI's
// persisted.installRecords shape, and `uninstall` removes the matching
// record and its $FAKE_EXT directory — mirroring the real CLI's behavior of
// cleaning up the npm project and the config registration together.
const fakeOpenclawScript = `#!/bin/sh
set -e
if [ "$1" = "plugins" ] && [ "$2" = "install" ]; then
  safe=$(printf '%s' "$3" | tr -c 'a-zA-Z0-9_.-' '_')
  mkdir -p "$FAKE_EXT/$safe"
  touch "$FAKE_EXT/$safe/.installed"
  pkg=$(printf '%s' "$3" | sed 's/@[^@/]*$//')
  printf '%s\t%s\n' "$safe" "$pkg" >> "$FAKE_REGISTRY"
  exit 0
fi
if [ "$1" = "plugins" ] && [ "$2" = "registry" ]; then
  printf '{"persisted":{"installRecords":{'
  first=1
  if [ -f "$FAKE_REGISTRY" ]; then
    while IFS="$(printf '\t')" read -r id pkg; do
      [ -z "$id" ] && continue
      [ "$first" = 1 ] || printf ','
      first=0
      printf '"%s":{"resolvedName":"%s"}' "$id" "$pkg"
    done < "$FAKE_REGISTRY"
  fi
  printf '}}}\n'
  exit 0
fi
if [ "$1" = "plugins" ] && [ "$2" = "uninstall" ]; then
  id="$3"
  rm -rf "$FAKE_EXT/$id"
  if [ -f "$FAKE_REGISTRY" ]; then
    grep -v "^$id	" "$FAKE_REGISTRY" > "$FAKE_REGISTRY.tmp" 2>/dev/null || true
    mv "$FAKE_REGISTRY.tmp" "$FAKE_REGISTRY"
  fi
  exit 0
fi
echo "unexpected fake openclaw invocation: $*" >&2
exit 1
`

// sanitizePluginDirName mirrors fakeOpenclawScript's own sanitization so
// tests can predict the directory name a given plugin spec produces.
func sanitizePluginDirName(pkg string) string {
	var b strings.Builder
	for _, r := range pkg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

type pluginScriptResult struct {
	stdout, stderr       string
	extDir               string
	manifestPath         string
	registryPath         string
	registryManifestPath string
}

// runPluginInstallScript generates the real install script for plugins,
// pre-seeds a fake extensions directory per existingManifest/preExistingDirs,
// a fake *live* openclaw plugin registry per preExistingLiveRegistry, and
// the operator's own record of what it previously installed per
// preExistingOperatorManifest, then executes it for real against the
// fakeOpenclawScript stand-in.
// existingManifest == nil means no .operator-managed manifest file is
// written at all (exercising the "no prior manifest" cleanup branch).
// preExistingLiveRegistry and preExistingOperatorManifest entries are
// "id\tpackageName" lines. They are deliberately separate: the live registry
// simulates everything openclaw currently has installed (from any source),
// while the operator manifest simulates only what the operator itself
// recorded installing in a previous run — the script must only ever
// uninstall entries present in the latter.
func runPluginInstallScript(
	t *testing.T,
	plugins []string,
	existingManifest []string,
	preExistingExtDirs []string,
	npmProjectsExists bool,
	preExistingLiveRegistry []string,
	preExistingOperatorManifest []string,
) pluginScriptResult {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found in PATH, skipping plugin install script tests")
	}

	tmpDir := t.TempDir()
	extDir := filepath.Join(tmpDir, "extensions")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	npmProjectsDir := filepath.Join(tmpDir, "npm-projects")

	for _, d := range preExistingExtDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(extDir, d), 0o755))
	}
	manifestPath := filepath.Join(extDir, ".operator-managed")
	if existingManifest != nil {
		require.NoError(t, os.WriteFile(manifestPath, []byte(strings.Join(existingManifest, "\n")+"\n"), 0o644))
	}
	if npmProjectsExists {
		require.NoError(t, os.MkdirAll(npmProjectsDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(npmProjectsDir, "stale-cache-entry"), []byte("stale"), 0o644))
	}

	fakeBinDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "openclaw"), []byte(fakeOpenclawScript), 0o755))
	fakeRegistry := filepath.Join(tmpDir, "registry.tsv")
	if preExistingLiveRegistry != nil {
		require.NoError(t, os.WriteFile(fakeRegistry, []byte(strings.Join(preExistingLiveRegistry, "\n")+"\n"), 0o644))
	}
	registryManifestPath := filepath.Join(tmpDir, "operator-managed-plugins")
	if preExistingOperatorManifest != nil {
		require.NoError(t, os.WriteFile(registryManifestPath,
			[]byte(strings.Join(preExistingOperatorManifest, "\n")+"\n"), 0o644))
	}

	script := generatePluginInstallScript(plugins)
	require.Contains(t, script, `EXT="/home/node/.openclaw/extensions"`,
		"generatePluginInstallScript EXT anchor changed — update this test's path substitution")
	require.Contains(t, script, `rm -rf "/home/node/.openclaw/npm/projects"`,
		"generatePluginInstallScript npm cache anchor changed — update this test's path substitution")
	require.Contains(t, script, `REGISTRY_MANIFEST="/home/node/.openclaw/.operator-managed-plugins"`,
		"generatePluginInstallScript REGISTRY_MANIFEST anchor changed — update this test's path substitution")
	script = strings.Replace(script, "/home/node/.openclaw/extensions", extDir, 1)
	script = strings.Replace(script, `rm -rf "/home/node/.openclaw/npm/projects"`,
		fmt.Sprintf("rm -rf %q", npmProjectsDir), 1)
	script = strings.Replace(script, "/home/node/.openclaw/.operator-managed-plugins", registryManifestPath, 1)

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping plugin install script tests")
	}
	cmd := exec.Command("sh", "-c", script) //nolint:gosec
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBinDir+":"+os.Getenv("PATH"), "FAKE_EXT="+extDir, "FAKE_REGISTRY="+fakeRegistry)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "plugin install script failed: stdout=%s stderr=%s", stdout.String(), stderr.String())

	return pluginScriptResult{
		stdout: stdout.String(), stderr: stderr.String(),
		extDir: extDir, manifestPath: manifestPath,
		registryPath: fakeRegistry, registryManifestPath: registryManifestPath,
	}
}

func readManifestLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "manifest file should exist after install")
	return strings.Fields(string(data))
}

func TestGeneratePluginInstallScriptExecution(t *testing.T) {
	t.Run("installs a plugin and records exactly it in the manifest", func(t *testing.T) {
		result := runPluginInstallScript(t, []string{"@openclaw/matrix"}, nil, nil, false, nil, nil)

		dirName := sanitizePluginDirName("@openclaw/matrix")
		_, err := os.Stat(filepath.Join(result.extDir, dirName, ".installed"))
		require.NoError(t, err, "fake openclaw should have created the plugin dir")

		assert.Equal(t, []string{dirName}, readManifestLines(t, result.manifestPath))
	})

	t.Run("removes only manifest-listed dirs and leaves unmanaged dirs alone", func(t *testing.T) {
		result := runPluginInstallScript(t, []string{"@openclaw/new-plugin"},
			[]string{"old-plugin-dir"},
			[]string{"old-plugin-dir", "user-created-dir"},
			false, nil, nil)

		_, err := os.Stat(filepath.Join(result.extDir, "old-plugin-dir"))
		assert.True(t, os.IsNotExist(err), "manifest-listed dir from a previous install should be removed")

		_, err = os.Stat(filepath.Join(result.extDir, "user-created-dir"))
		assert.NoError(t, err, "a dir not tracked by the manifest should be left untouched")

		newDirName := sanitizePluginDirName("@openclaw/new-plugin")
		manifest := readManifestLines(t, result.manifestPath)
		assert.Equal(t, []string{newDirName}, manifest,
			"new manifest should record only what was actually installed this run, not pre-existing untracked dirs")
	})

	t.Run("wipes all extension dirs when no manifest exists (orphan cleanup)", func(t *testing.T) {
		result := runPluginInstallScript(t, []string{"@openclaw/x"}, nil,
			[]string{"orphan1", "orphan2"}, false, nil, nil)

		for _, orphan := range []string{"orphan1", "orphan2"} {
			_, err := os.Stat(filepath.Join(result.extDir, orphan))
			assert.True(t, os.IsNotExist(err), "orphaned dir %q should be wiped when no manifest is present", orphan)
		}
		assert.Equal(t, []string{sanitizePluginDirName("@openclaw/x")}, readManifestLines(t, result.manifestPath))
	})

	t.Run("unconditionally wipes the npm project install cache", func(t *testing.T) {
		result := runPluginInstallScript(t, []string{"@openclaw/matrix"}, nil, nil, true, nil, nil)

		// npmProjectsDir is a sibling of extDir under the same temp root,
		// matching how runPluginInstallScript lays out its fixtures.
		npmProjectsDir := filepath.Join(filepath.Dir(result.extDir), "npm-projects")
		_, err := os.Stat(npmProjectsDir)
		assert.True(t, os.IsNotExist(err),
			"npm project cache must be wiped unconditionally to avoid stale 'plugin already exists' errors")
	})

	t.Run("uninstalls an operator-managed registry-tracked plugin no longer desired, even with no $EXT footprint",
		func(t *testing.T) {
			// Simulates a provider plugin (e.g. the Vertex AI SDK providers)
			// that openclaw tracks purely in its registry, never as a
			// directory under $EXT — the pre-existing registry entry has no
			// corresponding preExistingExtDirs entry. Both the live registry
			// and the operator's own manifest agree the operator installed
			// it previously, so it's safe to uninstall.
			liveRegistry := []string{"anthropic-vertex\t@openclaw/anthropic-vertex-provider"}
			result := runPluginInstallScript(t, []string{"@openclaw/new-plugin"}, nil, nil, false,
				liveRegistry, liveRegistry)

			registryContent, err := os.ReadFile(result.registryPath)
			require.NoError(t, err)
			assert.NotContains(t, string(registryContent), "anthropic-vertex",
				"an operator-managed registry-tracked plugin no longer desired should be uninstalled via the CLI")
			assert.Contains(t, string(registryContent), sanitizePluginDirName("@openclaw/new-plugin"),
				"the currently desired plugin should still be recorded in the registry")

			newManifest, err := os.ReadFile(result.registryManifestPath)
			require.NoError(t, err)
			assert.NotContains(t, string(newManifest), "anthropic-vertex",
				"the rebuilt operator manifest must drop entries that are no longer desired")
			assert.Contains(t, string(newManifest), sanitizePluginDirName("@openclaw/new-plugin"),
				"the rebuilt operator manifest should record the newly installed plugin")
		})

	t.Run("leaves an operator-managed registry-tracked plugin alone when it is still desired", func(t *testing.T) {
		liveRegistry := []string{"anthropic-vertex\t@openclaw/anthropic-vertex-provider"}
		result := runPluginInstallScript(t, []string{"@openclaw/anthropic-vertex-provider@2026.7.1"}, nil, nil, false,
			liveRegistry, liveRegistry)

		registryContent, err := os.ReadFile(result.registryPath)
		require.NoError(t, err)
		assert.Contains(t, string(registryContent), "anthropic-vertex",
			"a still-desired registry-tracked plugin must not be uninstalled")

		newManifest, err := os.ReadFile(result.registryManifestPath)
		require.NoError(t, err)
		assert.Contains(t, string(newManifest), "anthropic-vertex",
			"a still-desired plugin should remain recorded in the rebuilt operator manifest")
	})

	t.Run("never uninstalls a registry entry the operator did not itself install", func(t *testing.T) {
		// The live registry has a plugin (e.g. installed directly by a user
		// or via some other channel outside the operator's control) that is
		// NOT in the operator's own manifest — simulating no prior manifest
		// entry for it at all. Even though it's not in the current desired
		// list either, the script must leave it alone: only entries the
		// operator itself previously recorded are candidates for cleanup.
		result := runPluginInstallScript(t, []string{"@openclaw/new-plugin"}, nil, nil, false,
			[]string{"user-installed\t@some-org/user-plugin"}, nil)

		registryContent, err := os.ReadFile(result.registryPath)
		require.NoError(t, err)
		assert.Contains(t, string(registryContent), "user-installed",
			"a plugin present in the live registry but absent from the operator's own manifest must never be uninstalled")
	})

	t.Run("does not execute shell metacharacters embedded in a plugin name", func(t *testing.T) {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("sh not found in PATH")
		}
		tmpDir := t.TempDir()
		canary := filepath.Join(tmpDir, "pwned")

		extDir := filepath.Join(tmpDir, "extensions")
		require.NoError(t, os.MkdirAll(extDir, 0o755))
		fakeBinDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "openclaw"), []byte(fakeOpenclawScript), 0o755))

		maliciousPlugin := `x'; touch "$CANARY"; echo '`
		script := generatePluginInstallScript([]string{maliciousPlugin})
		script = strings.Replace(script, "/home/node/.openclaw/extensions", extDir, 1)
		script = strings.Replace(script, `rm -rf "/home/node/.openclaw/npm/projects"`,
			fmt.Sprintf("rm -rf %q", filepath.Join(tmpDir, "npm-projects")), 1)
		script = strings.Replace(script, "/home/node/.openclaw/.operator-managed-plugins",
			filepath.Join(tmpDir, "operator-managed-plugins"), 1)

		cmd := exec.Command("sh", "-c", script) //nolint:gosec
		cmd.Env = append(os.Environ(),
			"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
			"FAKE_EXT="+extDir,
			"FAKE_REGISTRY="+filepath.Join(tmpDir, "registry.tsv"),
			"CANARY="+canary,
		)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		require.NoError(t, err, "script should still succeed by treating the payload as an opaque package name: "+
			"stdout=%s stderr=%s", stdout.String(), stderr.String())

		_, statErr := os.Stat(canary)
		assert.True(t, os.IsNotExist(statErr),
			"shell metacharacters in a plugin name must never execute — canary file should not have been created")
	})

	t.Run("does not delete outside $EXT even if a manifest entry contains a path-traversal segment", func(t *testing.T) {
		tmpDir := t.TempDir()
		extDir := filepath.Join(tmpDir, "extensions")
		require.NoError(t, os.MkdirAll(extDir, 0o755))

		outside := filepath.Join(tmpDir, "evil-outside")
		require.NoError(t, os.MkdirAll(outside, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(outside, "marker"), []byte("still here"), 0o644))

		require.NoError(t, os.WriteFile(filepath.Join(extDir, ".operator-managed"),
			[]byte("../evil-outside\n"), 0o644))

		fakeBinDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(fakeBinDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(fakeBinDir, "openclaw"), []byte(fakeOpenclawScript), 0o755))

		script := generatePluginInstallScript([]string{"@openclaw/matrix"})
		script = strings.Replace(script, "/home/node/.openclaw/extensions", extDir, 1)
		script = strings.Replace(script, `rm -rf "/home/node/.openclaw/npm/projects"`,
			fmt.Sprintf("rm -rf %q", filepath.Join(tmpDir, "npm-projects")), 1)
		script = strings.Replace(script, "/home/node/.openclaw/.operator-managed-plugins",
			filepath.Join(tmpDir, "operator-managed-plugins"), 1)

		cmd := exec.Command("sh", "-c", script) //nolint:gosec
		cmd.Env = append(os.Environ(),
			"PATH="+fakeBinDir+":"+os.Getenv("PATH"),
			"FAKE_EXT="+extDir,
			"FAKE_REGISTRY="+filepath.Join(tmpDir, "registry.tsv"),
		)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		require.NoError(t, err, "stdout=%s stderr=%s", stdout.String(), stderr.String())

		_, err = os.Stat(filepath.Join(outside, "marker"))
		assert.NoError(t, err, "path-traversal manifest entry must not cause deletion outside $EXT")
	})
}
