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

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/testing/fake"
)

// This file includes tests for KCC resources from a cloud source repository.
// The test applies KCC resources and verifies the GCP resources
// are created successfully.
// It then deletes KCC resources and verifies the GCP resources
// are removed successfully.
func TestKCCResourcesOnCSR(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.KccTest, ntopts.RequireGKE(t))

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("sync to the kcc resources from a CSR repo")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "kcc", "branch": "main", "repo": "https://source.developers.google.com/p/%s/r/configsync-kcc", "auth": "gcpserviceaccount","gcpServiceAccountEmail": "e2e-test-csr-reader@%s.iam.gserviceaccount.com", "secretRef": {"name": ""}}, "sourceFormat": "unstructured"}}`, *e2e.GCPProject, *e2e.GCPProject))

	err := nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: "kcc",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify that the GCP resources are created.
	gvkPubSubTopic := schema.GroupVersionKind{
		Group:   "pubsub.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "PubSubTopic",
	}
	gvkPubSubSubscription := schema.GroupVersionKind{
		Group:   "pubsub.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "PubSubSubscription",
	}
	gvkServiceAccount := schema.GroupVersionKind{
		Group:   "iam.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "IAMServiceAccount",
	}
	gvkPolicyMember := schema.GroupVersionKind{
		Group:   "iam.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "IAMPolicyMember",
	}

	// Wait until all objects are reconciled
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkPubSubTopic, "test-cs", "foo")
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkPubSubSubscription, "test-cs-read", "foo")
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkServiceAccount, "pubsub-app", "foo")
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkPolicyMember, "policy-member-binding", "foo")
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	// Remove the kcc resources
	nt.T.Log("sync to an empty directory from a CSR repo")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kcc-empty"}}}`)
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: "kcc-empty",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Wait until all objects are not found
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkPubSubTopic, "test-cs", "foo")
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkPubSubSubscription, "test-cs-read", "foo")
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkServiceAccount, "pubsub-app", "foo")
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkPolicyMember, "policy-member-binding", "foo")
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}
