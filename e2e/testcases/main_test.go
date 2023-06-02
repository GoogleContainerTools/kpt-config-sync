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

	if *e2e.ShareTestEnv {
		if *e2e.TestCluster == e2e.GKE && e2e.RunInParallel() {
			fmt.Println("The test cannot use a shared test environment if it is running in parallel on GKE")
			return 1
		}
		defer nomostest.CleanSharedNTs()
		if err := nomostest.InitSharedEnvironments(); err != nil {
			fmt.Printf("Error in InitSharedEnvironments: %v\n", err)
			return 1
		}
	}
	exitCode := m.Run()
	return exitCode
}
