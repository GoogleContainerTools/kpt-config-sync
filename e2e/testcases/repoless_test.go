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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	testing2 "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/kinds"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

const (
	importerRepoless            = "../raw-nomos/manifests/importer_repoless.yaml"
	importerRepolessNoNS        = "../raw-nomos/manifests/importer_repoless-no-ns.yaml"
	importerRepolessNoPolicyDir = "../raw-nomos/manifests/importer_no-policy-dir.yaml"
)

func TestSyncCorrectlyWithExplicitNamespaceDeclarations(t *testing.T) {
	nt := nomostest.New(t, testing2.Reconciliation2, ntopts.Unstructured, ntopts.SkipMultiRepo)
	nt.MustKubectl("apply", "-f", importerRepoless)
	restartMonoRepoPods(nt)
	addAndCommitSampleRepo("../../examples/repoless", nt)
	validateResourcesReady(nt)
}

func TestSyncCorrectlyWithOnlyRoles(t *testing.T) {
	nt := nomostest.New(t, testing2.Reconciliation2, ntopts.Unstructured, ntopts.SkipMultiRepo)
	nt.MustKubectl("apply", "-f", importerRepolessNoNS)
	restartMonoRepoPods(nt)
	addAndCommitSampleRepo("../../examples/repoless-no-ns", nt)
	validateResourcesReady(nt)
}

func TestSyncCorrectlyWithNoPolicyDirSet(t *testing.T) {
	nt := nomostest.New(t, testing2.Reconciliation2, ntopts.Unstructured, ntopts.SkipMultiRepo)
	nt.MustKubectl("apply", "-f", importerRepolessNoPolicyDir)
	restartMonoRepoPods(nt)
	addAndCommitSampleRepo("../../examples/repoless", nt)
	validateResourcesReady(nt)
}

// Confirm all the namespaces and pods are up
func validateResourcesReady(nt *nomostest.NT) {
	time, e := nomostest.Retry(nt.DefaultReconcileTimeout, func() error {
		err := nt.Validate("backend", "", &corev1.Namespace{})
		if err != nil {
			nt.T.Fatal(err)
		}
		err = nt.Validate("backend-default", "", &corev1.Namespace{})
		if err != nil {
			nt.T.Fatal(err)
		}
		err = nt.Validate("repoless-admin", "", &rbacv1.ClusterRole{}, nomostest.StatusEquals(nt, kstatus.CurrentStatus))
		if err != nil {
			nt.T.Fatal(err)
		}
		err = nt.Validate("pod-reader-backend", "backend", &rbacv1.Role{}, nomostest.StatusEquals(nt, kstatus.CurrentStatus))
		if err != nil {
			nt.T.Fatal(err)
		}
		err = nt.Validate("pod-reader-default", "backend-default", &rbacv1.Role{}, nomostest.StatusEquals(nt, kstatus.CurrentStatus))
		if err != nil {
			nt.T.Fatal(err)
		}
		checkResourceCount(nt, kinds.Namespace(), "", 0, map[string]string{"backend.tree.hnc.x-k8s.io": "0"}, nil)
		checkResourceCount(nt, kinds.Namespace(), "", 0, map[string]string{"backend-default.tree.hnc.x-k8s.io": "0"}, nil)
		checkResourceCount(nt, kinds.Namespace(), "", 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
		return err
	})
	if e != nil {
		nt.T.Fatal("timed out waiting for resources to be ready")
	}
	nt.T.Logf("took %v to wait for resources to be ready", time)
}

// restart pods so that new manifest can be loaded
func restartMonoRepoPods(nt *nomostest.NT) {
	nomostest.DeletePodByLabel(nt, "app", "git-importer", false)
	nomostest.DeletePodByLabel(nt, "app", "monitor", false)
}

func addAndCommitSampleRepo(repo string, nt *nomostest.NT) {
	nt.RootRepos[configsync.RootSyncName].Copy(repo, ".")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Adding and commiting %s repo", repo))
	nt.WaitForRepoSyncs()
}
