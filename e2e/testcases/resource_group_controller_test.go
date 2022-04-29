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
	"time"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/resourcegroup"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestResourceGroupController(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.InstallResourceGroupController)

	ns := "rg-test"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/rg-test/ns.yaml",
		fake.NamespaceObject(ns))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/rg-test/configmap.yaml"
	cm := fake.ConfigMapObject(core.Name(cmName), core.Namespace(ns))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding a ConfigMap to repo")
	nt.WaitForRepoSyncs()

	// Checking that the ResourceGroup controller captures the status of the
	// managed resources.
	id := applier.InventoryID(configsync.RootSyncName, configsync.ControllerNamespace)
	_, err := nomostest.Retry(60*time.Second, func() error {
		rg := resourcegroup.Unstructured(configsync.RootSyncName, configsync.ControllerNamespace, id)
		err := nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, rg,
			nomostest.AllResourcesAreCurrent())
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}
