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
	"time"

	"k8s.io/klog/klogr"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kmetrics"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
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

	syncDir = flag.String("sync-dir", os.Getenv("SYNC_DIR"),
		"Relative path of the root directory within the repo.")

	hydrationPollingPeriodStr = flag.String("polling-period", os.Getenv(reconcilermanager.HydrationPollingPeriod),
		"Period of time between checking the filesystem for rendering the DRY configs.")

	// rehydratePeriod sets the hydration-controller to re-run the hydration process
	// periodically when errors happen. It retries on both transient errors and permanent errors.
	// Other ways to trigger the hydration process are:
	// - push a new commit
	// - delete the done file from the hydration-controller.
	rehydratePeriod = flag.Duration("rehydrate-period", 30*time.Minute,
		"Period of time between rehydrating on errors.")

	reconcilerName = flag.String("reconciler-name", os.Getenv(reconcilermanager.ReconcilerNameKey),
		"Name of the reconciler Deployment.")
)

func main() {
	log.Setup()
	profiler.Service()
	ctrl.SetLogger(klogr.New())

	// Register the kustomize usage metric views.
	if err := kmetrics.RegisterKustomizeMetricsViews(); err != nil {
		klog.Fatalf("Failed to register OpenCensus views: %v", err)
	}

	// Register the OC Agent exporter
	oce, err := kmetrics.RegisterOCAgentExporter()
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

	hydrationPollingPeriod, err := time.ParseDuration(*hydrationPollingPeriodStr)
	if err != nil {
		klog.Fatalf("Failed to get hydration polling period: %v", err)
	}

	hydrator := &hydrate.Hydrator{
		DonePath:           absDonePath,
		SourceRoot:         absSourceRootDir,
		HydratedRoot:       absHydratedRootDir,
		SourceLink:         *sourceLinkDir,
		HydratedLink:       *hydratedLinkDir,
		SyncDir:            relSyncDir,
		PollingFrequency:   hydrationPollingPeriod,
		RehydrateFrequency: *rehydratePeriod,
		ReconcilerName:     *reconcilerName,
	}

	hydrator.Run(context.Background())
}
