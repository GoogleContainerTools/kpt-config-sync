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

package log

import (
	"flag"

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// InitFlags registers the klog command flags with new defaults.
// Call flag.Parse() afterwards to parse input from the command line.
func InitFlags() {
	// Register klog flags
	klog.InitFlags(nil)

	// Override klog default values
	if err := flag.Set("v", "1"); err != nil {
		klog.Fatal(err)
	}
	if err := flag.Set("logtostderr", "true"); err != nil {
		klog.Fatal(err)
	}

	// Configure controller-runtime to use klog, via the klogr library
	ctrl.SetLogger(klogr.New())
}
