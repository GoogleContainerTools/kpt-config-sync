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
		if err := nomostest.TeardownSyncStatusWatchController(nt); err != nil {
			nt.T.Error(err)
		}
	})

	if err := nomostest.SetupSyncStatusWatchController(nt); err != nil {
		nt.T.Fatal(err)
	}

	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	syncBranch := "main" // Default branch name

	nt.Must(rootSyncGitRepo.Remove("acme/system/repo.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("cause source error in RootSync"))
	nt.Must(nt.Watcher.WatchForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, ""))

	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	if err := nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once", commit)
	logs, err := nt.GetPodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}

	logMessages := []string{"KNV1017: The system/ directory must declare a Repo Resource."}
	if err := assertLogEntryHasCount(logs, 1, commit, rootSyncID.Name, rootSyncID.Namespace, logMessages); err != nil {
		nt.T.Fatal(err)
	}

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
		if err := nomostest.TeardownSyncStatusWatchController(nt); err != nil {
			nt.T.Error(err)
		}
	})

	if err := nomostest.SetupSyncStatusWatchController(nt); err != nil {
		nt.T.Fatal(err)
	}

	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	syncBranch := "main"

	nt.Must(rootSyncGitRepo.Remove("acme/system/repo.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("cause source error in RootSync"))
	nt.Must(nt.Watcher.WatchForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, ""))

	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	if err := nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once in Cloud Logging", commit)
	filter := fmt.Sprintf(
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND resource.labels.cluster_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		nt.ClusterName,
		startTime.Format(time.RFC3339),
	)
	if err := waitForLogEntryInCloudLogs(nt, filter, 1, commit, rootSyncID.Name, rootSyncID.Namespace, []string{"KNV1017: The system/ directory must declare a Repo Resource."}, 120*time.Second); err != nil {
		nt.T.Fatal(err)
	}

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
		if err := nomostest.TeardownSyncStatusWatchController(nt); err != nil {
			nt.T.Error(err)
		}
	})

	if err := nomostest.SetupSyncStatusWatchController(nt); err != nil {
		nt.T.Fatal(err)
	}

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

	msg := "RootSync objects are only allowed in the config-management-system namespace, not in test-ns"
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

	logMessages := []string{msg}
	if err := assertLogEntryHasCount(logs, 1, "", rootSync.Name, rootSync.Namespace, logMessages); err != nil {
		nt.T.Fatal(err)
	}
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
		if err := nomostest.TeardownSyncStatusWatchController(nt); err != nil {
			nt.T.Error(err)
		}
	})

	if err := nomostest.SetupSyncStatusWatchController(nt); err != nil {
		nt.T.Fatal(err)
	}

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

	msg := "RootSync objects are only allowed in the config-management-system namespace, not in test-ns"
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
	if err := waitForLogEntryInCloudLogs(nt, filter, 1, "", rootSync.Name, rootSync.Namespace, []string{msg}, 120*time.Second); err != nil {
		nt.T.Fatal(err)
	}
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
		if err := nomostest.TeardownSyncStatusWatchController(nt); err != nil {
			nt.T.Error(err)
		}
	})

	if err := nomostest.SetupSyncStatusWatchController(nt); err != nil {
		nt.T.Fatal(err)
	}

	msg := "RepoSync bookstore/repo-sync must not manage itself in its repo"
	rs := &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	sanitizedRs := k8sobjects.RepoSyncObjectV1Beta1(rs.Namespace, rs.Name)
	sanitizedRs.Spec = rs.Spec
	nt.Must(repoSyncGitRepo.Add("acme/repo-sync.yaml", sanitizedRs))
	nt.Must(repoSyncGitRepo.CommitAndPush("create source error in RepoSync"))
	nt.Must(nt.Watcher.WatchForRepoSyncSourceError(rs.Namespace, rs.Name, validate.SelfReconcileErrorCode, msg))

	rs = &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once", commit)
	logs, err := nt.GetPodLogs(nomostest.StatusWatchNamespace, nomostest.StatusWatchName, "", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}

	logMessages := []string{msg, "\"generation\":1"}
	if err := assertLogEntryHasCount(logs, 1, commit, repoSyncID.Name, repoSyncID.Namespace, logMessages); err != nil {
		nt.T.Fatal(err)
	}
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
		if err := nomostest.TeardownSyncStatusWatchController(nt); err != nil {
			nt.T.Error(err)
		}
	})

	if err := nomostest.SetupSyncStatusWatchController(nt); err != nil {
		nt.T.Fatal(err)
	}

	msg := "RepoSync bookstore/repo-sync must not manage itself in its repo"
	rs := &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	sanitizedRs := k8sobjects.RepoSyncObjectV1Beta1(rs.Namespace, rs.Name)
	sanitizedRs.Spec = rs.Spec
	nt.Must(repoSyncGitRepo.Add("acme/repo-sync.yaml", sanitizedRs))
	nt.Must(repoSyncGitRepo.CommitAndPush("create source error in RepoSync"))
	nt.Must(nt.Watcher.WatchForRepoSyncSourceError(rs.Namespace, rs.Name, validate.SelfReconcileErrorCode, msg))

	rs = &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	commit := rs.Status.Source.Commit

	nt.T.Logf("Check for source related error message at commit %s occurrence once in Cloud Logging", commit)
	filter := fmt.Sprintf(
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND resource.labels.cluster_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		nt.ClusterName,
		startTime.Format(time.RFC3339),
	)
	if err := waitForLogEntryInCloudLogs(nt, filter, 1, commit, repoSyncID.Name, repoSyncID.Namespace, []string{msg, "\"generation\":1"}, 120*time.Second); err != nil {
		nt.T.Fatal(err)
	}
}

// assertLogEntryHasCount checks if the specified messages appear in the logs
// exactly the expected number of times, matching the given commit, name, and namespace.
func assertLogEntryHasCount(logs []string, expectedCount int, commit, rname, rnamespace string, messages []string) error {
	count := 0

	for _, line := range logs {
		if strings.Contains(line, commit) && strings.Contains(line, rname) && strings.Contains(line, rnamespace) {
			allFound := false
			for _, msg := range messages {
				if !strings.Contains(line, msg) {
					break
				}
				allFound = true
			}
			if allFound {
				count++
			}
		}
	}
	if count != expectedCount {
		return fmt.Errorf("Expected %d occurrences of error log entry %s, but found %d",
			expectedCount, messages, count)
	}
	return nil
}

// waitForLogEntryInCloudLogs queries Cloud Logging and asserts the log entry appears within the timeout.
func waitForLogEntryInCloudLogs(nt *nomostest.NT, filter string, expectedCount int, commit, rname, rnamespace string, messages []string, timeout time.Duration) error {
	ctx := nt.Context
	client, err := logadmin.NewClient(ctx, *e2e.GCPProject)
	if err != nil {
		return fmt.Errorf("failed to create logadmin client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			nt.T.Errorf("Failed to close logadmin client: %v", err)
		}
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

		if err := assertLogEntryHasCount(lastLogs, expectedCount, commit, rname, rnamespace, messages); err != nil {
			return fmt.Errorf("log entry not found yet: %w", err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to find log entry after retries: %v\nLast logs: %v", err, lastLogs)
	}
	return nil
}
