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
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
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
// │   └── config-management-system
// │       └── root-sync
// │           ├── acme
// │           │   └── level-0
// │           │       ├── rootsync-level-1.yaml
// │           │       ├── ns-test-ns.yaml
// │           │       ├── rb-test-ns-level-2.yaml
// │           │       ├── rb-test-ns-level-3.yaml
// │           │       └── .gitkeep
// │           ├── level-1
// │           │   ├── reposync-level-2.yaml
// │           │   └── .gitkeep
// │           ├── level-2
// │           │   ├── reposync-level-3.yaml
// │           │   └── .gitkeep
// │           └── level-3
// │               ├── configmap-level-4.yaml
// │               └── .gitkeep
//
// This tests multiple things:
// 1. RootSync -> RootSyncs -> RepoSyncs -> RepoSyncs
// 2. RepoSyncs & RepoSyncs can share a repository, using different directories.
// 3. RepoSyncs can share an ssh-key secret
// 4. R*Sync status isn't updated after sync without external input.
func TestComposition(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.MultiRepos,
		ntopts.Unstructured,
		ntopts.WithDelegatedControl,
		ntopts.RepoSyncPermissions(policy.RepoSyncAdmin(), policy.CoreAdmin()), // NS reconciler manages RepoSyncs and ConfigMaps
		ntopts.RootRepo(configsync.RootSyncName))

	lvl0NN := nomostest.RootSyncNN(configsync.RootSyncName)
	lvl1NN := nomostest.RootSyncNN("level-1")
	lvl2NN := types.NamespacedName{Namespace: testNs, Name: "level-2"}
	lvl3NN := types.NamespacedName{Namespace: testNs, Name: "level-3"}
	lvl4NN := types.NamespacedName{Namespace: testNs, Name: "level-4"}

	lvl0Repo := nt.RootRepos[lvl0NN.Name]

	lvl0Sync := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, lvl0NN.Name)
	lvl0Sync.Spec.Git.Dir = gitproviders.DefaultSyncDir
	// Use a subdirectory under the root-sync repo to make cleanup easy
	lvl0SubDir := filepath.Join(lvl0Sync.Spec.Git.Dir, "level-0")

	lvl1Sync := nomostest.RootSyncObjectV1Beta1FromOtherRootRepo(nt, lvl1NN.Name, lvl0NN.Name)
	lvl1Sync.Spec.Git.Dir = lvl1NN.Name

	lvl2RB := nomostest.RepoSyncRoleBinding(lvl2NN)
	lvl2Sync := nomostest.RepoSyncObjectV1Beta1FromOtherRootRepo(nt, lvl2NN, lvl0NN.Name)
	lvl2Sync.Spec.Git.Dir = lvl2NN.Name

	lvl3RB := nomostest.RepoSyncRoleBinding(lvl3NN)
	lvl3Sync := nomostest.RepoSyncObjectV1Beta1FromOtherRootRepo(nt, lvl3NN, lvl0NN.Name)
	lvl3Sync.Spec.Git.Dir = lvl3NN.Name

	// RepoSync level-2 depends on RoleBinding level-2.
	// RepoSync level-3 depends on RoleBinding level-3.
	// But the RoleBindings are managed by RootSync root-sync (level-0),
	// and Config Sync doesn't support external dependencies.
	// Since RepoSync level-3 is managed by RepoSync level-2,
	// and RepoSync level-2 is manage by RootSync level-1,
	// and RootSync level-1 is manage by RootSync root-sync (level-0),
	// we need to make RepoSync level-1 depend on both RoleBindings,
	// so the RoleBindings are deleted after both RepoSyncs.
	if err := nomostest.SetDependencies(lvl1Sync, lvl2RB, lvl3RB); err != nil {
		nt.T.Fatal(err)
	}

	// Remove files to prune objects before the R*Syncs are deleted.
	// Use separate functions to allow independent failure to be tolerated,
	// which can happen if the previous test failed when partially complete.
	// Cleanups execute in the reverse order they are added.
	t.Cleanup(func() {
		cleanupManagedSync(nt, lvl0Repo, lvl0SubDir, lvl0Sync)
	})
	t.Cleanup(func() {
		cleanupManagedSync(nt, lvl0Repo, lvl1Sync.Spec.Git.Dir, lvl1Sync)
	})
	t.Cleanup(func() {
		cleanupManagedSync(nt, lvl0Repo, lvl2Sync.Spec.Git.Dir, lvl2Sync)
	})
	t.Cleanup(func() {
		cleanupManagedSync(nt, lvl0Repo, lvl3Sync.Spec.Git.Dir, lvl3Sync)
	})
	// Print reconciler logs for R*Syncs that aren't in nt.RootRepos or nt.NonRootRepos.
	t.Cleanup(func() {
		if t.Failed() {
			nt.PodLogs(configmanagement.ControllerNamespace, core.RootReconcilerName(lvl1NN.Name),
				reconcilermanager.Reconciler, false)
			nt.PodLogs(configmanagement.ControllerNamespace, core.NsReconcilerName(lvl2NN.Namespace, lvl2NN.Name),
				reconcilermanager.Reconciler, false)
			nt.PodLogs(configmanagement.ControllerNamespace, core.NsReconcilerName(lvl3NN.Namespace, lvl3NN.Name),
				reconcilermanager.Reconciler, false)
		}
	})

	nt.T.Logf("Adding Namespace & RoleBindings for RepoSyncs")
	nt.Must(lvl0Repo.Add(filepath.Join(lvl0SubDir, fmt.Sprintf("ns-%s.yaml", testNs)), fake.NamespaceObject(testNs)))
	nt.Must(lvl0Repo.Add(filepath.Join(lvl0SubDir, fmt.Sprintf("rb-%s-%s.yaml", testNs, lvl2NN.Name)), lvl2RB))
	nt.Must(lvl0Repo.Add(filepath.Join(lvl0SubDir, fmt.Sprintf("rb-%s-%s.yaml", testNs, lvl3NN.Name)), lvl3RB))
	nt.Must(lvl0Repo.CommitAndPush("Adding Namespace & RoleBindings for RepoSyncs"))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, commitForRepo(lvl0Repo), lvl0Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl0SubDir, true)...)

	nt.T.Logf("Creating Secret for RepoSync: %s", lvl2NN)
	nt.Must(nomostest.CreateNamespaceSecret(nt, lvl2NN.Namespace))

	// lvl1 RootSync
	lvl1Path := filepath.Join(lvl0SubDir, fmt.Sprintf("rootsync-%s.yaml", lvl1NN.Name))
	nt.T.Logf("Adding RootSync %s to the shared repository: %s", lvl1NN.Name, lvl1Path)
	nt.Must(lvl0Repo.Add(lvl1Path, lvl1Sync))
	nt.Must(lvl0Repo.AddEmptyDir(lvl1Sync.Spec.Git.Dir))
	nt.Must(lvl0Repo.CommitAndPush(fmt.Sprintf("Adding RootSync: %s", lvl1NN)))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, commitForRepo(lvl0Repo), lvl0Sync, lvl1Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl0SubDir, true)...)
	// lvl1Sync.Spec.Git.Dir contains no yaml yet, so we don't need to test it for reconciliation yet.

	// lvl2 RepoSync
	lvl2Path := filepath.Join(lvl1Sync.Spec.Git.Dir, fmt.Sprintf("reposync-%s.yaml", lvl2NN.Name))
	nt.T.Logf("Adding RepoSync %s to the shared repository: %s", lvl2NN.Name, lvl2Path)
	nt.Must(lvl0Repo.Add(lvl2Path, lvl2Sync))
	nt.Must(lvl0Repo.AddEmptyDir(lvl2Sync.Spec.Git.Dir))
	nt.Must(lvl0Repo.CommitAndPush(fmt.Sprintf("Adding RepoSync: %s", lvl2NN)))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, commitForRepo(lvl0Repo), lvl0Sync, lvl1Sync, lvl2Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl0SubDir, true)...)
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl1Sync.Spec.Git.Dir, true)...)
	// lvl2Sync.Spec.Git.Dir contains no yaml yet, so we don't need to test it for reconciliation yet.

	// lvl3 RepoSync
	lvl3Path := filepath.Join(lvl2Sync.Spec.Git.Dir, fmt.Sprintf("reposync-%s.yaml", lvl3NN.Name))
	nt.T.Logf("Adding RepoSync %s to the shared repository: %s", lvl3NN.Name, lvl3Path)
	nt.Must(lvl0Repo.Add(lvl3Path, lvl3Sync))
	nt.Must(lvl0Repo.AddEmptyDir(lvl3Sync.Spec.Git.Dir))
	nt.Must(lvl0Repo.CommitAndPush(fmt.Sprintf("Adding RepoSync: %s", lvl3NN)))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, commitForRepo(lvl0Repo), lvl0Sync, lvl1Sync, lvl2Sync, lvl3Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl0SubDir, true)...)
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl1Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl2Sync.Spec.Git.Dir, true)...)
	// lvl2Sync.Spec.Git.Dir contains no yaml yet, so we don't need to test it for reconciliation yet.

	// lvl4 ConfigMap
	lvl4ConfigMap := &corev1.ConfigMap{}
	lvl4ConfigMap.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	lvl4ConfigMap.SetNamespace(testNs)
	lvl4ConfigMap.SetName(lvl4NN.Name)
	lvl4ConfigMap.Data = map[string]string{"key": "value"}
	lvl4Path := filepath.Join(lvl3Sync.Spec.Git.Dir, fmt.Sprintf("configmap-%s.yaml", lvl4NN.Name))
	nt.T.Logf("Adding ConfigMap %s to the shared repository: %s", lvl4NN.Name, lvl4Path)
	nt.Must(lvl0Repo.Add(lvl4Path, lvl4ConfigMap))
	nt.Must(lvl0Repo.CommitAndPush(fmt.Sprintf("Adding ConfigMap: %s", lvl4NN.Name)))

	nt.T.Log("Waiting for R*Syncs to be synced...")
	waitForSync(nt, commitForRepo(lvl0Repo), lvl0Sync, lvl1Sync, lvl2Sync, lvl3Sync)

	nt.T.Log("Validating synced objects are reconciled...")
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl0SubDir, true)...)
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl1Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl2Sync.Spec.Git.Dir, true)...)
	validateStatusCurrent(nt, lvl0Repo.MustGetAll(nt.T, lvl3Sync.Spec.Git.Dir, true)...)

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
		var predicates []testpredicates.Predicate
		predicates = append(predicates, testpredicates.StatusEquals(nt.Scheme, status.CurrentStatus))
		if mgr == (manager{}) {
			nt.T.Logf("Ensure %q exists, is reconciled, and is not managed", id)
			predicates = append(predicates, testpredicates.IsNotManaged(nt.Scheme))
		} else {
			nt.T.Logf("Ensure %q exists, is reconciled, and is managed by %q", id, mgr)
			predicates = append(predicates, testpredicates.IsManagedBy(nt.Scheme, mgr.Scope, mgr.Name))
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(id.GroupVersionKind)
		err := nt.Validate(id.Name, id.Namespace, obj, predicates...)
		if err != nil {
			nt.T.Fatal(err)
		}
		synedObjs[id] = obj
	}

	nt.T.Log("Watching for 1m to make sure there's no unnecessary spec updates...")
	// Watch the managed objects to log diffs (debug) and fail fast.
	// Allow status changes, but not spec changes.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tg := taskgroup.New()
	for id := range synedObjs {
		idPtr := id
		synedObj := synedObjs[id]
		tg.Go(func() error {
			// Stop early if the generation changes. Otherwise wait until timeout.
			gne := testpredicates.GenerationNotEquals(synedObj.GetGeneration())
			return nt.Watcher.WatchObject(idPtr.GroupVersionKind, idPtr.Name, idPtr.Namespace, []testpredicates.Predicate{
				func(obj client.Object) error {
					if err := ctx.Err(); err != nil {
						return err // cancelled
					}
					if err := gne(obj); err != nil {
						// Generation changed! Cancel other watches.
						cancel()
						return err
					}
					return nil
				},
			}, testwatcher.WatchTimeout(1*time.Minute))
		})
	}
	if err := tg.Wait(); err != nil {
		if me, ok := err.(multiErr); ok {
			for _, err := range me.Errors() {
				// Timeout or Cancel expected. Otherwise it probably means the Generation changed.
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					nt.T.Fatal(err)
				}
			}
		} else {
			// Timeout or Cancel expected. Otherwise it probably means the Generation changed.
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				nt.T.Fatal(err)
			}
		}
	}

	// Assuming the watches timed out, validate that the objects are still reconciled.
	for id, synedObj := range synedObjs {
		nt.T.Logf("Ensure %q exists, is current, and its Generation has not changed", id)
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(id.GroupVersionKind)
		err := nt.Validate(id.Name, id.Namespace, obj,
			testpredicates.StatusEquals(nt.Scheme, status.CurrentStatus),
			testpredicates.GenerationEquals(synedObj.GetGeneration()))
		if err != nil {
			// Error, not Fatal, so we can see all the diffs when it fails.
			nt.T.Error(err)
			// Log the diff so we can see what fields changed.
			nt.T.Logf("Diff (- Expected, + Actual):\n%s", cmp.Diff(synedObj, obj))
		}
	}
}

// multiErr matches multi-errors, like those from TaskGroup.Wait
type multiErr interface {
	Errors() []error
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

// waitForSync waits for the specified R*Syncs to be Synced.
//
// The reason we can't just use nt.WaitForRepoSyncs is that the R*Syncs for this
// test are not all in nt.RootRepos or nt.NonRootRepos, because they're all
// sharing the same repository.
//
// So this function uses the same sha1Func for all R*Syncs.
func waitForSync(nt *nomostest.NT, sha1Func nomostest.Sha1Func, objs ...client.Object) {
	nt.T.Helper()

	tg := taskgroup.New()
	for _, obj := range objs {
		switch rsync := obj.(type) {
		case *v1beta1.RootSync:
			tg.Go(func() error {
				return nt.WatchForSync(kinds.RootSyncV1Beta1(), rsync.Name, rsync.Namespace,
					sha1Func, nomostest.RootSyncHasStatusSyncCommit,
					&nomostest.SyncDirPredicatePair{
						Dir:       rsync.Spec.Git.Dir,
						Predicate: nomostest.RootSyncHasStatusSyncDirectory,
					})
			})
		case *v1beta1.RepoSync:
			tg.Go(func() error {
				return nt.WatchForSync(kinds.RepoSyncV1Beta1(), rsync.Name, rsync.Namespace,
					sha1Func, nomostest.RepoSyncHasStatusSyncCommit,
					&nomostest.SyncDirPredicatePair{
						Dir:       rsync.Spec.Git.Dir,
						Predicate: nomostest.RepoSyncHasStatusSyncDirectory,
					})
			})
		default:
			nt.T.Fatal("Invalid R*Sync type: %T", obj)
		}
	}
	if err := tg.Wait(); err != nil {
		nt.T.Fatalf("R*Syncs not synced: %v", err)
	}
}

func validateStatusCurrent(nt *nomostest.NT, objs ...client.Object) {
	tg := taskgroup.New()
	for _, obj := range objs {
		nn := client.ObjectKeyFromObject(obj)
		gvk, err := kinds.Lookup(obj, nt.KubeClient.Client.Scheme())
		require.NoError(nt.T, err)
		tg.Go(func() error {
			return nt.Watcher.WatchForCurrentStatus(gvk, nn.Name, nn.Namespace)
		})
	}
	err := tg.Wait()
	if err != nil {
		nt.T.Fatal(err)
	}
}

func commitForRepo(repo *gitproviders.Repository) nomostest.Sha1Func {
	return func(_ *nomostest.NT, _ types.NamespacedName) (string, error) {
		return repo.Hash()
	}
}

// cleanupManagedSync deletes the specified dirPath from the specified repo and
// then waits until the specified sync object is synced.
// This assumes the specified syncObj is configured to watch the specified
// dirPath in the specified repo.
func cleanupManagedSync(nt *nomostest.NT, repo *gitproviders.Repository, dirPath string, syncObj client.Object) {
	exists, err := repo.Exists(dirPath)
	if err != nil {
		nt.T.Fatal(err)
	}
	if !exists {
		return
	}
	syncGVK, err := kinds.Lookup(syncObj, nt.Scheme)
	if err != nil {
		nt.T.Fatal(err)
	}
	syncNN := client.ObjectKeyFromObject(syncObj)

	nt.T.Logf("Cleaning up %s %s...", syncGVK.Kind, syncNN)
	nt.Must(repo.Remove(dirPath))
	nt.Must(repo.AddEmptyDir(dirPath))
	nt.Must(repo.CommitAndPush(fmt.Sprintf("Remove dir contents: %s ", dirPath)))

	nt.T.Logf("Waiting for %s %s to be synced...", syncGVK.Kind, syncNN)
	waitForSync(nt, commitForRepo(repo), syncObj)
}
