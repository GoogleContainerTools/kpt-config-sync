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
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	syncBranch = "main"
)

// The CSR repo was built from the directory e2e/testdata/hydration/kustomize-components,
// which includes a kustomization.yaml file in the root directory that
// references resources for tenant-a, tenant-b, and tenant-c.
// Each tenant includes a NetworkPolicy, a Role and a RoleBinding.
func csrRepo() string {
	return fmt.Sprintf("%s/p/%s/r/kustomize-components", nomostesting.CSRHost, *e2e.GCPProject)
}

func gsaCSRReaderEmail() string {
	return fmt.Sprintf("e2e-test-csr-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// TestGCENode tests the `gcenode` auth type.
//
// The test will run on a GKE cluster only with following pre-requisites:
//
//  1. Workload Identity is NOT enabled
//  2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
//  3. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` has `source.reader`
//     access to Cloud Source Repository.
//
// Public documentation:
// https://cloud.google.com/anthos-config-management/docs/how-to/installing-config-sync#git-creds-secret
func TestGCENode(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest)

	if err := workloadidentity.ValidateDisabled(nt); err != nil {
		nt.T.Fatal(err)
	}

	tenant := "tenant-a"
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a CSR repo")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s", "branch": "%s", "repo": "%s", "auth": "gcenode", "secretRef": {"name": ""}}}}`,
		tenant, syncBranch, csrRepo()))

	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
	if err := testutils.ReconcilerPodMissingFWICredsAnnotation(nt, nomostest.DefaultRootReconcilerName); err != nil {
		nt.T.Fatal(err)
	}
}
