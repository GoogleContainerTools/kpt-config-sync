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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file includes tests for KCC resources from a cloud source repository.
// The test applies KCC resources and verifies the GCP resources
// are created successfully.
// It then deletes KCC resources and verifies the GCP resources
// are removed successfully.
func TestKCCResourcesOnCSR(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.KccTest)
	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("sync to the kcc resources from a CSR repo")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kcc", "branch": "main", "repo": "https://source.developers.google.com/p/stolos-dev/r/configsync-ci-cc", "auth": "gcpserviceaccount","gcpServiceAccountEmail": "e2e-test-csr-reader@stolos-dev.iam.gserviceaccount.com", "secretRef": {"name": ""}}, "sourceFormat": "unstructured"}}`)

	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "kcc"}))

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
	validateKCCResourceReady(nt, gvkPubSubTopic, "test-cs", "foo")
	validateKCCResourceReady(nt, gvkPubSubSubscription, "test-cs-read", "foo")
	validateKCCResourceReady(nt, gvkServiceAccount, "pubsub-app", "foo")
	validateKCCResourceReady(nt, gvkPolicyMember, "policy-member-binding", "foo")

	// Remove the kcc resources
	nt.T.Log("sync to an empty directory from a CSR repo")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kcc-empty"}}}`)
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "kcc-empty"}))

	// Verify that the GCP resources are removed.
	validateKCCResourceNotFound(nt, gvkPubSubTopic, "test-cs", "foo")
	validateKCCResourceNotFound(nt, gvkPubSubSubscription, "test-cs-read", "foo")
	validateKCCResourceNotFound(nt, gvkServiceAccount, "pubsub-app", "foo")
	validateKCCResourceNotFound(nt, gvkPolicyMember, "policy-member-binding", "foo")

	// Change the rs back so that it works in the shared test environment.
	defer nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}, "sourceFormat": "hierarchy"}}`, origRepoURL))
}

func validateKCCResourceReady(nt *nomostest.NT, gvk schema.GroupVersionKind, name, namespace string) {
	nomostest.Wait(nt.T, fmt.Sprintf("wait for kcc resources %q %v to be ready", name, gvk),
		nt.DefaultWaitTimeout, func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			return nt.Validate(name, namespace, u, kccResourceReady)
		})
}

func kccResourceReady(o client.Object) error {
	u := o.(*unstructured.Unstructured)
	conditions, found, err := unstructured.NestedSlice(u.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
		return fmt.Errorf(".status.conditions not found %v", err)
	}
	condition := (conditions[0]).(map[string]interface{})
	if condition["type"] != "Ready" || condition["status"] != "True" {
		return fmt.Errorf("resource is not ready %v", condition)
	}
	return nil
}

func validateKCCResourceNotFound(nt *nomostest.NT, gvk schema.GroupVersionKind, name, namespace string) {
	nomostest.Wait(nt.T, fmt.Sprintf("wait for %q %v to terminate", name, gvk),
		nt.DefaultWaitTimeout, func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			return nt.ValidateNotFound(name, namespace, u)
		})
}
