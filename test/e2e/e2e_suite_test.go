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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/codeready-toolchain/claw-operator/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// These variables are useful if CertManager is already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	// isCertManagerAlreadyInstalled will be set true when CertManager CRDs be found on the cluster
	isCertManagerAlreadyInstalled = false

	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectImage = "example.com/claw-operator:v0.0.1"

	// proxyImage is the credential proxy sidecar image, built and loaded alongside the operator.
	proxyImage = "example.com/claw-proxy:latest"

	// gatewayImage is the upstream OpenClaw image used by the claw deployment.
	// Pre-loaded into Kind so e2e tests can verify init container patching.
	gatewayImage = "ghcr.io/openclaw/openclaw:slim"
)

// TestMain runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purposed to be used in CI jobs.
// The default setup requires Kind, builds/loads the Manager container image locally, and installs
// CertManager.
func TestMain(m *testing.M) {
	fmt.Println("Starting claw-operator integration test suite")

	// TODO(user): If you want to change the e2e test vendor from Kind, ensure the image is
	// built and available before running the tests. Also, remove the following block.
	kindCluster := "kind"
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		kindCluster = v
	}

	// Build, save, and load the manager and proxy images into Kind.
	// Images are saved to tar first because podman and kind do not work well
	// together when loading from a docker registry.
	// See https://github.com/kubernetes-sigs/kind/issues/2038
	if err := buildAndLoadImage("manager", "container-build", "IMG", projectImage, kindCluster); err != nil {
		fmt.Fprintf(os.Stderr, "manager image setup failed: %v\n", err)
		os.Exit(1)
	}
	if err := buildAndLoadImage("proxy", "container-build-proxy", "PROXY_IMG", proxyImage, kindCluster); err != nil {
		fmt.Fprintf(os.Stderr, "proxy image setup failed: %v\n", err)
		os.Exit(1)
	}

	if err := pullAndLoadImage(gatewayImage, kindCluster); err != nil {
		fmt.Fprintf(os.Stderr, "gateway image setup failed: %v\n", err)
		os.Exit(1)
	}

	// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
	// To prevent errors when tests run in environments with CertManager already installed,
	// we check for its presence before execution.
	// Setup CertManager before the suite if not skipped and if not already installed
	if !skipCertManagerInstall {
		// Zero-value testing.T: TestMain has no real *testing.T; any t.Logf calls inside utils are silently discarded.
		t := &testing.T{} //nolint:govet // intentional zero-value T in TestMain context
		isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled(t)
		if !isCertManagerAlreadyInstalled {
			fmt.Println("Installing CertManager...")
			if err := utils.InstallCertManager(t); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to install CertManager: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Println("CertManager already installed, skipping.")
		}
	}

	code := m.Run()

	// Cleanup: Teardown CertManager after the suite if not skipped and if it was not already installed
	if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
		fmt.Println("Uninstalling CertManager...")
		t := &testing.T{} //nolint:govet // intentional zero-value T in TestMain context
		_ = utils.UninstallCertManager(t)
	}

	os.Exit(code)
}

// buildAndLoadImage builds a container image, saves it to a tar, and loads it into
// a Kind cluster. label is a human-readable name for log messages, buildTarget
// is the make target (e.g. "container-build"), imgVar is the make variable that
// receives the image name (e.g. "IMG" or "PROXY_IMG").
func buildAndLoadImage(label, buildTarget, imgVar, image, kindCluster string) error {
	tarFile := fmt.Sprintf("tmp/%s.tar", label)

	fmt.Printf("Building %s image...\n", label)
	if err := runStreaming("make", buildTarget, fmt.Sprintf("%s=%s", imgVar, image)); err != nil {
		return fmt.Errorf("build %s image: %w", label, err)
	}

	fmt.Printf("Loading %s image into Kind cluster...\n", label)
	if err := runStreaming("make", "container-save",
		fmt.Sprintf("IMG=%s", image),
		fmt.Sprintf("OUTPUT_FILE=%s", tarFile)); err != nil {
		return fmt.Errorf("save %s image: %w", label, err)
	}
	if err := runStreaming("kind", "load", "image-archive", tarFile,
		"--name", kindCluster); err != nil {
		return fmt.Errorf("load %s image into Kind: %w", label, err)
	}
	return nil
}

// pullAndLoadImage pulls a public container image and loads it into Kind.
func pullAndLoadImage(image, kindCluster string) error {
	tarFile := fmt.Sprintf("tmp/%s.tar", strings.ReplaceAll(
		strings.ReplaceAll(image, "/", "_"), ":", "_"))

	platform := fmt.Sprintf("linux/%s", runtime.GOARCH)
	fmt.Printf("Pulling gateway image %s (%s)...\n", image, platform)
	if err := runStreaming("podman", "pull", fmt.Sprintf("--platform=%s", platform), image); err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}

	fmt.Printf("Loading gateway image into Kind cluster...\n")
	_ = runStreaming("rm", "-f", tarFile)
	if err := runStreaming("podman", "save", "-o", tarFile, image); err != nil {
		return fmt.Errorf("save %s: %w", image, err)
	}
	if err := runStreaming("kind", "load", "image-archive", tarFile,
		"--name", kindCluster); err != nil {
		return fmt.Errorf("load %s into Kind: %w", image, err)
	}
	return nil
}

// runStreaming executes a command with stdout/stderr streamed directly to the
// console so that long-running setup steps (container build, image load) produce
// visible progress output instead of appearing to hang.
func runStreaming(name string, args ...string) error {
	dir, err := utils.GetProjectDir()
	if err != nil {
		return fmt.Errorf("failed to get project directory: %w", err)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	fmt.Printf("running: %s %s\n", name, strings.Join(args, " "))
	return cmd.Run()
}
