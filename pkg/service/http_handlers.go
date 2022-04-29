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

// HTTP handler functions, ready for reuse.

package service

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

var metricsPort = flag.Int("metrics-port", 8675, "The port to export prometheus metrics on.")

// NoCache positively turns off page caching.
func noCache(handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set(
			"Cache-Control",
			"no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		handler.ServeHTTP(w, req)
	}
}

// ServeMetrics spins up a standalone metrics HTTP endpoint.
func ServeMetrics() {
	// Expose prometheus metrics via HTTP.
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/threads", noCache(http.HandlerFunc(goRoutineHandler)))
	klog.Infof("Serving metrics on :%d/metrics", *metricsPort)
	err := http.ListenAndServe(fmt.Sprintf(":%d", *metricsPort), nil)
	if err != nil {
		klog.Fatalf("HTTP ListenAndServe for metrics: %+v", err)
	}
}
