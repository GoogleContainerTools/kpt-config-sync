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
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/reconciler/controllers"
	"kpt.dev/configsync/pkg/reconciler/finalizer"
	"kpt.dev/configsync/pkg/reconcilermanager"
	rmcontrollers "kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/remediator"
	"kpt.dev/configsync/pkg/remediator/cache"
	"kpt.dev/configsync/pkg/remediator/watch"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
	"kpt.dev/configsync/pkg/util"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
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
		"The type of repo being synced, must be git or oci or helm.")
	sourceRepo = flag.String("source-repo", os.Getenv(reconcilermanager.SourceRepoKey),
		"The URL of the git or OCI repo being synced.")
	sourceBranch = flag.String("source-branch", os.Getenv(reconcilermanager.SourceBranchKey),
		"The branch of the git repo being synced.")
	sourceRev = flag.String("source-rev", os.Getenv(reconcilermanager.SourceRevKey),
		"The reference we're syncing to in the repo. Could be a specific commit or a chart version.")
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
	resyncPeriod = flag.Duration("resync-period", configsync.DefaultReconcilerResyncPeriod,
		"Period of time between forced re-syncs from source (even without a new commit).")
	pollingPeriod = flag.Duration("filesystem-polling-period",
		rmcontrollers.PollingPeriod(reconcilermanager.ReconcilerPollingPeriod, configsync.DefaultReconcilerPollingPeriod),
		"Period of time between checking the filesystem for source updates to sync.")

	// Root-Repo-only flags. If set for a Namespace-scoped Reconciler, causes the Reconciler to fail immediately.
	sourceFormat = flag.String(flags.sourceFormat, os.Getenv(filesystem.SourceFormatKey),
		"The format of the repository.")
	// Applier flag, Make the reconcile/prune timeout configurable
	reconcileTimeout = flag.String(flags.reconcileTimeout, os.Getenv(reconcilermanager.ReconcileTimeout), "The timeout of applier reconcile and prune tasks")
	// Enable the applier to inject actuation status data into the ResourceGroup object
	statusMode = flag.String(flags.statusMode, os.Getenv(reconcilermanager.StatusMode),
		"When the value is enabled or empty, the applier injects actuation status data into the ResourceGroup object")

	apiServerTimeout = flag.String("api-server-timeout", os.Getenv(reconcilermanager.APIServerTimeout), "The client-side timeout for requests to the API server")

	debug = flag.Bool("debug", false,
		"Enable debug mode, panicking in many scenarios where normally an InternalError would be logged. "+
			"Do not use in production.")

	renderingEnabled  = flag.Bool("rendering-enabled", util.EnvBool(reconcilermanager.RenderingEnabled, false), "")
	namespaceStrategy = flag.String(flags.namespaceStrategy, util.EnvString(reconcilermanager.NamespaceStrategy, ""),
		fmt.Sprintf("Set the namespace strategy for the reconciler. Must be %s or %s. Default: %s.",
			configsync.NamespaceStrategyImplicit, configsync.NamespaceStrategyExplicit, configsync.NamespaceStrategyImplicit))
)

var flags = struct {
	sourceDir         string
	repoRootDir       string
	hydratedRootDir   string
	clusterName       string
	sourceFormat      string
	statusMode        string
	reconcileTimeout  string
	namespaceStrategy string
}{
	repoRootDir:       "repo-root",
	sourceDir:         "source-dir",
	hydratedRootDir:   "hydrated-root",
	clusterName:       "cluster-name",
	sourceFormat:      reconcilermanager.SourceFormat,
	statusMode:        "status-mode",
	reconcileTimeout:  "reconcile-timeout",
	namespaceStrategy: "namespace-strategy",
}

func timeStringToDuration(t string) (time.Duration, error) {
	duration, err := time.ParseDuration(t)
	if err != nil {
		return 0, fmt.Errorf("parsing time duration %s: %v", t, err)
	}
	if duration < 0 {
		return 0, fmt.Errorf("invalid time duration: %v, should not be negative", duration)
	}
	return duration, nil
}

func main() {
	log.Setup()
	profiler.Service()
	ctrl.SetLogger(klogr.New())

	if *debug {
		status.EnablePanicOnMisuse()
	}

	reconcilerScope := declared.Scope(*scope)
	fight.SetFightThreshold(*fightDetectionThreshold)

	cl, discoveryClient, baseApplier, supervisor, err := configureClients(reconcilerScope)
	if err != nil {
		klog.Error(err)
	}

	// Start listening to signals
	signalCtx := signals.SetupSignalHandler()
	mgrOptions := ctrl.Options{
		Scheme: core.Scheme,
		BaseContext: func() context.Context {
			return signalCtx
		},
	}
	// For Namespaced Reconcilers, set the default namespace to watch.
	// Otherwise, all namespaced informers will watch at the cluster-scope.
	// This prevents Namespaced Reconcilers from needing cluster-scoped read
	// permissions.
	if reconcilerScope != declared.RootReconciler {
		mgrOptions.Namespace = string(reconcilerScope)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOptions)
	if err != nil {
		klog.Fatalf("Error starting manager: %v", err)
	}

	// This cancelFunc will be used by the Finalizer to stop all the other
	// controllers (Syncer & Remediator).
	controllerCtx, stopControllers := context.WithCancel(signalCtx)

	// These channels will be closed when the corresponding controllers have exited,
	// signalling for the finalizer to continue.
	doneChannelForSyncer := make(chan struct{})
	doneChannelForRemediator := make(chan struct{})

	// Create the Finalizer
	// The caching client built by the controller-manager doesn't update
	// the GET reconcilerState on UPDATE/PATCH. So we need to use the non-caching client
	// for the finalizer, which does GET/LIST after UPDATE/PATCH.
	f := finalizer.New(reconcilerScope, supervisor, cl, // non-caching client
		stopControllers, doneChannelForSyncer, doneChannelForRemediator)

	// Create the Finalizer Controller
	finalizerController := &finalizer.Controller{
		SyncScope: reconcilerScope,
		SyncName:  *syncName,
		Client:    mgr.GetClient(), // caching client
		Scheme:    mgr.GetScheme(),
		Mapper:    mgr.GetRESTMapper(),
		Finalizer: f,
	}

	// Register the Finalizer Controller
	if err = finalizerController.SetupWithManager(mgr); err != nil {
		klog.Fatalf("Instantiating Finalizer: %v", err)
	}

	// Configure Syncer and Remediator.

	// Get a separate config for the remediator to talk to the apiserver since
	// we want a longer REST config timeout for the remediator to avoid restarting
	// idle watches too frequently.
	cfgForWatch, err := restconfig.NewRestConfig(watch.RESTConfigTimeout)
	if err != nil {
		klog.Fatalf("Error creating rest config for the remediator: %v", err)
	}

	// Instantiate the cache shared by both the Syncer and Remediator
	decls := &declared.Resources{}
	remResources := cache.NewRemediateResources()

	// Create the Remediator actor.
	// The Syncer uses it to send pause/resume signals to the Remediator, and to start watches for declared resources.
	// The Remediator uses it to receive the signal to perform remediation and to handle the remediate errors.
	remInterface, err := remediator.New(reconcilerScope, *syncName, cfgForWatch, decls, remResources)
	if err != nil {
		klog.Fatalf("Error creating remediator interface: %v", err)
	}
	// reconcilerState is the cache shared across all Syncer reconciliation loops.
	reconcilerState := parse.NewReconcilerState()
	parser, err := configureParser(cl, discoveryClient, decls, supervisor, remInterface)
	if err != nil {
		klog.Fatalf("Error creating parser: %v", err)
	}

	// Create the Syncer controller
	syncer := controllers.NewSyncer(
		controllerCtx, reconcilerScope, parser, reconcilerState, *pollingPeriod, *resyncPeriod,
		configsync.DefaultReconcilerRetryPeriod,
		configsync.DefaultReconcilerSyncStatusUpdatePeriod, doneChannelForSyncer)

	// Register the Syncer controller
	if err = syncer.SetupWithManager(mgr); err != nil {
		klog.Fatalf("Error creating controller %q: %v", controllers.SyncerController, err)
	}

	// Create the Remediator controller
	rem := controllers.Remediator{
		ControllerCtx:      controllerCtx,
		DoneCh:             doneChannelForRemediator,
		SyncScope:          reconcilerScope,
		SyncName:           *syncName,
		Applier:            baseApplier,
		Resources:          decls,
		RemediateResources: remResources,
		Remediator:         remInterface,
	}

	// Register the Remediator controller
	if err = rem.SetupWithManager(mgr); err != nil {
		klog.Fatalf("Error creating controller %q: %v", controllers.RemediatorController, err)
	}

	// Register the OpenCensus views
	if err := ocmetrics.RegisterReconcilerMetricsViews(); err != nil {
		klog.Fatalf("Failed to register OpenCensus views: %v", err)
	}

	// Register the OC Agent exporter
	oce, err := ocmetrics.RegisterOCAgentExporter(reconcilermanager.Reconciler)
	if err != nil {
		klog.Fatalf("Failed to register the OC Agent exporter: %v", err)
	}

	defer func() {
		if err := oce.Stop(); err != nil {
			klog.Fatalf("Unable to stop the OC Agent exporter: %v", err)
		}
	}()

	klog.Info("Starting manager")
	defer func() {
		// If the manager returned, there was either an error or a term/kill
		// signal. So stop the other controllers, if not already stopped.
		stopControllers()
	}()
	if err = mgr.Start(signalCtx); err != nil {
		klog.Errorf("Error running manager: %v", err)
		// os.Exit(1) does not run deferred functions so explicitly stopping the OC Agent exporter.
		if err := oce.Stop(); err != nil {
			klog.Errorf("Error stopping the OC Agent exporter: %v", err)
		}
		os.Exit(1)
	}

	// Wait for exit signal, if not already received.
	// This avoids unnecessary restarts after the finalizer has completed.
	<-signalCtx.Done()
	klog.Info("All controllers exited")
}

// configureClients configures the set of clients used by Syncer, Remediator and Finalizer.
//   - client.Client is used by the Finalizer to add/remove metadata.finalizer and set/remove finalizing condition
//     It is also used by the Syncer to set the RSync status field.
//   - DiscoveryInterface is used by the Syncer to build the scope of a resource.
//   - Applier is used by the Remediator to correct any drift.
//   - Supervisor is used by the Finalizer to finalize managed resources. It is also
//     used by the Syncer to apply/prune declared resources.
func configureClients(scope declared.Scope) (client.Client, discovery.DiscoveryInterface, reconcile.Applier, applier.Supervisor, error) {
	reconcileTimeoutDuration, err := timeStringToDuration(*reconcileTimeout)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("reconcileTimeout format error: %v", err)
	}

	// Get a config to talk to the apiserver.
	apiServerTimeoutDuration, err := time.ParseDuration(*apiServerTimeout)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("apiServerTimeout format error: %v", err)
	}

	cfg, err := restconfig.NewRestConfig(apiServerTimeoutDuration)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create rest config: %v", err)
	}

	configFlags, err := restconfig.NewConfigFlags(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create config flags from rest config: %v", err)
	}

	discoveryClient, err := configFlags.ToDiscoveryClient()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create discovery client: %v", err)
	}

	// Use the DynamicRESTMapper as the default RESTMapper does not detect when
	// new types become available.
	mapper, err := apiutil.NewDynamicRESTMapper(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create DynamicRESTMapper: %v", err)
	}

	cl, err := client.New(cfg, client.Options{
		Scheme: core.Scheme,
		Mapper: mapper,
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create client: %v", err)
	}

	// Configure the Applier.
	genericClient := syncerclient.New(cl, metrics.APICallDuration)
	baseApplier, err := reconcile.NewApplierForMultiRepo(cfg, genericClient)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create applier for the remediator: %v", err)
	}

	clientSet, err := applier.NewClientSet(cl, configFlags, *statusMode)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create clients: %v", err)
	}
	supervisor, err := applier.NewSupervisor(clientSet, scope, *syncName, reconcileTimeoutDuration)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create supervisor for the syncer: %v", err)
	}

	return cl, discoveryClient, baseApplier, supervisor, nil
}

// configureParser configures the Parser actor for the Syncer controller.
// The Parser parses the source configs from file format to unstructured Kubernetes objects,
// and sends K8s API calls to set the status field. It also contains an updater
// that applies the declared resources to the cluster, and update watches for drift correction.
func configureParser(cl client.Client, dc discovery.DiscoveryInterface, decls *declared.Resources, supervisor applier.Supervisor, rem *remediator.Remediator) (parse.Parser, error) {
	absRepoRoot, err := cmpath.AbsoluteOS(*repoRootDir)
	if err != nil {
		return nil, fmt.Errorf("%s must be an absolute path: %v", flags.repoRootDir, err)
	}

	// Normalize syncDirRelative.
	// Some users specify the directory as if the root of the repository is "/".
	// Strip this from the front of the passed directory so behavior is as
	// expected.
	dir := strings.TrimPrefix(*syncDir, "/")
	relSyncDir := cmpath.RelativeOS(dir)
	absSourceDir, err := cmpath.AbsoluteOS(*sourceDir)
	if err != nil {
		return nil, fmt.Errorf("%s must be an absolute path: %v", flags.sourceDir, err)
	}

	if err = declared.ValidateScope(*scope); err != nil {
		return nil, err
	}

	var format filesystem.SourceFormat
	var nsStrat configsync.NamespaceStrategy
	switch declared.Scope(*scope) {
	case declared.RootReconciler:
		klog.Info("Starting reconciler for: root")
		// Default to "hierarchy" if unset.
		format = filesystem.SourceFormat(*sourceFormat)
		if format == "" {
			format = filesystem.SourceFormatHierarchy
		}
		// Default to "implicit" if unset.
		nsStrat = configsync.NamespaceStrategy(*namespaceStrategy)
		if nsStrat == "" {
			nsStrat = configsync.NamespaceStrategyImplicit
		}
	default:
		klog.Infof("Starting reconciler for: %s", *scope)
		if *sourceFormat != "" {
			return nil, fmt.Errorf("flag %s and environment variable %s must not be passed to a Namespace reconciler",
				flags.sourceFormat, filesystem.SourceFormatKey)
		}
		if *namespaceStrategy != "" {
			return nil, fmt.Errorf("flag %s and environment variable %s must not be passed to a Namespace reconciler",
				flags.namespaceStrategy, reconcilermanager.NamespaceStrategy)
		}
	}

	var parser parse.Parser
	fs := parse.FileSource{
		SourceDir:    absSourceDir,
		RepoRoot:     absRepoRoot,
		HydratedRoot: *hydratedRootDir,
		HydratedLink: *hydratedLinkDir,
		SyncDir:      relSyncDir,
		SourceType:   v1beta1.SourceType(*sourceType),
		SourceRepo:   *sourceRepo,
		SourceBranch: *sourceBranch,
		SourceRev:    *sourceRev,
	}
	ds := declared.Scope(*scope)
	if ds == declared.RootReconciler {
		parser, err = parse.NewRootRunner(
			*clusterName,
			*syncName,
			*reconcilerName,
			format,
			&reader.File{},
			cl,
			*pollingPeriod,
			*resyncPeriod,
			configsync.DefaultReconcilerRetryPeriod,
			configsync.DefaultReconcilerSyncStatusUpdatePeriod,
			fs,
			dc,
			decls,
			supervisor,
			rem,
			*renderingEnabled,
			nsStrat)
		if err != nil {
			return nil, fmt.Errorf("instantiating Root Repository Parser: %v", err)
		}
	} else {
		parser, err = parse.NewNamespaceRunner(
			*clusterName,
			*syncName,
			*reconcilerName,
			ds,
			&reader.File{},
			cl,
			*pollingPeriod,
			*resyncPeriod,
			configsync.DefaultReconcilerRetryPeriod,
			configsync.DefaultReconcilerSyncStatusUpdatePeriod,
			fs,
			dc,
			decls,
			supervisor,
			rem,
			*renderingEnabled)
		if err != nil {
			return nil, fmt.Errorf("instantiating Namespace Repository Parser: %v", err)
		}
	}

	return parser, nil
}
