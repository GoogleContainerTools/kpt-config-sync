// Copyright 2022 Google LLC
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

package main

import (
	"context"
	"flag"
	"os"
	"strings"

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kmetrics"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	sourceType = flag.String("source-type", os.Getenv(reconcilermanager.SourceTypeKey),
		"The type of repo being synced, must be git or oci.")

	repoRootDir = flag.String("repo-root", "/repo",
		"the absolute path in the container running the hydration to the repo root directory.")

	sourceRootDir = flag.String("source-root", "source",
		"the name of the source root directory under --repo-root.")

	hydratedRootDir = flag.String("hydrated-root", "hydrated",
		"the name of the hydrated root directory under --repo-root.")

	sourceLinkDir = flag.String("source-link", "rev",
		"the name of (a symlink to) the source directory under --source-root, which contains the clone of the git repo.")

	hydratedLinkDir = flag.String("hydrated-link", "rev",
		"the name of (a symlink to) the hydrated directory under --hydrated-root, which contains the hydrated configs")

	syncDir = flag.String("sync-dir", os.Getenv(reconcilermanager.SyncDirKey),
		"Relative path of the root directory within the repo.")

	pollingPeriod = flag.Duration("polling-period",
		controllers.PollingPeriod(reconcilermanager.HydrationPollingPeriod, configsync.DefaultHydrationPollingPeriod),
		"Period of time between checking the filesystem for source updates to render.")

	// rehydratePeriod sets the hydration-controller to re-run the hydration process
	// periodically when errors happen. It retries on both transient errors and permanent errors.
	// Other ways to trigger the hydration process are:
	// - push a new commit
	// - delete the done file from the hydration-controller.
	rehydratePeriod = flag.Duration("rehydrate-period", configsync.DefaultHydrationRetryPeriod,
		"Period of time between rehydrating on errors.")

	reconcilerName = flag.String("reconciler-name", os.Getenv(reconcilermanager.ReconcilerNameKey),
		"Name of the reconciler Deployment.")
)

func main() {
	log.Setup()
	profiler.Service()
	ctrl.SetLogger(textlogger.NewLogger(textlogger.NewConfig()))

	// Register the kustomize usage metric views.
	if err := kmetrics.RegisterKustomizeMetricsViews(); err != nil {
		klog.Fatalf("Failed to register OpenCensus views: %v", err)
	}

	// Register the OC Agent exporter
	oce, err := kmetrics.RegisterOCAgentExporter(reconcilermanager.HydrationController)
	if err != nil {
		klog.Fatalf("Failed to register the OC Agent exporter: %v", err)
	}

	defer func() {
		if err := oce.Stop(); err != nil {
			klog.Fatalf("Unable to stop the OC Agent exporter: %v", err)
		}
	}()

	absRepoRootDir, err := cmpath.AbsoluteOS(*repoRootDir)
	if err != nil {
		klog.Fatalf("--repo-root must be an absolute path: %v", err)
	}
	absSourceRootDir := absRepoRootDir.Join(cmpath.RelativeSlash(*sourceRootDir))
	absHydratedRootDir := absRepoRootDir.Join(cmpath.RelativeSlash(*hydratedRootDir))
	absDonePath := absRepoRootDir.Join(cmpath.RelativeSlash(hydrate.DoneFile))

	// Normalize syncDirRelative.
	// Some users specify the directory as if the root of the repository is "/".
	// Strip this from the front of the passed directory so behavior is as
	// expected.
	dir := strings.TrimPrefix(*syncDir, "/")
	relSyncDir := cmpath.RelativeOS(dir)

	hydrator := &hydrate.Hydrator{
		DonePath:        absDonePath,
		SourceType:      v1beta1.SourceType(*sourceType),
		SourceRoot:      absSourceRootDir,
		HydratedRoot:    absHydratedRootDir,
		SourceLink:      *sourceLinkDir,
		HydratedLink:    *hydratedLinkDir,
		SyncDir:         relSyncDir,
		PollingPeriod:   *pollingPeriod,
		RehydratePeriod: *rehydratePeriod,
		ReconcilerName:  *reconcilerName,
	}

	hydrator.Run(context.Background())
}
