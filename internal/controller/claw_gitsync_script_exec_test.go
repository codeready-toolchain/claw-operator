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

// TestGenerateGitSyncScript (claw_workspace_test.go) only ever string-matches
// the generated script. These tests instead execute it for real under sh
// against real local git repositories (file:// transport — no network
// access required), which is the only way to actually prove the shallow
// clone / SHA pinning / branch selection logic works, not just that the
// right substrings appear in the script text.
//
// The three hardcoded absolute path families the real script depends on
// (/etc/proxy-ca/ca.crt, /tmp/combined-ca.crt, /git-sources/<i>) are
// redirected into per-test temp directories via string replacement, the
// same technique used throughout this package's other script-execution
// tests (see claw_merge_test.go, claw_seed_script_test.go).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clawv1alpha1 "github.com/codeready-toolchain/claw-operator/api/v1alpha1"
)

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), string(out))
	return strings.TrimSpace(string(out))
}

// gitFixture is a local repo with three points of reference: the initial
// commit SHA (content "v1"), a second commit on main (content "v2", HEAD),
// and a "feature" branch (content "feature").
type gitFixture struct {
	repoPath  string
	firstSHA  string
	secondSHA string
}

func createGitFixture(t *testing.T) gitFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH, skipping git-sync execution tests")
	}

	repoPath := t.TempDir()
	runGitCmd(t, repoPath, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "SOUL.md"), []byte("v1"), 0o644))
	runGitCmd(t, repoPath, "add", ".")
	runGitCmd(t, repoPath, "commit", "-q", "-m", "c1")
	firstSHA := runGitCmd(t, repoPath, "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "SOUL.md"), []byte("v2"), 0o644))
	runGitCmd(t, repoPath, "add", ".")
	runGitCmd(t, repoPath, "commit", "-q", "-m", "c2")
	secondSHA := runGitCmd(t, repoPath, "rev-parse", "HEAD")

	runGitCmd(t, repoPath, "checkout", "-q", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "SOUL.md"), []byte("feature"), 0o644))
	runGitCmd(t, repoPath, "add", ".")
	runGitCmd(t, repoPath, "commit", "-q", "-m", "c3")
	runGitCmd(t, repoPath, "checkout", "-q", "main")

	return gitFixture{repoPath: repoPath, firstSHA: firstSHA, secondSHA: secondSHA}
}

type gitSyncScriptResult struct {
	stdout, stderr string
	sourcesDir     string
}

// runGitSyncScript generates the real script for gitSources, redirects its
// hardcoded paths into tmpDir, and executes it for real via sh. extraEnv is
// merged into the child process environment (used for GIT_TOKEN_N and test
// canaries).
func runGitSyncScript(t *testing.T, gitSources []clawv1alpha1.GitSource, extraEnv ...string) gitSyncScriptResult {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH, skipping git-sync execution tests")
	}

	tmpDir := t.TempDir()
	sourcesDir := filepath.Join(tmpDir, "git-sources")
	require.NoError(t, os.MkdirAll(sourcesDir, 0o755))

	fakeCA := filepath.Join(tmpDir, "fake-ca.crt")
	require.NoError(t, os.WriteFile(fakeCA, []byte("dummy CA cert\n"), 0o644))
	combinedCA := filepath.Join(tmpDir, "combined-ca.crt")

	script := generateGitSyncScript(gitSources)
	require.Contains(t, script, "/etc/proxy-ca/ca.crt",
		"generateGitSyncScript proxy CA anchor changed — update this test's path substitution")
	require.Contains(t, script, "/tmp/combined-ca.crt",
		"generateGitSyncScript combined CA anchor changed — update this test's path substitution")
	require.Contains(t, script, "/git-sources/",
		"generateGitSyncScript destination anchor changed — update this test's path substitution")
	script = strings.ReplaceAll(script, "/etc/proxy-ca/ca.crt", fakeCA)
	script = strings.ReplaceAll(script, "/tmp/combined-ca.crt", combinedCA)
	script = strings.ReplaceAll(script, "/git-sources/", sourcesDir+"/")

	cmd := exec.Command("sh", "-c", script) //nolint:gosec
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "git sync script failed: stdout=%s stderr=%s", stdout.String(), stderr.String())

	return gitSyncScriptResult{stdout: stdout.String(), stderr: stderr.String(), sourcesDir: sourcesDir}
}

func TestGitSyncScriptExecution(t *testing.T) {
	t.Run("clones the default branch when no ref is given", func(t *testing.T) {
		fixture := createGitFixture(t)

		result := runGitSyncScript(t, []clawv1alpha1.GitSource{
			{URL: "file://" + fixture.repoPath},
		})

		content, err := os.ReadFile(filepath.Join(result.sourcesDir, "0", "SOUL.md"))
		require.NoError(t, err)
		assert.Equal(t, "v2", string(content), "should clone the tip of the default branch")
	})

	t.Run("checks out the requested branch, not the default", func(t *testing.T) {
		fixture := createGitFixture(t)

		result := runGitSyncScript(t, []clawv1alpha1.GitSource{
			{URL: "file://" + fixture.repoPath, Ref: "feature"},
		})

		content, err := os.ReadFile(filepath.Join(result.sourcesDir, "0", "SOUL.md"))
		require.NoError(t, err)
		assert.Equal(t, "feature", string(content))
	})

	t.Run("pins to an exact commit SHA, not the branch tip", func(t *testing.T) {
		fixture := createGitFixture(t)

		result := runGitSyncScript(t, []clawv1alpha1.GitSource{
			{URL: "file://" + fixture.repoPath, Ref: fixture.firstSHA},
		})

		content, err := os.ReadFile(filepath.Join(result.sourcesDir, "0", "SOUL.md"))
		require.NoError(t, err, "stdout=%s stderr=%s", result.stdout, result.stderr)
		assert.Equal(t, "v1", string(content),
			"SHA ref must fetch that exact commit, even though a newer commit exists on the branch")
	})

	t.Run("clones multiple sources independently into their own destinations", func(t *testing.T) {
		fixtureA := createGitFixture(t)
		fixtureB := createGitFixture(t)

		result := runGitSyncScript(t, []clawv1alpha1.GitSource{
			{URL: "file://" + fixtureA.repoPath, Ref: "feature"},
			{URL: "file://" + fixtureB.repoPath, Ref: fixtureB.firstSHA},
		})

		contentA, err := os.ReadFile(filepath.Join(result.sourcesDir, "0", "SOUL.md"))
		require.NoError(t, err)
		assert.Equal(t, "feature", string(contentA))

		contentB, err := os.ReadFile(filepath.Join(result.sourcesDir, "1", "SOUL.md"))
		require.NoError(t, err)
		assert.Equal(t, "v1", string(contentB))
	})

	t.Run("private repo path clones successfully with the full ASKPASS scaffolding active", func(t *testing.T) {
		fixture := createGitFixture(t)

		// file:// transport never actually prompts for credentials, but this
		// still exercises every line of the SecretRef branch for real —
		// mktemp, the printf'd askpass script, chmod +x, and the rm -f
		// cleanup — proving there's no syntax or quoting error in that path.
		result := runGitSyncScript(t, []clawv1alpha1.GitSource{
			{
				URL: "file://" + fixture.repoPath,
				SecretRef: &clawv1alpha1.SecretRefEntry{
					Name: "git-creds",
					Key:  "token",
				},
			},
		}, "GIT_TOKEN_0=fake-token-value")

		content, err := os.ReadFile(filepath.Join(result.sourcesDir, "0", "SOUL.md"))
		require.NoError(t, err, "stdout=%s stderr=%s", result.stdout, result.stderr)
		assert.Equal(t, "v2", string(content))
	})

	t.Run("askpass helper echoes exactly the configured token", func(t *testing.T) {
		// Rather than requiring a full authenticating git server to exercise
		// this over the wire, render just the askpass helper fragment the
		// real script generates and invoke it directly with GIT_TOKEN_0 set
		// — this is the actual security-sensitive logic (credentials must
		// never leak into argv or the clone URL) and it's fully testable in
		// isolation this way.
		script := generateGitSyncScript([]clawv1alpha1.GitSource{
			{
				URL:       "https://git.example.com/team/repo.git",
				SecretRef: &clawv1alpha1.SecretRefEntry{Name: "git-creds", Key: "token"},
			},
		})
		require.Contains(t, script, `printf '#!/bin/sh\necho "${GIT_TOKEN_0}"\n'`)

		tmpDir := t.TempDir()
		askpassPath := filepath.Join(tmpDir, "askpass.sh")
		setupScript := fmt.Sprintf(`printf '#!/bin/sh\necho "${GIT_TOKEN_0}"\n' > %q && chmod +x %q`,
			askpassPath, askpassPath)
		cmd := exec.Command("sh", "-c", setupScript) //nolint:gosec
		require.NoError(t, cmd.Run())

		// The rendered helper reads GIT_TOKEN_0 from its own environment.
		runCmd := exec.Command(askpassPath) //nolint:gosec
		runCmd.Env = append(os.Environ(), "GIT_TOKEN_0=super-secret-token")
		out, err := runCmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "super-secret-token\n", string(out))
	})

	t.Run("does not execute shell metacharacters embedded in a ref", func(t *testing.T) {
		fixture := createGitFixture(t)
		tmpDir := t.TempDir()
		canary := filepath.Join(tmpDir, "pwned")

		// An invalid ref just fails the clone (expected); the point is that
		// it fails as a *git error*, not by running the injected command.
		_ = runGitSyncScriptAllowFailure(t, []clawv1alpha1.GitSource{
			{URL: "file://" + fixture.repoPath, Ref: `nonexistent'; touch "$CANARY"; echo '`},
		}, "CANARY="+canary)

		_, statErr := os.Stat(canary)
		assert.True(t, os.IsNotExist(statErr),
			"shell metacharacters in a ref must never execute — canary file should not have been created")
	})
}

// runGitSyncScriptAllowFailure is like runGitSyncScript but tolerates the
// script exiting non-zero (used for negative/injection tests where the
// clone is expected to fail).
func runGitSyncScriptAllowFailure(t *testing.T, gitSources []clawv1alpha1.GitSource, extraEnv ...string) gitSyncScriptResult {
	t.Helper()
	tmpDir := t.TempDir()
	sourcesDir := filepath.Join(tmpDir, "git-sources")
	require.NoError(t, os.MkdirAll(sourcesDir, 0o755))

	fakeCA := filepath.Join(tmpDir, "fake-ca.crt")
	require.NoError(t, os.WriteFile(fakeCA, []byte("dummy CA cert\n"), 0o644))
	combinedCA := filepath.Join(tmpDir, "combined-ca.crt")

	script := generateGitSyncScript(gitSources)
	require.Contains(t, script, "/etc/proxy-ca/ca.crt",
		"generateGitSyncScript proxy CA anchor changed — update this test's path substitution")
	require.Contains(t, script, "/tmp/combined-ca.crt",
		"generateGitSyncScript combined CA anchor changed — update this test's path substitution")
	require.Contains(t, script, "/git-sources/",
		"generateGitSyncScript destination anchor changed — update this test's path substitution")
	script = strings.ReplaceAll(script, "/etc/proxy-ca/ca.crt", fakeCA)
	script = strings.ReplaceAll(script, "/tmp/combined-ca.crt", combinedCA)
	script = strings.ReplaceAll(script, "/git-sources/", sourcesDir+"/")

	cmd := exec.Command("sh", "-c", script) //nolint:gosec
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	return gitSyncScriptResult{stdout: stdout.String(), stderr: stderr.String(), sourcesDir: sourcesDir}
}
