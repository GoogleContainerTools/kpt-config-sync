// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nomostest

import (
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/kinds"

	"kpt.dev/configsync/pkg/core/k8sobjects"
)

// StatusWatchNamespace is the namespace for the sync status watch controller.
const StatusWatchNamespace = "sync-status-watch"

// StatusWatchName is the name of the sync status watch controller.
const StatusWatchName = "sync-status-watch"

// ImagePlaceholder is the placeholder in the manifest that will be replaced with the actual image.
const ImagePlaceholder = "SYNC_STATUS_WATCH_CONTROLLER_IMAGE_REGISTRY"

// ManifestPath is the path to the manifest file.
const ManifestPath = "../../examples/post-sync/sync-watch-manifest.yaml"

// TestImage is the image used for testing the sync status watch controller.
const TestImage = testing.TestInfraArtifactRepositoryAddress + "/sync-status-watch-controller:v1.0.0-539a1ddd"

// SetupSyncStatusWatchController sets up the sync status watch controller in the cluster.
// It creates the necessary namespace and deploys the controller.
// An optional custom manifest path can be provided to override the default.
func SetupSyncStatusWatchController(nt *NT, customManifestPath ...string) error {
	nt.T.Logf("applying sync status watch controller manifest")

	// Create namespace first to ensure it exists
	namespace := testSyncStatusWatchNamespace()
	if err := nt.KubeClient.Create(namespace); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating namespace %s: %w", namespace.Name, err)
	}

	// Use kubectl to apply the manifest
	if err := execManifestCommand(nt, "apply", customManifestPath...); err != nil {
		nt.describeNotRunningTestPods(StatusWatchNamespace)
		return fmt.Errorf("applying sync status watch manifest: %w", err)
	}

	// Wait for the deployment to be ready
	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), StatusWatchName, StatusWatchNamespace)
}

// TeardownSyncStatusWatchController tears down the sync status watch controller from the cluster.
// It removes the namespace and associated resources.
// An optional custom manifest path can be provided to match the one used during setup.
func TeardownSyncStatusWatchController(nt *NT, customManifestPath ...string) error {
	nt.T.Log("tearing down sync status watch controller")

	// Use kubectl to delete resources
	if err := execManifestCommand(nt, "delete --ignore-not-found", customManifestPath...); err != nil {
		nt.T.Logf("Error deleting resources: %v", err)
	}

	// Make sure the namespace is deleted
	if err := nt.KubeClient.Delete(testSyncStatusWatchNamespace()); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Logf("Error deleting Namespace: %v", err)
	}

	nt.T.Log("Sync status watch controller teardown complete")
	return nil
}

// execManifestCommand executes a kubectl command to apply or delete a manifest with the test image.
// It accepts an optional custom manifest path. If not provided, it uses the default ManifestPath.
func execManifestCommand(nt *NT, kubectlAction string, customManifestPath ...string) error {
	// Determine the manifest path to use
	manifestPath := ManifestPath
	if len(customManifestPath) > 0 && customManifestPath[0] != "" {
		manifestPath = customManifestPath[0]
	}

	// Read the manifest file
	input, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest file %s: %w", manifestPath, err)
	}

	// Replace the image placeholder
	content := strings.ReplaceAll(string(input), ImagePlaceholder, TestImage)

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "statuswatch-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up the temp file when done

	// Write the modified content to the temp file
	if err := os.WriteFile(tmpFile.Name(), []byte(content), 0644); err != nil {
		return fmt.Errorf("writing to temp file: %w", err)
	}

	// Execute kubectl with the temp file
	_, err = nt.Shell.ExecWithDebug("kubectl", kubectlAction, "-f", tmpFile.Name())
	if err != nil {
		return fmt.Errorf("executing kubectl %s: %w", kubectlAction, err)
	}

	return nil
}

func testSyncStatusWatchNamespace() *corev1.Namespace {
	return k8sobjects.NamespaceObject(StatusWatchNamespace)
}
