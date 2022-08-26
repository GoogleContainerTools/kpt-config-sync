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
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ArgoRolloutNS = "argo-rollouts"
	RolloutNS     = "rollout"
)

func TestRolloutResourcesOnCSR(t *testing.T) {
	nt := nomostest.New(
		t,
		ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(ArgoRolloutNS, configsync.RepoSyncName),
		ntopts.NamespaceRepo(RolloutNS, configsync.RepoSyncName),
	)

	// Get the absolute path of the rollout testdata
	testDataDir, err := filepath.Abs("../testdata/rollout/")
	if err != nil {
		nt.T.Fatal(err)
	}

	// install argo rollouts
	nn := nomostest.RepoSyncNN(ArgoRolloutNS, configsync.RepoSyncName)
	repo, exist := nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}
	repo.Copy(filepath.Join(testDataDir, "install.yaml"), "acme/namespaces/argo-rollouts")
	repo.CommitAndPush("install Argo Rollouts in the argo-rollouts namespace")
	nt.WaitForRepoSyncs()

	// sync with initial config
	nn = nomostest.RepoSyncNN(RolloutNS, configsync.RepoSyncName)
	repo, exist = nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}
	repo.Copy(filepath.Join(testDataDir, "rollout-blue.yaml"), "acme/namespaces/rollout/rollout.yaml")
	repo.Copy(filepath.Join(testDataDir, "service.yaml"), "acme/namespaces/rollout/service.yaml")
	repo.Copy(filepath.Join(testDataDir, "test-success.yaml"), "acme/namespaces/rollout/test.yaml")
	repo.CommitAndPush("initial config")
	nt.WaitForRepoSyncs()

	// Verify that the Rollout object is in the desired state.
	// We do not need to verify the analysisTemplate object
	// as it does not have a status specification
	// it will always in the Current status
	gvkRollout := schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Rollout",
	}

	// First update needs to be healthy
	validateRolloutResourceHealthy(nt, gvkRollout, "rollouts-demo", RolloutNS)

	// Update the image tag in the rollout.yaml
	repo.Copy(filepath.Join(testDataDir, "rollout-yellow.yaml"), "acme/namespaces/rollout/rollout.yaml")
	repo.CommitAndPush("update the image tag to yellow")
	nt.WaitForRepoSyncs()

	// Analysis should succeed, so the rollout object needs to be Healthy
	validateRolloutResourceHealthy(nt, gvkRollout, "rollouts-demo", RolloutNS)

	// Update the rollout.yaml and test.yaml
	repo.Copy(filepath.Join(testDataDir, "rollout-blue.yaml"), "acme/namespaces/rollout/rollout.yaml")
	repo.Copy(filepath.Join(testDataDir, "test-failed.yaml"), "acme/namespaces/rollout/test.yaml")
	repo.CommitAndPush("update the image tag to blue and force the analysis runs failed")
	nt.WaitForRepoSyncs()

	// Analysis should fail, so the rollout object needs to be Degraded
	validateRolloutResourceDegraded(nt, gvkRollout, "rollouts-demo", RolloutNS)

}

func validateRolloutResourceHealthy(nt *nomostest.NT, gvk schema.GroupVersionKind, name, namespace string) {
	nomostest.Wait(nt.T, fmt.Sprintf("wait for rollout resources %q %v to be healthty", name, gvk),
		nt.DefaultWaitTimeout, func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			return nt.Validate(name, namespace, u, rolloutResourceHealthy)
		})
}

func validateRolloutResourceDegraded(nt *nomostest.NT, gvk schema.GroupVersionKind, name, namespace string) {
	nomostest.Wait(nt.T, fmt.Sprintf("wait for rollout resources %q %v to be degraded", name, gvk),
		nt.DefaultWaitTimeout, func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			return nt.Validate(name, namespace, u, rolloutResourceDegraded)
		})
}

func rolloutResourceHealthy(o client.Object) error {
	u := o.(*unstructured.Unstructured)
	phase, found, err := unstructured.NestedString(u.Object, "status", "phase")
	if err != nil || !found || phase != "Healthy" {
		return fmt.Errorf(".status.phase not found %v", err)
	}
	return nil
}

func rolloutResourceDegraded(o client.Object) error {
	u := o.(*unstructured.Unstructured)
	phase, found, err := unstructured.NestedString(u.Object, "status", "phase")
	if err != nil || !found || phase != "Degraded" {
		return fmt.Errorf(".status.phase not found %v", err)
	}
	return nil
}
