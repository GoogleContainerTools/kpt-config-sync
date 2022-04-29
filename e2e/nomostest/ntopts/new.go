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

package ntopts

import (
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/testing"
)

// Opt is an option type for ntopts.New.
type Opt func(opt *New)

// New is the set of options for instantiating a new NT test.
type New struct {
	// Name is the name of the test. Overrides the one generated from the test
	// name.
	Name string

	// TmpDir is the base temporary directory to use for the test. Overrides the
	// generated directory based on Name and the OS's main temporary directory.
	TmpDir string

	// RESTConfig is the config for creating a Client connection to a K8s cluster.
	RESTConfig *rest.Config

	// SkipAutopilot will skip the test if running on an Autopilot cluster.
	SkipAutopilot bool

	Nomos
	MultiRepo
	TestType
}

// RequireManual requires the --manual flag is set. Otherwise it will skip the test.
// This avoids running tests (e.g stress tests) that aren't safe to run against a remote cluster automatically.
func RequireManual(t testing.NTB) Opt {
	if !*e2e.Manual {
		t.Skip("Must pass --manual so this isn't accidentally run against a test cluster automatically.")
	}
	return func(opt *New) {}
}

// SkipAutopilotCluster will skip the test on the autopilot cluster.
func SkipAutopilotCluster(opt *New) {
	opt.SkipAutopilot = true
}

// RequireGKE requires the --test-cluster flag to be `gke` so that the test only runs on GKE clusters.
func RequireGKE(t testing.NTB) Opt {
	if *e2e.TestCluster != e2e.GKE {
		t.Skip("The --test-cluster flag must be set to `gke` to run this test.")
	}
	return func(opt *New) {}
}
