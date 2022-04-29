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
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"kpt.dev/configsync/pkg/testing/fake"
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
			obj:  fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy")),
			want: false,
		},
		{
			name: "An object with the `OwningInventoryKey` annotation",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.OwningInventoryKey, "random-value")),
			want: true,
		},
		{
			name: "An object with the `LifecycleMutationAnnotation` annotation",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.LifecycleMutationAnnotation, "random-value")),
			want: true,
		},
		{
			name: "An object with the `client.lifecycle.config.k8s.io/others` annotation",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.LifecyclePrefix+"/others", "random-value")),
			want: false,
		},
		{
			name: "An object with the `ResourceManagementKey` annotation",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)),
			want: true,
		},
		{
			name: "An object with the `ResourceIDKey` annotation",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.ResourceIDKey, "random-value")),
			want: true,
		},
		{
			name: "An object with the `HNCManagedBy` annotation (random value)",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.HNCManagedBy, "random-value")),
			want: false,
		},
		{
			name: "An object with the `HNCManagedBy` annotation (correct value)",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Annotation(metadata.HNCManagedBy, configmanagement.GroupName)),
			want: true,
		},
		{
			name: "An object with the `DeclaredVersionLabel` label",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.DeclaredVersionLabel, "v1")),
			want: true,
		},
		{
			name: "An object with the `SystemLabel` label",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.SystemLabel, "random-value")),
			want: true,
		},
		{
			name: "An object with the `ManagedByKey` label (correct value)",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue)),
			want: true,
		},
		{
			name: "An object with the `ManagedByKey` label (random value)",
			obj: fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
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
	obj := fake.UnstructuredObject(kinds.Deployment(), core.Name("deploy"),
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
