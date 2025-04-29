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

package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

var (
	runtimeScheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(runtimeScheme))
	utilruntime.Must(v1beta1.AddToScheme(runtimeScheme))
}

// run initializes and starts the controller manager with RootSync and RepoSync controllers.
// It returns an error if any step fails.
func run() error {
	zapLog, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer zapLog.Sync()
	logger := zapr.NewLogger(zapLog)

	ctrl.SetLogger(logger)

	logger.Info("Starting standalone SyncStatusController")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 runtimeScheme,
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// Add health check endpoint
	if err := mgr.AddHealthzCheck("healthz", func(_ *http.Request) error { return nil }); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	// Create a shared status tracker
	statusTracker := NewStatusTracker()

	// Create a dedicated controller for RootSync
	rootSyncController := NewSyncStatusController(
		mgr.GetClient(),
		logger.WithName("rootsync-controller"),
		statusTracker,
		RootSyncKind,
	)

	// Create a dedicated controller for RepoSync
	repoSyncController := NewSyncStatusController(
		mgr.GetClient(),
		logger.WithName("reposync-controller"),
		statusTracker,
		RepoSyncKind,
	)

	// Set up RootSync controller
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.RootSync{}).
		Complete(rootSyncController); err != nil {
		return fmt.Errorf("unable to set up RootSync controller: %w", err)
	}

	// Set up RepoSync controller
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.RepoSync{}).
		Complete(repoSyncController); err != nil {
		return fmt.Errorf("unable to set up RepoSync controller: %w", err)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
