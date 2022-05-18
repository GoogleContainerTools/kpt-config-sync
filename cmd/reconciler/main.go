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
	"flag"
	"os"
	"strings"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	clusterName = flag.String(flags.clusterName, os.Getenv(reconcilermanager.ClusterNameKey),
		"Cluster name to use for Cluster selection")
	scope = flag.String("scope", os.Getenv(reconcilermanager.ScopeKey),
		"Scope of the reconciler, either a namespace or ':root'.")
	syncName = flag.String("sync-name", os.Getenv(reconcilermanager.SyncNameKey),
		"Name of the RootSync or RepoSync object.")
	reconcilerName = flag.String("reconciler-name", os.Getenv(reconcilermanager.ReconcilerNameKey),
		"Name of the reconciler Deployment.")

	// source configuration flags. These values originate in the ConfigManagement and
	// configure git-sync/oci-sync to clone the desired repository/reference we want.
	sourceType = flag.String("source-type", os.Getenv(reconcilermanager.SourceTypeKey),
		"The type of repo being synced, must be git or oci.")
	sourceRepo = flag.String("source-repo", os.Getenv(reconcilermanager.SourceRepoKey),
		"The URL of the git or OCI repo being synced.")
	sourceBranch = flag.String("source-branch", os.Getenv(reconcilermanager.SourceBranchKey),
		"The branch of the git repo being synced.")
	sourceRev = flag.String("source-rev", os.Getenv(reconcilermanager.SourceRevKey),
		"The git reference we're syncing to in the git repo. Could be a specific commit.")
	syncDir = flag.String("sync-dir", os.Getenv(reconcilermanager.SyncDirKey),
		"The relative path of the root configuration directory within the repo.")

	// Performance tuning flags.
	sourceDir = flag.String(flags.sourceDir, "/repo/source/rev",
		"The absolute path in the container running the reconciler to the clone of the source repo.")
	repoRootDir = flag.String(flags.repoRootDir, "/repo",
		"The absolute path in the container running the reconciler to the repo root directory.")
	hydratedRootDir = flag.String(flags.hydratedRootDir, "/repo/hydrated",
		"The absolute path in the container running the reconciler to the hydrated root directory.")
	hydratedLinkDir = flag.String("hydrated-link", "rev",
		"The name of (a symlink to) the source directory under --hydrated-root, which contains the hydrated configs")
	fightDetectionThreshold = flag.Float64(
		"fight-detection-threshold", 5.0,
		"The rate of updates per minute to an API Resource at which the Syncer logs warnings about too many updates to the resource.")
	resyncPeriod = flag.Duration("resync-period", time.Hour,
		"Period of time between forced re-syncs from source (even without a new commit).")
	workers = flag.Int("workers", 1,
		"Number of concurrent remediator workers to run at once.")
	filesystemPollingPeriod = flag.Duration("filesystem-polling-period", controllers.PollingPeriod(reconcilermanager.ReconcilerPollingPeriod, configsync.DefaultReconcilerPollingPeriod),
		"Period of time between checking the filesystem for updates to the source or rendered configs.")

	// Root-Repo-only flags. If set for a Namespace-scoped Reconciler, causes the Reconciler to fail immediately.
	sourceFormat = flag.String(flags.sourceFormat, os.Getenv(filesystem.SourceFormatKey),
		"The format of the repository.")
	// Applier flag, Make the reconcile/prune timeout configurable
	reconcileTimeout = flag.String(flags.reconcileTimeout, os.Getenv(reconcilermanager.ReconcileTimeout), "The timeout of applier reconcile and prune tasks")
	// Enable the applier to inject actuation status data into the ResourceGroup object
	statusMode = flag.String(flags.statusMode, os.Getenv(reconcilermanager.StatusMode),
		"When the value is enabled or empty, the applier injects actuation status data into the ResourceGroup object")

	debug = flag.Bool("debug", false,
		"Enable debug mode, panicking in many scenarios where normally an InternalError would be logged. "+
			"Do not use in production.")
)

var flags = struct {
	sourceDir        string
	repoRootDir      string
	hydratedRootDir  string
	clusterName      string
	sourceFormat     string
	statusMode       string
	reconcileTimeout string
}{
	repoRootDir:      "repo-root",
	sourceDir:        "source-dir",
	hydratedRootDir:  "hydrated-root",
	clusterName:      "cluster-name",
	sourceFormat:     reconcilermanager.SourceFormat,
	statusMode:       "status-mode",
	reconcileTimeout: "reconcile-timeout",
}

func main() {
	log.Setup()
	profiler.Service()
	ctrl.SetLogger(klogr.New())

	if *debug {
		status.EnablePanicOnMisuse()
	}

	// Register the OpenCensus views
	if err := ocmetrics.RegisterReconcilerMetricsViews(); err != nil {
		klog.Fatalf("Failed to register OpenCensus views: %v", err)
	}

	// Register the OC Agent exporter
	oce, err := ocmetrics.RegisterOCAgentExporter()
	if err != nil {
		klog.Fatalf("Failed to register the OC Agent exporter: %v", err)
	}

	defer func() {
		if err := oce.Stop(); err != nil {
			klog.Fatalf("Unable to stop the OC Agent exporter: %v", err)
		}
	}()

	absRepoRoot, err := cmpath.AbsoluteOS(*repoRootDir)
	if err != nil {
		klog.Fatalf("%s must be an absolute path: %v", flags.repoRootDir, err)
	}

	// Normalize syncDirRelative.
	// Some users specify the directory as if the root of the repository is "/".
	// Strip this from the front of the passed directory so behavior is as
	// expected.
	dir := strings.TrimPrefix(*syncDir, "/")
	relSyncDir := cmpath.RelativeOS(dir)
	absSourceDir, err := cmpath.AbsoluteOS(*sourceDir)
	if err != nil {
		klog.Fatalf("%s must be an absolute path: %v", flags.sourceDir, err)
	}

	err = declared.ValidateScope(*scope)
	if err != nil {
		klog.Fatal(err)
	}

	dc, err := importer.DefaultCLIOptions.ToDiscoveryClient()
	if err != nil {
		klog.Fatalf("Failed to get DiscoveryClient: %v", err)
	}

	opts := reconciler.Options{
		ClusterName:                *clusterName,
		FightDetectionThreshold:    *fightDetectionThreshold,
		NumWorkers:                 *workers,
		ReconcilerScope:            declared.Scope(*scope),
		ResyncPeriod:               *resyncPeriod,
		FilesystemPollingFrequency: *filesystemPollingPeriod,
		SourceRoot:                 absSourceDir,
		RepoRoot:                   absRepoRoot,
		HydratedRoot:               *hydratedRootDir,
		HydratedLink:               *hydratedLinkDir,
		SourceRev:                  *sourceRev,
		SourceBranch:               *sourceBranch,
		SourceType:                 v1beta1.SourceType(*sourceType),
		SourceRepo:                 *sourceRepo,
		SyncDir:                    relSyncDir,
		DiscoveryClient:            dc,
		SyncName:                   *syncName,
		ReconcilerName:             *reconcilerName,
		StatusMode:                 *statusMode,
		ReconcileTimeout:           *reconcileTimeout,
	}

	if declared.Scope(*scope) == declared.RootReconciler {
		// Default to "hierarchy" if unset.
		format := filesystem.SourceFormat(*sourceFormat)
		if format == "" {
			format = filesystem.SourceFormatHierarchy
		}

		klog.Info("Starting reconciler for: root")
		opts.RootOptions = &reconciler.RootOptions{
			SourceFormat: format,
		}
	} else {
		klog.Infof("Starting reconciler for: %s", *scope)

		if *sourceFormat != "" {
			klog.Fatalf("Flag %s and Environment variable%q must not be passed to a Namespace reconciler",
				flags.sourceFormat, filesystem.SourceFormatKey)
		}
	}
	reconciler.Run(opts)
}
