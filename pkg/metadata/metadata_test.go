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

// Set the package name to `metadata_test` to avoid import cycles.
package metadata_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestHasConfigSyncMetadata(t *testing.T) {
	testcases := []struct {
		name string
		obj  client.Object
		want bool
	}{
		{
			name: "An object without Config Sync metadata",
			obj:  k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy")),
			want: false,
		},
		{
			name: "An object with the `OwningInventoryKey` annotation",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value")),
			want: true,
		},
		{
			name: "An object with the `LifecycleMutationAnnotation` annotation",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.LifecycleMutationAnnotation, "random-value")),
			want: true,
		},
		{
			name: "An object with the `client.lifecycle.config.k8s.io/others` annotation",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.LifecyclePrefix+"/others", "random-value")),
			want: false,
		},
		{
			name: "An object with the `ResourceManagementKey` annotation",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				metadata.WithManagementMode(metadata.ManagementEnabled)),
			want: true,
		},
		{
			name: "An object with the `ResourceIDKey` annotation",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.ResourceIDKey, "random-value")),
			want: true,
		},
		{
			name: "An object with the `HNCManagedBy` annotation (random value)",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.HNCManagedBy, "random-value")),
			want: false,
		},
		{
			name: "An object with the `HNCManagedBy` annotation (correct value)",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.HNCManagedBy, configmanagement.GroupName)),
			want: true,
		},
		{
			name: "An object with the `DeclaredVersionLabel` label",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.DeclaredVersionLabel, "v1")),
			want: true,
		},
		{
			name: "An object with the `SystemLabel` label",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.SystemLabel, "random-value")),
			want: true,
		},
		{
			name: "An object with the `ManagedByKey` label (correct value)",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue)),
			want: true,
		},
		{
			name: "An object with the `ManagedByKey` label (random value)",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.ManagedByKey, "random-value")),
			want: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := metadata.HasConfigSyncMetadata(tc.obj)
			if got != tc.want {
				t.Errorf("got HasConfigSyncMetadata() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemoveConfigSyncMetadata(t *testing.T) {
	obj := k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
		syncertest.ManagementEnabled,
		core.Annotation(metadata.OwningInventoryKey, "random-value"),
		core.Annotation(metadata.LifecycleMutationAnnotation, "random-value"),
		core.Annotation(metadata.HNCManagedBy, configmanagement.GroupName),
		core.Label(metadata.DeclaredVersionLabel, "v1"),
		core.Label(metadata.SystemLabel, "random-value"),
		core.Label(metadata.ManagedByKey, metadata.ManagedByValue))
	updated := metadata.RemoveConfigSyncMetadata(obj)
	if !updated {
		t.Errorf("updated should be true")
	}
	labels := obj.GetLabels()
	if len(labels) > 0 {
		t.Errorf("labels should be empty, but got %v", labels)
	}

	annotations := obj.GetAnnotations()
	expectedAnnotation := map[string]string{
		metadata.LifecycleMutationAnnotation: "random-value",
	}
	if diff := cmp.Diff(annotations, expectedAnnotation); diff != "" {
		t.Errorf("Diff from the annotations is %s", diff)
	}

	updated = metadata.RemoveConfigSyncMetadata(obj)
	if updated {
		t.Errorf("the labels and annotations shouldn't be updated in this case")
	}
}

func TestRemoveApplySetPartOfLabel(t *testing.T) {
	tests := []struct {
		name        string
		obj         client.Object
		applySetID  string
		wantUpdated bool
		wantObj     client.Object
	}{
		{
			name: "noop no labels",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value")),
			applySetID:  "example",
			wantUpdated: false,
			wantObj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value")),
		},
		{
			name: "noop no key match",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value"),
				core.Label(metadata.SystemLabel, "random-value")),
			applySetID:  "example",
			wantUpdated: false,
			wantObj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value"),
				core.Label(metadata.SystemLabel, "random-value")),
		},
		{
			name: "noop no value match",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value"),
				core.Label(metadata.SystemLabel, "random-value"),
				core.Label(metadata.ApplySetPartOfLabel, "example")),
			applySetID:  "example-2",
			wantUpdated: false,
			wantObj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value"),
				core.Label(metadata.SystemLabel, "random-value"),
				core.Label(metadata.ApplySetPartOfLabel, "example")),
		},
		{
			name: "removal",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value"),
				core.Label(metadata.SystemLabel, "random-value"),
				core.Label(metadata.ApplySetPartOfLabel, "example")),
			applySetID:  "example",
			wantUpdated: true,
			wantObj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value"),
				core.Label(metadata.SystemLabel, "random-value")),
		},
		{
			name: "removal to empty",
			obj: k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.ApplySetPartOfLabel, "example")),
			applySetID:  "example",
			wantUpdated: true,
			wantObj:     k8sobjects.UnstructuredObject(kinds.Deployment(), core.Name("deploy")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := kinds.ObjectAsClientObject(tt.obj.DeepCopyObject())
			require.NoError(t, err)
			updated := metadata.RemoveApplySetPartOfLabel(obj, tt.applySetID)
			assert.Equal(t, tt.wantUpdated, updated)
			testutil.AssertEqual(t, tt.wantObj, obj)
		})
	}
}

func TestUpdateConfigSyncMetadata(t *testing.T) {
	tests := []struct {
		name        string
		fromObj     client.Object
		toObj       client.Object
		expectedObj client.Object
	}{
		{
			name:        "fromObj + toObj don't have CS metadata",
			fromObj:     k8sobjects.NamespaceObject("test-ns"),
			toObj:       k8sobjects.NamespaceObject("test-ns"),
			expectedObj: k8sobjects.NamespaceObject("test-ns"),
		},
		{
			name:        "fromObj has the ignore mutation annotation but toObj doesn't",
			fromObj:     k8sobjects.NamespaceObject("test-ns", syncertest.IgnoreMutationAnnotation, syncertest.ManagementEnabled),
			toObj:       k8sobjects.NamespaceObject("test-ns"),
			expectedObj: k8sobjects.NamespaceObject("test-ns", syncertest.IgnoreMutationAnnotation, syncertest.ManagementEnabled),
		},
		{
			name:        "fromObj doesn't have the ignore mutation annotation but toObj does",
			fromObj:     k8sobjects.NamespaceObject("test-ns", syncertest.ManagementEnabled),
			toObj:       k8sobjects.NamespaceObject("test-ns", syncertest.IgnoreMutationAnnotation),
			expectedObj: k8sobjects.NamespaceObject("test-ns", syncertest.ManagementEnabled, syncertest.IgnoreMutationAnnotation),
		},
		{
			name:        "fromObj doesn't have a non-CS annotation but toObj does",
			fromObj:     k8sobjects.NamespaceObject("test-ns", syncertest.ManagementEnabled),
			toObj:       k8sobjects.NamespaceObject("test-ns", syncertest.IgnoreMutationAnnotation, core.Annotation("foo", "bar")),
			expectedObj: k8sobjects.NamespaceObject("test-ns", syncertest.ManagementEnabled, syncertest.IgnoreMutationAnnotation, core.Annotation("foo", "bar")),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			toObj, err := kinds.ObjectAsClientObject(tc.toObj.DeepCopyObject())
			require.NoError(t, err)
			metadata.UpdateConfigSyncMetadata(tc.fromObj, toObj)
			testutil.AssertEqual(t, tc.expectedObj, toObj)
		})
	}
}
