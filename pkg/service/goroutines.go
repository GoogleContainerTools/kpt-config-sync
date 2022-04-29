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

package service

import (
	"fmt"
	"net/http"
	"runtime/pprof"
)

// GoRoutineHandler is a handler that will print the goroutine stacks to the response.
func goRoutineHandler(w http.ResponseWriter, _ *http.Request) {
	ps := pprof.Profiles()
	for _, p := range ps {
		if p.Name() == "goroutine" {
			if err := p.WriteTo(w, 2); err != nil {
				response := fmt.Sprintf("error while writing goroutine stakcs: %s", err)
				// nolint:errcheck
				_, _ = w.Write([]byte(response))
			}
			return
		}
	}

	response := "unable to find profile for goroutines"
	// nolint:errcheck
	_, _ = w.Write([]byte(response))
}
