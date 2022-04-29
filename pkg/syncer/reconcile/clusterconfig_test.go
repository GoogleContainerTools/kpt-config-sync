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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	clusterReconcileComplete = testingfake.NewEvent(
		fake.ClusterConfigObject(), corev1.EventTypeNormal, v1.EventReasonReconcileComplete)
)

func clusterSyncError(err v1.ConfigManagementError) fake.ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		cc.Status.SyncErrors = append(cc.Status.SyncErrors, err)
	}
}

func clusterConfig(state v1.ConfigSyncState, opts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	result := fake.ClusterConfigObject(opts...)
	result.Status.SyncState = state
	return result
}

func persistentVolume(reclaimPolicy corev1.PersistentVolumeReclaimPolicy, opts ...core.MetaMutator) *corev1.PersistentVolume {
	result := fake.PersistentVolumeObject(opts...)
	result.Spec.PersistentVolumeReclaimPolicy = reclaimPolicy
	return result
}

var (
	mgrInitTime = syncertest.Now()
	clusterCfg  = clusterConfig(v1.StateSynced, syncertest.ClusterConfigImportToken(syncertest.Token),
		syncertest.ClusterConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute))))
	clusterCfgSynced = clusterConfig(v1.StateSynced, syncertest.ClusterConfigImportToken(syncertest.Token),
		syncertest.ClusterConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute))),
		syncertest.ClusterConfigSyncTime(), syncertest.ClusterConfigSyncToken())
)

func TestClusterConfigReconcile(t *testing.T) {
	testCases := []struct {
		name      string
		actual    client.Object
		declared  client.Object
		want      []client.Object
		wantEvent *testingfake.Event
	}{
		{
			name:     "create from declared state",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.TokenAnnotation,
					syncertest.ManagementEnabled),
			},
			wantEvent: clusterReconcileComplete,
		},
		{
			name:     "do not create if management disabled",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementDisabled),
			want: []client.Object{
				clusterCfgSynced,
			},
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not create if management invalid",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementInvalid),
			want: []client.Object{
				clusterCfgSynced,
			},
			wantEvent: testingfake.NewEvent(
				persistentVolume(corev1.PersistentVolumeReclaimRecycle), corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation),
		},
		{
			name:     "update to declared state",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle),
			actual:   persistentVolume(corev1.PersistentVolumeReclaimDelete, syncertest.ManagementEnabled),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			wantEvent: clusterReconcileComplete,
		},
		{
			name:     "update to declared state even if actual managed unset",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle),
			actual:   persistentVolume(corev1.PersistentVolumeReclaimDelete),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			wantEvent: clusterReconcileComplete,
		},
		// The declared state is fine, so overwrite the invalid one on the API Server.
		{
			name:     "update to declared state if actual managed invalid",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle),
			actual:   persistentVolume(corev1.PersistentVolumeReclaimDelete, syncertest.ManagementInvalid),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			wantEvent: clusterReconcileComplete,
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not update if declared management invalid",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementInvalid),
			actual:   persistentVolume(corev1.PersistentVolumeReclaimDelete),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimDelete),
			},
			wantEvent: testingfake.NewEvent(
				persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementInvalid, syncertest.TokenAnnotation),
				corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation),
		},
		{
			name:     "update to unmanaged",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementDisabled),
			actual:   persistentVolume(corev1.PersistentVolumeReclaimDelete, syncertest.ManagementEnabled),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimDelete),
			},
			wantEvent: clusterReconcileComplete,
		},
		{
			name:     "do not update if unmanaged",
			declared: persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementDisabled),
			actual:   persistentVolume(corev1.PersistentVolumeReclaimDelete),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimDelete),
			},
		},
		{
			name:   "delete if managed",
			actual: persistentVolume(corev1.PersistentVolumeReclaimDelete, syncertest.ManagementEnabled),
			want: []client.Object{
				clusterCfgSynced,
			},
			wantEvent: clusterReconcileComplete,
		},
		{
			name:   "do not delete if not declared",
			actual: persistentVolume(corev1.PersistentVolumeReclaimDelete),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimDelete),
			},
		},
		// There is no declared state, just an invalid annotation.
		{
			name:   "unmanage if invalid",
			actual: persistentVolume(corev1.PersistentVolumeReclaimDelete, syncertest.ManagementInvalid),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimDelete, syncertest.ManagementInvalid),
			},
		},
		{
			name: "resource with owner reference is ignored",
			actual: persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementEnabled,
				fake.OwnerReference(
					"some_operator_config_object",
					schema.GroupVersionKind{Group: "operator.config.group", Kind: "OperatorConfigObject", Version: "v1"}),
			),
			want: []client.Object{
				clusterCfgSynced,
				persistentVolume(corev1.PersistentVolumeReclaimRecycle, syncertest.ManagementEnabled,
					fake.OwnerReference(
						"some_operator_config_object",
						schema.GroupVersionKind{Group: "operator.config.group", Kind: "OperatorConfigObject", Version: "v1"}),
				),
			},
		},
	}

	toSync := []schema.GroupVersionKind{kinds.PersistentVolume()}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter, tc.declared))
			fakeEventRecorder := testingfake.NewEventRecorder(t)
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(kinds.PersistentVolume(), &corev1.PersistentVolume{})
			actual := []client.Object{clusterCfg}
			if tc.actual != nil {
				actual = append(actual, tc.actual)
			}
			fakeClient := testingfake.NewClient(t, s, actual...)

			testReconciler := syncerreconcile.NewClusterConfigReconciler(
				syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder, fakeDecoder, syncertest.Now, toSync, mgrInitTime)

			_, err := testReconciler.Reconcile(ctx,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: v1.ClusterConfigName,
					},
				})

			if tc.wantEvent != nil {
				fakeEventRecorder.Check(t, *tc.wantEvent)
			} else {
				fakeEventRecorder.Check(t)
			}
			fakeClient.Check(t, tc.want...)
			if err != nil {
				t.Errorf("unexpected reconciliation error: %v", err)
			}
		})
	}
}

func TestInvalidClusterConfig(t *testing.T) {
	testCases := []struct {
		name      string
		actual    *v1.ClusterConfig
		want      *v1.ClusterConfig
		wantEvent *testingfake.Event
	}{
		{
			name: "error on clusterconfig with invalid name",
			actual: clusterConfig(v1.StateSynced, fake.ClusterConfigMeta(core.Name("some-incorrect-name")),
				syncertest.ClusterConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute)))),
			want: clusterConfig(v1.StateError,
				fake.ClusterConfigMeta(core.Name("some-incorrect-name")),
				syncertest.ClusterConfigImportTime(metav1.NewTime(mgrInitTime.Add(time.Minute))),
				syncertest.ClusterConfigSyncTime(),
				clusterSyncError(v1.ConfigManagementError{
					ErrorResources: []v1.ErrorResource{
						{
							ResourceName: "some-incorrect-name",
							ResourceGVK:  v1.SchemeGroupVersion.WithKind(configmanagement.ClusterConfigKind),
						},
					},
					ErrorMessage: `ClusterConfig resource has invalid name "some-incorrect-name". To fix, delete the ClusterConfig.`,
				}),
			),
			wantEvent: testingfake.NewEvent(
				fake.ClusterConfigObject(fake.ClusterConfigMeta(core.Name("some-incorrect-name"))),
				corev1.EventTypeWarning,
				v1.EventReasonInvalidClusterConfig,
			),
		},
	}

	toSync := []schema.GroupVersionKind{kinds.PersistentVolume()}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter, nil))
			fakeEventRecorder := testingfake.NewEventRecorder(t)
			fakeClient := testingfake.NewClient(t, runtime.NewScheme(), tc.actual)
			testReconciler := syncerreconcile.NewClusterConfigReconciler(
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

			if tc.wantEvent != nil {
				fakeEventRecorder.Check(t, *tc.wantEvent)
			} else {
				fakeEventRecorder.Check(t)
			}
			fakeClient.Check(t, tc.want)
		})
	}
}
