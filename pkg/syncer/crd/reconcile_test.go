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

package crd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"kpt.dev/configsync/pkg/syncer/sync"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimereconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	v1Version      = "v1"
	v1beta1Version = "v1beta1"
)

var (
	clusterReconcileComplete = *testingfake.NewEvent(
		fake.CRDClusterConfigObject(), corev1.EventTypeNormal, v1.EventReasonReconcileComplete)

	crdUpdated = *testingfake.NewEvent(
		fake.CRDClusterConfigObject(), corev1.EventTypeNormal, v1.EventReasonCRDChange)
)

func clusterConfig(state v1.ConfigSyncState, opts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	result := fake.ClusterConfigObject(opts...)
	result.Status.SyncState = state
	return result
}

func customResourceDefinitionV1Beta1(version string, opts ...core.MetaMutator) *v1beta1.CustomResourceDefinition {
	result := fake.CustomResourceDefinitionV1Beta1Object(opts...)
	result.Spec.Version = version
	result.Spec.Versions = []v1beta1.CustomResourceDefinitionVersion{{Name: version}}
	return result
}

func v1CRDList(gvks []schema.GroupVersionKind) []apiextv1.CustomResourceDefinition {
	crdSpecs := map[schema.GroupKind][]string{}
	for _, gvk := range gvks {
		gk := gvk.GroupKind()
		crdSpecs[gk] = append(crdSpecs[gk], gvk.Version)
	}
	var crdList []apiextv1.CustomResourceDefinition
	for gk, vers := range crdSpecs {
		crd := apiextv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kinds.CustomResourceDefinitionV1().GroupVersion().String(),
				Kind:       kinds.CustomResourceDefinitionV1().Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.ToLower(gk.Kind + "s." + gk.Group),
			},
		}
		crd.Spec.Group = gk.Group
		crd.Spec.Names.Kind = gk.Kind
		for _, ver := range vers {
			crd.Spec.Versions = append(crd.Spec.Versions, apiextv1.CustomResourceDefinitionVersion{
				Name: ver,
			})
		}
		crdList = append(crdList, crd)
	}
	return crdList
}

func v1beta1CRDList(gvks []schema.GroupVersionKind) []apiextv1beta1.CustomResourceDefinition {
	crdSpecs := map[schema.GroupKind][]string{}
	for _, gvk := range gvks {
		gk := gvk.GroupKind()
		crdSpecs[gk] = append(crdSpecs[gk], gvk.Version)
	}
	var crdList []apiextv1beta1.CustomResourceDefinition
	for gk, vers := range crdSpecs {
		crd := apiextv1beta1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kinds.CustomResourceDefinitionV1Beta1().GroupVersion().String(),
				Kind:       kinds.CustomResourceDefinitionV1Beta1().Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.ToLower(gk.Kind + "s." + gk.Group),
			},
		}
		crd.Spec.Group = gk.Group
		crd.Spec.Names.Kind = gk.Kind
		crd.Spec.Version = gvks[0].Version
		for _, ver := range vers {
			crd.Spec.Versions = append(crd.Spec.Versions, apiextv1beta1.CustomResourceDefinitionVersion{
				Name: ver,
			})
		}
		// crd.Spec.PreserveUnknownFields = pointer.Bool(true)
		crdList = append(crdList, crd)
	}
	return crdList
}

func clusterCfgSynced(moreOpts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	opts := []fake.ClusterConfigMutator{
		syncertest.ClusterConfigImportToken(syncertest.Token),
		syncertest.ClusterConfigImportTime(metav1.NewTime(syncertest.Now().Add(time.Minute))),
		fake.ClusterConfigMeta(core.Name(v1.CRDClusterConfigName)),
		syncertest.ClusterConfigSyncTime(),
		syncertest.ClusterConfigSyncToken(),
	}
	opts = append(opts, moreOpts...)
	return clusterConfig(v1.StateSynced, opts...)
}

func clusterConfigDefaults(scheme *runtime.Scheme) fake.ClusterConfigMutator {
	return func(o *v1.ClusterConfig) {
		scheme.Default(o)
	}
}

var (
	clusterCfg = clusterConfig(v1.StateSynced,
		syncertest.ClusterConfigImportToken(syncertest.Token),
		syncertest.ClusterConfigImportTime(metav1.NewTime(syncertest.Now().Add(time.Minute))),
		fake.ClusterConfigMeta(core.Name(v1.CRDClusterConfigName)),
	)
)

type crdTestCase struct {
	name     string
	actual   []client.Object
	declared client.Object
	// initialCrds is the list of CRDs on the reconciler at start
	initialCrds []schema.GroupVersionKind
	// listCrds if the list of CRDs on the API Server
	listCrds      []schema.GroupVersionKind
	want          []client.Object
	expectEvents  []testingfake.Event
	expectRestart bool
}

func TestClusterConfigReconcile(t *testing.T) {
	scheme := core.Scheme

	testCases := []crdTestCase{
		{
			name:     "create from declared state",
			declared: customResourceDefinitionV1Beta1(v1Version),
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1Version,
					syncertest.TokenAnnotation,
					syncertest.ManagementEnabled,
				),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name:     "do not create if management disabled",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementDisabled),
			want: []client.Object{
				clusterCfgSynced(),
			},
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not create if management invalid",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementInvalid),
			want: []client.Object{
				clusterCfgSynced(),
			},
			expectEvents: []testingfake.Event{
				*testingfake.NewEvent(fake.CustomResourceDefinitionV1Beta1Object(), corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation),
			},
		},
		{
			name:     "update to declared state",
			declared: customResourceDefinitionV1Beta1(v1Version),
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementEnabled),
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1Version,
					syncertest.TokenAnnotation,
					syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name:     "update to declared state even if actual managed unset",
			declared: customResourceDefinitionV1Beta1(v1Version),
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1Version,
					syncertest.TokenAnnotation,
					syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		// The declared state is fine, so overwrite the invalid one on the API Server.
		{
			name:     "update to declared state if actual managed invalid",
			declared: customResourceDefinitionV1Beta1(v1Version),
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementInvalid),
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1Version,
					syncertest.TokenAnnotation,
					syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not update if declared management invalid",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementInvalid),
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
			expectEvents: []testingfake.Event{
				*testingfake.NewEvent(fake.CustomResourceDefinitionV1Beta1Object(), corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation),
			},
		},
		{
			name:     "update to unmanaged",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementDisabled),
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementEnabled),
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name:     "do not update if unmanaged",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementDisabled),
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
		},
		{
			name: "delete if managed",
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementEnabled),
			},
			want: []client.Object{
				clusterCfgSynced(),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete},
			expectRestart: true,
		},
		{
			name: "do not delete if unmanaged",
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1beta1Version),
			},
		},
		// There is no declared state, just an invalid annotation.
		{
			name: "unmanage noop",
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementInvalid),
			},
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1beta1Version,
					syncertest.ManagementInvalid),
			},
		},
		{
			name: "resource with owner reference is ignored",
			actual: []client.Object{
				customResourceDefinitionV1Beta1(v1Version,
					syncertest.ManagementEnabled,
					fake.OwnerReference(
						"some_operator_config_object",
						schema.GroupVersionKind{Group: "operator.config.group", Kind: "OperatorConfigObject", Version: v1Version})),
			},
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1Version, Kind: ""},
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementEnabled,
					fake.OwnerReference(
						"some_operator_config_object",
						schema.GroupVersionKind{Group: "operator.config.group", Kind: "OperatorConfigObject", Version: v1Version})),
			},
		},
		{
			name:     "create from declared state and external crd change",
			declared: customResourceDefinitionV1Beta1(v1Version),
			initialCrds: []schema.GroupVersionKind{
				{Group: "foo.xyz", Version: "v1", Kind: "Stuff"},
			},
			listCrds: []schema.GroupVersionKind{
				{Group: "foo.xyz", Version: "v1", Kind: "Stuff"},
				{Group: "bar.xyz", Version: "v1", Kind: "MoreStuff"},
			},
			want: []client.Object{
				clusterCfgSynced(),
				customResourceDefinitionV1Beta1(v1Version,
					syncertest.TokenAnnotation,
					syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name: "external crd change triggers restart",
			want: []client.Object{
				clusterCfgSynced(),
			},
			expectEvents: []testingfake.Event{crdUpdated},
			initialCrds: []schema.GroupVersionKind{
				{Group: "foo.xyz", Version: "v1", Kind: "Stuff"},
			},
			listCrds: []schema.GroupVersionKind{
				{Group: "foo.xyz", Version: "v1", Kind: "Stuff"},
				{Group: "bar.xyz", Version: "v1", Kind: "MoreStuff"},
			},
			expectRestart: true,
		},
		{
			name: "no change",
			want: []client.Object{
				clusterCfgSynced(clusterConfigDefaults(scheme)),
			},
			initialCrds: []schema.GroupVersionKind{
				{Group: "foo.xyz", Version: "v1", Kind: "Stuff"},
			},
			listCrds: []schema.GroupVersionKind{
				{Group: "foo.xyz", Version: "v1", Kind: "Stuff"},
			},
		},
	}

	for _, tc := range testCases {
		tc.actual = append(tc.actual, clusterCfg.DeepCopy())

		// Add listCrds to the actual and expected objects.
		for _, crd := range v1beta1CRDList(tc.listCrds) {
			tc.actual = append(tc.actual, crd.DeepCopy())
			tc.want = append(tc.want, crd.DeepCopy())
		}
		// Copy the wanted objects, so we can set type-specific defaults
		wantCopy := deepCopyList(tc.want)
		// Set version-specific defaults (simulate apiserver/fakeClient.Create)
		for i := range tc.want {
			scheme.Default(tc.want[i])
		}

		t.Run(tc.name+" v1beta1", tc.runWith(scheme))

		// Reset wanted objects to before defaults were set
		tc.want = wantCopy

		// Convert the test case's v1beta1 CRDs to v1 CRDs
		if tc.declared != nil {
			tObj, err := kinds.ToTypedWithVersion(tc.declared, kinds.CustomResourceDefinitionV1(), scheme)
			if err != nil {
				t.Fatal(err)
			}
			tc.declared = tObj.(client.Object)
		}
		for i, o := range tc.actual {
			if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1Beta1() {
				tObj, err := kinds.ToTypedWithVersion(o, kinds.CustomResourceDefinitionV1(), scheme)
				if err != nil {
					t.Fatal(err)
				}
				tc.actual[i] = tObj.(client.Object)
			}
		}
		for i, o := range tc.want {
			if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1Beta1() {
				tObj, err := kinds.ToTypedWithVersion(o, kinds.CustomResourceDefinitionV1(), scheme)
				if err != nil {
					t.Fatal(err)
				}
				tc.want[i] = tObj.(client.Object)
			}
		}
		for i, event := range tc.expectEvents {
			if event.GroupVersionKind == kinds.CustomResourceDefinitionV1Beta1() {
				// Only change event object type if it is a v1beta1.CRD.
				tc.expectEvents[i].GroupVersionKind = kinds.CustomResourceDefinitionV1()
			}
		}
		// Set version-specific defaults (simulate apiserver/fakeClient.Create)
		for i := range tc.want {
			scheme.Default(tc.want[i])
		}

		t.Run(tc.name+" v1", tc.runWith(scheme))
	}
}

func (tc crdTestCase) runWith(s *runtime.Scheme) func(t *testing.T) {
	return func(t *testing.T) {
		tc.run(t, s)
	}
}

func (tc crdTestCase) run(t *testing.T, scheme *runtime.Scheme) {
	fakeDecoder := testingfake.NewDecoder(syncertest.ObjectToUnstructuredList(t, scheme, tc.declared))
	fakeEventRecorder := testingfake.NewEventRecorder(t)
	fakeSignal := RestartSignalRecorder{}

	fakeClient := testingfake.NewClient(t, scheme, deepCopyList(tc.actual)...)

	testReconciler := newReconciler(syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder,
		fakeDecoder, syncertest.Now, &fakeSignal)
	testReconciler.allCrds = testReconciler.toCrdSet(v1CRDList(tc.initialCrds))

	_, err := testReconciler.Reconcile(context.Background(),
		runtimereconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: v1.CRDClusterConfigName,
			},
		})

	if tc.expectRestart {
		fakeSignal.Check(t, restartSignal)
	} else {
		fakeSignal.Check(t)
	}
	fakeEventRecorder.Check(t, tc.expectEvents...)

	fakeClient.Check(t, tc.want...)
	if err != nil {
		t.Errorf("unexpected reconciliation error: %v", err)
	}
}

func deepCopyList(objs []client.Object) []client.Object {
	copy := make([]client.Object, len(objs))
	for i, o := range objs {
		copy[i] = o.DeepCopyObject().(client.Object)
	}
	return copy
}

// RestartSignalRecorder implements a fake sync.RestartSignal.
type RestartSignalRecorder struct {
	Restarts []string
}

var _ sync.RestartSignal = &RestartSignalRecorder{}

// Restart implements RestartSignal.
func (r *RestartSignalRecorder) Restart(signal string) {
	r.Restarts = append(r.Restarts, signal)
}

// Check ensures that the RestartSignal was called exactly with the passed
// sequence of signals.
func (r *RestartSignalRecorder) Check(t *testing.T, want ...string) {
	if diff := cmp.Diff(want, r.Restarts); diff != "" {
		t.Errorf("Diff in calls to fake.RestartSignalRecorder.Restart(): %s", diff)
	}
}
