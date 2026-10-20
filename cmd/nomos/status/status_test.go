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
	"errors"
	"testing"
	"text/tabwriter"

	"github.com/GoogleContainerTools/config-sync/cmd/nomos/util"
	"github.com/GoogleContainerTools/config-sync/pkg/api/configmanagement"
	v1 "github.com/GoogleContainerTools/config-sync/pkg/api/configmanagement/v1"
	"github.com/GoogleContainerTools/config-sync/pkg/api/configsync"
	"github.com/GoogleContainerTools/config-sync/pkg/api/configsync/v1beta1"
	kptv1alpha1 "github.com/GoogleContainerTools/config-sync/pkg/api/kpt.dev/v1alpha1"
	"github.com/GoogleContainerTools/config-sync/pkg/client/restconfig"
	"github.com/GoogleContainerTools/config-sync/pkg/core"
	"github.com/GoogleContainerTools/config-sync/pkg/core/k8sobjects"
	csfake "github.com/GoogleContainerTools/config-sync/pkg/generated/clientset/versioned/fake"
	"github.com/GoogleContainerTools/config-sync/pkg/kinds"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newTestClusterClient(t *testing.T, client client.Client, k8sClient kubernetes.Interface, cmObj *unstructured.Unstructured) *ClusterClient {
	t.Helper()

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
	//nolint:staticcheck // allow deprecated function for backwards compatibility
	csClient := csfake.NewSimpleClientset(repoObj)

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

	return &ClusterClient{
		Client:           client,
		repos:            csClient.ConfigmanagementV1().Repos(),
		K8sClient:        k8sClient,
		ConfigManagement: cmClient,
	}
}

func newFakeClient(objs []client.Object, errorFuncs *interceptor.Funcs) client.Client {
	cb := fake.NewClientBuilder().WithScheme(core.Scheme).WithObjects(objs...)

	if errorFuncs != nil {
		cb.WithInterceptorFuncs(*errorFuncs)
	}

	return cb.Build()
}

func configManagementObject(enableMultiRepo bool) *unstructured.Unstructured {
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

func rootSyncObject(name string) *v1beta1.RootSync {
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

func repoSyncObject(ns, name string) *v1beta1.RepoSync {
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

func resourceGroupObject(name, ns string) *kptv1alpha1.ResourceGroup {
	resource := kptv1alpha1.ObjMetadata{
		Name:      "test",
		Namespace: "bookstore",
		GroupKind: kptv1alpha1.GroupKind{
			Group: "apps",
			Kind:  "Deployment",
		},
	}
	resourceStatus := kptv1alpha1.ResourceStatus{
		ObjMetadata: kptv1alpha1.ObjMetadata{
			Name:      "test",
			Namespace: "bookstore",
			GroupKind: kptv1alpha1.GroupKind{
				Group: "apps",
				Kind:  "Deployment",
			},
		},
		Status:     kptv1alpha1.Current,
		Reconcile:  kptv1alpha1.ReconcileSucceeded,
		Strategy:   kptv1alpha1.Apply,
		SourceHash: "abcd123",
	}
	rg := k8sobjects.ResourceGroupObject(ns, name,
		k8sobjects.WithRGResourceStatuses(resourceStatus),
		k8sobjects.WithRGResources(resource))
	return rg
}

func TestClusterStates(t *testing.T) {
	cmNamespace := k8sobjects.NamespaceObject(configmanagement.ControllerNamespace, core.Label("configmanagement.gke.io/system", "true"))
	operatorDeployment := k8sobjects.DeploymentObject(core.Name(util.ACMOperatorDeployment), core.Namespace(configmanagement.ControllerNamespace))
	operatorPod := k8sobjects.PodObject("operator-pod", []corev1.Container{}, core.Namespace(configmanagement.ControllerNamespace), core.Labels(map[string]string{"k8s-app": "config-management-operator"}))
	operatorPod.Status.Phase = corev1.PodRunning

	cmObjMono := configManagementObject(false)
	cmObjMulti := configManagementObject(true)

	rootSync := rootSyncObject(configsync.RootSyncName)
	rootSyncRG := resourceGroupObject(configsync.RootSyncName, configsync.ControllerNamespace)
	rootSyncCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: configsync.RootSyncCRDName,
		},
	}

	repoSync := repoSyncObject("test-ns", configsync.RepoSyncName)

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
					newFakeClient([]client.Object{}, nil),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
					cmObjMono),
			},
			wantStateMap: map[string]*ClusterState{
				"mono-repo-cluster": {
					Ref:     "mono-repo-cluster",
					isMulti: &[]bool{false}[0],
					status:  util.ErrorMsg,
					Error:   "This cluster is running in legacy mono-repo mode, which is no longer supported",
				},
			},
			wantMonoRepoClusters: []string{"mono-repo-cluster"},
		},

		{
			name: "multi-repo cluster with RepoSync fetch error",
			clientMap: map[string]*ClusterClient{
				"rootsync-error-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{}, &interceptor.Funcs{
						List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if _, ok := list.(*v1beta1.RepoSyncList); ok {
								return errors.New("generic error fetching RepoSyncs")
							}
							return cl.List(ctx, list, opts...)
						},
					}),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
					cmObjMulti),
			},
			wantStateMap: map[string]*ClusterState{
				"rootsync-error-cluster": {
					Ref:     "rootsync-error-cluster",
					isMulti: &[]bool{true}[0],
					status:  util.ErrorMsg,
					Error:   "generic error fetching RepoSyncs",
				},
			},
		},
		{
			name: "multi-repo cluster",
			clientMap: map[string]*ClusterClient{
				"multi-repo-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{rootSync, rootSyncRG}, nil),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
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
							resources: []kptv1alpha1.ResourceStatus{
								{
									ObjMetadata: kptv1alpha1.ObjMetadata{
										Name:      "test",
										Namespace: "bookstore",
										GroupKind: kptv1alpha1.GroupKind{
											Group: "apps",
											Kind:  "Deployment",
										},
									},
									Status:     kptv1alpha1.Current,
									SourceHash: "abcd123",
									Strategy:   kptv1alpha1.Apply,
									Reconcile:  kptv1alpha1.ReconcileSucceeded,
								},
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
					newFakeClient([]client.Object{rootSync, rootSyncRG, rootSyncCRD}, nil),
					k8sfake.NewClientset(),
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
							resources: []kptv1alpha1.ResourceStatus{
								{
									ObjMetadata: kptv1alpha1.ObjMetadata{
										Name:      "test",
										Namespace: "bookstore",
										GroupKind: kptv1alpha1.GroupKind{
											Group: "apps",
											Kind:  "Deployment",
										},
									},
									Status:     kptv1alpha1.Current,
									SourceHash: "abcd123",
									Strategy:   kptv1alpha1.Apply,
									Reconcile:  kptv1alpha1.ReconcileSucceeded,
								},
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
					newFakeClient([]client.Object{rootSyncCRD}, nil),
					k8sfake.NewClientset(),
					nil),
			},
			wantStateMap: map[string]*ClusterState{
				"multi-repo-cluster": {
					Ref:           "multi-repo-cluster",
					Error:         "No RootSync resources found; No RepoSync resources found",
					noSyncObjects: true,
				},
			},
			wantMonoRepoClusters: nil,
		},
		{
			name: "multi-repo cluster with no CM object and missing RG objects",
			clientMap: map[string]*ClusterClient{
				"multi-repo-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{rootSync, rootSyncCRD, repoSync}, nil),
					k8sfake.NewClientset(),
					nil),
			},
			wantStateMap: map[string]*ClusterState{
				"multi-repo-cluster": {
					Ref:    "multi-repo-cluster",
					status: util.ErrorMsg,
					Error:  `resourcegroups.kpt.dev "root-sync" not found in namespace "config-management-system"; resourcegroups.kpt.dev "repo-sync" not found in namespace "test-ns"`,
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
						{
							scope:             "test-ns",
							syncName:          configsync.RepoSyncName,
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
			name: "mixed clusters",
			clientMap: map[string]*ClusterClient{
				"unavailable-cluster": nil,
				"mono-repo-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{}, nil),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
					cmObjMono),
				"multi-repo-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{rootSync, rootSyncRG}, nil),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
					cmObjMulti),
			},
			wantStateMap: map[string]*ClusterState{
				"unavailable-cluster": unavailableCluster("unavailable-cluster"),
				"mono-repo-cluster": {
					Ref:     "mono-repo-cluster",
					isMulti: &[]bool{false}[0],
					status:  util.ErrorMsg,
					Error:   "This cluster is running in legacy mono-repo mode, which is no longer supported",
				},
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
							resources: []kptv1alpha1.ResourceStatus{
								{
									ObjMetadata: kptv1alpha1.ObjMetadata{
										Name:      "test",
										Namespace: "bookstore",
										GroupKind: kptv1alpha1.GroupKind{
											Group: "apps",
											Kind:  "Deployment",
										},
									},
									Status:     kptv1alpha1.Current,
									SourceHash: "abcd123",
									Strategy:   kptv1alpha1.Apply,
									Reconcile:  kptv1alpha1.ReconcileSucceeded,
								},
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

	cmObjMono := configManagementObject(false)
	cmObjMulti := configManagementObject(true)

	rootSync := rootSyncObject(configsync.RootSyncName)
	repoSync := repoSyncObject("test-ns", configsync.RepoSyncName)
	rootSyncRG := resourceGroupObject(configsync.RootSyncName, configsync.ControllerNamespace)
	repoSyncRG := resourceGroupObject(configsync.RepoSyncName, `test-ns`)
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
					newFakeClient([]client.Object{}, nil),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
					cmObjMono),
				"multi-repo-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{rootSync, rootSyncRG}, nil),
					k8sfake.NewClientset(cmNamespace, operatorDeployment, operatorPod),
					cmObjMulti),
				"repo-sync-cluster": newTestClusterClient(t,
					newFakeClient([]client.Object{repoSync, repoSyncRG, rootSyncCRD}, nil),
					k8sfake.NewClientset(),
					cmObjMulti),
			},
			names:          []string{"mono-repo-cluster", "multi-repo-cluster", "unavailable-cluster", "repo-sync-cluster"},
			currentContext: "multi-repo-cluster",
			want: "\x1b[33mNotice: The cluster \"mono-repo-cluster\" is still running in the legacy mode.\nRun `nomos migrate` to enable multi-repo mode. " +
				"It provides you with additional features and gives you the flexibility to sync to a single repository, or multiple repositories.\x1b[0m\n" +
				"\nmono-repo-cluster\n" +
				"  --------------------\n" +
				"  ERROR     This cluster is running in legacy mono-repo mode, which is no longer supported\n" +
				"\n*multi-repo-cluster\n" +
				"  --------------------\n" +
				"  <root>:root-sync                           https://github.com/my/repo/acme@v1.2.3     \n" +
				"  SYNCED @ 2022-08-15 12:00:00 +0000 UTC     abcdef                                     \n" +
				"  Managed resources:\n" +
				"       NAMESPACE     NAME                     STATUS      SOURCEHASH\n" +
				"       bookstore     deployment.apps/test     Current     abcd123\n" +
				"\nunavailable-cluster\n" +
				"  --------------------\n" +
				"  N/A     Failed to connect to cluster\n" +
				"\nrepo-sync-cluster\n" +
				"  --------------------\n" +
				"  test-ns:repo-sync                          https://github.com/my/repo/acme@v1.2.3     \n" +
				"  SYNCED @ 2022-08-15 12:00:00 +0000 UTC     abcdef                                     \n" +
				"  Managed resources:\n" +
				"       NAMESPACE     NAME                     STATUS      SOURCEHASH\n" +
				"       bookstore     deployment.apps/test     Current     abcd123\n",
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

			stateMap, monoRepoClusters := clusterStates(context.Background(), tc.clientMap)
			printStatus(writer, stateMap, monoRepoClusters, tc.names)

			got := buf.String()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("printStatus() returned diff (-want +got):\n%s", diff)
			}
		})
	}
}
