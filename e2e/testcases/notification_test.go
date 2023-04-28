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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/object/dependson"
)

const (
	// password for RootSync notifications
	rootSyncNotificationPassword = "pass123"
	// hash of "user:pass123" (base64)
	rootSyncNotificationCredentialHash = "Basic dXNlcjpwYXNzMTIz"
	// password for RepoSync notifications
	repoSyncNotificationPassword = "pass456"
	// hash of "user:pass456" (base64)
	repoSyncNotificationCredentialHash = "Basic dXNlcjpwYXNzNDU2"
)

func TestNotification(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN("root-sync-2")
	repoSyncNN := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	rootReconcilerName := core.RootReconcilerName(rootSyncNN.Name)
	nsReconcilerName := core.NsReconcilerName(repoSyncNN.Namespace, repoSyncNN.Name)
	nt := nomostest.New(t, nomostesting.Notification,
		ntopts.Unstructured,
		ntopts.InstallNotificationServer,
		ntopts.RootRepo(rootSyncNN.Name),
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name),
	)
	var err error
	credentialMap := map[string]string{
		rootSyncNN.Namespace: rootSyncNotificationPassword,
		repoSyncNN.Namespace: repoSyncNotificationPassword,
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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RootSync"))
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
		hash, err := nt.RootRepos[rootSyncNN.Name].Hash()
		if err != nil {
			return err
		}
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
				ObservedGeneration: rootSyncObj.Generation,
				Commit:             hash,
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
				Auth:    rootSyncNotificationCredentialHash, // base64 encoded username/pass
			},
		},
	}, *records)
	// Enable notifications on RepoSync
	repoSync := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncNN)
	nomostest.SubscribeRepoSyncNotification(repoSync, "on-sync-synced", "local")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), repoSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RepoSync"))
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
		hash, err := nt.NonRootRepos[repoSyncNN].Hash()
		if err != nil {
			return err
		}
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
				ObservedGeneration: repoSyncObj.Generation,
				Commit:             hash,
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
				Auth:    rootSyncNotificationCredentialHash, // base64 encoded username/pass
			},
			{
				Message: "{\n  \"content\": {\n    \"raw\": \"RepoSync repo-sync is synced!\"\n  }\n}",
				Auth:    repoSyncNotificationCredentialHash, // base64 encoded username/pass
			},
		},
	}, *records)
	// Unsubscribe notification for RootSync and RepoSync
	nomostest.UnsubscribeRootSyncNotification(rootSync)
	nomostest.UnsubscribeRepoSyncNotification(repoSync)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), repoSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Disable notifications on RootSync and RepoSync"))
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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), repoSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RootSync and RepoSync"))
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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove root-sync-2 and repo-sync-2"))
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

func TestNotificationOnSyncFailed(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN("root-sync-2")
	rootReconcilerName := core.RootReconcilerName(rootSyncNN.Name)
	nt := nomostest.New(t, nomostesting.Notification,
		ntopts.Unstructured,
		ntopts.InstallNotificationServer,
		ntopts.RootRepo(rootSyncNN.Name),
	)
	var err error
	credentialMap := map[string]string{
		rootSyncNN.Namespace: rootSyncNotificationPassword,
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
			nomostest.WithOnSyncFailedTrigger,
			nomostest.WithSyncFailedTemplate,
			nomostest.WithLocalWebhookService,
		)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	// Enable notifications on RootSync
	rootSync := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, rootSyncNN.Name)
	nomostest.SubscribeRootSyncNotification(rootSync, "on-sync-failed", "local")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RootSync"))
	err = nt.Watcher.WatchObject(kinds.Deployment(), rootReconcilerName, rootSyncNN.Namespace,
		[]testpredicates.Predicate{testpredicates.DeploymentHasContainer(reconcilermanager.Notification)})
	if err != nil {
		nt.T.Fatal(err)
	}
	// cause the RootSync to break
	rootSync.Spec.Git.Branch = "nonexistent-branch"
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Switch RootSync %s to nonexistent-branch", rootSyncNN.Name)))
	nt.WaitForRootSyncSourceError(rootSyncNN.Name,
		status.SourceErrorCode,
		"Remote branch nonexistent-branch not found in upstream")
	// query RootSync to get the generation
	rootSyncObj := &v1beta1.RootSync{}
	if err := nt.KubeClient.Get(rootSyncNN.Name, rootSyncNN.Namespace, rootSyncObj); err != nil {
		nt.T.Fatal(err)
	}
	// first iteration should set AlreadyNotified to false, subsequently true
	// skip the false check as it is a race condition whether we catch it
	err = nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace,
		[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
			ObservedGeneration: rootSyncObj.Generation,
			Commit:             "", // commit is empty, because the Syncing condition does not have the commit
			Deliveries: []v1beta1.NotificationDelivery{
				{
					Trigger:         "on-sync-failed",
					Service:         "local",
					Recipient:       "",
					AlreadyNotified: true,
				},
			},
		})},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	// notification from first branch failure, including warning of new known host
	// notification from first branch failure
	records, err := waitForNotifications(nt, 2)
	if err != nil {
		nt.T.Fatal(err)
	}
	// first notification error includes a warning of known host added which includes an IP address.
	// the IP address is dynamic, so skip checking the message for the first notification
	require.Equal(t, rootSyncNotificationCredentialHash, records.Records[0].Auth)
	// subsequent git-sync iterations do not include the warning, but otherwise should
	// surface a stable error message.
	require.Equal(t, rootSyncNotificationCredentialHash, records.Records[1].Auth)
	// note the commit is empty because the branch does not exist
	require.Equal(t, `{
  "content": {
    "raw": "RootSync root-sync-2 failed to sync commit  on branch nonexistent-branch!\n\nKNV2004: error in the git-sync container: {\"Msg\":\"error syncing repo, will retry\",\"Err\":\"Run(git clone -v --no-checkout -b nonexistent-branch --depth 1 git@test-git-server.config-management-system-test:/git-server/repos/config-management-system/root-sync-2 /repo/source): exit status 128: { stdout: \\\"\\\", stderr: \\\"Cloning into \'/repo/source\'...\\\\nwarning: Could not find remote branch nonexistent-branch to clone.\\\\nfatal: Remote branch nonexistent-branch not found in upstream origin\\\\nfatal: The remote end hung up unexpectedly\\\" }\"}\u000A\u000AFor more information, see https://g.co/cloud/acm-errors#knv2004\n\n"
  }
}`, records.Records[1].Message)
	// creating a new error should produce another notification
	rootSync.Spec.Git.Branch = "another-nonexistent-branch"
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Switch RootSync %s to another-nonexistent-branch", rootSyncNN.Name)))
	nt.WaitForRootSyncSourceError(rootSyncNN.Name,
		status.SourceErrorCode,
		"Remote branch another-nonexistent-branch not found in upstream")
	// query RootSync to get the generation
	rootSyncObj = &v1beta1.RootSync{}
	if err := nt.KubeClient.Get(rootSyncNN.Name, rootSyncNN.Namespace, rootSyncObj); err != nil {
		nt.T.Fatal(err)
	}
	// first iteration should set AlreadyNotified to false, subsequently true
	// skip the false check as it is a race condition whether we catch it
	err = nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace,
		[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
			ObservedGeneration: rootSyncObj.Generation,
			Commit:             "", // commit is empty, because the Syncing condition does not have the commit
			Deliveries: []v1beta1.NotificationDelivery{
				{
					Trigger:         "on-sync-failed",
					Service:         "local",
					Recipient:       "",
					AlreadyNotified: true,
				},
			},
		})},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// notification from first branch failure, including warning of new known host
	// notification from first branch failure
	// notification of second branch failure, including warning of new known host
	// notification of second branch failure
	records, err = waitForNotifications(nt, 4)
	if err != nil {
		nt.T.Fatal(err)
	}
	// first notification error includes a warning of known host added which includes an IP address.
	// the IP address is dynamic, so skip checking the message for the first notification
	require.Equal(t, rootSyncNotificationCredentialHash, records.Records[2].Auth)
	// subsequent git-sync iterations do not include the warning, but otherwise should
	// surface a stable error message.
	require.Equal(t, rootSyncNotificationCredentialHash, records.Records[3].Auth)
	// note the commit is empty because the branch does not exist
	require.Equal(t, `{
  "content": {
    "raw": "RootSync root-sync-2 failed to sync commit  on branch another-nonexistent-branch!\n\nKNV2004: error in the git-sync container: {\"Msg\":\"error syncing repo, will retry\",\"Err\":\"Run(git clone -v --no-checkout -b another-nonexistent-branch --depth 1 git@test-git-server.config-management-system-test:/git-server/repos/config-management-system/root-sync-2 /repo/source): exit status 128: { stdout: \\\"\\\", stderr: \\\"Cloning into \'/repo/source\'...\\\\nwarning: Could not find remote branch another-nonexistent-branch to clone.\\\\nfatal: Remote branch another-nonexistent-branch not found in upstream origin\\\\nfatal: The remote end hung up unexpectedly\\\" }\"}\u000A\u000AFor more information, see https://g.co/cloud/acm-errors#knv2004\n\n"
  }
}`, records.Records[3].Message)
	// fix the RootSync by resetting to a valid branch
	rootSync.Spec.Git.Branch = gitproviders.MainBranch
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Switch RootSync %s to master", rootSyncNN.Name)))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	// creating a new Syncing failure based on the contents of a commit
	namespaceName := "bookstore"
	cm1Name := "cm1"
	cm2Name := "cm2"
	namespace := fake.NamespaceObject(namespaceName)
	nt.Must(nt.RootRepos[rootSyncNN.Name].Add("acme/ns.yaml", namespace))
	// cm1 depends on cm2
	nt.Must(nt.RootRepos[rootSyncNN.Name].Add("acme/cm1.yaml", fake.ConfigMapObject(core.Name(cm1Name), core.Namespace(namespaceName),
		core.Annotation(dependson.Annotation, "/namespaces/bookstore/ConfigMap/cm2"))))
	// cm2 depends on cm1
	nt.Must(nt.RootRepos[rootSyncNN.Name].Add("acme/cm2.yaml", fake.ConfigMapObject(core.Name(cm2Name), core.Namespace(namespaceName),
		core.Annotation(dependson.Annotation, "/namespaces/bookstore/ConfigMap/cm1"))))
	nt.Must(nt.RootRepos[rootSyncNN.Name].CommitAndPush("Add namespace, and ConfigMaps (cm1 and cm2) which depend on each other"))
	nt.WaitForRootSyncSyncError(rootSyncNN.Name,
		applier.ApplierErrorCode,
		"cyclic dependency")
	// notification of conflicting depends-on relationships
	records, err = waitForNotifications(nt, 5)
	if err != nil {
		nt.T.Fatal(err)
	}
	commit := nt.RootRepos[rootSyncNN.Name].MustHash(nt.T)
	require.Equal(t, rootSyncNotificationCredentialHash, records.Records[4].Auth)
	require.Equal(t, fmt.Sprintf(`{
  "content": {
    "raw": "RootSync root-sync-2 failed to sync commit %s on branch main!\n\nKNV2009: invalid objects: [\"bookstore_cm1__ConfigMap\", \"bookstore_cm2__ConfigMap\"] cyclic dependency: /namespaces/bookstore/ConfigMap/cm1 -\u003E /namespaces/bookstore/ConfigMap/cm2; /namespaces/bookstore/ConfigMap/cm2 -\u003E /namespaces/bookstore/ConfigMap/cm1\u000A\u000AFor more information, see https://g.co/cloud/acm-errors#knv2009\n\n"
  }
}`, commit), records.Records[4].Message)
	// push a new commit which should produce another notification with the same error
	cm3Name := "cm3"
	nt.Must(nt.RootRepos[rootSyncNN.Name].Add("acme/cm3.yaml", fake.ConfigMapObject(core.Name(cm3Name), core.Namespace(namespaceName))))
	nt.Must(nt.RootRepos[rootSyncNN.Name].CommitAndPush("Add ConfigMap cm3"))
	nt.WaitForRootSyncSyncError(rootSyncNN.Name,
		applier.ApplierErrorCode,
		"cyclic dependency")
	// same notification of conflicting depends-on relationships, but for a new commit.
	// commit is included as part of the unique key for the trigger (oncePer)
	records, err = waitForNotifications(nt, 6)
	if err != nil {
		nt.T.Fatal(err)
	}
	commit = nt.RootRepos[rootSyncNN.Name].MustHash(nt.T)
	require.Equal(t, rootSyncNotificationCredentialHash, records.Records[5].Auth)
	require.Equal(t, fmt.Sprintf(`{
  "content": {
    "raw": "RootSync root-sync-2 failed to sync commit %s on branch main!\n\nKNV2009: invalid objects: [\"bookstore_cm1__ConfigMap\", \"bookstore_cm2__ConfigMap\"] cyclic dependency: /namespaces/bookstore/ConfigMap/cm1 -\u003E /namespaces/bookstore/ConfigMap/cm2; /namespaces/bookstore/ConfigMap/cm2 -\u003E /namespaces/bookstore/ConfigMap/cm1\u000A\u000AFor more information, see https://g.co/cloud/acm-errors#knv2009\n\n"
  }
}`, commit), records.Records[5].Message)
}

func TestNotificationOnSyncCreated(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN("root-sync-2")
	rootReconcilerName := core.RootReconcilerName(rootSyncNN.Name)
	nt := nomostest.New(t, nomostesting.Notification,
		ntopts.Unstructured,
		ntopts.InstallNotificationServer,
		ntopts.RootRepo(rootSyncNN.Name),
	)
	var err error
	credentialMap := map[string]string{
		rootSyncNN.Namespace: rootSyncNotificationPassword,
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
			nomostest.WithOnSyncCreatedTrigger,
			nomostest.WithSyncCreatedTemplate,
			nomostest.WithLocalWebhookService,
		)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	// Enable notifications on RootSync
	rootSync := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, rootSyncNN.Name)
	nomostest.SubscribeRootSyncNotification(rootSync, "on-sync-created", "local")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name), rootSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Enable notifications on RootSync"))
	err = nt.Watcher.WatchObject(kinds.Deployment(), rootReconcilerName, rootSyncNN.Namespace,
		[]testpredicates.Predicate{testpredicates.DeploymentHasContainer(reconcilermanager.Notification)})
	if err != nil {
		nt.T.Fatal(err)
	}

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
		hash, err := nt.RootRepos[rootSyncNN.Name].Hash()
		if err != nil {
			return err
		}
		return nt.Watcher.WatchObject(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace,
			[]testpredicates.Predicate{testpredicates.NotificationHasStatus(v1beta1.NotificationStatus{
				ObservedGeneration: rootSyncObj.Generation,
				Commit:             hash,
				Deliveries: []v1beta1.NotificationDelivery{
					{
						Trigger:         "on-sync-created",
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

	// validate notification
	require.Equal(nt.T, nomostest.NotificationRecords{
		Records: []nomostest.NotificationRecord{
			{
				Message: "{\n  \"content\": {\n    \"raw\": \"RootSync root-sync-2 created\"\n  }\n}",
				Auth:    rootSyncNotificationCredentialHash, // base64 encoded username/pass
			},
		},
	}, *records)

	// deleting root-sync-2
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(rootSyncNN.Namespace, rootSyncNN.Name)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove root-sync-2"))
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.NotificationV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func waitForNotifications(nt *nomostest.NT, expectedNum int) (*nomostest.NotificationRecords, error) {
	var records *nomostest.NotificationRecords
	var err error
	took, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
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
	if jsonStr, err := json.MarshalIndent(records.Records, "", "  "); err == nil {
		nt.T.Logf("records:\n%s", jsonStr)
	}
	if err != nil {
		return records, err
	} else if len(records.Records) != expectedNum { // check if got more records than expected
		return records, fmt.Errorf("want %d, got %d", expectedNum, len(records.Records))
	}
	return records, nil
}
