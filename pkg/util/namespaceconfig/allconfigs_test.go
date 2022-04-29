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

package namespaceconfig_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"kpt.dev/configsync/testing/testoutput"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func withClusterResources(os ...client.Object) fake.ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		for _, o := range os {
			cc.AddResource(o)
		}
	}
}

func withNamespaceResources(os ...client.Object) core.MetaMutator {
	return func(co client.Object) {
		nsc := co.(*v1.NamespaceConfig)
		for _, o := range os {
			nsc.AddResource(o)
		}
	}
}

func TestNewAllConfigs(t *testing.T) {
	testCases := []struct {
		name        string
		fileObjects []ast.FileObject
		want        *namespaceconfig.AllConfigs
	}{
		{
			name: "empty AllConfigs",
		},
		{
			name: "v1beta1 CRD",
			fileObjects: []ast.FileObject{
				fake.CustomResourceDefinitionV1Beta1(),
			},
			want: &namespaceconfig.AllConfigs{
				CRDClusterConfig: fake.CRDClusterConfigObject(withClusterResources(
					fake.CustomResourceDefinitionV1Beta1Unstructured(),
				)),
				Syncs: testoutput.Syncs(kinds.CustomResourceDefinitionV1Beta1()),
			},
		},
		{
			name: "v1 CRD",
			fileObjects: []ast.FileObject{
				fake.CustomResourceDefinitionV1(),
			},
			want: &namespaceconfig.AllConfigs{
				CRDClusterConfig: fake.CRDClusterConfigObject(withClusterResources(
					fake.CustomResourceDefinitionV1Unstructured(),
				)),
				Syncs: testoutput.Syncs(kinds.CustomResourceDefinitionV1()),
			},
		},
		{
			name: "both v1 and v1beta1 CRDs",
			fileObjects: []ast.FileObject{
				fake.CustomResourceDefinitionV1(),
				fake.CustomResourceDefinitionV1Beta1(),
			},
			want: &namespaceconfig.AllConfigs{
				CRDClusterConfig: fake.CRDClusterConfigObject(withClusterResources(
					fake.CustomResourceDefinitionV1Unstructured(),
					fake.CustomResourceDefinitionV1Beta1Unstructured(),
				)),
				Syncs: testoutput.Syncs(
					kinds.CustomResourceDefinitionV1Beta1(),
					kinds.CustomResourceDefinitionV1(),
				),
			},
		},
		{
			name: "explicit Namespace does not have Deletion lifecycle annotation",
			fileObjects: []ast.FileObject{
				fake.Namespace("namespaces/shipping"),
				fake.ConfigMap(core.Name("my-configMap"), core.Namespace("shipping")),
			},
			want: &namespaceconfig.AllConfigs{
				NamespaceConfigs: map[string]v1.NamespaceConfig{
					"shipping": *fake.NamespaceConfigObject(
						core.Name("shipping"),
						// No Deletion annotation
						withNamespaceResources(
							fake.UnstructuredObject(kinds.ConfigMap(), core.Name("my-configMap"), core.Namespace("shipping")),
						)),
				},
				Syncs: testoutput.Syncs(kinds.ConfigMap()),
			},
		},
		{
			name: "explicit Namespace second does not have Deletion lifecycle annotation",
			fileObjects: []ast.FileObject{
				fake.ConfigMap(core.Name("my-configMap"), core.Namespace("shipping")),
				fake.Namespace("namespaces/shipping"),
			},
			want: &namespaceconfig.AllConfigs{
				NamespaceConfigs: map[string]v1.NamespaceConfig{
					"shipping": *fake.NamespaceConfigObject(
						core.Name("shipping"),
						// No Deletion annotation
						withNamespaceResources(
							fake.UnstructuredObject(kinds.ConfigMap(), core.Name("my-configMap"), core.Namespace("shipping")),
						)),
				},
				Syncs: testoutput.Syncs(kinds.ConfigMap()),
			},
		},
		{
			name: "implicit Namespace has Deletion lifecycle annotation",
			fileObjects: []ast.FileObject{
				fake.ConfigMap(core.Name("my-configMap"), core.Namespace("shipping")),
			},
			want: &namespaceconfig.AllConfigs{
				NamespaceConfigs: map[string]v1.NamespaceConfig{
					"shipping": *fake.NamespaceConfigObject(
						core.Name("shipping"),
						core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
						withNamespaceResources(
							fake.UnstructuredObject(kinds.ConfigMap(), core.Name("my-configMap"), core.Namespace("shipping")),
						)),
				},
				Syncs: testoutput.Syncs(kinds.ConfigMap()),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			want := namespaceconfig.NewAllConfigs("", metav1.Time{}, nil)
			if tc.want != nil {
				if tc.want.ClusterConfig != nil {
					want.ClusterConfig = tc.want.ClusterConfig
				}
				if tc.want.CRDClusterConfig != nil {
					want.CRDClusterConfig = tc.want.CRDClusterConfig
				}
				if tc.want.NamespaceConfigs != nil {
					want.NamespaceConfigs = tc.want.NamespaceConfigs
				}
				if tc.want.Syncs != nil {
					want.Syncs = tc.want.Syncs
				}
			}

			actual := namespaceconfig.NewAllConfigs("", metav1.Time{}, tc.fileObjects)

			if diff := cmp.Diff(want, actual, cmpopts.EquateEmpty()); diff != "" {
				t.Error(diff)
			}
		})
	}
}
