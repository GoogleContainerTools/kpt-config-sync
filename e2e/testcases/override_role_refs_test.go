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

	"github.com/pkg/errors"
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
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestRootSyncRoleRefs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured,
		ntopts.RootRepo("sync-a"), // will declare namespace-a explicitly
	)
	rootSyncA := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, "sync-a")
	syncAReconcilerName := core.RootReconcilerName(rootSyncA.Name)
	syncANN := types.NamespacedName{
		Name:      rootSyncA.Name,
		Namespace: rootSyncA.Namespace,
	}
	if err := nt.Validate(controllers.RootSyncLegacyClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
		testpredicates.ClusterRoleBindingHasSubjects(nomostest.DefaultRootReconcilerName, syncAReconcilerName)); err != nil {
		nt.T.Fatal(err)
	}
	// Set some custom roleRef overrides
	rootSyncA.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
		{
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: "Role",
				Name: "foo-role",
			},
			Namespace: "foo",
		},
		{
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: "ClusterRole",
				Name: "foo-role",
			},
		},
		{
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: "ClusterRole",
				Name: "bar-role",
			},
		},
		{
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: "ClusterRole",
				Name: "foo-role",
			},
			Namespace: "foo",
		},
	}
	roleObject := fake.RoleObject(core.Name("foo-role"), core.Namespace("foo"))
	clusterRoleObject := fake.ClusterRoleObject(core.Name("foo-role"))
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
	clusterRoleObject2 := fake.ClusterRoleObject(core.Name("bar-role"))
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
			testpredicates.ClusterRoleBindingHasSubjects(nomostest.DefaultRootReconcilerName))
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncBaseClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingHasSubjects(syncAReconcilerName))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// Remove some but not all roleRef overrides
	rootSyncA.Spec.SafeOverride().RoleRefs = []v1beta1.RootSyncRoleRef{
		{
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: "ClusterRole",
				Name: "foo-role",
			},
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
			testpredicates.ClusterRoleBindingHasSubjects(nomostest.DefaultRootReconcilerName))
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncBaseClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingHasSubjects(syncAReconcilerName))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// Delete the RootSync sync-a
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(
		nomostest.StructuredNSPath(rootSyncA.Namespace, rootSyncA.Name),
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Prune RootSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return validateRoleRefs(nt, configsync.RootSyncKind, syncANN, []v1beta1.RootSyncRoleRef{})
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RootSyncLegacyClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{},
			testpredicates.ClusterRoleBindingHasSubjects(nomostest.DefaultRootReconcilerName))
	})
	tg.Go(func() error {
		return nt.ValidateNotFound(controllers.RootSyncBaseClusterRoleBindingName, "", &rbacv1.ClusterRoleBinding{})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestRepoSyncRoleRefs(t *testing.T) {
	repoSyncNN := types.NamespacedName{
		Name:      "sync-a",
		Namespace: "foo",
	}
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name), // will declare namespace-a explicitly
	)
	repoSyncA := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncNN)
	nsReconcilerName := core.NsReconcilerName(repoSyncNN.Namespace, repoSyncNN.Name)
	// Set some custom roleRef overrides
	repoSyncA.Spec.SafeOverride().RoleRefs = []v1beta1.RoleRefBase{
		{
			Kind: "Role",
			Name: "foo-role",
		},
		{
			Kind: "ClusterRole",
			Name: "foo-role",
		},
		{
			Kind: "ClusterRole",
			Name: "bar-role",
		},
	}
	roleObject := fake.RoleObject(core.Name("foo-role"), core.Namespace("foo"))
	clusterRoleObject := fake.ClusterRoleObject(core.Name("foo-role"))
	clusterRoleObject2 := fake.ClusterRoleObject(core.Name("bar-role"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name),
		repoSyncA,
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
		return validateRepoSyncRoleRefs(nt, repoSyncA)
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RepoSyncBaseRoleBindingName, repoSyncNN.Namespace, &rbacv1.RoleBinding{},
			testpredicates.RoleBindingHasSubjects(nsReconcilerName))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// Remove some but not all roleRef overrides
	repoSyncA.Spec.SafeOverride().RoleRefs = []v1beta1.RoleRefBase{
		{
			Kind: "ClusterRole",
			Name: "foo-role",
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name),
		repoSyncA,
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove RoleRefs"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return validateRepoSyncRoleRefs(nt, repoSyncA)
	})
	tg.Go(func() error {
		return nt.Validate(controllers.RepoSyncBaseRoleBindingName, repoSyncNN.Namespace, &rbacv1.RoleBinding{},
			testpredicates.RoleBindingHasSubjects(nsReconcilerName))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// Delete the RepoSync sync-a
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(
		nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name),
	))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Prune RepoSync"))
	if err := nt.WatchForAllSyncs(nomostest.RootSyncOnly()); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return validateRoleRefs(nt, configsync.RepoSyncKind, repoSyncNN, []v1beta1.RootSyncRoleRef{})
	})
	tg.Go(func() error {
		return nt.ValidateNotFound(controllers.RepoSyncBaseRoleBindingName, repoSyncNN.Namespace, &rbacv1.RoleBinding{})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func validateRoleRefs(nt *nomostest.NT, syncKind string, rsRef types.NamespacedName, expected []v1beta1.RootSyncRoleRef) error {
	roleBindings, err := nt.ListReconcilerRoleBindings(syncKind, rsRef)
	if err != nil {
		return err
	}
	actualRoleRefCount := make(map[v1beta1.RootSyncRoleRef]int)
	for _, rb := range roleBindings {
		roleRef := v1beta1.RootSyncRoleRef{
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: rb.RoleRef.Kind,
				Name: rb.RoleRef.Name,
			},
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
			RoleRefBase: v1beta1.RoleRefBase{
				Kind: crb.RoleRef.Kind,
				Name: crb.RoleRef.Name,
			},
		}
		actualRoleRefCount[roleRef]++
	}
	totalBindings := len(roleBindings) + len(clusterRoleBindings)
	if len(expected) != totalBindings {
		return errors.Errorf("expected %d bindings but found %d",
			len(expected), totalBindings)
	}
	for _, roleRef := range expected {
		if actualRoleRefCount[roleRef] != 1 {
			return errors.Errorf("expected to find one binding mapping to %s, found %d",
				roleRef, actualRoleRefCount[roleRef])
		}
	}
	return nil
}

func validateRepoSyncRoleRefs(nt *nomostest.NT, repoSync *v1beta1.RepoSync) error {
	var expectedRoleRefs []v1beta1.RootSyncRoleRef
	for _, roleRef := range repoSync.Spec.SafeOverride().RoleRefs {
		expectedRoleRefs = append(expectedRoleRefs, v1beta1.RootSyncRoleRef{
			RoleRefBase: roleRef,
			Namespace:   repoSync.Namespace,
		})
	}
	rsNN := types.NamespacedName{
		Name:      repoSync.Name,
		Namespace: repoSync.Namespace,
	}
	return validateRoleRefs(nt, configsync.RepoSyncKind, rsNN, expectedRoleRefs)
}
