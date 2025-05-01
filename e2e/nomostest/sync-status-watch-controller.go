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
const ImagePlaceholder = "SYNC_STATUS_WATCH_CONTROLLER_IMAGE"

// ManifestPath is the path to the manifest file.
const ManifestPath = "../../examples/post-sync/sync-watch-manifest.yaml"

// TestImage is the image used for testing the sync status watch controller.
const TestImage = testing.TestInfraArtifactRepositoryAddress + "/sync-status-watch-controller:v1.0.0-6ea8969a"

// SetupSyncStatusWatchController sets up the sync status watch controller in the cluster.
// It creates the necessary namespace and deploys the controller.
func SetupSyncStatusWatchController(nt *NT) error {
	nt.T.Logf("applying sync status watch controller manifest")

	// Create namespace first to ensure it exists
	namespace := testSyncStatusWatchNamespace()
	if err := nt.KubeClient.Create(namespace); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating namespace %s: %w", namespace.Name, err)
	}

	if err := execManifestCommand(nt, "apply"); err != nil {
		nt.describeNotRunningTestPods(StatusWatchNamespace)
		return fmt.Errorf("applying sync status watch manifest: %w", err)
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), StatusWatchName, StatusWatchNamespace)
}

// TeardownSyncStatusWatchController tears down the sync status watch controller from the cluster.
// It removes the namespace and associated resources.
func TeardownSyncStatusWatchController(nt *NT) error {
	nt.T.Log("tearing down sync status watch controller")

	if err := execManifestCommand(nt, "delete"); err != nil {
		nt.T.Errorf("Error deleting resources: %v", err)
	}

	if err := nt.KubeClient.Delete(testSyncStatusWatchNamespace()); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Errorf("Error deleting Namespace: %v", err)
	}

	nt.T.Log("Sync status watch controller teardown complete")
	return nil
}

// execManifestCommand executes a kubectl command to apply or delete a manifest with the test image.
func execManifestCommand(nt *NT, kubectlAction string) error {
	input, err := os.ReadFile(ManifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest file %s: %w", ManifestPath, err)
	}

	content := strings.ReplaceAll(string(input), ImagePlaceholder, TestImage)

	tmpFile, err := os.CreateTemp("", "statuswatch-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			nt.T.Errorf("Warning: failed to remove temporary file %s: %v", tmpFile.Name(), removeErr)
		}
	}() // Clean up the temp file when done

	if err := os.WriteFile(tmpFile.Name(), []byte(content), 0644); err != nil {
		return fmt.Errorf("writing to temp file: %w", err)
	}

	_, err = nt.Shell.ExecWithDebug("kubectl", kubectlAction, "-f", tmpFile.Name())
	if err != nil {
		return fmt.Errorf("executing kubectl %s: %w", kubectlAction, err)
	}

	return nil
}

func testSyncStatusWatchNamespace() *corev1.Namespace {
	return k8sobjects.NamespaceObject(StatusWatchNamespace)
}
