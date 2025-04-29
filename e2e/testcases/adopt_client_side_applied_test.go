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

	"github.com/google/go-cmp/cmp"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
)

func TestAdoptClientSideAppliedResource(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))

	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Declare a ClusterRole and apply it to the cluster.
	nsViewerName := "ns-viewer"
	preClusterRole := k8sobjects.ClusterRoleObject(core.Name(nsViewerName))
	preClusterRole.SetAnnotations(map[string]string{"keep": "annotation"})
	preClusterRole.SetLabels(map[string]string{"keep": "label"})
	preClusterRole.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"namespaces"},
		Verbs:     []string{"get", "list"},
	}}
	nt.Must(nt.KubeClient.Create(preClusterRole))

	// Add the ClusterRole and wait for ConfigSync to sync it.
	managedClusterRole := k8sobjects.ClusterRoleObject(core.Name(nsViewerName))
	managedClusterRole.SetAnnotations(map[string]string{"declared": "annotation"})
	managedClusterRole.SetLabels(map[string]string{"declared": "label"})
	managedClusterRole.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"namespaces"},
		Verbs:     []string{"get"},
	}}
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("%s/ns-viewer-cr.yaml", gitproviders.DefaultSyncDir),
		managedClusterRole))
	nt.Must(rootSyncGitRepo.CommitAndPush("add namespace-viewer ClusterRole"))
	nt.Must(nt.WatchForAllSyncs())

	// Validate:
	// - the ClusterRole exists
	// - the annotations/labels are merged with the adopted metadata
	// - the Rules are overwritten by what is applied from the repository
	role := &rbacv1.ClusterRole{}
	nt.Must(nt.Validate(nsViewerName, "", role,
		testpredicates.HasAnnotation("keep", "annotation"),
		testpredicates.HasLabel("keep", "label"),
		testpredicates.HasAnnotation("declared", "annotation"),
		testpredicates.HasLabel("declared", "label")))

	if diff := cmp.Diff(role.Rules[0].Verbs, []string{"get"}); diff != "" {
		nt.T.Errorf(diff)
	}
}
