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
	"testing"
	"time"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
)

func TestMain(m *testing.M) {
	// This TestMain function is required in every e2e test case file.
	flag.Parse()

	if !*e2e.E2E && !*e2e.Load && !*e2e.Stress {
		// This allows `go test ./...` to function as expected without triggering any long running tests.
		return
	}
	rand.Seed(time.Now().UnixNano())

	if *e2e.ShareTestEnv {
		if e2e.RunInParallel() {
			fmt.Println("The test cannot use a shared test environment if it is running in parallel")
			os.Exit(1)
		}
		sharedNT := nomostest.NewSharedNT()
		exitCode := m.Run()
		nomostest.Clean(sharedNT, false)
		os.Exit(exitCode)
	} else {
		os.Exit(m.Run())
	}
}
