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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2/klogr"
	configmanagementv1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	ctrl "sigs.k8s.io/controller-runtime"
	// +kubebuilder:scaffold:imports
)

var (
	clusterName = flag.String("cluster-name", os.Getenv(reconcilermanager.ClusterNameKey),
		"Cluster name to use for Cluster selection")

	reconcilerPollingPeriod = flag.Duration("reconciler-polling-period", controllers.PollingPeriod(reconcilermanager.ReconcilerPollingPeriod, configsync.DefaultReconcilerPollingPeriod),
		"How often the reconciler should poll the filesystem for updates to the source or rendered configs.")

	hydrationPollingPeriod = flag.Duration("hydration-polling-period", controllers.PollingPeriod(reconcilermanager.HydrationPollingPeriod, configsync.DefaultHydrationPollingPeriod),
		"How often the hydration-controller should poll the filesystem for rendering the DRY configs.")

	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	// klogr flags
	_ = flag.Set("v", "1")
	_ = flag.Set("logtostderr", "true")

	_ = clientgoscheme.AddToScheme(scheme)
	_ = configmanagementv1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)
	_ = hubv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	profiler.Service()
	ctrl.SetLogger(klogr.New())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	watchFleetMembership := fleetMembershipCRDExists(mgr.GetConfig(), mgr.GetRESTMapper())

	repoSync := controllers.NewRepoSyncReconciler(*clusterName, *reconcilerPollingPeriod, *hydrationPollingPeriod, mgr.GetClient(),
		ctrl.Log.WithName("controllers").WithName("RepoSync"),
		mgr.GetScheme())
	if err := repoSync.SetupWithManager(mgr, watchFleetMembership); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RepoSync")
		os.Exit(1)
	}

	rootSync := controllers.NewRootSyncReconciler(*clusterName, *reconcilerPollingPeriod, *hydrationPollingPeriod, mgr.GetClient(),
		ctrl.Log.WithName("controllers").WithName("RootSync"),
		mgr.GetScheme())
	if err := rootSync.SetupWithManager(mgr, watchFleetMembership); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RootSync")
		os.Exit(1)
	}

	otel := controllers.NewOtelReconciler(*clusterName, mgr.GetClient(),
		ctrl.Log.WithName("controllers").WithName("Otel"),
		mgr.GetScheme())
	if err := otel.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Otel")
		os.Exit(1)
	}

	// Register the OpenCensus views
	if err := metrics.RegisterReconcilerManagerMetricsViews(); err != nil {
		setupLog.Error(err, "failed to register OpenCensus views")
	}

	// Register the OC Agent exporter
	oce, err := metrics.RegisterOCAgentExporter()
	if err != nil {
		setupLog.Error(err, "failed to register the OC Agent exporter")
		os.Exit(1)
	}

	defer func() {
		if err := oce.Stop(); err != nil {
			setupLog.Error(err, "unable to stop the OC Agent exporter")
		}
	}()

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		// os.Exit(1) does not run deferred functions so explicitly stopping the OC Agent exporter.
		if err := oce.Stop(); err != nil {
			setupLog.Error(err, "unable to stop the OC Agent exporter")
		}
		os.Exit(1)
	}
}

// fleetMembershipCRDExists checks if the fleet membership CRD exists.
// It checks the CRD first so that the controller can watch the Membership resource in the startup time.
func fleetMembershipCRDExists(config *rest.Config, mapper meta.RESTMapper) bool {
	dc, err := dynamic.NewForConfig(config)
	if err != nil {
		setupLog.Error(err, "failed to build dynamic client")
		os.Exit(1)
	}
	crdRESTMapping, err := mapper.RESTMapping(kinds.CustomResourceDefinition())
	if err != nil {
		setupLog.Error(err, "failed to get mapping of CRD type")
		os.Exit(1)
	}
	_, err = dc.Resource(crdRESTMapping.Resource).Get(context.TODO(), "memberships.hub.gke.io", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			setupLog.Info("The memberships CRD doesn't exist")
		} else {
			setupLog.Error(err, "failed to GET the CRD for the memberships resource from the cluster")
		}
		return false
	}
	return true
}
