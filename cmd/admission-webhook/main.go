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

	"k8s.io/klog/klogr"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/util/log"
	"kpt.dev/configsync/pkg/webhook"
	"kpt.dev/configsync/pkg/webhook/configuration"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	setupLog = ctrl.Log.WithName("setup")

	restartOnSecretRefresh bool
)

func main() {
	// Default to true since not restarting doesn't make sense?
	flag.BoolVar(&restartOnSecretRefresh, "cert-restart-on-secret-refresh", true, "Kills the process when secrets are refreshed so that the pod can be restarted (secrets take up to 60s to be updated by running pods)")

	log.Setup()

	profiler.Service()
	ctrl.SetLogger(klogr.New())

	setupLog.Info("starting manager")
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Port:    configuration.ContainerPort,
		CertDir: configuration.CertDir,
	})
	if err != nil {
		setupLog.Error(err, "starting manager")
		os.Exit(1)
	}

	setupLog.Info("creating certificate rotator for webhook")
	certDone, err := webhook.CreateCertsIfNeeded(mgr, restartOnSecretRefresh)
	if err != nil {
		setupLog.Error(err, "creating certificate rotator for webhook")
	}

	// We can't block on waiting for the cert rotator to be up: the Manager
	// launches this process in Start(). However, we must wait for the cert
	// rotator before registering the webhook in the Manager.
	go startControllers(mgr, certDone)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "running manager")
		os.Exit(1)
	}
}

func startControllers(mgr manager.Manager, certDone chan struct{}) {
	setupLog.Info("waiting for certificate rotator")
	<-certDone

	setupLog.Info("registering validating webhook")
	if err := webhook.AddValidator(mgr); err != nil {
		setupLog.Error(err, "registering validating webhook")
		os.Exit(1)
	}
}
