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

	"k8s.io/apimachinery/pkg/api/equality"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	resourcegroupv1alpha1 "kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file includes testcases to enable status
// or disable status in the kpt applier.

func TestStatusEnabledAndDisabled(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	id := applier.InventoryID(configsync.RootSyncName, configsync.ControllerNamespace)

	rootSync := k8sobjects.RootSyncObjectV1Alpha1(configsync.RootSyncName)
	// Override the statusMode for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"statusMode": "disabled"}}}`)
	nt.Must(nt.WatchForAllSyncs())

	namespaceName := "status-test"
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespaceObject(namespaceName, nil)))
	nt.Must(rootSyncGitRepo.Add("acme/cm1.yaml", k8sobjects.ConfigMapObject(core.Name("cm1"), core.Namespace(namespaceName))))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a namespace and a configmap"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(),
		configsync.RootSyncName, configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupHasNoStatus(),
			testpredicates.HasLabel(common.InventoryLabel, id),
		),
		testwatcher.WatchTimeout(nt.DefaultWaitTimeout)))

	// Override the statusMode for root-reconciler to re-enable the status
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"statusMode": "enabled"}}}`)
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(),
		configsync.RootSyncName, configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			resourceGroupHasStatus,
			testpredicates.HasLabel(common.InventoryLabel, id),
		),
		testwatcher.WatchTimeout(nt.DefaultWaitTimeout)))
}

func resourceGroupHasStatus(obj client.Object) error {
	if obj == nil {
		return testpredicates.ErrObjectNotFound
	}
	rg, ok := obj.(*resourcegroupv1alpha1.ResourceGroup)
	if !ok {
		return testpredicates.WrongTypeErr(obj, &resourcegroupv1alpha1.ResourceGroup{})
	}
	emptyStatus := resourcegroupv1alpha1.ResourceGroupStatus{}
	if equality.Semantic.DeepEqual(emptyStatus, rg.Status) {
		return fmt.Errorf("found empty status in %s", kinds.ObjectSummary(rg))
	}
	return nil
}
