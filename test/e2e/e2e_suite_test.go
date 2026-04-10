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
	"strings"
	"testing"

	"github.com/codeready-toolchain/openclaw-operator/test/utils"
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
	projectImage = "example.com/openclaw-operator:v0.0.1"
)

// TestMain runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purposed to be used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestMain(m *testing.M) {
	fmt.Println("Starting openclaw-operator integration test suite")

	// Build the manager(Operator) image with output streamed to stdout/stderr
	// so we can see Docker build progress (utils.Run captures output silently).
	fmt.Println("Building manager image...")
	if err := runStreaming("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage)); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build the manager image: %v\n", err)
		os.Exit(1)
	}

	// TODO(user): If you want to change the e2e test vendor from Kind, ensure the image is
	// built and available before running the tests. Also, remove the following block.
	// Load the manager(Operator) image on Kind
	fmt.Println("Loading image into Kind cluster...")
	kindCluster := "kind"
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		kindCluster = v
	}
	if err := runStreaming("kind", "load", "docker-image", projectImage,
		"--name", kindCluster); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load image into Kind: %v\n", err)
		os.Exit(1)
	}

	// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
	// To prevent errors when tests run in environments with CertManager already installed,
	// we check for its presence before execution.
	// Setup CertManager before the suite if not skipped and if not already installed
	if !skipCertManagerInstall {
		t := &testing.T{}
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
		t := &testing.T{}
		_ = utils.UninstallCertManager(t)
	}

	os.Exit(code)
}

// runStreaming executes a command with stdout/stderr streamed directly to the
// console so that long-running setup steps (Docker build, image load) produce
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
