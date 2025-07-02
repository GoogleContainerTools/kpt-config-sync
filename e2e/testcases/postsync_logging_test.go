// Copyright 2025 Google LLC
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
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/validate/raw/validate"
)

// TestStatusEventLogRootSync tests that error events from RootSync are properly logged
// to the SyncStatusWatchController pod logs. This test can run in any environment.
func TestStatusEventLogRootSync(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	startTime := time.Now().UTC()

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.PodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false)
		}
		nt.Must(nomostest.TeardownSyncStatusWatchController(nt))
	})

	nt.Must(nomostest.SetupSyncStatusWatchController(nt))

	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	syncBranch := "main" // Default branch name

	nt.Must(rootSyncGitRepo.Remove("acme/system/repo.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("cause source error in RootSync"))
	nt.Must(nt.Watcher.WatchForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, ""))

	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	nt.Must(nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rs))
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once", commit)
	logs, err := nt.GetPodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}

	message := "KNV1017: The system/ directory must declare a Repo Resource."
	nt.Must(assertOneLogLineWithMessage(logs, commit, rootSyncID.Name, rootSyncID.Namespace, message))

	nt.T.Log("Reset test repo")
	nt.Must(rootSyncGitRepo.Git("reset", "--hard", "HEAD^"))
	nt.Must(rootSyncGitRepo.Push(syncBranch, "-f"))
	nt.Must(nt.WatchForAllSyncs())
}

// TestCloudLoggingRootSync tests that error events from RootSync are properly sent
// to Google Cloud Logging. This test runs in post-submit GKE clusters to communicate
// with Google Cloud APIs. Workload Identity is not required but can be used if available.
func TestCloudLoggingRootSync(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.RequireGKE(t))
	startTime := time.Now().UTC()

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.PodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false)
		}
		nt.Must(nomostest.TeardownSyncStatusWatchController(nt))
	})

	nt.Must(nomostest.SetupSyncStatusWatchController(nt))

	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	syncBranch := "main"

	nt.Must(rootSyncGitRepo.Remove("acme/system/repo.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("cause source error in RootSync"))
	nt.Must(nt.Watcher.WatchForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, ""))

	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	nt.Must(nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rs))
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once in Cloud Logging", commit)
	filter := fmt.Sprintf(
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND resource.labels.cluster_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		nt.ClusterName,
		startTime.Format(time.RFC3339),
	)
	message := "KNV1017: The system/ directory must declare a Repo Resource."
	nt.Must(waitForLogEntryInCloudLogs(nt, filter, commit, rootSyncID.Name, rootSyncID.Namespace, message, 120*time.Second))

	nt.T.Log("Reset test repo")
	nt.Must(rootSyncGitRepo.Git("reset", "--hard", "HEAD^"))
	nt.Must(rootSyncGitRepo.Push(syncBranch, "-f"))
	nt.Must(nt.WatchForAllSyncs())
}

// TestConditionEventLogRootSync tests that condition-based errors from RootSync
// (like namespace validation errors) are properly logged to the SyncStatusWatchController
// pod logs. This test can run in any environment.
func TestConditionEventLogRootSync(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	startTime := time.Now().UTC()

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.PodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false)
		}
		nt.Must(nomostest.TeardownSyncStatusWatchController(nt))
	})

	nt.Must(nomostest.SetupSyncStatusWatchController(nt))

	testNamespace := k8sobjects.NamespaceObject(testNs)
	nt.Must(nt.KubeClient.Create(testNamespace))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, testNamespace))
	})

	nt.T.Logf("Validate RootSync can only exist in the config-management-system namespace")
	rootSync := k8sobjects.RootSyncObjectV1Beta1("rs-test", core.Namespace(testNs))
	rootSync.Spec.Git = &v1beta1.Git{Auth: configsync.AuthNone}
	nomostest.SetRSyncTestDefaults(nt, rootSync)
	nt.Must(nt.KubeClient.Create(rootSync))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, rootSync))
	})

	msg := fmt.Sprintf("KNV1061: RootSync objects are only allowed in the %s namespace, not in %s\n\nFor more information, see https://g.co/cloud/acm-errors#knv1061", configsync.ControllerNamespace, testNs)
	expectedCondition := &v1beta1.RootSyncCondition{
		Type:    v1beta1.RootSyncStalled,
		Status:  metav1.ConditionTrue,
		Reason:  "Validation",
		Message: msg,
		ErrorSummary: &v1beta1.ErrorSummary{
			ErrorCountAfterTruncation: 1,
			TotalCount:                1,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.Name, rootSync.Namespace,
		testwatcher.WatchPredicates(testpredicates.RootSyncHasCondition(expectedCondition))))

	nt.T.Log("Check for condition related error message occurrence once")
	logs, err := nt.GetPodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(assertOneLogLineWithMessage(logs, "", rootSync.Name, rootSync.Namespace, strings.Split(msg, "\n")[0]))
}

// TestCloudLoggingConditionEventRootSync tests that condition-based errors from RootSync
// are properly sent to Google Cloud Logging. This test runs in post-submit GKE clusters
// to communicate with Google Cloud APIs. Workload Identity is not required but can be
// used if available.
func TestCloudLoggingConditionEventRootSync(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.RequireGKE(t))
	startTime := time.Now().UTC()

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.PodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false)
		}
		nt.Must(nomostest.TeardownSyncStatusWatchController(nt))
	})

	nt.Must(nomostest.SetupSyncStatusWatchController(nt))

	testNamespace := k8sobjects.NamespaceObject(testNs)
	nt.Must(nt.KubeClient.Create(testNamespace))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, testNamespace))
	})

	nt.T.Logf("Validate RootSync can only exist in the config-management-system namespace")
	rootSync := k8sobjects.RootSyncObjectV1Beta1("rs-test", core.Namespace(testNs))
	rootSync.Spec.Git = &v1beta1.Git{Auth: configsync.AuthNone}
	nomostest.SetRSyncTestDefaults(nt, rootSync)
	nt.Must(nt.KubeClient.Create(rootSync))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, rootSync))
	})

	msg := fmt.Sprintf("KNV1061: RootSync objects are only allowed in the %s namespace, not in %s\n\nFor more information, see https://g.co/cloud/acm-errors#knv1061", configsync.ControllerNamespace, testNs)
	expectedCondition := &v1beta1.RootSyncCondition{
		Type:    v1beta1.RootSyncStalled,
		Status:  metav1.ConditionTrue,
		Reason:  "Validation",
		Message: msg,
		ErrorSummary: &v1beta1.ErrorSummary{
			ErrorCountAfterTruncation: 1,
			TotalCount:                1,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.Name, rootSync.Namespace,
		testwatcher.WatchPredicates(testpredicates.RootSyncHasCondition(expectedCondition))))

	nt.T.Logf("Check for condition related error message occurrence once in Cloud Logging")
	filter := fmt.Sprintf(
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND resource.labels.cluster_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		nt.ClusterName,
		startTime.Format(time.RFC3339),
	)
	nt.Must(waitForLogEntryInCloudLogs(nt, filter, "", rootSync.Name, rootSync.Namespace, strings.Split(msg, "\n")[0], 120*time.Second))
}

// TestStatusEventLogRepoSync tests that error events from RepoSync are properly logged
// to the SyncStatusWatchController pod logs. This test can run in any environment.
func TestStatusEventLogRepoSync(t *testing.T) {
	bsNamespace := "bookstore"
	repoSyncID := core.RepoSyncID(configsync.RepoSyncName, bsNamespace)
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
		ntopts.SyncWithGitSource(repoSyncID))
	repoSyncGitRepo := nt.SyncSourceGitReadWriteRepository(repoSyncID)
	startTime := time.Now().UTC()

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.PodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false)
		}
		nt.Must(nomostest.TeardownSyncStatusWatchController(nt))
	})

	nt.Must(nomostest.SetupSyncStatusWatchController(nt))

	msg := "RepoSync bookstore/repo-sync must not manage itself in its repo"
	rs := &v1beta1.RepoSync{}
	nt.Must(nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs))
	sanitizedRs := k8sobjects.RepoSyncObjectV1Beta1(rs.Namespace, rs.Name)
	sanitizedRs.Spec = rs.Spec
	nt.Must(repoSyncGitRepo.Add("acme/repo-sync.yaml", sanitizedRs))
	nt.Must(repoSyncGitRepo.CommitAndPush("create source error in RepoSync"))
	nt.Must(nt.Watcher.WatchForRepoSyncSourceError(rs.Namespace, rs.Name, validate.SelfReconcileErrorCode, msg))

	rs = &v1beta1.RepoSync{}
	nt.Must(nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs))
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once", commit)
	logs, err := nt.GetPodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(assertOneLogLineWithMessage(logs, commit, repoSyncID.Name, repoSyncID.Namespace, msg))
	nt.Must(assertOneLogLineWithMessage(logs, commit, repoSyncID.Name, repoSyncID.Namespace, "\"generation\":1"))
}

// TestCloudLoggingStatusEventRepoSync tests that error events from RepoSync are properly sent
// to Google Cloud Logging. This test runs in post-submit GKE clusters to communicate with
// Google Cloud APIs. Workload Identity is not required but can be used if available.
func TestCloudLoggingStatusEventRepoSync(t *testing.T) {
	bsNamespace := "bookstore"
	repoSyncID := core.RepoSyncID(configsync.RepoSyncName, bsNamespace)
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.RequireGKE(t),
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
		ntopts.SyncWithGitSource(repoSyncID))
	repoSyncGitRepo := nt.SyncSourceGitReadWriteRepository(repoSyncID)
	startTime := time.Now().UTC()

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.PodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false)
		}
		nt.Must(nomostest.TeardownSyncStatusWatchController(nt))
	})

	nt.Must(nomostest.SetupSyncStatusWatchController(nt))

	msg := "RepoSync bookstore/repo-sync must not manage itself in its repo"
	rs := &v1beta1.RepoSync{}
	nt.Must(nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs))
	sanitizedRs := k8sobjects.RepoSyncObjectV1Beta1(rs.Namespace, rs.Name)
	sanitizedRs.Spec = rs.Spec
	nt.Must(repoSyncGitRepo.Add("acme/repo-sync.yaml", sanitizedRs))
	nt.Must(repoSyncGitRepo.CommitAndPush("create source error in RepoSync"))
	nt.Must(nt.Watcher.WatchForRepoSyncSourceError(rs.Namespace, rs.Name, validate.SelfReconcileErrorCode, msg))

	rs = &v1beta1.RepoSync{}
	nt.Must(nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs))
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once in Cloud Logging", commit)
	filter := fmt.Sprintf(
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND resource.labels.cluster_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		nt.ClusterName,
		startTime.Format(time.RFC3339),
	)
	nt.Must(waitForLogEntryInCloudLogs(nt, filter, commit, repoSyncID.Name, repoSyncID.Namespace, msg, 120*time.Second))
}

// assertOneLogLineWithMessage checks if the specified message appears in exactly one line
// among the log entries which match the given commit, resource name, and namespace.
func assertOneLogLineWithMessage(logs []string, commit, resourceName, resourceNamespace, message string) error {
	count := 0
	for _, line := range logs {
		if !(strings.Contains(line, commit) && strings.Contains(line, resourceName) && strings.Contains(line, resourceNamespace)) {
			continue
		}
		if strings.Contains(line, message) {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("failed to find single occurrence of message in logs, counted %d", count)
	}
	return nil
}

// waitForLogEntryInCloudLogs queries Cloud Logging and asserts the log entry appears within the timeout.
func waitForLogEntryInCloudLogs(nt *nomostest.NT, filter string, commit, rname, rnamespace, message string, timeout time.Duration) error {
	ctx := nt.Context
	client, err := logadmin.NewClient(ctx, *e2e.GCPProject)
	if err != nil {
		return fmt.Errorf("failed to create logadmin client: %v", err)
	}
	defer func() {
		nt.Must(client.Close())
	}()

	var lastLogs []string
	_, err = retry.Retry(timeout, func() error {
		lastLogs = nil
		it := client.Entries(ctx,
			logadmin.Filter(filter),
			logadmin.NewestFirst(),
		)

		for {
			entry, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			lastLogs = append(lastLogs, fmt.Sprintf("%s: %v", entry.Timestamp.Format(time.RFC3339), entry.Payload))
		}

		return assertOneLogLineWithMessage(lastLogs, commit, rname, rnamespace, message)
	})

	if err != nil {
		return fmt.Errorf("failed to find log entry after retries: %v\nLast logs: %v", err, lastLogs)
	}
	return nil
}
