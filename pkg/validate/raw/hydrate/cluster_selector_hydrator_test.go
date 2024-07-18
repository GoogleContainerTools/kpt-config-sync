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

package hydrate

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

const (
	prodClusterName    = "prod-cluster"
	devClusterName     = "dev-cluster"
	unknownClusterName = "unknown-cluster"

	prodClusterSelectorName = "prod-selector"
	devClusterSelectorName  = "dev-selector"
)

var (
	prodCluster = cluster(prodClusterName, "environment", "prod")
	devCluster  = cluster(devClusterName, "environment", "dev")

	prodSelector = clusterSelector(prodClusterSelectorName, "environment", "prod")
	devSelector  = clusterSelector(devClusterSelectorName, "environment", "dev")
)

// clusterSelector creates a FileObject containing a ClusterSelector named "name",
// which matches Cluster objects with label "label" set to "value".
func clusterSelector(name, label, value string) ast.FileObject {
	cs := k8sobjects.ClusterSelectorObject(core.Name(name))
	cs.Spec.Selector.MatchLabels = map[string]string{label: value}
	return k8sobjects.FileObject(cs, fmt.Sprintf("clusterregistry/cs-%s.yaml", name))
}

// cluster creates a FileObject containing a Cluster named "name", with label "label"
// set to "value".
func cluster(name, label, value string) ast.FileObject {
	return k8sobjects.Cluster(core.Name(name), core.Label(label, value))
}

// withLegacyClusterSelector modifies a FileObject to have a legacy cluster-selector annotation
// referencing the ClusterSelector named "name".
func withLegacyClusterSelector(name string) core.MetaMutator {
	return core.Annotation(metadata.LegacyClusterSelectorAnnotationKey, name)
}

// withInlineClusterNameSelector modifies a FileObject to have an inline cluster-selector annotation
// referencing the cluster matched with the labelSelector.
func withInlineClusterNameSelector(clusters string) core.MetaMutator {
	return core.Annotation(metadata.ClusterNameSelectorAnnotationKey, clusters)
}

var (
	withProdLegacyClusterSelector    = withLegacyClusterSelector(prodClusterSelectorName)
	withDevLegacyClusterSelector     = withLegacyClusterSelector(devClusterSelectorName)
	withUnknownLegacyClusterSelector = withLegacyClusterSelector("stateUnknown")
	withProdInlineMatchLabels        = withInlineClusterNameSelector(prodClusterName)
	withDevInlineMatchLabels         = withInlineClusterNameSelector(devClusterName)
)

func TestClusterSelectors(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *fileobjects.Raw
		want     *fileobjects.Raw
		wantErrs status.MultiError
	}{
		{
			name: "No objects",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
			},
		},
		{
			name: "Keep object with no cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.ClusterRole(),
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.ClusterRole(),
				},
			},
		},
		{
			name: "Keep object in namespace with no cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.Role(core.Namespace("foo")),
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.Role(core.Namespace("foo")),
				},
			},
		},
		{
			name: "Keep object in namespace with stateActive legacy cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo", withProdLegacyClusterSelector),
					k8sobjects.Role(core.Namespace("foo")),
					prodCluster,
					devCluster,
					prodSelector,
					devSelector,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo", withProdLegacyClusterSelector),
					k8sobjects.Role(core.Namespace("foo")),
				},
			},
		},
		{
			name: "Remove object and namespace with stateInactive legacy cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo", withDevLegacyClusterSelector),
					k8sobjects.Role(core.Namespace("foo")),
					prodCluster,
					devCluster,
					prodSelector,
					devSelector,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
			},
		},
		{
			name: "Keep object in namespace with stateActive inline cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo", withProdInlineMatchLabels),
					k8sobjects.Role(core.Namespace("foo")),
					prodCluster,
					devCluster,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo", withProdInlineMatchLabels),
					k8sobjects.Role(core.Namespace("foo")),
				},
			},
		},
		{
			name: "Remove object and namespace with stateInactive inline cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo", withDevInlineMatchLabels),
					k8sobjects.Role(core.Namespace("foo")),
					prodCluster,
					devCluster,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
			},
		},
		{
			name: "Remove object with legacy cluster selector from stateUnknown cluster",
			objs: &fileobjects.Raw{
				ClusterName: unknownClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(core.Namespace("foo"), withDevLegacyClusterSelector),
					devCluster,
					devSelector,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: unknownClusterName,
			},
		},
		{
			name: "Remove object with inline cluster selector from stateUnknown cluster",
			objs: &fileobjects.Raw{
				ClusterName: unknownClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(core.Namespace("foo"), withDevInlineMatchLabels),
				},
			},
			want: &fileobjects.Raw{
				ClusterName: unknownClusterName,
			},
		},
		{
			name: "Keep object with inline cluster selector listing multiple clusters",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withInlineClusterNameSelector(fmt.Sprintf("%s, %s", devClusterName, prodClusterName))),
					prodCluster,
					devCluster,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withInlineClusterNameSelector(fmt.Sprintf("%s, %s", devClusterName, prodClusterName))),
				},
			},
		},
		{
			name: "Remove object with empty inline cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withInlineClusterNameSelector("")),
					prodCluster,
					devCluster,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
			},
		},
		{
			name: "Remove object with inline cluster selector from empty name cluster",
			objs: &fileobjects.Raw{
				ClusterName: "",
				Objects: []ast.FileObject{
					k8sobjects.Role(withInlineClusterNameSelector("a,,b")),
					prodCluster,
					devCluster,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: "",
			},
		},
		{
			name: "Error if object has both legacy and inline cluster selectors",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withProdLegacyClusterSelector, withProdInlineMatchLabels),
					prodCluster,
					prodSelector,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withProdLegacyClusterSelector, withProdInlineMatchLabels),
					prodCluster,
					prodSelector,
				},
			},
			wantErrs: selectors.ClusterSelectorAnnotationConflictError(k8sobjects.Role()),
		},
		{
			name: "Error if object has stateUnknown legacy cluster selector",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withUnknownLegacyClusterSelector),
					prodCluster,
					prodSelector,
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.Role(withUnknownLegacyClusterSelector),
					prodCluster,
					prodSelector,
				},
			},
			wantErrs: selectors.ObjectHasUnknownClusterSelector(k8sobjects.Role(), "stateUnknown"),
		},
		{
			name: "Error if ClusterSelector is invalid",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					prodCluster,
					prodSelector,
					clusterSelector("invalid", "environment", "xin prod"),
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					prodCluster,
					prodSelector,
					clusterSelector("invalid", "environment", "xin prod"),
				},
			},
			wantErrs: selectors.InvalidSelectorError(k8sobjects.ClusterSelector(), errors.New("")),
		},
		{
			name: "Error if ClusterSelector is empty",
			objs: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.ClusterSelector(core.Name("empty")),
				},
			},
			want: &fileobjects.Raw{
				ClusterName: prodClusterName,
				Objects: []ast.FileObject{
					k8sobjects.ClusterSelector(core.Name("empty")),
				},
			},
			wantErrs: selectors.EmptySelectorError(k8sobjects.ClusterSelector()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ClusterSelectors(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("Got ClusterSelectors() error %v, want %v", errs, tc.wantErrs)
			}
			if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
