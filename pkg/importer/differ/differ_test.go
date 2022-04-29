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

package differ

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var testTime = metav1.NewTime(time.Unix(1234, 5678)).Rfc3339Copy()

func namespaceConfig(name string, opts ...core.MetaMutator) *v1.NamespaceConfig {
	opts = append(opts, core.Name(name))
	nc := fake.NamespaceConfigObject(opts...)
	nc.Spec.ImportTime = metav1.NewTime(testTime.Add(time.Minute)).Rfc3339Copy()
	return nc
}

func stalledNamespaceConfig(name string, syncState v1.ConfigSyncState, opts ...core.MetaMutator) *v1.NamespaceConfig {
	opts = append(opts, core.Name(name))
	nc := fake.NamespaceConfigObject(opts...)
	nc.Spec.ImportTime = metav1.NewTime(testTime.Add(-time.Minute)).Rfc3339Copy()
	nc.Status.SyncState = syncState
	return nc
}

func clusterConfig(opts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	cc := fake.ClusterConfigObject(opts...)
	cc.Spec.ImportTime = metav1.NewTime(testTime.Add(time.Minute)).Rfc3339Copy()
	return cc
}

func stalledClusterConfig(syncState v1.ConfigSyncState, opts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	cc := fake.ClusterConfigObject(opts...)
	cc.Spec.ImportTime = metav1.NewTime(testTime.Add(-time.Minute)).Rfc3339Copy()
	cc.Status.SyncState = syncState
	return cc
}

func crdClusterConfig(opts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	ccc := fake.CRDClusterConfigObject(opts...)
	ccc.Spec.ImportTime = metav1.NewTime(testTime.Add(time.Minute)).Rfc3339Copy()
	return ccc
}

func stalledCRDClusterConfig(syncState v1.ConfigSyncState, opts ...fake.ClusterConfigMutator) *v1.ClusterConfig {
	ccc := fake.CRDClusterConfigObject(opts...)
	ccc.Spec.ImportTime = metav1.NewTime(testTime.Add(-time.Minute)).Rfc3339Copy()
	ccc.Status.SyncState = syncState
	return ccc
}

func markedForDeletion(o client.Object) {
	nc := o.(*v1.NamespaceConfig)
	nc.Spec.DeleteSyncedTime = testTime
}

// allConfigs constructs a (potentially-invalid) AllConfigs for the purpose
// of Differ tests.
//
// Not intended for use with other tests - this is to make differ tests easy to
// specify, not replicate code that creates AllConfigs.
func allConfigs(t *testing.T, objs []client.Object) *namespaceconfig.AllConfigs {
	t.Helper()

	result := &namespaceconfig.AllConfigs{
		NamespaceConfigs: make(map[string]v1.NamespaceConfig),
		Syncs:            make(map[string]v1.Sync),
	}
	for _, o := range objs {
		switch obj := o.(type) {
		case *v1.NamespaceConfig:
			result.NamespaceConfigs[obj.Name] = *obj
		case *v1.Sync:
			result.Syncs[obj.Name] = *obj
		case *v1.ClusterConfig:
			switch obj.Name {
			case v1.ClusterConfigName:
				result.ClusterConfig = obj
			case v1.CRDClusterConfigName:
				result.CRDClusterConfig = obj
			default:
				t.Fatalf("unsupported ClusterConfig name %q", obj.Name)
			}
		default:
			t.Fatalf("unsupported AllConfigs type: %T", o)
		}
	}

	return result
}

func TestDiffer(t *testing.T) {
	// Mock out metav1.Now for testing.
	now = func() metav1.Time {
		return testTime
	}

	tcs := []struct {
		testName string
		actual   []client.Object
		declared []client.Object
		want     []client.Object
	}{
		// NamespaceConfig tests
		{
			testName: "create Namespace node",
			declared: []client.Object{namespaceConfig("foo")},
			want:     []client.Object{namespaceConfig("foo")},
		},
		{
			testName: "no-op Namespace node",
			actual:   []client.Object{namespaceConfig("foo")},
			declared: []client.Object{namespaceConfig("foo")},
			want:     []client.Object{namespaceConfig("foo")},
		},
		{
			testName: "update stalled Namespace nodes",
			actual: []client.Object{
				stalledNamespaceConfig("foo", v1.StateSynced),
				stalledNamespaceConfig("bar", v1.StateSynced),
			},
			declared: []client.Object{
				stalledNamespaceConfig("foo", v1.StateUnknown),
				stalledNamespaceConfig("bar", v1.StateStale),
			},
			want: []client.Object{
				stalledNamespaceConfig("foo", v1.StateSynced),
				stalledNamespaceConfig("bar", v1.StateStale),
			},
		},
		{
			testName: "update Namespace node",
			actual: []client.Object{namespaceConfig("foo",
				core.Annotation("key", "old"))},
			declared: []client.Object{namespaceConfig("foo",
				core.Annotation("key", "new"))},
			want: []client.Object{namespaceConfig("foo",
				core.Annotation("key", "new"))},
		},
		{
			testName: "delete Namespace node",
			actual:   []client.Object{namespaceConfig("foo")},
			want:     []client.Object{namespaceConfig("foo", markedForDeletion)},
		},
		{
			testName: "replace one Namespace node",
			actual:   []client.Object{namespaceConfig("foo")},
			declared: []client.Object{namespaceConfig("bar")},
			want: []client.Object{
				namespaceConfig("foo", markedForDeletion),
				namespaceConfig("bar"),
			},
		},
		{
			testName: "create two Namespace nodes",
			declared: []client.Object{
				namespaceConfig("foo"),
				namespaceConfig("bar"),
			},
			want: []client.Object{
				namespaceConfig("foo"),
				namespaceConfig("bar"),
			},
		},
		{
			testName: "keep one, create two, delete two Namespace nodes",
			actual: []client.Object{
				namespaceConfig("alp"),
				namespaceConfig("foo"),
				namespaceConfig("bar"),
			},
			declared: []client.Object{
				namespaceConfig("alp"),
				namespaceConfig("qux"),
				namespaceConfig("pim"),
			},
			want: []client.Object{
				namespaceConfig("alp"),
				namespaceConfig("foo", markedForDeletion),
				namespaceConfig("bar", markedForDeletion),
				namespaceConfig("qux"),
				namespaceConfig("pim"),
			},
		},
		// ClusterConfig tests
		{
			testName: "create ClusterConfig",
			declared: []client.Object{clusterConfig()},
			want:     []client.Object{clusterConfig()},
		},
		{
			testName: "no-op ClusterConfig",
			actual:   []client.Object{clusterConfig()},
			declared: []client.Object{clusterConfig()},
			want:     []client.Object{clusterConfig()},
		},
		{
			testName: "update stalled ClusterConfig to its original state",
			actual:   []client.Object{stalledClusterConfig(v1.StateSynced)},
			declared: []client.Object{stalledClusterConfig(v1.StateUnknown)},
			want:     []client.Object{stalledClusterConfig(v1.StateSynced)},
		},
		{
			testName: "update stalled ClusterConfig to the desired state",
			actual:   []client.Object{stalledClusterConfig(v1.StateSynced)},
			declared: []client.Object{stalledClusterConfig(v1.StateStale)},
			want:     []client.Object{stalledClusterConfig(v1.StateStale)},
		},
		{
			testName: "delete ClusterConfig",
			actual:   []client.Object{clusterConfig()},
		},
		{
			testName: "create CRD ClusterConfig",
			declared: []client.Object{crdClusterConfig()},
			want:     []client.Object{crdClusterConfig()},
		},
		{
			testName: "no-op CRD ClusterConfig",
			actual:   []client.Object{crdClusterConfig()},
			declared: []client.Object{crdClusterConfig()},
			want:     []client.Object{crdClusterConfig()},
		},
		{
			testName: "update stalled CRD ClusterConfig to its original state",
			actual:   []client.Object{stalledCRDClusterConfig(v1.StateSynced)},
			declared: []client.Object{stalledCRDClusterConfig(v1.StateUnknown)},
			want:     []client.Object{stalledCRDClusterConfig(v1.StateSynced)},
		},
		{
			testName: "update stalled CRD ClusterConfig to the desired state",
			actual:   []client.Object{stalledCRDClusterConfig(v1.StateSynced)},
			declared: []client.Object{stalledCRDClusterConfig(v1.StateStale)},
			want:     []client.Object{stalledCRDClusterConfig(v1.StateStale)},
		},
		{
			testName: "delete CRD ClusterConfig",
			actual:   []client.Object{crdClusterConfig()},
		},
		// Sync tests
		{
			testName: "create Sync",
			declared: []client.Object{fake.SyncObject(kinds.Anvil().GroupKind())},
			want:     []client.Object{fake.SyncObject(kinds.Anvil().GroupKind())},
		},
		{
			testName: "no-op Sync",
			actual:   []client.Object{fake.SyncObject(kinds.Anvil().GroupKind())},
			declared: []client.Object{fake.SyncObject(kinds.Anvil().GroupKind())},
			want:     []client.Object{fake.SyncObject(kinds.Anvil().GroupKind())},
		},
		{
			testName: "delete Sync",
			actual:   []client.Object{fake.SyncObject(kinds.Anvil().GroupKind())},
		},
		// Test all at once
		{
			testName: "multiple diffs at once",
			actual: []client.Object{
				crdClusterConfig(),
				namespaceConfig("foo"),
				namespaceConfig("bar"),
			},
			declared: []client.Object{
				clusterConfig(),
				namespaceConfig("foo"),
				namespaceConfig("qux"),
			},
			want: []client.Object{
				clusterConfig(),
				namespaceConfig("foo"),
				namespaceConfig("bar", markedForDeletion),
				namespaceConfig("qux"),
			},
		},
		{
			testName: "multiple diffs at once with various sync states",
			actual: []client.Object{
				stalledClusterConfig(v1.StateSynced),
				stalledCRDClusterConfig(v1.StateSynced),
				stalledNamespaceConfig("foo", v1.StateSynced),
				stalledNamespaceConfig("bar", v1.StateSynced),
				stalledNamespaceConfig("baz", v1.StateSynced),
			},
			declared: []client.Object{
				stalledClusterConfig(v1.StateStale),
				stalledCRDClusterConfig(v1.StateUnknown),
				stalledNamespaceConfig("foo", v1.StateStale),
				stalledNamespaceConfig("bar", v1.StateUnknown),
				stalledNamespaceConfig("qux", v1.StateStale),
				stalledNamespaceConfig("quux", v1.StateUnknown),
			},
			want: []client.Object{
				stalledClusterConfig(v1.StateStale),
				stalledCRDClusterConfig(v1.StateSynced),
				stalledNamespaceConfig("foo", v1.StateStale),
				stalledNamespaceConfig("bar", v1.StateSynced),
				stalledNamespaceConfig("baz", v1.StateSynced, markedForDeletion),
				stalledNamespaceConfig("qux", v1.StateStale),
				stalledNamespaceConfig("quux", v1.StateUnknown),
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.testName, func(t *testing.T) {
			fakeClient := testingfake.NewClient(t, runtime.NewScheme(), tc.actual...)

			actual := allConfigs(t, tc.actual)

			declared := allConfigs(t, tc.declared)

			err := Update(context.Background(), syncerclient.New(fakeClient, metrics.APICallDuration),
				testingfake.NewDecoder(nil), *actual, *declared, testTime.Time)

			if err != nil {
				t.Errorf("unexpected error in diff: %v", err)
			}
			fakeClient.Check(t, tc.want...)
		})
	}
}
