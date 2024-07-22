// Copyright 2023 Google LLC
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

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

func TestRootSyncRoleRefs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured,
		ntopts.RootRepo("sync-a"),
	)
	rootSyncA := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, "sync-a")
	syncAReconcilerName := core.RootReconcilerName(rootSyncA.Name)
	syncANN := types.NamespacedName{
		Name:      rootSyncA.Name,
		Namespace: rootSyncA.Namespace,
	}
	if err := nt.Validate(controllers.RootSyncLegacyClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
		testpredicates.ClusterRoleBindingSubjectNamesEqual(nomostest.DefaultRootReconcilerName, syncAReconcilerName)); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Logf("Set custom roleRef overrides on RootSync %s", syncANN.Name)
	rootSyncA.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
		{
			Kind:      "Role",
			Name:      "foo-role",
			Namespace: "foo",
		},
		{
			Kind: "ClusterRole",
			Name: "foo-role",
		},
		{
			Kind: "ClusterRole",
			Name: "bar-role",
		},
		{
			Kind:      "ClusterRole",
			Name:      "foo-role",
			Namespace: "foo",
		},
	}
	roleObject := k8sobjects.RoleObject(core.Name("foo-role"), core.Namespace("foo"))
	clusterRoleObject := k8sobjects.ClusterRoleObject(core.Name("foo-role"))
	clusterRoleObject.Rules = []rbacv1.PolicyRule{
		{ // permission to manage the "safety clusterrole"
			Verbs:     []string{"*"},
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"clusterroles"},
		},
		{ // permission to manage the "safety namespace"
			Verbs:     []string{"*"},
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
		},
	}
	clusterRoleObject2 := k8sobjects.ClusterRoleObject(core.Name("bar-role"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(rootSyncA.Namespace, rootSyncA.Name),
		rootSyncA,
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/%s.yaml", roleObject.Namespace, roleObject.Name),
		roleObject,
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s.yaml", clusterRoleObject.Name),
		clusterRoleObject,
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s.yaml", clusterRoleObject2.Name),
		clusterRoleObject2,
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add Roles and RoleRefs"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	tg := taskgroup.New()
	tg.Go(func() error {
		return validateRoleRefs(nt, configsync.RootSyncKind, syncANN, rootSyncA.Spec.SafeOverride().RoleRefs)
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncLegacyClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingSubjectNamesEqual(nomostest.DefaultRootReconcilerName))
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncBaseClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingSubjectNamesEqual(syncAReconcilerName))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove some but not all roleRefs from %s to verify garbage collection", syncANN.Name)
	rootSyncA.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
		{
			Kind: "ClusterRole",
			Name: "foo-role",
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/%s.yaml", configsync.ControllerNamespace, rootSyncA.Name),
		rootSyncA,
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Reduce RoleRefs"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return validateRoleRefs(nt, configsync.RootSyncKind, syncANN, rootSyncA.Spec.SafeOverride().RoleRefs)
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncLegacyClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingSubjectNamesEqual(nomostest.DefaultRootReconcilerName))
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncBaseClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingSubjectNamesEqual(syncAReconcilerName))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Delete the RootSync %s to verify garbage collection", syncANN.Name)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(
		nomostest.StructuredNSPath(rootSyncA.Namespace, rootSyncA.Name),
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Prune RootSync"))
	if err := nt.WatchForSync(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		nomostest.DefaultRootSha1Fn, nomostest.RootSyncHasStatusSyncCommit, nil); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), syncANN.Name, syncANN.Namespace); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return validateRoleRefs(nt, configsync.RootSyncKind, syncANN, []v1beta1.RootSyncRoleRef{})
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncLegacyClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingSubjectNamesEqual(nomostest.DefaultRootReconcilerName))
	})
	tg.Go(func() error {
		return nt.ValidateNotFound(controllers.RootSyncBaseClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

// This helper function verifies that the specified role refs are mapped to
// bindings on the cluster. The bindings are looked up using labels based on the
// RSync kind/name/namespace. Returns an error if what is found on the cluster
// is not an exact match.
func validateRoleRefs(nt *nomostest.NT, syncKind string, rsRef types.NamespacedName, expected []v1beta1.RootSyncRoleRef) error {
	roleBindings, err := nt.ListReconcilerRoleBindings(syncKind, rsRef)
	if err != nil {
		return err
	}
	actualRoleRefCount := make(map[v1beta1.RootSyncRoleRef]int)
	for _, rb := range roleBindings {
		roleRef := v1beta1.RootSyncRoleRef{
			Kind:      rb.RoleRef.Kind,
			Name:      rb.RoleRef.Name,
			Namespace: rb.Namespace,
		}
		actualRoleRefCount[roleRef]++
	}
	clusterRoleBindings, err := nt.ListReconcilerClusterRoleBindings(syncKind, rsRef)
	if err != nil {
		return err
	}
	for _, crb := range clusterRoleBindings {
		roleRef := v1beta1.RootSyncRoleRef{
			Kind: crb.RoleRef.Kind,
			Name: crb.RoleRef.Name,
		}
		actualRoleRefCount[roleRef]++
	}
	totalBindings := len(roleBindings) + len(clusterRoleBindings)
	if len(expected) != totalBindings {
		return fmt.Errorf("expected %d bindings but found %d",
			len(expected), totalBindings)
	}
	for _, roleRef := range expected {
		if actualRoleRefCount[roleRef] != 1 {
			return fmt.Errorf("expected to find one binding mapping to %s, found %d",
				roleRef, actualRoleRefCount[roleRef])
		}
	}
	return nil
}
