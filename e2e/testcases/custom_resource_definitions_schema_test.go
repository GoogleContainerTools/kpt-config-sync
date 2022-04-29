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
	"io/ioutil"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestChangeCustomResourceDefinitionSchema(t *testing.T) {
	nt := nomostest.New(t)

	oldCRDFile := filepath.Join(".", "..", "testdata", "customresources", "changed_schema_crds", "old_schema_crd.yaml")
	newCRDFile := filepath.Join(".", "..", "testdata", "customresources", "changed_schema_crds", "new_schema_crd.yaml")
	oldCRFile := filepath.Join(".", "..", "testdata", "customresources", "changed_schema_crds", "old_schema_cr.yaml")
	newCRFile := filepath.Join(".", "..", "testdata", "customresources", "changed_schema_crds", "new_schema_cr.yaml")

	// Add a CRD and CR to the repo
	crdContent, err := ioutil.ReadFile(oldCRDFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	crContent, err := ioutil.ReadFile(oldCRFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.RootRepos[configsync.RootSyncName].AddFile("acme/cluster/crd.yaml", crdContent)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].AddFile("acme/namespaces/foo/cr.yaml", crContent)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding a CRD and CR")
	nt.WaitForRepoSyncs()

	err = nt.Validate("my-cron-object", "foo", crForSchema())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Restart the ConfigSync importer or reconciler pods.
	// So that the old schema of the CRD is picked.
	if nt.MultiRepo {
		nt.MustKubectl("delete", "pods", "-n", "config-management-system", "-l", "configsync.gke.io/reconciler=root-reconciler")
	} else {
		nt.MustKubectl("delete", "pods", "-n", "config-management-system", "-l", "app=git-importer")
	}
	nt.WaitForRepoSyncs()

	// Add the CRD with a new schema and a CR using the new schema to the repo
	crdContent, err = ioutil.ReadFile(newCRDFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	crContent, err = ioutil.ReadFile(newCRFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.RootRepos[configsync.RootSyncName].AddFile("acme/cluster/crd.yaml", crdContent)
	nt.RootRepos[configsync.RootSyncName].AddFile("acme/namespaces/foo/cr.yaml", crContent)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding the CRD with new schema and a CR using the new schema")
	nt.WaitForRepoSyncs()

	err = nt.Validate("my-new-cron-object", "foo", crForSchema())
	if err != nil {
		nt.T.Fatal(err)
	}
}

func crForSchema() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "stable.example.com",
		Version: "v1",
		Kind:    "CronTab",
	})
	return u
}
