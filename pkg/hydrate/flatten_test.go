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

package hydrate_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"kpt.dev/configsync/testing/testoutput"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestFlatten(t *testing.T) {
	testCases := []struct {
		name     string
		configs  *namespaceconfig.AllConfigs
		expected []client.Object
	}{
		{
			name: "nil AllConfigs",
		},
		{
			name:    "empty AllConfigs",
			configs: &namespaceconfig.AllConfigs{},
		},
		{
			name: "one v1Beta1 CRD",
			configs: &namespaceconfig.AllConfigs{
				CRDClusterConfig: testoutput.CRDClusterConfig(
					fake.CustomResourceDefinitionV1Beta1Object(),
				),
			},
			expected: []client.Object{
				fake.CustomResourceDefinitionV1Beta1Object(),
			},
		},
		{
			name: "one v1 CRD",
			configs: &namespaceconfig.AllConfigs{
				CRDClusterConfig: testoutput.CRDClusterConfig(
					fake.CustomResourceDefinitionV1Object(),
				),
			},
			expected: []client.Object{
				fake.CustomResourceDefinitionV1Object(),
			},
		},
		{
			name: "one Cluster object",
			configs: &namespaceconfig.AllConfigs{
				ClusterConfig: testoutput.ClusterConfig(
					fake.ClusterRoleBindingObject(),
				),
			},
			expected: []client.Object{
				fake.ClusterRoleBindingObject(),
			},
		},
		{
			name: "one Namespaced object",
			configs: &namespaceconfig.AllConfigs{
				NamespaceConfigs: testoutput.NamespaceConfigs(testoutput.NamespaceConfig(
					"", "namespaces/bar", core.WithoutAnnotation(metadata.SourcePathAnnotationKey),
					fake.RoleBindingObject(),
				)),
			},
			expected: []client.Object{
				fake.NamespaceObject("bar"),
				fake.RoleBindingObject(core.Namespace("bar")),
			},
		},
		{
			name: "two Namespaced objects",
			configs: &namespaceconfig.AllConfigs{
				NamespaceConfigs: testoutput.NamespaceConfigs(testoutput.NamespaceConfig(
					"", "namespaces/bar", core.WithoutAnnotation(metadata.SourcePathAnnotationKey),
					fake.RoleBindingObject(),
				), testoutput.NamespaceConfig(
					"", "namespaces/foo", core.WithoutAnnotation(metadata.SourcePathAnnotationKey),
					fake.RoleObject(),
				)),
			},
			expected: []client.Object{
				fake.NamespaceObject("bar"),
				fake.RoleBindingObject(core.Namespace("bar")),
				fake.NamespaceObject("foo"),
				fake.RoleObject(core.Namespace("foo")),
			},
		},
		{
			name: "one of each",
			configs: &namespaceconfig.AllConfigs{
				CRDClusterConfig: testoutput.CRDClusterConfig(
					fake.CustomResourceDefinitionV1Beta1Object(),
				),
				ClusterConfig: testoutput.ClusterConfig(
					fake.ClusterRoleBindingObject(),
				),
				NamespaceConfigs: testoutput.NamespaceConfigs(testoutput.NamespaceConfig(
					"", "namespaces/bar", core.WithoutAnnotation(metadata.SourcePathAnnotationKey),
					fake.RoleBindingObject(),
				)),
			},
			expected: []client.Object{
				fake.CustomResourceDefinitionV1Beta1Object(),
				fake.ClusterRoleBindingObject(),
				fake.NamespaceObject("bar"),
				fake.RoleBindingObject(core.Namespace("bar")),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := hydrate.Flatten(tc.configs)

			if diff := cmp.Diff(tc.expected, actual, cmpopts.SortSlices(sortObjects)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func sortObjects(x, y client.Object) bool {
	gvkX := x.GetObjectKind().GroupVersionKind()
	gvkY := y.GetObjectKind().GroupVersionKind()
	if gvkX.Group != gvkY.Group {
		return gvkX.Group < gvkY.Group
	}
	if gvkX.Kind != gvkY.Kind {
		return gvkX.Kind < gvkY.Kind
	}

	if x.GetNamespace() != y.GetNamespace() {
		return x.GetNamespace() < y.GetNamespace()
	}
	return x.GetName() < y.GetName()
}
