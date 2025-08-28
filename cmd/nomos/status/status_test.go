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

package status

import (
	"bytes"
	"context"
	"testing"
	"text/tabwriter"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	csfake "kpt.dev/configsync/pkg/generated/clientset/versioned/fake"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestClusterClient(t *testing.T, k8sObjs []runtime.Object, csObjs []runtime.Object, cmObj *unstructured.Unstructured) *ClusterClient {
	t.Helper()

	var allClientObjs []client.Object
	for _, o := range k8sObjs {
		allClientObjs = append(allClientObjs, o.(client.Object))
	}
	for _, o := range csObjs {
		allClientObjs = append(allClientObjs, o.(client.Object))
	}
	if cmObj != nil {
		allClientObjs = append(allClientObjs, cmObj)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(core.Scheme).WithObjects(allClientObjs...).Build()
	k8sClient := k8sfake.NewClientset(k8sObjs...)

	var dynamicObjs []runtime.Object
	if cmObj != nil {
		dynamicObjs = append(dynamicObjs, cmObj)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(core.Scheme, dynamicObjs...)
	util.DynamicClient = func(_ *rest.Config) (dynamic.Interface, error) {
		return dynamicClient, nil
	}
	cmClient, err := util.NewConfigManagementClient(&rest.Config{})
	if err != nil {
		t.Fatalf("failed to create ConfigManagementClient: %v", err)
	}

	repoObj := &v1.Repo{
		TypeMeta: metav1.TypeMeta{APIVersion: v1.SchemeGroupVersion.String(), Kind: "Repo"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "repo",
		},
		Status: v1.RepoStatus{
			Source: v1.RepoSourceStatus{Token: "abc1234"},
			Sync:   v1.RepoSyncStatus{LatestToken: "abc1234"},
		},
	}

	csClient := csfake.NewSimpleClientset(repoObj)

	return &ClusterClient{
		Client:           fakeClient,
		repos:            csClient.ConfigmanagementV1().Repos(),
		K8sClient:        k8sClient,
		ConfigManagement: cmClient,
	}
}

func configManagementObj(enableMultiRepo bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{},
	}
	u.SetGroupVersionKind(kinds.ConfigManagement())
	u.SetName(util.ConfigManagementName)
	_ = unstructured.SetNestedField(u.Object, "1.1", "status", util.ConfigManagementVersionName)
	_ = unstructured.SetNestedField(u.Object, enableMultiRepo, "spec", "enableMultiRepo")
	_ = unstructured.SetNestedMap(u.UnstructuredContent(), map[string]interface{}{
		"syncRepo":   "https://github.com/my/repo",
		"syncBranch": "main",
		"policyDir":  "acme",
		"syncRev":    "v1.2.3",
	}, "spec", "git")

	return u
}

func rootSyncObj(name string) *v1beta1.RootSync {
	rootSyncObj := k8sobjects.RootSyncObjectV1Beta1(name)
	rootSyncObj.Spec.Git = &v1beta1.Git{
		Repo:     "https://github.com/my/repo",
		Branch:   "main",
		Dir:      "acme",
		Revision: "v1.2.3",
	}
	rootSyncObj.Status.Sync.Commit = "abcdef"
	rootSyncObj.Status.Source.Commit = "abcdef"
	rootSyncObj.Status.Rendering.Commit = "abcdef"
	rootSyncObj.Status.Sync.LastUpdate = lastSyncTimestamp
	rootSyncObj.Status.Conditions = []v1beta1.RootSyncCondition{
		{Type: v1beta1.RootSyncSyncing, Status: metav1.ConditionFalse, Commit: "abcdef"},
	}
	return rootSyncObj
}

func repoSyncObj(ns, name string) *v1beta1.RepoSync {
	repoSyncObj := k8sobjects.RepoSyncObjectV1Beta1(ns, name)
	repoSyncObj.Spec.Git = &v1beta1.Git{
		Repo:     "https://github.com/my/repo",
		Branch:   "main",
		Dir:      "acme",
		Revision: "v1.2.3",
	}
	repoSyncObj.Status.Sync.Commit = "abcdef"
	repoSyncObj.Status.Source.Commit = "abcdef"
	repoSyncObj.Status.Rendering.Commit = "abcdef"
	repoSyncObj.Status.Sync.LastUpdate = lastSyncTimestamp
	repoSyncObj.Status.Conditions = []v1beta1.RepoSyncCondition{
		{Type: v1beta1.RepoSyncSyncing, Status: metav1.ConditionFalse, Commit: "abcdef"},
	}
	return repoSyncObj
}

func TestClusterStates(t *testing.T) {
	cmNamespace := k8sobjects.NamespaceObject(configmanagement.ControllerNamespace, core.Label("configmanagement.gke.io/system", "true"))
	operatorDeployment := k8sobjects.DeploymentObject(core.Name(util.ACMOperatorDeployment), core.Namespace(configmanagement.ControllerNamespace))
	operatorPod := k8sobjects.PodObject("operator-pod", []corev1.Container{}, core.Namespace(configmanagement.ControllerNamespace), core.Labels(map[string]string{"k8s-app": "config-management-operator"}))
	operatorPod.Status.Phase = corev1.PodRunning

	cmObjMono := configManagementObj(false)
	cmObjMulti := configManagementObj(true)

	rootSyncObj := rootSyncObj(configsync.RootSyncName)
	rootSyncRG := k8sobjects.ResourceGroupObject(core.Namespace(configsync.ControllerNamespace), core.Name(configsync.RootSyncName))
	rootSyncCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: configsync.RootSyncCRDName,
		},
	}

	testCases := []struct {
		name                 string
		clientMap            map[string]*ClusterClient
		wantStateMap         map[string]*ClusterState
		wantMonoRepoClusters []string
	}{
		{
			name: "unavailable cluster",
			clientMap: map[string]*ClusterClient{
				"unavailable-cluster": nil,
			},
			wantStateMap: map[string]*ClusterState{
				"unavailable-cluster": unavailableCluster("unavailable-cluster"),
			},
			wantMonoRepoClusters: nil,
		},
		{
			name: "mono-repo cluster",
			clientMap: map[string]*ClusterClient{
				"mono-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{operatorDeployment, operatorPod},
					[]runtime.Object{},
					cmObjMono),
			},
			wantStateMap:         map[string]*ClusterState{},
			wantMonoRepoClusters: []string{"mono-repo-cluster"},
		},
		{
			name: "multi-repo cluster",
			clientMap: map[string]*ClusterClient{
				"multi-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{cmNamespace, operatorDeployment, operatorPod},
					[]runtime.Object{rootSyncObj, rootSyncRG},
					cmObjMulti),
			},
			wantStateMap: map[string]*ClusterState{
				"multi-repo-cluster": {
					Ref:     "multi-repo-cluster",
					isMulti: &[]bool{true}[0],
					repos: []*RepoState{
						{
							scope:             "<root>",
							syncName:          configsync.RootSyncName,
							status:            syncedMsg,
							commit:            "abcdef",
							lastSyncTimestamp: lastSyncTimestamp,
							git: &v1beta1.Git{
								Repo:     "https://github.com/my/repo",
								Branch:   "main",
								Dir:      "acme",
								Revision: "v1.2.3",
							},
						},
					},
				},
			},
			wantMonoRepoClusters: nil,
		},
		{
			name: "multi-repo cluster with no CM object",
			clientMap: map[string]*ClusterClient{
				"multi-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{},
					[]runtime.Object{rootSyncObj, rootSyncRG, rootSyncCRD},
					nil),
			},
			wantStateMap: map[string]*ClusterState{
				"multi-repo-cluster": {
					Ref: "multi-repo-cluster",
					repos: []*RepoState{
						{
							scope:             "<root>",
							syncName:          configsync.RootSyncName,
							status:            syncedMsg,
							commit:            "abcdef",
							lastSyncTimestamp: lastSyncTimestamp,
							git: &v1beta1.Git{
								Repo:     "https://github.com/my/repo",
								Branch:   "main",
								Dir:      "acme",
								Revision: "v1.2.3",
							},
						},
					},
				},
			},
			wantMonoRepoClusters: nil,
		},
		{
			name: "multi-repo cluster with no CM object or RSync",
			clientMap: map[string]*ClusterClient{
				"multi-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{},
					[]runtime.Object{rootSyncCRD},
					nil),
			},
			wantStateMap: map[string]*ClusterState{
				"multi-repo-cluster": {
					Ref:    "multi-repo-cluster",
					status: util.UnknownMsg,
					Error:  "Root repo error: No RootSync resources found, Namespace repo error: No RepoSync resources found",
				},
			},
			wantMonoRepoClusters: nil,
		},
		{
			name: "mixed clusters",
			clientMap: map[string]*ClusterClient{
				"unavailable-cluster": nil,
				"mono-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{operatorDeployment, operatorPod},
					[]runtime.Object{},
					cmObjMono),
				"multi-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{cmNamespace, operatorDeployment, operatorPod},
					[]runtime.Object{rootSyncObj, rootSyncRG},
					cmObjMulti),
			},
			wantStateMap: map[string]*ClusterState{
				"unavailable-cluster": unavailableCluster("unavailable-cluster"),
				"multi-repo-cluster": {
					Ref:     "multi-repo-cluster",
					isMulti: &[]bool{true}[0],
					repos: []*RepoState{
						{
							scope:             "<root>",
							syncName:          configsync.RootSyncName,
							status:            syncedMsg,
							commit:            "abcdef",
							lastSyncTimestamp: lastSyncTimestamp,
							git: &v1beta1.Git{
								Repo:     "https://github.com/my/repo",
								Branch:   "main",
								Dir:      "acme",
								Revision: "v1.2.3",
							},
						},
					},
				},
			},
			wantMonoRepoClusters: []string{"mono-repo-cluster"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotStateMap, gotMonoRepoClusters := clusterStates(context.Background(), tc.clientMap)

			if diff := cmp.Diff(tc.wantStateMap, gotStateMap, cmp.AllowUnexported(ClusterState{}, RepoState{})); diff != "" {
				t.Errorf("clusterStates() stateMap returned diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantMonoRepoClusters, gotMonoRepoClusters, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("clusterStates() monoRepoClusters returned diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPrintStatus(t *testing.T) {
	cmNamespace := k8sobjects.NamespaceObject(configmanagement.ControllerNamespace, core.Label("configmanagement.gke.io/system", "true"))
	operatorDeployment := k8sobjects.DeploymentObject(core.Name(util.ACMOperatorDeployment), core.Namespace(configmanagement.ControllerNamespace))
	operatorPod := k8sobjects.PodObject("operator-pod", []corev1.Container{}, core.Namespace(configmanagement.ControllerNamespace), core.Labels(map[string]string{"k8s-app": "config-management-operator"}))
	operatorPod.Status.Phase = corev1.PodRunning

	cmObjMono := configManagementObj(false)
	cmObjMulti := configManagementObj(true)

	rootSync := rootSyncObj(configsync.RootSyncName)
	repoSync := repoSyncObj("test-ns", configsync.RepoSyncName)
	rootSyncRG := k8sobjects.ResourceGroupObject(core.Namespace(configsync.ControllerNamespace), core.Name(configsync.RootSyncName))
	repoSyncRG := k8sobjects.ResourceGroupObject(core.Namespace("test-ns"), core.Name(configsync.RepoSyncName))
	rootSyncCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: configsync.RootSyncCRDName,
		},
	}

	testCases := []struct {
		name           string
		clientMap      map[string]*ClusterClient
		names          []string
		currentContext string
		want           string
	}{
		{
			name: "mixed clusters with current context",
			clientMap: map[string]*ClusterClient{
				"unavailable-cluster": nil,
				"mono-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{operatorDeployment, operatorPod},
					[]runtime.Object{},
					cmObjMono),
				"multi-repo-cluster": newTestClusterClient(t,
					[]runtime.Object{cmNamespace, operatorDeployment, operatorPod},
					[]runtime.Object{rootSync, rootSyncRG},
					cmObjMulti),
				"repo-sync-cluster": newTestClusterClient(t,
					[]runtime.Object{},
					[]runtime.Object{repoSync, repoSyncRG, rootSyncCRD},
					nil),
			},
			names:          []string{"mono-repo-cluster", "multi-repo-cluster", "unavailable-cluster", "repo-sync-cluster"},
			currentContext: "multi-repo-cluster",
			want: "\x1b[33mNotice: The cluster \"mono-repo-cluster\" is still running in the legacy mode.\nRun `nomos migrate` to enable multi-repo mode. " +
				"It provides you with additional features and gives you the flexibility to sync to a single repository, or multiple repositories.\x1b[0m\n" +
				"\n*multi-repo-cluster\n" +
				"  --------------------\n" +
				"  <root>:root-sync                           https://github.com/my/repo/acme@v1.2.3     \n" +
				"  SYNCED @ 2022-08-15 12:00:00 +0000 UTC     abcdef                                     \n" +
				"\nunavailable-cluster\n" +
				"  --------------------\n" +
				"  N/A     Failed to connect to cluster\n" +
				"\nrepo-sync-cluster\n" +
				"  --------------------\n" +
				"  UNKNOWN     Root repo error: No RootSync resources found\n" +
				"  --------------------\n" +
				"  test-ns:repo-sync                          https://github.com/my/repo/acme@v1.2.3     \n" +
				"  SYNCED @ 2022-08-15 12:00:00 +0000 UTC     abcdef                                     \n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			origGetCurrentContext := restconfig.CurrentContextName

			restconfig.CurrentContextName = func() (string, error) {
				return tc.currentContext, nil
			}

			t.Cleanup(func() {
				restconfig.CurrentContextName = origGetCurrentContext
			})

			var buf bytes.Buffer
			writer := tabwriter.NewWriter(&buf, 0, 0, 5, ' ', 0)

			printStatus(context.Background(), writer, tc.clientMap, tc.names)

			got := buf.String()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("printStatus() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
