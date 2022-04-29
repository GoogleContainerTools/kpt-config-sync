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
	"os/exec"
	"strings"
	"testing"
	"time"

	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/testing/fake"

	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	baseURL = "localhost:8001/api/v1/namespaces/config-management-system/services"
	port    = "metrics/proxy"
)

var configSyncManagementAnnotations = map[string]string{"configmanagement.gke.io/managed": "enabled", "hnc.x-k8s.io/managed-by": "configmanagement.gke.io"}

func configSyncManagementLabels(namespace, folder string) map[string]string {
	labels := map[string]string{fmt.Sprintf("%s.tree.hnc.x-k8s.io/depth", namespace): "0"}
	if folder != "" {
		labels[fmt.Sprintf("%s.tree.hnc.x-k8s.io/depth", folder)] = "1"
	}
	return labels
}

func TestAcmeCorpRepo(t *testing.T) {
	nt := nomostest.New(t)

	nsToFolder := map[string]string{
		"analytics": "eng",
		"backend":   "eng",
		"frontend":  "eng",
		"new-prj":   "rnd",
		"newer-prj": "rnd",
		nt.RootRepos[configsync.RootSyncName].SafetyNSName: ""}
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme", ".")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Initialize the acme directory")
	nt.WaitForRepoSyncs()

	checkResourceCount(nt, kinds.Namespace(), "", len(nsToFolder), nil, configSyncManagementAnnotations)
	for namespace, folder := range nsToFolder {
		checkNamespaceExists(nt, namespace, configSyncManagementLabels(namespace, folder), configSyncManagementAnnotations)
	}

	// Check ClusterRoles
	checkResourceCount(nt, kinds.ClusterRole(), "", 3, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ClusterRole(), "", 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &rbacv1.ClusterRole{}, "", "acme-admin", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if err := checkResource(nt, &rbacv1.ClusterRole{}, "", "namespace-viewer", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if err := checkResource(nt, &rbacv1.ClusterRole{}, "", "rbac-viewer", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	// Check ClusterRoleBindings
	checkResourceCount(nt, kinds.ClusterRoleBinding(), "", 2, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ClusterRoleBinding(), "", 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &rbacv1.ClusterRoleBinding{}, "", "namespace-viewers", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if err := checkResource(nt, &rbacv1.ClusterRoleBinding{}, "", "rbac-viewers", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	// Check PodSecurityPolicy
	checkResourceCount(nt, kinds.PodSecurityPolicy(), "", 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &policyv1beta1.PodSecurityPolicy{}, "", "example", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	// Check Namespace-scoped resources
	namespace := "analytics"
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 2, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &rbacv1.RoleBinding{}, namespace, "mike-rolebinding", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if err := checkResource(nt, &rbacv1.RoleBinding{}, namespace, "alice-rolebinding", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &corev1.ResourceQuota{}, namespace, "pod-quota", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	namespace = "backend"
	checkResourceCount(nt, kinds.ConfigMap(), namespace, 1, map[string]string{"app.kubernetes.io/managed-by": "configmanagement.gke.io"}, nil)
	if err := checkResource(nt, &corev1.ConfigMap{}, namespace, "store-inventory", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 2, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &rbacv1.RoleBinding{}, namespace, "bob-rolebinding", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if err := checkResource(nt, &rbacv1.RoleBinding{}, namespace, "alice-rolebinding", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	resourceQuota := &corev1.ResourceQuota{}
	if err := checkResource(nt, resourceQuota, namespace, "pod-quota", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if resourceQuota.Spec.Hard.Pods().String() != "1" {
		nt.T.Fatalf("expected resourcequota.spec.hard.pods: 1, got %s", resourceQuota.Spec.Hard.Pods().String())
	}

	namespace = "frontend"
	checkNamespaceExists(nt, namespace, map[string]string{"env": "prod"}, map[string]string{"audit": "true"})
	checkResourceCount(nt, kinds.ConfigMap(), namespace, 1, map[string]string{"app.kubernetes.io/managed-by": "configmanagement.gke.io"}, nil)
	if err := checkResource(nt, &corev1.ConfigMap{}, namespace, "store-inventory", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 2, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &rbacv1.RoleBinding{}, namespace, "alice-rolebinding", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	if err := checkResource(nt, &rbacv1.RoleBinding{}, namespace, "sre-admin", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &corev1.ResourceQuota{}, namespace, "pod-quota", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	namespace = "new-prj"
	checkResourceCount(nt, kinds.Role(), namespace, 1, nil, nil)
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &rbacv1.Role{}, namespace, "acme-admin", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &corev1.ResourceQuota{}, namespace, "quota", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	namespace = "newer-prj"
	checkResourceCount(nt, kinds.Role(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.RoleBinding(), namespace, 0, nil, nil)
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 1, nil, map[string]string{"configmanagement.gke.io/managed": "enabled"})
	checkResourceCount(nt, kinds.ResourceQuota(), namespace, 0, nil, map[string]string{"hnc.x-k8s.io/managed-by": "configmanagement.gke.io"})
	if err := checkResource(nt, &corev1.ResourceQuota{}, namespace, "quota", nil, map[string]string{"configmanagement.gke.io/managed": "enabled"}); err != nil {
		nt.T.Fatal(err)
	}

	checkLegacyMetricsPages(nt)

	// gracefully delete cluster-scoped resources to pass the safety check (KNV2006).
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster")
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/test-clusterrole.yaml", fake.ClusterRoleObject())
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Reset the acme directory")
	nt.WaitForRepoSyncs()
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
	if err := nt.Client.List(nt.Context, list, opts...); err != nil {
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

func checkResource(nt *nomostest.NT, obj client.Object, namespace, name string, labels, annotations map[string]string) error {
	if err := nt.Get(name, namespace, obj); err != nil {
		return err
	}
	if !containsSubMap(obj.GetLabels(), labels) {
		return fmt.Errorf("%s/%s doesn't include all expected labels: object.labels=%v, expected=%v",
			namespace, name, labels, obj.GetLabels())
	}
	if !containsSubMap(obj.GetAnnotations(), annotations) {
		return fmt.Errorf("%s/%s doesn't include all expected annotations: object.annotations=%v, expected=%v",
			namespace, name, annotations, obj.GetAnnotations())
	}
	return nil
}

func checkNamespaceExists(nt *nomostest.NT, name string, labels, annotations map[string]string) {
	nomostest.Wait(nt.T, "namespace exists", nt.DefaultWaitTimeout, func() error {
		return checkResource(nt, &corev1.Namespace{}, "", name, labels, annotations)
	})
}

func containsSubMap(m1, m2 map[string]string) bool {
	for k2, v2 := range m2 {
		if v1, ok := m1[k2]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}

func checkLegacyMetricsPages(nt *nomostest.NT) {
	if nt.MultiRepo {
		return
	}
	// creates a proxy server or application-level gateway between localhost and the Kubernetes API Server
	cmd := exec.Command("kubectl", "proxy", "--kubeconfig", nt.KubeconfigPath()) //nolint:staticcheck
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Start()
	if err != nil || stderr.Len() != 0 {
		nt.T.Fatal(err)
	}
	nt.T.Cleanup(func() {
		err := cmd.Process.Kill()
		if err != nil {
			nt.T.Errorf("killing port forward process: %v", err)
		}
	})

	service := "git-importer"
	_, err = nomostest.Retry(30*time.Second, func() error {
		out, err := exec.Command("curl", "-s", fmt.Sprintf("%s/%s:%s/threads", baseURL, service, port)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to scrape %s /threads %v %s", service, err, string(out))
		}
		out, err = exec.Command("curl", "-s", fmt.Sprintf("%s/%s:%s/metrics", baseURL, service, port)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to scrape %s /metrics %v %s", service, err, string(out))
		}
		return nil
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}
