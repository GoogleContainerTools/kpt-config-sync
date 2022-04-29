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

package status

import (
	"flag"

	"k8s.io/klog/v2"
)

var panicOnMisuse = false

func init() {
	if flag.Lookup("test.v") != nil {
		// Running with "go test"
		EnablePanicOnMisuse()
	}
}

// EnablePanicOnMisuse makes status.Error ensure errors are properly formatted,
// aren't wrapped unnecessarily, and so on. Should only be enabled in debugging
// and test settings, not in production.
func EnablePanicOnMisuse() {
	panicOnMisuse = true
}

// reportMisuse either panics, or logs an error with klog.Errorf depending on
// whether panicOnMisuse is true.
func reportMisuse(message string) {
	if panicOnMisuse {
		// We're in debug mode, so halt execution so the runner is likely to get
		// a signal that something is wrong.
		panic(message)
	} else {
		// Show it in the logs, but don't kill the application in production.
		klog.Errorf("internal error: %s", message)
	}
}
