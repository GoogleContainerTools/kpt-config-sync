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

	"kpt.dev/configsync/e2e/nomostest"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/logging"
	"kpt.dev/configsync/pkg/status"
)

func TestVersionedLogging(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	startTime := time.Now().UTC()
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.Must(nt.WatchForAllSyncs())
	syncMetadata := logging.NewSyncMetadata(rootSyncID.Name, rootSyncID.Namespace, rootSyncID.Kind)
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	if err := nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	commit := rs.Status.Source.Commit
	nt.T.Log("Check for sync succeeded message at commit %s occurrence once", commit)
	logs, err := nt.GetPodLogs(configsync.ControllerNamespace, nomostest.DefaultRootReconcilerName, "reconciler", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}
	syncSuccessLog := logging.NewVersionedLogEntry(logging.SyncSucceeded, syncMetadata, "", commit, nil)
	if err := assertLogEntryHasCount(logs, syncSuccessLog, 1); err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(rootSyncGitRepo.Remove("acme/system/repo.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Cause source error"))
	nt.Must(nt.Watcher.WatchForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, ""))

	rs = k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	if err := nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	commit = rs.Status.Source.Commit
	nt.T.Log("Check for sync failed mesasge at commit %s occurrence once", commit)
	var expectedErrs status.MultiError
	expectedErrs = status.Append(expectedErrs, system.MissingRepoError())
	logs, err = nt.GetPodLogs(configsync.ControllerNamespace, nomostest.DefaultRootReconcilerName, "reconciler", false, &startTime)
	if err != nil {
		nt.T.Fatal(err)
	}
	SyncFailedLog := logging.NewVersionedLogEntry(logging.SyncFailed, syncMetadata, logging.GenerateSyncResultMessage(false, logging.ReconcileStageParse), commit, expectedErrs)
	if err := assertLogEntryHasCount(logs, SyncFailedLog, 1); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Reset test repo")
	nt.Must(rootSyncGitRepo.Git("reset", "--hard", "HEAD^"))
	nt.Must(rootSyncGitRepo.Push(syncBranch, "-f"))
	nt.Must(nt.WatchForAllSyncs())
}

func assertLogEntryHasCount(logs []string, targetLog logging.VersionedLog, expectedCount int) error {
	count := 0
	normalizedTargetLog := normalizeString(targetLog.String())

	for _, line := range logs {
		if strings.Contains(normalizeString(line), normalizedTargetLog) {
			count++
		}
	}
	if count != expectedCount {
		return fmt.Errorf("Expected %d occurrences of versioned log entry %s, but found %d",
			expectedCount, normalizedTargetLog, count)
	}
	return nil
}

func normalizeString(s string) string {
	parts := strings.SplitN(s, "\n", 2)
	return strings.TrimSpace(parts[0])
}
