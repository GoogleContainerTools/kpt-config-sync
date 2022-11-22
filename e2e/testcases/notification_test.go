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
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

func TestNotification(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN("root-sync-2")
	repoSyncNN := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	rootReconcilerName := core.RootReconcilerName(rootSyncNN.Name)
	nsReconcilerName := core.NsReconcilerName(repoSyncNN.Namespace, repoSyncNN.Name)
	nt := nomostest.New(t, nomostesting.SyncSource,
		ntopts.Unstructured,
		ntopts.InstallNotificationServer,
		ntopts.RootRepo(rootSyncNN.Name),
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name),
	)
	var err error
	credentialMap := map[string]string{
		rootSyncNN.Namespace: "pass123",
		repoSyncNN.Namespace: "pass456",
	}
	for ns, pass := range credentialMap {
		_, err = nomostest.NotificationSecret(nt, ns,
			nomostest.WithNotificationUsername("user"),
			nomostest.WithNotificationPassword(pass),
		)
		if err != nil {
			nt.T.Fatal(err)
		}
		_, err = nomostest.NotificationConfigMap(nt, ns,
			nomostest.WithOnSyncSyncedTrigger,
			nomostest.WithSyncSyncedTemplate,
			nomostest.WithLocalWebhookService,
		)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	// Enable notifications on RootSYnc
	rootSync := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, rootSyncNN.Name)
	nomostest.SubscribeRootSyncNotification(rootSync, "on-sync-synced", "local")
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RootSync")
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Deployment(), rootReconcilerName, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.DeploymentHasContainer(reconcilermanager.Notification)})
	})
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.ObjectHasAnnotation("notified.notifications.configsync.gke.io")})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// query RootSync to get the generation
	rootSyncObj := &v1beta1.RootSync{}
	if err := nt.KubeClient.Get(rootSyncNN.Name, rootSyncNN.Namespace, rootSyncObj); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
				ObservedGeneration: rootSyncObj.Generation,
				Commit:             nt.RootRepos[rootSyncNN.Name].Hash(),
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "on-sync-synced",
						Service:         "local",
						Recipient:       "",
						AlreadyNotified: true,
					},
				},
			})},
		)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	records, err := waitForNotifications(nt, 1)
	if err != nil {
		nt.T.Fatal(err)
	}
	require.Equal(nt.T, nomostest.NotificationRecords{
		Records: []nomostest.NotificationRecord{
			{
				Message: "{\n  \"content\": {\n    \"raw\": \"RootSync root-sync-2 is synced!\"\n  }\n}",
				Auth:    fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte("user:pass123"))), // base64 encoded username/pass
			},
		},
	}, *records)
	// Enable notifications on RepoSync
	repoSync := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncNN)
	nomostest.SubscribeRepoSyncNotification(repoSync, "on-sync-synced", "local")
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), repoSync)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RepoSync")
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Deployment(),
			core.NsReconcilerName(repoSyncNN.Namespace, repoSyncNN.Name), configsync.ControllerNamespace,
			[]testpredicates.Predicate{testpredicates.DeploymentHasContainer(reconcilermanager.Notification)})
	})
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.ObjectHasAnnotation("notified.notifications.configsync.gke.io")})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// query RootSync to get the generation
	repoSyncObj := &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(repoSyncNN.Name, repoSyncNN.Namespace, repoSyncObj); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
				ObservedGeneration: repoSyncObj.Generation,
				Commit:             nt.NonRootRepos[repoSyncNN].Hash(),
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "on-sync-synced",
						Service:         "local",
						Recipient:       "",
						AlreadyNotified: true,
					},
				},
			})},
		)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	records, err = waitForNotifications(nt, 2)
	if err != nil {
		nt.T.Fatal(err)
	}
	// RootSync notification for one commit, RepoSync notification for one commit
	require.Equal(nt.T, nomostest.NotificationRecords{
		Records: []nomostest.NotificationRecord{
			{
				Message: "{\n  \"content\": {\n    \"raw\": \"RootSync root-sync-2 is synced!\"\n  }\n}",
				Auth:    fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte("user:pass123"))), // base64 encoded username/pass
			},
			{
				Message: "{\n  \"content\": {\n    \"raw\": \"RepoSync repo-sync is synced!\"\n  }\n}",
				Auth:    fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte("user:pass456"))), // base64 encoded username/pass
			},
		},
	}, *records)
	// Unsubscribe notification for RootSync and RepoSync
	nomostest.UnsubscribeRootSyncNotification(rootSync)
	nomostest.UnsubscribeRepoSyncNotification(repoSync)
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync)
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), repoSync)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Disable notifications on RootSync and RepoSync")
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Deployment(), rootReconcilerName, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.DeploymentMissingContainer(reconcilermanager.Notification)},
		)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Deployment(), nsReconcilerName, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.DeploymentMissingContainer(reconcilermanager.Notification)},
		)
	})
	tg.Go(func() error { // unregistering the RootSync NotificationConfig should delete the Notification CR
		return nt.Watcher.WatchForNotFound(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace)
	})
	tg.Go(func() error { // unregistering the RepoSync NotificationConfig should delete the Notification CR
		return nt.Watcher.WatchForNotFound(kinds.NotificationV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	// Re-enable notifications for RootSync and RepoSync
	nomostest.SubscribeRootSyncNotification(rootSync, "on-sync-synced", "local")
	nomostest.SubscribeRepoSyncNotification(repoSync, "on-sync-synced", "local")
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync)
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), repoSync)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RootSync and RepoSync")
	tg = taskgroup.New()
	tg.Go(func() error { // registering the RootSync NotificationConfig should create the Notification CR
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace, []testpredicates.Predicate{})
	})
	tg.Go(func() error { // registering the RepoSync NotificationConfig should create the Notification CR
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace, []testpredicates.Predicate{})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name))
	nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove root-sync-2 and repo-sync-2")
	tg = taskgroup.New()
	tg.Go(func() error { // deleting the RootSync should delete the Notification CR
		return nt.Watcher.WatchForNotFound(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace)
	})
	tg.Go(func() error { // deleting the RepoSync should delete the Notification CR
		return nt.Watcher.WatchForNotFound(kinds.NotificationV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func waitForNotifications(nt *nomostest.NT, expectedNum int) (*nomostest.NotificationRecords, error) {
	var records *nomostest.NotificationRecords
	var err error
	took, err := retry.Retry(30*time.Second, func() error {
		records, err = nt.NotificationServer.DoGet()
		if err != nil {
			return err
		}
		if len(records.Records) < expectedNum {
			return fmt.Errorf("want %d, got %d", expectedNum, len(records.Records))
		}
		return nil
	})
	nt.T.Logf("took %v to wait for %d notification(s). Got %d", took, expectedNum, len(records.Records))
	if err != nil {
		return records, err
	} else if len(records.Records) != expectedNum { // check if got more records than expected
		return records, fmt.Errorf("want %d, got %d", expectedNum, len(records.Records))
	}
	return records, nil
}
