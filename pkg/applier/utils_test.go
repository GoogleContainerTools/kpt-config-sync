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

package applier

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPartitionObjs(t *testing.T) {
	testcases := []struct {
		name          string
		objs          []client.Object
		enabledCount  int
		disabledCount int
	}{
		{
			name: "all managed objs",
			objs: []client.Object{
				fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"), syncertest.ManagementEnabled),
				fake.ServiceObject(core.Name("service"), core.Namespace("default"), syncertest.ManagementEnabled),
			},
			enabledCount:  2,
			disabledCount: 0,
		},
		{
			name: "all disabled objs",
			objs: []client.Object{
				fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"), syncertest.ManagementDisabled),
				fake.ServiceObject(core.Name("service"), core.Namespace("default"), syncertest.ManagementDisabled),
			},
			enabledCount:  0,
			disabledCount: 2,
		},
		{
			name: "mixed objs",
			objs: []client.Object{
				fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"), syncertest.ManagementEnabled),
				fake.ServiceObject(core.Name("service"), core.Namespace("default"), syncertest.ManagementDisabled),
			},
			enabledCount:  1,
			disabledCount: 1,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			enabled, disabled := partitionObjs(tc.objs)
			if len(enabled) != tc.enabledCount {
				t.Errorf("expected %d enabled objects, but got %d", tc.enabledCount, enabled)
			}
			if len(disabled) != tc.disabledCount {
				t.Errorf("expected %d disabled objects, but got %d", tc.disabledCount, enabled)
			}
		})
	}
}

func TestObjMetaFrom(t *testing.T) {
	d := fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))
	expected := object.ObjMetadata{
		Namespace: "default",
		Name:      "deploy",
		GroupKind: schema.GroupKind{
			Group: "apps",
			Kind:  "Deployment",
		},
	}
	actual := objMetaFrom(d)
	if actual != expected {
		t.Errorf("expected %v but got %v", expected, actual)
	}
}

func TestIDFrom(t *testing.T) {
	d := fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))
	meta := objMetaFrom(d)
	id := idFrom(meta)
	if id != core.IDOf(d) {
		t.Errorf("expected %v but got %v", core.IDOf(d), id)
	}
}

func TestRemoveFrom(t *testing.T) {
	testcases := []struct {
		name       string
		allObjMeta []object.ObjMetadata
		objs       []client.Object
		expected   []object.ObjMetadata
	}{
		{
			name: "toRemove is empty",
			allObjMeta: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
				objMetaFrom(fake.ServiceObject(core.Name("service"), core.Namespace("default"))),
			},
			objs: nil,
			expected: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
				objMetaFrom(fake.ServiceObject(core.Name("service"), core.Namespace("default"))),
			},
		},
		{
			name: "all toRemove are in the original list",
			allObjMeta: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
				objMetaFrom(fake.ServiceObject(core.Name("service"), core.Namespace("default"))),
			},
			objs: []client.Object{
				fake.ServiceObject(core.Name("service"), core.Namespace("default")),
			},
			expected: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
			},
		},
		{
			name: "some toRemove are not in the original list",
			allObjMeta: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
				objMetaFrom(fake.ServiceObject(core.Name("service"), core.Namespace("default"))),
			},
			objs: []client.Object{
				fake.ServiceObject(core.Name("service"), core.Namespace("default")),
				fake.ConfigMapObject(core.Name("cm"), core.Namespace("default")),
			},
			expected: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
			},
		},
		{
			name: "toRemove are the same as original objects",
			allObjMeta: []object.ObjMetadata{
				objMetaFrom(fake.DeploymentObject(core.Name("deploy"), core.Namespace("default"))),
				objMetaFrom(fake.ServiceObject(core.Name("service"), core.Namespace("default"))),
			},
			objs: []client.Object{
				fake.DeploymentObject(core.Name("deploy"), core.Namespace("default")),
				fake.ServiceObject(core.Name("service"), core.Namespace("default")),
			},
			expected: nil,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual := removeFrom(tc.allObjMeta, tc.objs)
			if diff := cmp.Diff(tc.expected, actual, cmpopts.SortSlices(
				func(x, y object.ObjMetadata) bool { return x.String() < y.String() })); diff != "" {
				t.Errorf("%s: Diff of removeFrom is: %s", tc.name, diff)
			}
		})
	}
}

func TestGetObjectSize(t *testing.T) {
	u := newInventoryUnstructured("inv-1", "test", "disabled")
	size, err := getObjectSize(u)
	if err != nil {
		t.Fatal(err)
	}
	if size > 1000 {
		t.Fatalf("An empty inventory object shouldn't have a large size: %d", size)
	}
}
