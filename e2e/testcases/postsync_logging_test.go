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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/validate/raw/validate"
)

// loggingGSA is the name of the Google Service Account used for logging permissions
const loggingGSA = "e2e-test-log-writer"

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

	nt.Must(setupLoggingPermission(nt, nomostest.StatusWatchName, nomostest.StatusWatchNamespace))

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
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		startTime.Format(time.RFC3339),
	)
	cloudLogs, err := queryCloudLogs(nt, filter)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.Logger.Debugf("Cloud Logs: %v", cloudLogs)

	logMessages := []string{"KNV1017: The system/ directory must declare a Repo Resource."}
	if err := assertLogEntryHasCount(cloudLogs, 1, commit, rootSyncID.Name, rootSyncID.Namespace, logMessages); err != nil {
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

	nt.Must(setupLoggingPermission(nt, nomostest.StatusWatchName, nomostest.StatusWatchNamespace))

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
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		startTime.Format(time.RFC3339),
	)
	cloudLogs, err := queryCloudLogs(nt, filter)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.Logger.Debugf("Cloud Logs: %v", cloudLogs)

	logMessages := []string{msg}
	if err := assertLogEntryHasCount(cloudLogs, 1, "", rootSync.Name, rootSync.Namespace, logMessages); err != nil {
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

	nt.Must(setupLoggingPermission(nt, nomostest.StatusWatchName, nomostest.StatusWatchNamespace))

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
		`resource.type="k8s_container" AND resource.labels.namespace_name="%s" AND timestamp >= "%s"`,
		nomostest.StatusWatchNamespace,
		startTime.Format(time.RFC3339),
	)
	cloudLogs, err := queryCloudLogs(nt, filter)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.Logger.Debugf("Cloud Logs: %v", cloudLogs)

	logMessages := []string{msg, "\"generation\":1"}
	if err := assertLogEntryHasCount(cloudLogs, 1, commit, repoSyncID.Name, repoSyncID.Namespace, logMessages); err != nil {
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

// queryCloudLogs retrieves log entries from Google Cloud Logging that match the specified filter.
// The function retries for up to 120 seconds if no logs are found initially.
func queryCloudLogs(nt *nomostest.NT, filter string) ([]string, error) {
	ctx := nt.Context
	client, err := logadmin.NewClient(ctx, *e2e.GCPProject)
	if err != nil {
		return nil, fmt.Errorf("failed to create logadmin client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			nt.T.Errorf("Failed to close logadmin client: %v", err)
		}
	}()

	var logs []string
	_, err = retry.Retry(120*time.Second, func() error {
		logs = nil
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
			logs = append(logs, fmt.Sprintf("%s: %v", entry.Timestamp.Format(time.RFC3339), entry.Payload))
		}

		if len(logs) == 0 {
			return fmt.Errorf("no log entries found")
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to query logs after retries: %v", err)
	}
	return logs, nil
}

// setupLoggingPermission configures the necessary permissions for the post-sync tests
// to write logs to Google Cloud Logging. This function works with GKE clusters
// and checks for Workload Identity support, but Workload Identity is not mandatory.
// The function is used when the test needs to communicate with Google APIs.
func setupLoggingPermission(nt *nomostest.NT, ksaName, namespace string) error {
	nt.T.Logf("Checking workload identity for %s in namespace %s", ksaName, namespace)
	workloadPool, err := workloadidentity.GetWorkloadPool(nt)
	if err != nil {
		return fmt.Errorf("failed to get workload pool: %v", err)
	}

	if workloadPool == "" {
		return nil // Workload Identity not enabled
	}

	nt.T.Logf("Setting up logging permission for %s in namespace %s", loggingGSA, *e2e.GCPProject)
	gsaEmail := fmt.Sprintf("%s@%s.iam.gserviceaccount.com", loggingGSA, *e2e.GCPProject)

	// Create IAM binding between GSA and KSA
	member := fmt.Sprintf("serviceAccount:%s.svc.id.goog[%s/%s]",
		*e2e.GCPProject,
		namespace,
		ksaName,
	)

	_, err = nt.Shell.ExecWithDebug("gcloud", "iam", "service-accounts", "add-iam-policy-binding",
		gsaEmail,
		"--role=roles/iam.workloadIdentityUser",
		fmt.Sprintf("--member=%s", member),
	)
	if err != nil {
		return fmt.Errorf("failed to create IAM binding: %v", err)
	}

	// Annotate Kubernetes Service Account
	ksa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ksaName,
			Namespace: namespace,
		},
	}
	if err := nt.KubeClient.Get(ksaName, namespace, ksa); err != nil {
		return fmt.Errorf("failed to get service account: %v", err)
	}

	if core.SetAnnotation(ksa, "iam.gke.io/gcp-service-account", gsaEmail) {
		if err := nt.KubeClient.Update(ksa); err != nil {
			return fmt.Errorf("failed to annotate service account: %v", err)
		}
	}

	return nil
}
