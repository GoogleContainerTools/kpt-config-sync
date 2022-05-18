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

// Controller responsible for importing policies from a Git repo and materializing CRDs
// on the local cluster.
package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/configsync"
	"kpt.dev/configsync/pkg/profiler"
	"kpt.dev/configsync/pkg/service"
	"kpt.dev/configsync/pkg/util/log"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	watchDirectory = flag.String("watch-directory", "", "Watch a directory and log filesystem changes instead of running as importer")
	watchPeriod    = flag.Duration("watch-period", getEnvDuration("WATCH_PERIOD", time.Second), "Period at which to poll the watch directory for changes.")
)

func getEnvDuration(key string, defaultDuration time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultDuration
	}

	duration, err := time.ParseDuration(val)
	if err != nil {
		klog.Errorf("Failed to parse duration %q from env var %s: %s", val, key, err)
		return defaultDuration
	}
	return duration
}

func main() {
	log.Setup()
	profiler.Service()
	ctrl.SetLogger(klogr.New())
	if *watchDirectory != "" {
		configsync.DirWatcher(*watchDirectory, *watchPeriod)
		os.Exit(0)
	}

	go service.ServeMetrics()
	configsync.RunImporter()
}
