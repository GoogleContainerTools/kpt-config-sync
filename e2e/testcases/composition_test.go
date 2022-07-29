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
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object/mutation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestComposition validates multi-layer composition of R*Syncs sharing a single
// git repo, using different directories.
//
// ├── repos
// │   └── config-management-system
// │       └── root-sync
// │           ├── acme
// │           │   ├── level-1.yaml (RootSync)
// │           │   ├── ns-test-ns.yaml
// │           │   ├── rb-test-ns.yaml
// │           │   └── README.md
// │           ├── level-1
// │           │   ├── level-2.yaml (RepoSync)
// │           │   └── README.md
// │           └── level-2
// │               ├── level-3.yaml (RepoSync)
// │               └── README.md
// │           └── level-3
// │               ├── level-4.yaml (ConfigMap)
// │               └── README.md
//
// This tests multiple things:
// 1. RootSync -> RootSyncs -> RepoSyncs -> RepoSyncs
// 2. RepoSyncs & RepoSyncs can share a repository, using different directories.
// 3. RepoSyncs can share an ssh-key secret
// 4. R*Sync status isn't updated after sync without external input.
func TestComposition(t *testing.T) {
	nt := nomostest.New(t,
		ntopts.SkipMonoRepo,
		ntopts.Unstructured,
		ntopts.WithDelegatedControl,
		ntopts.RootRepo(configsync.RootSyncName))

	lvl0NN := nomostest.RootSyncNN(configsync.RootSyncName)
	lvl1NN := nomostest.RootSyncNN("level-1")
	lvl2NN := types.NamespacedName{Namespace: testNs, Name: "level-2"}
	lvl3NN := types.NamespacedName{Namespace: testNs, Name: "level-3"}
	lvl4NN := types.NamespacedName{Namespace: testNs, Name: "level-4"}

	lvl0Repo := nt.RootRepos[lvl0NN.Name]

	t.Cleanup(func() {
		if t.Failed() {
			// level-0 is root-sync, which is always printed on test failure.
			// Print the rest of the deployment logs, which aren't registered under
			// nt.RootRepos/NonRootRepos, because they all share the root-sync repo.
			nt.PodLogs(configmanagement.ControllerNamespace, core.RootReconcilerName(lvl1NN.Name),
				reconcilermanager.Reconciler, false)
			nt.PodLogs(configmanagement.ControllerNamespace, core.NsReconcilerName(lvl2NN.Namespace, lvl2NN.Name),
				reconcilermanager.Reconciler, false)
			nt.PodLogs(configmanagement.ControllerNamespace, core.NsReconcilerName(lvl3NN.Namespace, lvl3NN.Name),
				reconcilermanager.Reconciler, false)
		}
	})

	nt.T.Log("Waiting for R*Syncs to be synced...")
	nt.WaitForRepoSyncs()

	// lvl0 RootSync syncs the acme/ dir
	lvl0Sync := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, lvl0NN.Name)
	lvl0Sync.Spec.Git.Dir = nomostest.AcmeDir

	// All R*Syncs will use different directories in the same root repo.
	// So this func returns the latest sha1 of the shared repo.
	rootSha1Fn := func(nt *nomostest.NT, _ types.NamespacedName) (string, error) {
		return lvl0Repo.Hash(), nil
	}

	nt.T.Logf("Adding Namespace & RoleBinding for RepoSync: %s", lvl2NN.Name)
	lvl0Repo.Add(filepath.Join(lvl0Sync.Spec.Git.Dir, fmt.Sprintf("ns-%s.yaml", testNs)), fake.NamespaceObject(lvl2NN.Namespace))
	lvl0Repo.Add(filepath.Join(lvl0Sync.Spec.Git.Dir, fmt.Sprintf("rb-%s.yaml", testNs)), nomostest.RepoSyncRoleBinding(lvl2NN))
	lvl0Repo.CommitAndPush(fmt.Sprintf("Adding Namespace & RoleBinding for RepoSync: %s", lvl2NN))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, rootSha1Fn, lvl0Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl0Sync.Spec.Git.Dir, true)...)

	nt.T.Logf("Creating Secret for RepoSync: %s", lvl2NN)
	nomostest.CreateNamespaceSecret(nt, lvl2NN.Namespace)

	// lvl1 RootSync
	lvl1Sync := nomostest.RootSyncObjectV1Beta1FromOtherRootRepo(nt, lvl1NN.Name, lvl0NN.Name)
	lvl1Sync.Spec.Git.Dir = lvl1NN.Name
	lvl1Path := filepath.Join(lvl0Sync.Spec.Git.Dir, fmt.Sprintf("%s.yaml", lvl1NN.Name))
	nt.T.Logf("Adding RootSync %s to the shared repository: %s", lvl1NN.Name, lvl1Path)
	lvl0Repo.Add(lvl1Path, lvl1Sync)
	// Add readme to create the git directory being synced, otherwise the RootSync won't reconcile
	lvl0Repo.AddFile(filepath.Join(lvl1Sync.Spec.Git.Dir, "README.md"), []byte("Test repository."))
	lvl0Repo.CommitAndPush(fmt.Sprintf("Adding RootSync: %s", lvl1NN))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, rootSha1Fn, lvl0Sync, lvl1Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl0Sync.Spec.Git.Dir, true)...)
	// lvl1Sync.Spec.Git.Dir contains no yaml yet, so we don't need to test it for reconciliation yet.

	// lvl2 RepoSync
	lvl2Sync := nomostest.RepoSyncObjectV1Beta1FromOtherRootRepo(nt, lvl2NN, lvl0NN.Name)
	lvl2Sync.Spec.Git.Dir = lvl2NN.Name
	lvl2Path := filepath.Join(lvl1Sync.Spec.Git.Dir, fmt.Sprintf("%s.yaml", lvl2NN.Name))
	nt.T.Logf("Adding RepoSync %s to the shared repository: %s", lvl2NN.Name, lvl2Path)
	lvl0Repo.Add(lvl2Path, lvl2Sync)
	// Add readme to create the git directory being synced, otherwise the RepoSync won't reconcile
	lvl0Repo.AddFile(filepath.Join(lvl2Sync.Spec.Git.Dir, "README.md"), []byte("Test repository."))
	lvl0Repo.CommitAndPush(fmt.Sprintf("Adding RepoSync: %s", lvl2NN))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, rootSha1Fn, lvl0Sync, lvl1Sync, lvl2Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl0Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl1Sync.Spec.Git.Dir, true)...)
	// lvl2Sync.Spec.Git.Dir contains no yaml yet, so we don't need to test it for reconciliation yet.

	// lvl3 RepoSync
	lvl3Sync := nomostest.RepoSyncObjectV1Beta1FromOtherRootRepo(nt, lvl3NN, lvl0NN.Name)
	lvl3Sync.Spec.Git.Dir = lvl3NN.Name
	lvl3Path := filepath.Join(lvl2Sync.Spec.Git.Dir, fmt.Sprintf("%s.yaml", lvl3NN.Name))
	nt.T.Logf("Adding RepoSync %s to the shared repository: %s", lvl3NN.Name, lvl3Path)
	lvl0Repo.Add(lvl3Path, lvl3Sync)
	// Add readme to create the git directory being synced, otherwise the RepoSync won't reconcile
	lvl0Repo.AddFile(filepath.Join(lvl3Sync.Spec.Git.Dir, "README.md"), []byte("Test repository."))
	lvl0Repo.CommitAndPush(fmt.Sprintf("Adding RepoSync: %s", lvl3NN))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, rootSha1Fn, lvl0Sync, lvl1Sync, lvl2Sync, lvl3Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl0Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl1Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl2Sync.Spec.Git.Dir, true)...)
	// lvl2Sync.Spec.Git.Dir contains no yaml yet, so we don't need to test it for reconciliation yet.

	// lvl4 ConfigMap
	lvl4ConfigMap := &corev1.ConfigMap{}
	lvl4ConfigMap.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	lvl4ConfigMap.SetNamespace(testNs)
	lvl4ConfigMap.SetName(lvl4NN.Name)
	lvl4ConfigMap.Data = map[string]string{"key": "value"}
	lvl4Path := filepath.Join(lvl3Sync.Spec.Git.Dir, fmt.Sprintf("%s.yaml", lvl4NN.Name))
	nt.T.Logf("Adding ConfigMap %s to the shared repository: %s", lvl4NN.Name, lvl4Path)
	lvl0Repo.Add(lvl4Path, lvl4ConfigMap)
	lvl0Repo.CommitAndPush(fmt.Sprintf("Adding ConfigMap: %s", lvl4NN.Name))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, rootSha1Fn, lvl0Sync, lvl1Sync, lvl2Sync, lvl3Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl0Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl1Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl2Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.GetAll(lvl3Sync.Spec.Git.Dir, true)...)

	// Validate that the R*Syncs and ConfigMap exist, are reconciled, and have the right manager.
	managedObjs := map[gvknn]manager{
		{kinds.RootSyncV1Beta1(), lvl0NN}: {}, // no manager
		{kinds.RootSyncV1Beta1(), lvl1NN}: {declared.RootReconciler, lvl0NN.Name},
		{kinds.RepoSyncV1Beta1(), lvl2NN}: {declared.RootReconciler, lvl1NN.Name},
		{kinds.RepoSyncV1Beta1(), lvl3NN}: {declared.Scope(lvl2NN.Namespace), lvl2NN.Name},
		{kinds.ConfigMap(), lvl4NN}:       {declared.Scope(lvl3NN.Namespace), lvl3NN.Name},
	}

	synedObjs := make(map[gvknn]*unstructured.Unstructured, len(managedObjs))

	for id, mgr := range managedObjs {
		var predicates []nomostest.Predicate
		predicates = append(predicates, nomostest.StatusEquals(nt, status.CurrentStatus))
		if mgr == (manager{}) {
			nt.T.Logf("Ensure %q exists, is reconciled, and is not managed", id)
			predicates = append(predicates, nomostest.IsNotManaged(nt))
		} else {
			nt.T.Logf("Ensure %q exists, is reconciled, and is managed by %q", id, mgr)
			predicates = append(predicates, nomostest.IsManagedBy(nt, mgr.Scope, mgr.Name))
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(id.GroupVersionKind)
		err := nt.Validate(id.Name, id.Namespace, obj, predicates...)
		if err != nil {
			nt.T.Fatal(err)
		}
		synedObjs[id] = obj
	}

	nt.T.Log("Waiting 1m to make sure there's no unnecessary updates...")
	time.Sleep(1 * time.Minute)

	for id, synedObj := range synedObjs {
		nt.T.Logf("Ensure %q exists, is current, and its ResourceVersion has not changed", id)
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(id.GroupVersionKind)
		err := nt.Validate(id.Name, id.Namespace, obj,
			nomostest.StatusEquals(nt, status.CurrentStatus),
			nomostest.ResourceVersionEquals(nt, synedObj.GetResourceVersion()))
		if err != nil {
			// Error, not Fatal, so we can see all the diffs when it fails.
			nt.T.Error(err)
			// Log the diff so we can see what fields changed.
			nt.T.Logf("Diff (- Expected, + Actual):\n%s", cmp.Diff(synedObj, obj))
		}
	}
}

type manager struct {
	Scope declared.Scope
	Name  string
}

type gvknn struct {
	schema.GroupVersionKind
	types.NamespacedName
}

// ToResourceReference converts from gvknn to ResourceReference.
func (id gvknn) ToResourceReference() mutation.ResourceReference {
	apiVersion, kind := id.ToAPIVersionAndKind()
	return mutation.ResourceReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       id.Name,
		Namespace:  id.Namespace,
	}
}

// String returns the gvknn in ResourceReference string format.
func (id gvknn) String() string {
	return id.ToResourceReference().String()
}

func waitForSync(nt *nomostest.NT, sha1Func nomostest.Sha1Func, objs ...client.Object) {
	nt.T.Helper()
	var wg sync.WaitGroup
	for _, obj := range objs {
		switch rsync := obj.(type) {
		case *v1beta1.RootSync:
			wg.Add(1)
			go func() {
				defer wg.Done()
				nt.WaitForSync(kinds.RootSyncV1Beta1(), rsync.Name, rsync.Namespace,
					nt.DefaultWaitTimeout, sha1Func, nomostest.RootSyncHasStatusSyncCommit,
					&nomostest.SyncDirPredicatePair{
						Dir:       rsync.Spec.Git.Dir,
						Predicate: nomostest.RootSyncHasStatusSyncDirectory,
					})
			}()
		case *v1beta1.RepoSync:
			wg.Add(1)
			go func() {
				defer wg.Done()
				nt.WaitForSync(kinds.RepoSyncV1Beta1(), rsync.Name, rsync.Namespace,
					nt.DefaultWaitTimeout, sha1Func, nomostest.RepoSyncHasStatusSyncCommit,
					&nomostest.SyncDirPredicatePair{
						Dir:       rsync.Spec.Git.Dir,
						Predicate: nomostest.RepoSyncHasStatusSyncDirectory,
					})
			}()
		default:
			nt.T.Fatal("Invalid R*Sync type: %T", obj)
		}
	}
	wg.Wait()
	if nt.T.Failed() {
		nt.T.Fatal("R*Syncs not synced")
	}
}

func validateStatusCurrent(nt *nomostest.NT, objs ...client.Object) {
	for _, obj := range objs {
		err := nt.Validate(obj.GetName(), obj.GetNamespace(), obj,
			nomostest.StatusEquals(nt, status.CurrentStatus))
		if err != nil {
			nt.T.Fatal(err)
		}
	}
}
