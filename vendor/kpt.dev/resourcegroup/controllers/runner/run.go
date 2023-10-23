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

package runner

import (
	"flag"
	"fmt"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // register gcp auth provider plugin
	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"kpt.dev/resourcegroup/controllers/log"
	ocmetrics "kpt.dev/resourcegroup/controllers/metrics"
	"kpt.dev/resourcegroup/controllers/profiler"
	"kpt.dev/resourcegroup/controllers/resourcegroup"
	"kpt.dev/resourcegroup/controllers/resourcemap"
	"kpt.dev/resourcegroup/controllers/root"
	"kpt.dev/resourcegroup/controllers/typeresolver"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// Run starts all controllers and returns an integer exit code.
func Run() int {
	if err := run(); err != nil {
		setupLog.Error(err, "exiting")
		return 1
	}
	return 0
}

func run() error {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = v1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme

	_ = apiextensionsv1.AddToScheme(scheme)

	log.InitFlags()

	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	profiler.Service()

	// Register the OpenCensus views
	if err := ocmetrics.RegisterReconcilerMetricsViews(); err != nil {
		return fmt.Errorf("failed to register OpenCensus views: %w", err)
	}

	// Register the OC Agent exporter
	oce, err := ocmetrics.RegisterOCAgentExporter()
	if err != nil {
		return fmt.Errorf("failed to register the OC Agent exporter: %w", err)
	}

	defer func() {
		if err := oce.Stop(); err != nil {
			setupLog.Error(err, "Unable to stop the OC Agent exporter")
		}
	}()
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "413d8c8e.gke.io",
	})
	if err != nil {
		return fmt.Errorf("failed to build manager: %w", err)
	}

	logger := ctrl.Log.WithName("controllers")

	for _, group := range []string{root.KptGroup} {
		if err := registerControllersForGroup(mgr, logger, group); err != nil {
			return fmt.Errorf("failed to register controllers for group %s: %w", group, err)
		}
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("failed to start controller-manager: %w", err)
	}
	return nil
}

func registerControllersForGroup(mgr ctrl.Manager, logger logr.Logger, group string) error {
	// channel is watched by ResourceGroup controller.
	// The Root controller pushes events to it and
	// the ResourceGroup controller consumes events.
	channel := make(chan event.GenericEvent)

	setupLog.Info("adding the type resolver")
	resolver, err := typeresolver.NewTypeResolver(mgr, logger.WithName("TypeResolver"))
	if err != nil {
		return fmt.Errorf("unable to set up the type resolver: %w", err)
	}
	resolver.Refresh()

	setupLog.Info("adding the Root controller for group " + group)
	resMap := resourcemap.NewResourceMap()
	if err := root.NewController(mgr, channel, logger.WithName("Root"), resolver, group, resMap); err != nil {
		return fmt.Errorf("unable to create the root controller for group %s: %w", group, err)
	}

	setupLog.Info("adding the ResourceGroup controller for group " + group)
	if err := resourcegroup.NewRGController(mgr, channel, logger.WithName(v1alpha1.ResourceGroupKind), resolver, resMap, resourcegroup.DefaultDuration); err != nil {
		return fmt.Errorf("unable to create the ResourceGroup controller %s: %w", group, err)
	}
	return nil
}
