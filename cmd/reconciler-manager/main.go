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

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2/textlogger"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var (
	clusterName = flag.String("cluster-name", os.Getenv(reconcilermanager.ClusterNameKey),
		"Cluster name to use for Cluster selection")

	reconcilerPollingPeriod = flag.Duration("reconciler-polling-period",
		controllers.PollingPeriod(reconcilermanager.ReconcilerPollingPeriod, configsync.DefaultReconcilerPollingPeriod),
		"Period of time between checking the filesystem for source updates to sync.")

	hydrationPollingPeriod = flag.Duration("hydration-polling-period",
		controllers.PollingPeriod(reconcilermanager.HydrationPollingPeriod, configsync.DefaultHydrationPollingPeriod),
		"Period of time between checking the filesystem for source updates to render.")
)

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	log.Setup()
	setupLog := textlogger.NewLogger(textlogger.NewConfig()).WithName("setup")

	profiler.Service()
	ctrl.SetLogger(textlogger.NewLogger(textlogger.NewConfig()))

	setupLog.Info(fmt.Sprintf("running with flags --cluster-name=%s; --reconciler-polling-period=%s; --hydration-polling-period=%s",
		*clusterName, *reconcilerPollingPeriod, *hydrationPollingPeriod))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: core.Scheme,
	})
	if err != nil {
		setupLog.Error(err, "failed to start manager")
		os.Exit(1)
	}
	// The Client built by ctrl.NewManager uses caching by default, and doesn't
	// support the Watch method. So build another with shared config.
	// This one can be used for watching and bypassing the cache, as needed.
	// Use with discretion.
	watcher, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: mgr.GetScheme(),
		Mapper: mgr.GetRESTMapper(),
	})
	if err != nil {
		setupLog.Error(err, "failed to create watching client")
		os.Exit(1)
	}
	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "failed to build dynamic client")
		os.Exit(1)
	}
	watchFleetMembership := fleetMembershipCRDExists(dynamicClient, mgr.GetRESTMapper(), &setupLog)

	crdController := controllers.NewCRDReconciler(
		textlogger.NewLogger(textlogger.NewConfig()).WithName("controllers").WithName("CRD"))
	if err := crdController.Register(mgr); err != nil {
		setupLog.Error(err, "failed to register controller", "controller", "CRD")
		os.Exit(1)
	}
	setupLog.Info("CRD controller registration successful")

	repoSyncController := controllers.NewRepoSyncReconciler(*clusterName,
		*reconcilerPollingPeriod, *hydrationPollingPeriod,
		mgr.GetClient(), watcher, dynamicClient,
		textlogger.NewLogger(textlogger.NewConfig()).WithName("controllers").WithName(configsync.RepoSyncKind),
		mgr.GetScheme())
	crdController.SetCRDHandler(configsync.RepoSyncCRDName, func() error {
		if err := repoSyncController.Register(mgr, watchFleetMembership); err != nil {
			return fmt.Errorf("registering %s controller: %w", configsync.RepoSyncKind, err)
		}
		setupLog.Info("RepoSync controller registration successful")
		return nil
	})
	setupLog.Info("RepoSync controller registration scheduled")

	rootSyncController := controllers.NewRootSyncReconciler(*clusterName,
		*reconcilerPollingPeriod, *hydrationPollingPeriod,
		mgr.GetClient(), watcher, dynamicClient,
		textlogger.NewLogger(textlogger.NewConfig()).WithName("controllers").WithName(configsync.RootSyncKind),
		mgr.GetScheme())
	crdController.SetCRDHandler(configsync.RootSyncCRDName, func() error {
		if err := rootSyncController.Register(mgr, watchFleetMembership); err != nil {
			return fmt.Errorf("registering %s controller: %w", configsync.RootSyncKind, err)
		}
		setupLog.Info("RootSync controller registration successful")
		return nil
	})
	setupLog.Info("RootSync controller registration scheduled")

	otel := controllers.NewOtelReconciler(*clusterName, mgr.GetClient(),
		textlogger.NewLogger(textlogger.NewConfig()).WithName("controllers").WithName("Otel"),
		mgr.GetScheme())
	if err := otel.Register(mgr); err != nil {
		setupLog.Error(err, "failed to register controller", "controller", "Otel")
		os.Exit(1)
	}
	setupLog.Info("Otel controller registration successful")

	otelSA := controllers.NewOtelSAReconciler(*clusterName, mgr.GetClient(),
		textlogger.NewLogger(textlogger.NewConfig()).WithName("controllers").WithName(controllers.OtelSALoggerName),
		mgr.GetScheme())
	if err := otelSA.Register(mgr); err != nil {
		setupLog.Error(err, "failed to register controller", "controller", "OtelSA")
		os.Exit(1)
	}
	setupLog.Info("OtelSA controller registration successful")

	// Register the OpenCensus views
	if err := metrics.RegisterReconcilerManagerMetricsViews(); err != nil {
		setupLog.Error(err, "failed to register OpenCensus views")
	}

	// Register the OC Agent exporter
	oce, err := metrics.RegisterOCAgentExporter(reconcilermanager.ManagerName)
	if err != nil {
		setupLog.Error(err, "failed to register the OC Agent exporter")
		os.Exit(1)
	}

	defer func() {
		if err := oce.Stop(); err != nil {
			setupLog.Error(err, "failed to stop the OC Agent exporter")
		}
	}()

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		// os.Exit(1) does not run deferred functions so explicitly stopping the OC Agent exporter.
		if err := oce.Stop(); err != nil {
			setupLog.Error(err, "failed to stop the OC Agent exporter")
		}
		os.Exit(1)
	}
}

// fleetMembershipCRDExists checks if the fleet membership CRD exists.
// It checks the CRD first so that the controller can watch the Membership
// resource in the startup time.
func fleetMembershipCRDExists(dc dynamic.Interface, mapper meta.RESTMapper,
	logger *logr.Logger) bool {

	crdRESTMapping, err := mapper.RESTMapping(kinds.CustomResourceDefinition())
	if err != nil {
		logger.Error(err, "failed to get mapping of CRD type")
		os.Exit(1)
	}
	_, err = dc.Resource(crdRESTMapping.Resource).Get(context.TODO(), "memberships.hub.gke.io", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("The memberships CRD doesn't exist")
		} else {
			logger.Error(err, "failed to GET the CRD for the memberships resource from the cluster")
		}
		return false
	}
	return true
}
