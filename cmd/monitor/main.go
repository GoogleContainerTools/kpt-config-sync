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

// Controller responsible for monitoring the state of Nomos resources on the cluster.
package main

import (
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/monitor"
	"kpt.dev/configsync/pkg/service"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func main() {
	log.Setup()
	ctrl.SetLogger(klogr.New())

	cfg, err := restconfig.NewRestConfig(restconfig.DefaultTimeout)
	if err != nil {
		klog.Fatalf("Failed to create rest config: %v", err)
	}

	// Create a new Manager to provide shared dependencies and start components.
	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		klog.Fatalf("Failed to create manager: %v", err)
	}

	go service.ServeMetrics()

	// Setup all Controllers
	if err := monitor.AddToManager(mgr); err != nil {
		klog.Fatalf("Failed to add controller to manager: %v", err)
	}

	// Start the Manager.
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		klog.Fatalf("Error starting controller: %v", err)
	}
}
