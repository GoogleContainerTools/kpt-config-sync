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

package configsync

import (
	"flag"
	"os"
	"strings"
	"time"

	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/importer/dirwatcher"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/policycontroller"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/syncer/controller"
	"kpt.dev/configsync/pkg/syncer/meta"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var (
	clusterName       = flag.String("cluster-name", os.Getenv(reconcilermanager.ClusterNameKey), "Cluster name to use for Cluster selection")
	gitDir            = flag.String("git-dir", "/repo/rev", "Absolute path to the git repo")
	policyDirRelative = flag.String("policy-dir", os.Getenv("POLICY_DIR"), "Relative path of root policy directory in the repo")
	pollPeriod        = flag.Duration("poll-period", time.Second*5, "Poll period for checking if --git-dir target directory has changed")

	resyncPeriod = flag.Duration(
		"resync_period", time.Minute, "The resync period for the syncer system")
	fightDetectionThreshold = flag.Float64(
		"fight_detection_threshold", 5.0,
		"The rate of updates per minute to an API Resource at which the Syncer logs warnings about too many updates to the resource.")
)

// RunImporter encapsulates the main() logic for the importer.
func RunImporter() {
	reconcile.SetFightThreshold(*fightDetectionThreshold)

	// Get a config to talk to the apiserver.
	cfg, err := restconfig.NewRestConfig(restconfig.DefaultTimeout)
	if err != nil {
		klog.Fatalf("failed to create rest config: %+v", err)
	}

	// Create a new Manager to provide shared dependencies and start components.
	mgr, err := manager.New(cfg, manager.Options{
		SyncPeriod: resyncPeriod,
	})
	if err != nil {
		klog.Fatalf("Failed to create manager: %+v", err)
	}

	// Set up Scheme for nomos resources.
	if err := v1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatalf("Error adding configmanagement resources to scheme: %v", err)
	}

	// Normalize policyDirRelative.
	// Some users specify the directory as if the root of the repository is "/".
	// Strip this from the front of the passed directory so behavior is as
	// expected.
	dir := strings.TrimPrefix(*policyDirRelative, "/")

	// Set up controllers.
	if err := meta.AddControllers(mgr); err != nil {
		klog.Fatalf("Error adding Sync controller: %+v", err)
	}

	if err := filesystem.AddController(*clusterName, mgr, *gitDir,
		dir, *pollPeriod); err != nil {
		klog.Fatalf("Error adding Importer controller: %+v", err)
	}

	if err := controller.AddRepoStatus(mgr); err != nil {
		klog.Fatalf("Error adding RepoStatus controller: %+v", err)
	}

	ctx := signals.SetupSignalHandler()
	if err := policycontroller.AddControllers(ctx, mgr); err != nil {
		klog.Fatalf("Error adding PolicyController controller: %+v", err)
	}

	// Start the Manager.
	if err := mgr.Start(ctx); err != nil {
		klog.Fatalf("Error starting controller: %+v", err)
	}

	klog.Info("Exiting")
}

// DirWatcher watches the filesystem of a given directory until a shutdown signal is received.
func DirWatcher(dir string, period time.Duration) {
	if dir == "" {
		return
	}
	watcher := dirwatcher.NewWatcher(dir)
	watcher.Watch(signals.SetupSignalHandler(), period)
}
