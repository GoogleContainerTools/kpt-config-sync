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
	"testing"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestIgnoreKptfiles(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1)

	// Add multiple Kptfiles
	nt.Must(nt.RootRepos[configsync.RootSyncName].AddFile("acme/cluster/Kptfile", []byte("random content")))
	nt.Must(nt.RootRepos[configsync.RootSyncName].AddFile("acme/namespaces/foo/Kptfile", nil))
	nt.Must(nt.RootRepos[configsync.RootSyncName].AddFile("acme/namespaces/foo/subdir/Kptfile", []byte("# some comment")))
	nsObj := fake.NamespaceObject("foo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding multiple Kptfiles"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.RenewClient()

	err := nt.Validate("foo", "", fake.NamespaceObject("foo"))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}
