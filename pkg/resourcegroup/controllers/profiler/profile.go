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

package profiler

import (
	"flag"
	"fmt"
	"net/http"

	//nolint:gosec // pprof init() registers handlers, which we serve with ListenAndServe
	_ "net/http/pprof"
	"time"

	"k8s.io/klog/v2"
)

var enableProfiler = flag.Bool("enable-pprof", false, "enable pprof profiling")
var profilerPort = flag.Int("pprof-port", 6060, "port for pprof profiling. defaulted to 6060 if unspecified")

// Service starts the profiler http endpoint if --enable-pprof flag is passed
func Service() {
	if *enableProfiler {
		go func() {
			klog.Infof("Starting profiling on port %d", *profilerPort)
			addr := fmt.Sprintf(":%d", *profilerPort)
			server := &http.Server{
				Addr:              addr,
				ReadHeaderTimeout: 30 * time.Second,
			}
			err := server.ListenAndServe()
			if err != nil {
				klog.Fatalf("Profiler server failed to start: %+v", err)
			}
		}()
	}
}
