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
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sortPolicyRules sorts PolicyRules lexicographically by JSON representation.
func sortPolicyRules(l, r rbacv1.PolicyRule) bool {
	jsnL, _ := json.Marshal(l)
	jsnR, _ := json.Marshal(r)
	return string(jsnL) < string(jsnR)
}

func clusterRoleHasRules(rules []rbacv1.PolicyRule) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		cr, ok := o.(*rbacv1.ClusterRole)
		if !ok {
			return testpredicates.WrongTypeErr(cr, &rbacv1.ClusterRole{})
		}

		// Ignore the order of the policy rules.
		if diff := cmp.Diff(rules, cr.Rules, cmpopts.SortSlices(sortPolicyRules)); diff != "" {
			return errors.New(diff)
		}
		return nil
	}
}

func managerFieldsNonEmpty() testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		fields := o.GetManagedFields()
		if len(fields) == 0 {
			return errors.New("expect non empty manager fields")
		}
		return nil
	}
}

// TestRevertClusterRole ensures that we revert conflicting manually-applied
// changes to cluster-scoped objects.
func TestRevertClusterRole(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl)

	crName := "e2e-test-clusterrole"

	err := nt.ValidateNotFound(crName, "", k8sobjects.ClusterRoleObject())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Declare the ClusterRole.
	declaredRules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{kinds.Deployment().Kind},
			Verbs:     []string{"get", "list", "create"},
		},
	}
	declaredCr := k8sobjects.ClusterRoleObject(core.Name(crName))
	declaredCr.Rules = declaredRules
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/clusterrole.yaml", declaredCr))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add get/list/create ClusterRole"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate(crName, "", &rbacv1.ClusterRole{},
		clusterRoleHasRules(declaredRules))
	if err != nil {
		nt.T.Fatalf("validating ClusterRole precondition: %v", err)
	}

	// Apply a conflicting ClusterRole.
	appliedRules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{kinds.Deployment().Kind},
			Verbs:     []string{"get", "list"}, // missing "create"
		},
	}
	appliedCr := k8sobjects.ClusterRoleObject(core.Name(crName))
	appliedCr.Rules = appliedRules
	err = nt.KubeClient.Update(appliedCr)
	// The admission webhook should deny the conflicting change.
	if err == nil {
		nt.T.Fatal("got Update error = nil, want admission webhook to deny conflicting update")
	}

	// Ensure the conflict is reverted.
	err = nt.Watcher.WatchObject(kinds.ClusterRole(), crName, "",
		[]testpredicates.Predicate{clusterRoleHasRules(declaredRules)})
	if err != nil {
		nt.T.Error(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, declaredCr)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestClusterRoleLifecycle ensures we can add/update/delete cluster-scoped
// resources.
func TestClusterRoleLifecycle(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1)

	crName := "e2e-test-clusterrole"

	err := nt.ValidateNotFound(crName, "", k8sobjects.ClusterRoleObject())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Declare the ClusterRole in repo.
	declaredRules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{kinds.Deployment().Kind},
			Verbs:     []string{"get", "list", "create"},
		},
	}
	declaredCr := k8sobjects.ClusterRoleObject(core.Name(crName))
	declaredCr.Rules = declaredRules
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/clusterrole.yaml", declaredCr))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add get/list/create ClusterRole"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate(crName, "", &rbacv1.ClusterRole{},
		clusterRoleHasRules(declaredRules), managerFieldsNonEmpty())
	if err != nil {
		nt.T.Fatalf("validating ClusterRole precondition: %v", err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, declaredCr)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update the ClusterRole in the SOT.
	updatedRules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{kinds.Deployment().Kind},
			Verbs:     []string{"get", "list"}, // missing "create"
		},
	}
	updatedCr := k8sobjects.ClusterRoleObject(core.Name(crName))
	updatedCr.Rules = updatedRules
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/clusterrole.yaml", updatedCr))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("update ClusterRole to get/list"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Ensure the resource is updated.
	if err = nt.Validate(crName, "", &rbacv1.ClusterRole{}, clusterRoleHasRules(updatedRules), managerFieldsNonEmpty()); err != nil {
		nt.T.Errorf("updating ClusterRole: %v", err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, updatedCr)

	// Validate multi-repo metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the ClusterRole from the SOT.
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/clusterrole.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("deleting ClusterRole"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateNotFound(crName, "", &rbacv1.ClusterRole{})
	if err != nil {
		nt.T.Errorf("deleting ClusterRole: %v", err)
	}

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, updatedCr)

	// Validate multi-repo metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}
