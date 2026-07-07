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

// seedScript (internal/controller/claw_plugins.go) had zero test coverage of
// any kind before this file: not even string-matching. It's real logic that
// runs in the init-seed container on every single pod boot (shell control
// flow, tab-delimited IFS parsing via an embedded `node -e` one-liner,
// seedIfMissing vs overwrite semantics), so it deserves the same "actually
// execute it" treatment claw_merge_test.go already gives merge.js.
//
// The two hardcoded absolute paths the real script uses in production
// (MANIFEST, WORKSPACE) are redirected to per-test temp directories via
// targeted string replacement before execution — the same technique
// claw_merge_test.go uses for merge.js's configDir/pvcDir. Manifest "source"
// entries already are, by design, absolute paths the real script just
// `cp`'s from, so tests can point them straight at temp fixture files
// without any further indirection.

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
)

type seedScriptResult struct {
	stdout       string
	workspaceDir string
}

// runSeedScript executes the real seedScript constant under sh, with MANIFEST
// and WORKSPACE redirected into tmpDir. Passing a nil manifest skips writing
// the manifest file entirely, exercising the "no manifest found" fast path;
// pass an empty (non-nil) slice to exercise an empty-but-present manifest.
func runSeedScript(t *testing.T, tmpDir string, manifest []seedManifestEntry) seedScriptResult {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping seedScript tests")
	}

	workspaceDir := filepath.Join(tmpDir, "workspace")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o755))

	manifestPath := filepath.Join(tmpDir, "_seed_manifest.json")
	if manifest != nil {
		data, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(manifestPath, data, 0o644))
	}

	script := seedScript
	require.Contains(t, script, `MANIFEST="/config/_seed_manifest.json"`,
		"seedScript MANIFEST anchor changed — update this test's path substitution")
	require.Contains(t, script, `WORKSPACE="/home/node/.openclaw/workspace"`,
		"seedScript WORKSPACE anchor changed — update this test's path substitution")
	script = strings.Replace(script, `MANIFEST="/config/_seed_manifest.json"`,
		fmt.Sprintf("MANIFEST=%q", manifestPath), 1)
	script = strings.Replace(script, `WORKSPACE="/home/node/.openclaw/workspace"`,
		fmt.Sprintf("WORKSPACE=%q", workspaceDir), 1)

	cmd := exec.Command("sh", "-c", script) //nolint:gosec
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "seedScript failed: stdout=%s stderr=%s", stdout.String(), stderr.String())

	return seedScriptResult{stdout: stdout.String(), workspaceDir: workspaceDir}
}

func writeSourceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestSeedScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found in PATH, skipping seedScript tests")
	}

	t.Run("seeds a missing target file", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := writeSourceFile(t, tmpDir, "AGENTS.md", "hello agents")

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: src, Target: "AGENTS.md", Mode: "seedIfMissing"},
		})

		content, err := os.ReadFile(filepath.Join(result.workspaceDir, "AGENTS.md"))
		require.NoError(t, err)
		assert.Equal(t, "hello agents", string(content))
		assert.Contains(t, result.stdout, "seeded: AGENTS.md")
	})

	t.Run("seedIfMissing skips an existing target and preserves its content", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := writeSourceFile(t, tmpDir, "SOUL.md", "operator default soul")
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "workspace"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "workspace", "SOUL.md"), []byte("user's own soul"), 0o644))

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: src, Target: "SOUL.md", Mode: "seedIfMissing"},
		})

		content, err := os.ReadFile(filepath.Join(result.workspaceDir, "SOUL.md"))
		require.NoError(t, err)
		assert.Equal(t, "user's own soul", string(content), "existing user file must not be overwritten")
		assert.Contains(t, result.stdout, "skip (exists): SOUL.md")
	})

	t.Run("overwrite mode always replaces the target", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := writeSourceFile(t, tmpDir, "TOOLS.md", "new tools content")
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "workspace"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "workspace", "TOOLS.md"), []byte("old tools content"), 0o644))

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: src, Target: "TOOLS.md", Mode: "overwrite"},
		})

		content, err := os.ReadFile(filepath.Join(result.workspaceDir, "TOOLS.md"))
		require.NoError(t, err)
		assert.Equal(t, "new tools content", string(content))
	})

	t.Run("creates nested target directories on demand", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := writeSourceFile(t, tmpDir, "nested.md", "nested content")

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: src, Target: "a/b/c/nested.md", Mode: "overwrite"},
		})

		content, err := os.ReadFile(filepath.Join(result.workspaceDir, "a", "b", "c", "nested.md"))
		require.NoError(t, err)
		assert.Equal(t, "nested content", string(content))
	})

	t.Run("warns and continues past a missing source without failing the script", func(t *testing.T) {
		tmpDir := t.TempDir()
		goodSrc := writeSourceFile(t, tmpDir, "good.md", "good content")
		missingSrc := filepath.Join(tmpDir, "does-not-exist.md")

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: missingSrc, Target: "missing.md", Mode: "overwrite"},
			{Source: goodSrc, Target: "good.md", Mode: "overwrite"},
		})

		assert.Contains(t, result.stdout, "WARN: source not found")
		_, err := os.Stat(filepath.Join(result.workspaceDir, "missing.md"))
		assert.True(t, os.IsNotExist(err), "no file should be created for a missing source")

		content, err := os.ReadFile(filepath.Join(result.workspaceDir, "good.md"))
		require.NoError(t, err, "processing must continue to subsequent manifest entries after a missing source")
		assert.Equal(t, "good content", string(content))
	})

	t.Run("handles multiple manifest entries independently", func(t *testing.T) {
		tmpDir := t.TempDir()
		src1 := writeSourceFile(t, tmpDir, "one.md", "content one")
		src2 := writeSourceFile(t, tmpDir, "two.md", "content two")

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: src1, Target: "one.md", Mode: "overwrite"},
			{Source: src2, Target: "two.md", Mode: "overwrite"},
		})

		c1, err := os.ReadFile(filepath.Join(result.workspaceDir, "one.md"))
		require.NoError(t, err)
		assert.Equal(t, "content one", string(c1))
		c2, err := os.ReadFile(filepath.Join(result.workspaceDir, "two.md"))
		require.NoError(t, err)
		assert.Equal(t, "content two", string(c2))
	})

	t.Run("skips gracefully when no manifest file exists at all", func(t *testing.T) {
		tmpDir := t.TempDir()

		result := runSeedScript(t, tmpDir, nil)

		assert.Contains(t, result.stdout, "no seed manifest found, skipping")
		entries, err := os.ReadDir(result.workspaceDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "workspace should be untouched when there is no manifest")
	})

	t.Run("preserves spaces in target paths despite tab-delimited IFS parsing", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := writeSourceFile(t, tmpDir, "notes.md", "note content")

		result := runSeedScript(t, tmpDir, []seedManifestEntry{
			{Source: src, Target: "my notes/AGENTS.md", Mode: "overwrite"},
		})

		content, err := os.ReadFile(filepath.Join(result.workspaceDir, "my notes", "AGENTS.md"))
		require.NoError(t, err, "target path containing a space must be handled correctly by the tab-only IFS read loop")
		assert.Equal(t, "note content", string(content))
	})
}
