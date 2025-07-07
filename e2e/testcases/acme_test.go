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

	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/metadata"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAcmeCorpRepo(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1)

	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nsToFolder := map[string]string{
		"analytics":                  "eng",
		"backend":                    "eng",
		"frontend":                   "eng",
		"new-prj":                    "rnd",
		"newer-prj":                  "rnd",
		rootSyncGitRepo.SafetyNSName: "",
	}
	nt.Must(rootSyncGitRepo.Copy("../../examples/acme", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("Initialize the acme directory"))
	nt.Must(nt.WatchForAllSyncs())

	wantAnnotations := map[string]string{
		metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String(),
		metadata.HNCManagedBy:                metadata.ManagedByValue,
	}
	checkResourceCount(nt, kinds.Namespace(), "", len(nsToFolder), nil, wantAnnotations)
	for namespace, folder := range nsToFolder {
		predicates := []testpredicates.Predicate{
			testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String()),
			testpredicates.HasAnnotation(metadata.HNCManagedBy, metadata.ManagedByValue),
			testpredicates.HasLabel(fmt.Sprintf("%s.tree.hnc.x-k8s.io/depth", namespace), "0"),
		}
		if folder != "" {
			predicates = append(predicates, testpredicates.HasLabel(fmt.Sprintf("%s.tree.hnc.x-k8s.io/depth", folder), "1"))
		}
		nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace, "",
			testwatcher.WatchPredicates(predicates...)))
	}

	// Check ClusterRoles (add one for the 'safety' ClusterRole)
	checkResourceCount(nt, kinds.ClusterRole(), "", 4, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ClusterRole(), "", 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("acme-admin", "", &rbacv1.ClusterRole{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	nt.Must(nt.Validate("namespace-viewer", "", &rbacv1.ClusterRole{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	nt.Must(nt.Validate("rbac-viewer", "", &rbacv1.ClusterRole{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))

	// Check ClusterRoleBindings
	checkResourceCount(nt, kinds.ClusterRoleBinding(), "", 2, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ClusterRoleBinding(), "", 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("namespace-viewers", "", &rbacv1.ClusterRoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	nt.Must(nt.Validate("rbac-viewers", "", &rbacv1.ClusterRoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))

	// Check Namespace-scoped resources
	namespace := "analytics"
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 2, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("mike-rolebinding", namespace, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	nt.Must(nt.Validate("alice-rolebinding", namespace, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("pod-quota", namespace, &corev1.ResourceQuota{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))

	namespace = "backend"
	checkResourceCount(nt, kinds.ConfigMap(), namespace, 1, map[string]string{metadata.ManagedByKey: metadata.ManagedByValue}, nil)
	nt.Must(nt.Validate("store-inventory", namespace, &corev1.ConfigMap{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 2, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("bob-rolebinding", namespace, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	nt.Must(nt.Validate("alice-rolebinding", namespace, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	rq := &corev1.ResourceQuota{}
	nt.Must(nt.Validate("pod-quota", namespace, rq,
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	if rq.Spec.Hard.Pods().String() != "1" {
		nt.T.Fatalf("expected resourcequota.spec.hard.pods: 1, got %s", rq.Spec.Hard.Pods().String())
	}

	namespace = "frontend"
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace, "",
		testwatcher.WatchPredicates(
			testpredicates.HasLabel("env", "prod"),
			testpredicates.HasAnnotation("audit", "true"),
		)))
	checkResourceCount(nt, kinds.ConfigMap(), namespace, 1, map[string]string{metadata.ManagedByKey: metadata.ManagedByValue}, nil)
	nt.Must(nt.Validate("store-inventory", namespace, &corev1.ConfigMap{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 2, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("alice-rolebinding", namespace, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	nt.Must(nt.Validate("sre-admin", namespace, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("pod-quota", namespace, &corev1.ResourceQuota{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))

	namespace = "new-prj"
	checkResourceCount(nt, kinds.Role(), namespace, 1, nil, nil)
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("acme-admin", namespace, &rbacv1.Role{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("quota", namespace, &corev1.ResourceQuota{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))

	namespace = "newer-prj"
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String()})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{metadata.HNCManagedBy: metadata.ManagedByValue})
	nt.Must(nt.Validate("quota", namespace, &corev1.ResourceQuota{},
		testpredicates.HasAnnotation(metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())))

	nt.Must(rootSyncGitRepo.Remove("acme/cluster"))
	// Add back the safety ClusterRole to pass the safety check (KNV2006).
	nt.Must(rootSyncGitRepo.AddSafetyClusterRole())
	nt.Must(rootSyncGitRepo.CommitAndPush("Reset the acme directory"))
	nt.Must(nt.WatchForAllSyncs())
}

// TestObjectInCMSNamespace will test that user can sync object to CMS namespace
func TestObjectInCMSNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))

	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.Must(rootSyncGitRepo.Copy("../testdata/object-in-cms-namespace", "acme"))
	nt.Must(rootSyncGitRepo.CommitAndPush("adding resource to config-management-system namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// validate the resources synced successfully in CMS namespace
	namespace := configmanagement.ControllerNamespace
	err := nt.Validate("cms-configmap", namespace, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("cms-roles", namespace, &rbacv1.Role{})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func checkResourceCount(nt *nomostest.NT, gvk schema.GroupVersionKind, namespace string, count int, labels, annotations map[string]string) {
	list := &unstructured.UnstructuredList{}
	list.GetObjectKind().SetGroupVersionKind(gvk)
	var opts []client.ListOption
	if len(namespace) > 0 {
		opts = append(opts, client.InNamespace(namespace))
	}
	if len(labels) > 0 {
		opts = append(opts, (client.MatchingLabels)(labels))
	}
	if err := nt.KubeClient.List(list, opts...); err != nil {
		nt.T.Fatal(err)
	}

	actualCount := 0
	for _, obj := range list.Items {
		if containsSubMap(obj.GetAnnotations(), annotations) {
			actualCount++
		}
	}
	if actualCount != count {
		nt.T.Fatalf("expected %d resources(gvk: %s), got %d", count, gvk.String(), actualCount)
	}
}

func containsSubMap(m1, m2 map[string]string) bool {
	for k2, v2 := range m2 {
		if v1, ok := m1[k2]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}
