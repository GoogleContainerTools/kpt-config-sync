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

package reconcile_test

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func deployment(deploymentStrategy appsv1.DeploymentStrategyType, opts ...core.MetaMutator) *appsv1.Deployment {
	mutators := append([]core.MetaMutator{core.Namespace(eng)}, opts...)
	result := fake.DeploymentObject(mutators...)
	result.Spec.Strategy.Type = deploymentStrategy
	return result
}

func namespace(name string, opts ...core.MetaMutator) *corev1.Namespace {
	return fake.NamespaceObject(name, opts...)
}

func namespaceConfig(name string, state v1.ConfigSyncState, opts ...core.MetaMutator) *v1.NamespaceConfig {
	result := fake.NamespaceConfigObject(opts...)
	result.Name = name
	result.Status.SyncState = state
	return result
}

var (
	eng              = "eng"
	managedNamespace = namespace(eng, syncertest.ManagementEnabled, syncertest.TokenAnnotation)

	namespaceCfg = namespaceConfig(eng, v1.StateSynced, syncertest.NamespaceConfigImportToken(syncertest.Token),
		syncertest.NamespaceConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute))))
	namespaceCfgSynced = namespaceConfig(eng, v1.StateSynced, syncertest.NamespaceConfigImportToken(syncertest.Token),
		syncertest.NamespaceConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute))),
		syncertest.NamespaceConfigSyncTime(), syncertest.NamespaceConfigSyncToken())

	managedNamespaceReconcileComplete = testingfake.NewEvent(namespaceCfg, corev1.EventTypeNormal, v1.EventReasonReconcileComplete)
)

func TestManagedNamespaceConfigReconcile(t *testing.T) {
	testCases := []struct {
		name      string
		declared  client.Object
		actual    client.Object
		want      []client.Object
		wantEvent *testingfake.Event
	}{
		{
			name:     "create from declared state",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementEnabled, syncertest.TokenAnnotation),
			},
			wantEvent: managedNamespaceReconcileComplete,
		},
		{
			name:     "do not create if management disabled",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementDisabled),
			want: []client.Object{
				namespaceCfgSynced,
			},
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not create if declared managed invalid",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementInvalid),
			want: []client.Object{
				namespaceCfgSynced,
			},
			wantEvent: testingfake.NewEvent(
				deployment(appsv1.RollingUpdateDeploymentStrategyType),
				corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation),
		},
		{
			name:     "update to declared state",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType),
			actual:   deployment(appsv1.RecreateDeploymentStrategyType, syncertest.ManagementEnabled),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementEnabled, syncertest.TokenAnnotation),
			},
			wantEvent: managedNamespaceReconcileComplete,
		},
		{
			name:     "update to declared state even if actual managed unset",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType),
			actual:   deployment(appsv1.RecreateDeploymentStrategyType),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementEnabled, syncertest.TokenAnnotation),
			},
			wantEvent: managedNamespaceReconcileComplete,
		},
		// The declared state is fine, so overwrite the invalid one on the API Server.
		{
			name:     "update to declared state if actual managed invalid",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType),
			actual:   deployment(appsv1.RecreateDeploymentStrategyType, syncertest.ManagementInvalid),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementEnabled, syncertest.TokenAnnotation),
			},
			wantEvent: managedNamespaceReconcileComplete,
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not update if declared managed invalid",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementInvalid),
			actual:   deployment(appsv1.RecreateDeploymentStrategyType),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RecreateDeploymentStrategyType),
			},
			wantEvent: testingfake.NewEvent(
				deployment(appsv1.RollingUpdateDeploymentStrategyType),
				corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation),
		},
		{
			name:     "update to unmanaged",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementDisabled),
			actual:   deployment(appsv1.RecreateDeploymentStrategyType, syncertest.ManagementEnabled),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RecreateDeploymentStrategyType),
			},
			wantEvent: managedNamespaceReconcileComplete,
		},
		{
			name:     "do not update if unmanaged",
			declared: deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementDisabled),
			actual:   deployment(appsv1.RecreateDeploymentStrategyType),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RecreateDeploymentStrategyType),
			},
		},
		{
			name:   "delete if managed",
			actual: deployment(appsv1.RecreateDeploymentStrategyType, syncertest.ManagementEnabled),
			want: []client.Object{
				namespaceCfgSynced,
			},
			wantEvent: managedNamespaceReconcileComplete,
		},
		{
			name:   "do not delete if unmanaged",
			actual: deployment(appsv1.RecreateDeploymentStrategyType),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RecreateDeploymentStrategyType),
			},
		},
		// There is no declared state, just an invalid annotation.
		{
			name:   "unmanage noop",
			actual: deployment(appsv1.RecreateDeploymentStrategyType, syncertest.ManagementInvalid),
			want: []client.Object{
				namespaceCfgSynced,
				deployment(appsv1.RecreateDeploymentStrategyType, syncertest.ManagementInvalid),
			},
		},
	}

	toSync := []schema.GroupVersionKind{kinds.Deployment()}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter, tc.declared))
			fakeEventRecorder := testingfake.NewEventRecorder(t)
			actual := []client.Object{namespaceCfg, managedNamespace}
			if tc.actual != nil {
				actual = append(actual, tc.actual)
			}
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(kinds.Namespace(), &corev1.Namespace{})
			s.AddKnownTypeWithName(kinds.Deployment(), &appsv1.Deployment{})
			fakeClient := testingfake.NewClient(t, s, actual...)

			testReconciler := syncerreconcile.NewNamespaceConfigReconciler(
				syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder, fakeDecoder, syncertest.Now, toSync, mgrInitTime)

			_, err := testReconciler.Reconcile(ctx,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: managedNamespace.Name,
					},
				})
			if err != nil {
				t.Errorf("unexpected reconciliation error: %v", err)
			}

			if tc.wantEvent != nil {
				fakeEventRecorder.Check(t, *tc.wantEvent)
			} else {
				fakeEventRecorder.Check(t)
			}
			want := append([]client.Object{managedNamespace}, tc.want...)
			fakeClient.Check(t, want...)
		})
	}
}

func TestUnmanagedNamespaceReconcile(t *testing.T) {
	testCases := []struct {
		name                   string
		actualNamespaceConfig  *v1.NamespaceConfig
		namespace              *corev1.Namespace
		wantNamespace          *corev1.Namespace
		updatedNamespaceConfig *v1.NamespaceConfig
		declared               client.Object
		actual                 client.Object
		wantEvent              *testingfake.Event
		want                   client.Object
	}{
		{
			name:                  "clean up unmanaged Namespace with namespaceconfig",
			actualNamespaceConfig: namespaceConfig("eng", v1.StateSynced, syncertest.NamespaceConfigImportToken(syncertest.Token), syncertest.NamespaceConfigSyncToken(), syncertest.ManagementDisabled),
			namespace:             namespace("eng", syncertest.ManagementEnabled),
			wantNamespace:         namespace("eng"),
		},
		{
			name:                  "do nothing to explicitly unmanaged resources",
			actualNamespaceConfig: namespaceConfig("eng", v1.StateSynced, syncertest.NamespaceConfigImportToken(syncertest.Token), syncertest.NamespaceConfigSyncToken(), syncertest.ManagementDisabled, core.Label("not", "synced")),
			namespace:             namespace("eng"),
			declared:              deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementDisabled, core.Label("also not", "synced")),
			actual:                deployment(appsv1.RecreateDeploymentStrategyType),
			wantNamespace:         namespace("eng"),
			want:                  deployment(appsv1.RecreateDeploymentStrategyType),
		},
		{
			name:                   "delete resources in unmanaged Namespace",
			actualNamespaceConfig:  namespaceConfig("eng", v1.StateSynced, syncertest.ManagementDisabled),
			namespace:              namespace("eng"),
			updatedNamespaceConfig: namespaceConfig("eng", v1.StateSynced, syncertest.ManagementDisabled),
			actual:                 deployment(appsv1.RollingUpdateDeploymentStrategyType, syncertest.ManagementEnabled, syncertest.TokenAnnotation),
			wantEvent: testingfake.NewEvent(namespaceConfig("eng", v1.StateSynced),
				corev1.EventTypeNormal, v1.EventReasonReconcileComplete),
			wantNamespace: namespace("eng"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			toSync := []schema.GroupVersionKind{kinds.Deployment()}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter))
			fakeEventRecorder := testingfake.NewEventRecorder(t)
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(kinds.Namespace(), &corev1.Namespace{})
			actual := []client.Object{tc.actualNamespaceConfig, tc.namespace}
			if tc.actual != nil {
				actual = append(actual, tc.actual)
			}
			fakeClient := testingfake.NewClient(t, s, actual...)

			testReconciler := syncerreconcile.NewNamespaceConfigReconciler(
				syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder, fakeDecoder, syncertest.Now, toSync, mgrInitTime)

			_, err := testReconciler.Reconcile(ctx,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: tc.namespace.Name,
					},
				})

			if err != nil {
				t.Errorf("unexpected reconciliation error: %v", err)
			}

			want := []client.Object{tc.wantNamespace}
			if tc.updatedNamespaceConfig != nil {
				want = append(want, tc.updatedNamespaceConfig)
			} else {
				want = append(want, tc.actualNamespaceConfig)
			}
			if tc.want != nil {
				want = append(want, tc.want)
			}
			fakeClient.Check(t, want...)

			if tc.wantEvent != nil {
				fakeEventRecorder.Check(t, *tc.wantEvent)
			} else {
				fakeEventRecorder.Check(t)
			}
		})
	}
}

func TestSpecialNamespaceReconcile(t *testing.T) {
	testCases := []struct {
		name          string
		declared      *v1.NamespaceConfig
		actual        *corev1.Namespace
		wantNamespace *corev1.Namespace
		want          *v1.NamespaceConfig
	}{
		{
			name: "do not add quota enforcement label on managed kube-system",
			declared: namespaceConfig(metav1.NamespaceSystem, v1.StateSynced, syncertest.NamespaceConfigImportToken(syncertest.Token),
				syncertest.NamespaceConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute)))),
			actual:        namespace(metav1.NamespaceSystem, syncertest.ManagementEnabled),
			wantNamespace: namespace(metav1.NamespaceSystem, syncertest.ManagementEnabled, syncertest.TokenAnnotation),
			want: namespaceConfig(metav1.NamespaceSystem, v1.StateSynced, syncertest.NamespaceConfigImportToken(syncertest.Token),
				syncertest.NamespaceConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute))),
				syncertest.NamespaceConfigSyncTime(), syncertest.NamespaceConfigSyncToken()),
		},
		{
			name:          "unmanage kube-system",
			declared:      namespaceConfig(metav1.NamespaceSystem, v1.StateSynced, syncertest.ManagementDisabled),
			actual:        namespace(metav1.NamespaceSystem, syncertest.ManagementEnabled),
			wantNamespace: namespace(metav1.NamespaceSystem),
			want:          namespaceConfig(metav1.NamespaceSystem, v1.StateSynced, syncertest.ManagementDisabled),
		},
		{
			name:          "unmanage default",
			declared:      namespaceConfig(metav1.NamespaceDefault, v1.StateSynced, syncertest.ManagementDisabled),
			actual:        namespace(metav1.NamespaceDefault, syncertest.ManagementEnabled),
			wantNamespace: namespace(metav1.NamespaceDefault),
			want:          namespaceConfig(metav1.NamespaceDefault, v1.StateSynced, syncertest.ManagementDisabled),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			toSync := []schema.GroupVersionKind{kinds.Deployment()}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter, nil))
			fakeEventRecorder := testingfake.NewEventRecorder(t)
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(kinds.Namespace(), &corev1.Namespace{})
			fakeClient := testingfake.NewClient(t, s, tc.declared, tc.actual)

			testReconciler := syncerreconcile.NewNamespaceConfigReconciler(
				syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder, fakeDecoder, syncertest.Now, toSync, mgrInitTime)

			_, err := testReconciler.Reconcile(ctx,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: tc.actual.Name,
					},
				})

			if err != nil {
				t.Errorf("unexpected reconciliation error: %v", err)
			}

			fakeEventRecorder.Check(t)
			fakeClient.Check(t, tc.want, tc.wantNamespace)
		})
	}
}

func TestNamespaceConfigReconcile(t *testing.T) {
	testCases := []struct {
		name string
		// What the namespace resource looks like on the cluster before the
		// reconcile. Set to nil if the namespace is not present.
		namespace       *corev1.Namespace
		namespaceConfig *v1.NamespaceConfig
		// The objects present in the corresponding namespace on the cluster.
		actual []client.Object
		// want is the objects on the API Server after reconciliation.
		want []client.Object
		// The events that are expected to be emitted as the result of the
		// operation.
		wantEvent *testingfake.Event
		// By default all tests only sync Deployments.
		// Specify this to override the resources being synced.
		toSyncOverride schema.GroupVersionKind
	}{
		{
			name: "default namespace is not deleted when namespace config is removed",
			namespace: namespace(metav1.NamespaceDefault, core.Annotations(
				map[string]string{
					metadata.ClusterNameAnnotationKey:           "cluster-name",
					metadata.LegacyClusterSelectorAnnotationKey: "some-selector",
					metadata.NamespaceSelectorAnnotationKey:     "some-selector",
					metadata.ResourceManagementKey:              metadata.ResourceManagementEnabled,
					metadata.SourcePathAnnotationKey:            "some-path",
					metadata.SyncTokenAnnotationKey:             "syncertest.Token",
					"some-user-annotation":                      "some-annotation-value",
				},
			),
				core.Labels(
					map[string]string{
						"some-user-label": "some-label-value",
					},
				),
			),
			namespaceConfig: namespaceConfig(metav1.NamespaceDefault,
				v1.StateSynced,
				syncertest.NamespaceConfigImportToken(syncertest.Token),
				syncertest.MarkForDeletion(),
			),
			actual: []client.Object{
				deployment(
					appsv1.RecreateDeploymentStrategyType,
					core.Namespace(metav1.NamespaceDefault),
					core.Name("my-deployment"),
					syncertest.ManagementEnabled),
				deployment(appsv1.RecreateDeploymentStrategyType,
					core.Namespace(metav1.NamespaceDefault),
					core.Name("your-deployment")),
			},
			want: []client.Object{
				namespace(metav1.NamespaceDefault,
					core.Annotation("some-user-annotation", "some-annotation-value"),
					core.Label("some-user-label", "some-label-value"),
				),
				deployment(appsv1.RecreateDeploymentStrategyType,
					core.Namespace(metav1.NamespaceDefault),
					core.Name("your-deployment")),
			},
			wantEvent: testingfake.NewEvent(namespaceConfig("", v1.StateUnknown),
				corev1.EventTypeNormal, v1.EventReasonReconcileComplete),
		},
		{
			name: "kube-system namespace is not deleted when namespace config is removed",
			namespace: namespace(metav1.NamespaceSystem, core.Annotations(
				map[string]string{
					metadata.ClusterNameAnnotationKey:           "cluster-name",
					metadata.LegacyClusterSelectorAnnotationKey: "some-selector",
					metadata.NamespaceSelectorAnnotationKey:     "some-selector",
					metadata.ResourceManagementKey:              metadata.ResourceManagementEnabled,
					metadata.SourcePathAnnotationKey:            "some-path",
					metadata.SyncTokenAnnotationKey:             "syncertest.Token",
					"some-user-annotation":                      "some-annotation-value",
				},
			),
				core.Labels(
					map[string]string{
						"some-user-label": "some-label-value",
					},
				),
			),
			namespaceConfig: namespaceConfig(metav1.NamespaceSystem,
				v1.StateSynced,
				syncertest.NamespaceConfigImportToken(syncertest.Token),
				syncertest.MarkForDeletion(),
			),
			actual: []client.Object{
				deployment(
					appsv1.RecreateDeploymentStrategyType,
					core.Namespace(metav1.NamespaceSystem),
					core.Name("my-deployment"),
					syncertest.ManagementEnabled),
				deployment(appsv1.RecreateDeploymentStrategyType,
					core.Namespace(metav1.NamespaceSystem),
					core.Name("your-deployment")),
			},
			want: []client.Object{
				namespace(metav1.NamespaceSystem,
					core.Annotation("some-user-annotation", "some-annotation-value"),
					core.Label("some-user-label", "some-label-value"),
				),
				deployment(appsv1.RecreateDeploymentStrategyType,
					core.Namespace(metav1.NamespaceSystem),
					core.Name("your-deployment")),
			},
			wantEvent: testingfake.NewEvent(namespaceConfig("", v1.StateUnknown),
				corev1.EventTypeNormal, v1.EventReasonReconcileComplete),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			toSync := []schema.GroupVersionKind{kinds.Deployment()}
			if !tc.toSyncOverride.Empty() {
				toSync = []schema.GroupVersionKind{tc.toSyncOverride}
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter, nil))
			fakeEventRecorder := testingfake.NewEventRecorder(t)
			actual := []client.Object{tc.namespaceConfig, tc.namespace}
			actual = append(actual, tc.actual...)
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(kinds.Namespace(), &corev1.Namespace{})
			fakeClient := testingfake.NewClient(t, s, actual...)

			testReconciler := syncerreconcile.NewNamespaceConfigReconciler(
				syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder, fakeDecoder, syncertest.Now, toSync, mgrInitTime)

			_, err := testReconciler.Reconcile(ctx,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: tc.namespace.Name,
					},
				})

			fakeClient.Check(t, tc.want...)
			if tc.wantEvent != nil {
				fakeEventRecorder.Check(t, *tc.wantEvent)
			} else {
				fakeEventRecorder.Check(t)
			}
			if err != nil {
				t.Errorf("unexpected reconciliation error: %v", err)
			}
		})
	}
}
