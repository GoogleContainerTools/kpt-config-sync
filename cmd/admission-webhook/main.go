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
	"fmt"
	"net/http"
	"os"
	"time"

	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/util/log"
	"kpt.dev/configsync/pkg/webhook"
	"kpt.dev/configsync/pkg/webhook/configuration"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

var (
	setupLog = ctrl.Log.WithName("setup")

	restartOnSecretRefresh  bool
	healthProbeBindAddress  string
	gracefulShutdownTimeout time.Duration
	cacheSyncTimeout        time.Duration
)

func main() {
	// Default to true since not restarting doesn't make sense?
	flag.BoolVar(&restartOnSecretRefresh, "cert-restart-on-secret-refresh", true, "Kills the process when secrets are refreshed so that the pod can be restarted (secrets take up to 60s to be updated by running pods)")
	flag.StringVar(&healthProbeBindAddress, "health-probe-bind-addr", fmt.Sprintf(":%d", configuration.HealthProbePort), "The address the healthz & readyz probes bind to.")
	flag.DurationVar(&gracefulShutdownTimeout, "graceful-shutdown-timeout", configuration.GracefulShutdownTimeout, "The duration of time to wait while shutting down for all controllers to stop.")
	flag.DurationVar(&cacheSyncTimeout, "cache-sync-timeout", configuration.CacheSyncTimeout, "The duration of time to wait while informers synchronize.")

	log.Setup()

	profiler.Service()
	ctrl.SetLogger(klogr.New())

	setupLog.Info("starting manager")
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Port:    configuration.ContainerPort,
		CertDir: configuration.CertDir,
		// Required for the ReadyzCheck
		HealthProbeBindAddress:  healthProbeBindAddress,
		GracefulShutdownTimeout: &gracefulShutdownTimeout,
		Controller: v1alpha1.ControllerConfigurationSpec{
			CacheSyncTimeout: &cacheSyncTimeout,
		},
	})
	if err != nil {
		setupLog.Error(err, "starting manager")
		os.Exit(1)
	}

	setupLog.Info("registering certificate rotator for webhook")
	certDone, err := webhook.CreateCertsIfNeeded(mgr, restartOnSecretRefresh)
	if err != nil {
		setupLog.Error(err, "unable to register certificate rotator for webhook")
		os.Exit(1)
	}

	validatorDone := make(chan struct{})
	var checker healthz.Checker

	// Block readiness on the validating webhook being registered.
	// We can't use mgr.GetWebhookServer().StartedChecker() yet,
	// because that starts the webhook. But we also can't call AddReadyzCheck
	// after Manager.Start. So we need a custom ready check that delegates to
	// the real ready check after the cert has been injected and validator started.
	setupLog.Info("registering /readyz endpoint for webhook")
	err = mgr.AddReadyzCheck("webhook", func(req *http.Request) error {
		select {
		case <-validatorDone:
			// done waiting
			return checker(req)
		default:
			// still waiting
			return fmt.Errorf("webhook validator has not been started yet")
		}
	})
	if err != nil {
		setupLog.Error(err, "unable to register ready check for webhook")
		os.Exit(1)
	}

	// The webhook depends on the cert rotator.
	// So block until the certs have been injected, then start the webhook.
	go func() {
		defer close(validatorDone)
		setupLog.Info("waiting for certificate rotator")
		<-certDone

		setupLog.Info("registering validator for webhook")
		if err := webhook.AddValidator(mgr); err != nil {
			setupLog.Error(err, "unable to register validator for webhook")
			os.Exit(1)
		}

		// Initialize checker so the existing readycheck can delegate to it.
		checker = mgr.GetWebhookServer().StartedChecker()
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "running manager")
		os.Exit(1)
	}
}
