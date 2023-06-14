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

package e2e

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/errors"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
)

// This is a bit of a hack to enforce our --num-clusters flag over the --test.parallel
// parameter of `go test`. The `go test` defaults to GOMAXPROCS, which is sane
// for unit tests but is an undesirable default for e2e tests which create
// a cluster per thread. Default to 1 instead.
func setParallelFlag() error {
	err := flag.Set("test.parallel", strconv.Itoa(e2e.NumParallel()))
	return err
}

func validateArgs() error {
	if *e2e.TestCluster == e2e.GKE { // required vars for GKE
		if *e2e.GCPProject == "" {
			return errors.Errorf("Environment variable GCP_PROJECT is required for GKE clusters")
		}
		if *e2e.GCPCluster == "" && !*e2e.CreateClusters {
			return errors.Errorf("One of GCP_CLUSTER or CREATE_CLUSTERS is required for GKE clusters")
		}
		if *e2e.GCPCluster != "" && *e2e.CreateClusters {
			return errors.Errorf("At most one of GCP_CLUSTER or CREATE_CLUSTERS may be specified")
		}
		if e2e.RunInParallel() && !*e2e.CreateClusters {
			return errors.Errorf("Must provide CREATE_CLUSTERS to run in parallel on GKE")
		}
		if *e2e.GCPRegion == "" && *e2e.GCPZone == "" {
			return errors.Errorf("One of GCP_REGION or GCP_ZONE is required for GKE clusters")
		}
		if *e2e.GCPRegion != "" && *e2e.GCPZone != "" {
			return errors.Errorf("At most one of GCP_ZONE or GCP_REGION may be specified")
		}
		if *e2e.GKEAutopilot && *e2e.GCPRegion == "" {
			return errors.Errorf("Autopilot clusters must be created with a region")
		}
		if *e2e.GKEAutopilot && *e2e.GceNode {
			return errors.Errorf("Cannot run gcenode tests on autopilot clusters")
		}
	}
	return nil
}

func TestMain(m *testing.M) {
	os.Exit(main(m))
}

func main(m *testing.M) int {
	// This TestMain function is required in every e2e test case file.
	flag.Parse()

	if !*e2e.E2E && !*e2e.Load && !*e2e.Stress {
		// This allows `go test ./...` to function as expected without triggering any long running tests.
		return 0
	}
	if *e2e.Usage {
		flag.Usage()
		return 0
	}
	if err := setParallelFlag(); err != nil {
		fmt.Printf("Error setting test.parallel: %v\n", err)
		return 1
	}
	rand.Seed(time.Now().UnixNano())

	if err := validateArgs(); err != nil {
		fmt.Printf("error validating e2e test args: %v\n", err)
		return 1
	}

	if *e2e.ShareTestEnv {
		defer nomostest.CleanSharedNTs()
		if err := nomostest.InitSharedEnvironments(); err != nil {
			fmt.Printf("Error in InitSharedEnvironments: %v\n", err)
			return 1
		}
	}
	exitCode := m.Run()
	return exitCode
}
