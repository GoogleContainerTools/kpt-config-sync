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
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	configmanagementv1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
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
		fake.CRDClusterConfigObject(), corev1.EventTypeNormal, configmanagementv1.EventReasonReconcileComplete)

	crdUpdated = *testingfake.NewEvent(
		fake.CRDClusterConfigObject(), corev1.EventTypeNormal, configmanagementv1.EventReasonCRDChange)
)

func clusterConfig(state configmanagementv1.ConfigSyncState, opts ...fake.ClusterConfigMutator) *configmanagementv1.ClusterConfig {
	result := fake.ClusterConfigObject(opts...)
	result.Status.SyncState = state
	return result
}

func customResourceDefinitionV1Beta1(version string, opts ...core.MetaMutator) *apiextv1beta1.CustomResourceDefinition {
	result := fake.CustomResourceDefinitionV1Beta1Object(opts...)
	result.Spec.Version = version
	result.Spec.Versions = []apiextv1beta1.CustomResourceDefinitionVersion{{Name: version}}
	return result
}

func crdListV1beta1(gvks []schema.GroupVersionKind) []apiextv1beta1.CustomResourceDefinition {
	crdSpecs := map[schema.GroupKind][]string{}
	for _, gvk := range gvks {
		gk := gvk.GroupKind()
		crdSpecs[gk] = append(crdSpecs[gk], gvk.Version)
	}
	var crdList []apiextv1beta1.CustomResourceDefinition
	for gk, vers := range crdSpecs {
		crd := apiextv1beta1.CustomResourceDefinition{}
		crd.SetGroupVersionKind(kinds.CustomResourceDefinitionV1Beta1())
		crd.Name = strings.ToLower(gk.Kind + "s." + gk.Group)
		crd.Spec.Group = gk.Group
		crd.Spec.Names.Kind = gk.Kind
		for _, ver := range vers {
			crd.Spec.Versions = append(crd.Spec.Versions, apiextv1beta1.CustomResourceDefinitionVersion{
				Name: ver,
			})
		}
		crdList = append(crdList, crd)
	}
	return crdList
}

func crdListV1(gvks []schema.GroupVersionKind) []apiextv1.CustomResourceDefinition {
	crdSpecs := map[schema.GroupKind][]string{}
	for _, gvk := range gvks {
		gk := gvk.GroupKind()
		crdSpecs[gk] = append(crdSpecs[gk], gvk.Version)
	}
	var crdList []apiextv1.CustomResourceDefinition
	for gk, vers := range crdSpecs {
		crd := apiextv1.CustomResourceDefinition{}
		crd.SetGroupVersionKind(kinds.CustomResourceDefinitionV1())
		crd.Name = strings.ToLower(gk.Kind + "s." + gk.Group)
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

func withResourceVersion(obj *configmanagementv1.ClusterConfig, rv string) *configmanagementv1.ClusterConfig {
	obj = obj.DeepCopy()
	obj.ResourceVersion = rv
	return obj
}

func preserveUnknownFields(t *testing.T, preserved bool) core.MetaMutator {
	return func(o client.Object) {
		switch crd := o.(type) {
		case *apiextv1beta1.CustomResourceDefinition:
			crd.Spec.PreserveUnknownFields = &preserved
		case *apiextv1.CustomResourceDefinition:
			crd.Spec.PreserveUnknownFields = preserved
		default:
			t.Fatalf("not a v1beta1.CRD or v1.CRD: %T", o)
		}
	}
}

var (
	clusterCfg = clusterConfig(configmanagementv1.StateSynced,
		syncertest.ClusterConfigImportToken(syncertest.Token),
		syncertest.ClusterConfigImportTime(metav1.NewTime(syncertest.Now().Add(time.Minute))),
		fake.ClusterConfigMeta(core.Name(configmanagementv1.CRDClusterConfigName)),
	)

	clusterCfgSynced = clusterConfig(configmanagementv1.StateSynced,
		syncertest.ClusterConfigImportToken(syncertest.Token),
		syncertest.ClusterConfigImportTime(metav1.NewTime(syncertest.Now().Add(time.Minute))),
		fake.ClusterConfigMeta(core.Name(configmanagementv1.CRDClusterConfigName)),
		syncertest.ClusterConfigSyncTime(),
		syncertest.ClusterConfigSyncToken(),
	)
)

type crdTestCase struct {
	name     string
	actual   client.Object
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
	s := runtime.NewScheme()
	if err := apiextensions.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := apiextv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := apiextv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := configmanagementv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	testCases := []crdTestCase{
		{
			name:     "create from declared state",
			declared: customResourceDefinitionV1Beta1(v1Version),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1Version, core.ResourceVersion("1"),
					syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name:     "do not create if management disabled",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementDisabled),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
			},
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not create if management invalid",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementInvalid),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
			},
			expectEvents: []testingfake.Event{
				*testingfake.NewEvent(fake.CustomResourceDefinitionV1Beta1Object(), corev1.EventTypeWarning, configmanagementv1.EventReasonInvalidAnnotation),
			},
		},
		{
			name:     "update to declared state",
			declared: customResourceDefinitionV1Beta1(v1Version),
			actual:   customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementEnabled),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1Version, core.ResourceVersion("2"),
					syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name:     "update to declared state even if actual managed unset",
			declared: customResourceDefinitionV1Beta1(v1Version),
			actual:   customResourceDefinitionV1Beta1(v1beta1Version),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1Version, core.ResourceVersion("2"),
					syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		// The declared state is fine, so overwrite the invalid one on the API Server.
		{
			name:     "update to declared state if actual managed invalid",
			declared: customResourceDefinitionV1Beta1(v1Version),
			actual:   customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementInvalid),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1Version, core.ResourceVersion("2"),
					syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		// The declared state is invalid, so take no action.
		{
			name:     "do not update if declared management invalid",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementInvalid),
			actual:   customResourceDefinitionV1Beta1(v1beta1Version),
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1beta1Version, core.ResourceVersion("1")),
			},
			expectEvents: []testingfake.Event{
				*testingfake.NewEvent(fake.CustomResourceDefinitionV1Beta1Object(), corev1.EventTypeWarning, configmanagementv1.EventReasonInvalidAnnotation),
			},
		},
		{
			name:     "update to unmanaged",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementDisabled),
			actual:   customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementEnabled),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1beta1Version, core.ResourceVersion("2"),
					preserveUnknownFields(t, false)),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name:     "do not update if unmanaged",
			declared: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementDisabled),
			actual:   customResourceDefinitionV1Beta1(v1beta1Version),
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1beta1Version, core.ResourceVersion("1")),
			},
		},
		{
			name:   "delete if managed",
			actual: customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementEnabled),
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete},
			expectRestart: true,
		},
		{
			name:   "do not delete if unmanaged",
			actual: customResourceDefinitionV1Beta1(v1beta1Version),
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1beta1Version, core.ResourceVersion("1")),
			},
		},
		// There is no declared state, just an invalid annotation.
		{
			name:   "unmanage noop",
			actual: customResourceDefinitionV1Beta1(v1beta1Version, syncertest.ManagementInvalid),
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1beta1Version, Kind: ""},
			},
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1beta1Version, core.ResourceVersion("1"),
					syncertest.ManagementInvalid),
			},
		},
		{
			name: "resource with owner reference is ignored",
			actual: customResourceDefinitionV1Beta1(v1Version, syncertest.ManagementEnabled,
				fake.OwnerReference(
					"some_operator_config_object",
					schema.GroupVersionKind{Group: "operator.config.group", Kind: "OperatorConfigObject", Version: v1Version}),
			),
			initialCrds: []schema.GroupVersionKind{
				{Group: "", Version: v1Version, Kind: ""},
			},
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1Version, core.ResourceVersion("1"),
					syncertest.ManagementEnabled,
					fake.OwnerReference(
						"some_operator_config_object",
						schema.GroupVersionKind{Group: "operator.config.group", Kind: "OperatorConfigObject", Version: v1Version}),
				),
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
				withResourceVersion(clusterCfgSynced, "2"),
				customResourceDefinitionV1Beta1(v1Version, core.ResourceVersion("1"),
					syncertest.TokenAnnotation, syncertest.ManagementEnabled),
			},
			expectEvents:  []testingfake.Event{clusterReconcileComplete, crdUpdated},
			expectRestart: true,
		},
		{
			name: "external crd change triggers restart",
			want: []client.Object{
				withResourceVersion(clusterCfgSynced, "2"),
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
				withResourceVersion(clusterCfgSynced, "2"),
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
		t.Run(tc.name+" v1beta1", tc.runWith(s, false))
		t.Run(tc.name+" v1", tc.runWith(s, true))
	}
}

func (tc crdTestCase) runWith(s *runtime.Scheme, convertToV1 bool) func(t *testing.T) {
	return func(t *testing.T) {
		tc.run(t, s, convertToV1)
	}
}

func (tc crdTestCase) run(t *testing.T, s *runtime.Scheme, convertToV1 bool) {
	declared := tc.declared
	if declared != nil && convertToV1 {
		declared = convertCRDV1beta1ToV1(t, s, declared)
	}
	fakeDecoder := testingfake.NewDecoder(syncertest.ToUnstructuredList(t, syncertest.Converter, declared))
	fakeEventRecorder := testingfake.NewEventRecorder(t)
	fakeSignal := RestartSignalRecorder{}
	actual := []client.Object{clusterCfg.DeepCopy()}
	if tc.actual != nil {
		if convertToV1 {
			actual = append(actual, convertCRDV1beta1ToV1(t, s, tc.actual))
		} else {
			actual = append(actual, tc.actual)
		}
	}
	// Convert tc.listCrds to CRD objects and add to the actual objects list
	if tc.listCrds != nil {
		if convertToV1 {
			for _, crd := range crdListV1(tc.listCrds) {
				actual = append(actual, crd.DeepCopy())
			}
		} else {
			for _, crd := range crdListV1beta1(tc.listCrds) {
				actual = append(actual, crd.DeepCopy())
			}
		}
	}

	fakeClient := testingfake.NewClient(t, s, actual...)
	fakeClient.Now = syncertest.Now

	testReconciler := newReconciler(syncerclient.New(fakeClient, metrics.APICallDuration), fakeClient.Applier(), fakeClient, fakeEventRecorder,
		fakeDecoder, syncertest.Now, &fakeSignal)

	if convertToV1 {
		testReconciler.allCrds = testReconciler.toCrdSet(crdListV1(tc.initialCrds))
	} else {
		testReconciler.allCrds = toCrdSetFromV1beta1(crdListV1beta1(tc.initialCrds))
	}

	_, err := testReconciler.Reconcile(context.Background(),
		runtimereconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: configmanagementv1.CRDClusterConfigName,
			},
		})

	if tc.expectRestart {
		fakeSignal.Check(t, restartSignal)
	} else {
		fakeSignal.Check(t)
	}
	expectEvents := tc.expectEvents
	if convertToV1 {
		for i, event := range expectEvents {
			if event.GroupVersionKind == kinds.CustomResourceDefinitionV1Beta1() {
				expectEvents[i].GroupVersionKind = kinds.CustomResourceDefinitionV1()
			}
		}
	}
	fakeEventRecorder.Check(t, expectEvents...)

	want := tc.want
	if convertToV1 {
		for i, o := range want {
			if o.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1Beta1() {
				want[i] = convertCRDV1beta1ToV1(t, s, o)
			}
		}
	}
	// Convert tc.listCrds to CRD objects and add to the expected objects list
	if tc.listCrds != nil {
		if convertToV1 {
			for _, crd := range crdListV1(tc.listCrds) {
				crd.SetResourceVersion("1")
				want = append(want, crd.DeepCopy())
			}
		} else {
			for _, crd := range crdListV1beta1(tc.listCrds) {
				crd.SetResourceVersion("1")
				want = append(want, crd.DeepCopy())
			}
		}
	}
	fakeClient.Check(t, want...)
	if err != nil {
		t.Errorf("unexpected reconciliation error: %v", err)
	}
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
	t.Helper()
	if diff := cmp.Diff(want, r.Restarts); diff != "" {
		t.Errorf("Diff in calls to fake.RestartSignalRecorder.Restart(): %s", diff)
	}
}

func convertCRDV1beta1ToV1(t *testing.T, s *runtime.Scheme, obj client.Object) client.Object {
	targetGVK := kinds.CustomResourceDefinitionV1()
	// Version conversion goes through the unversioned internal type first.
	// This avoids all versions needing to know how to convert to all other versions.
	untypedObj, err := s.New(targetGVK.GroupKind().WithVersion(runtime.APIVersionInternal))
	if err != nil {
		t.Fatal(err)
	}
	err = s.Convert(obj, untypedObj, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Convert to the desired typed object
	tObj, err := s.New(targetGVK)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Convert(untypedObj, tObj, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Set GVK for the fake decoder, which assumes it's present
	tObj.GetObjectKind().SetGroupVersionKind(targetGVK)
	return tObj.(client.Object)
}

func toCrdSetFromV1beta1(crds []apiextv1beta1.CustomResourceDefinition) map[schema.GroupVersionKind]struct{} {
	allCRDs := map[schema.GroupVersionKind]struct{}{}
	for _, crd := range crds {
		crdGk := schema.GroupKind{
			Group: crd.Spec.Group,
			Kind:  crd.Spec.Names.Kind,
		}

		for _, ver := range crd.Spec.Versions {
			allCRDs[crdGk.WithVersion(ver.Name)] = struct{}{}
		}
	}
	if len(allCRDs) == 0 {
		return nil
	}
	return allCRDs
}
