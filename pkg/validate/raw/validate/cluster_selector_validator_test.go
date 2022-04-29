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

package validate

import (
	"errors"
	"testing"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	csmetadata "kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
)

var (
	legacyClusterSelectorAnnotation = core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-selector")
	inlineClusterSelectorAnnotation = core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod-cluster")
)

func TestClusterSelectorsForHierarchical(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *objects.Raw
		wantErrs status.MultiError
	}{
		{
			name: "No objects",
			objs: &objects.Raw{},
		},
		{
			name: "One ClusterSelector",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterSelector(core.Name("first")),
				},
			},
		},
		{
			name: "Two ClusterSelectors",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterSelector(core.Name("first")),
					fake.ClusterSelector(core.Name("second")),
				},
			},
		},
		{
			name: "Duplicate ClusterSelectors",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterSelector(core.Name("first")),
					fake.ClusterSelector(core.Name("first")),
				},
			},
			wantErrs: nonhierarchical.SelectorMetadataNameCollisionError(kinds.ClusterSelector().Kind, "first", fake.ClusterSelector()),
		},
		{
			name: "Objects with no cluster selector",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterRole(),
					fake.CustomResourceDefinitionV1(),
					fake.CustomResourceDefinitionV1Beta1(),
				},
			},
		},
		{
			name: "Objects with legacy cluster selector",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterRole(legacyClusterSelectorAnnotation),
					fake.CustomResourceDefinitionV1(legacyClusterSelectorAnnotation),
					fake.CustomResourceDefinitionV1Beta1(legacyClusterSelectorAnnotation),
				},
			},
		},
		{
			name: "Objects with inline cluster selector",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterRole(inlineClusterSelectorAnnotation),
					fake.CustomResourceDefinitionV1(inlineClusterSelectorAnnotation),
					fake.CustomResourceDefinitionV1Beta1(inlineClusterSelectorAnnotation),
				},
			},
		},
		{
			name: "Non-selectable objects with no cluster selectors",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Cluster(),
					fake.ClusterSelector(),
					fake.NamespaceSelector(),
				},
			},
		},
		{
			name: "Non-selectable objects with legacy cluster selectors",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Cluster(legacyClusterSelectorAnnotation),
					fake.ClusterSelector(legacyClusterSelectorAnnotation),
					fake.NamespaceSelector(legacyClusterSelectorAnnotation),
				},
			},
			wantErrs: status.Append(nil,
				nonhierarchical.IllegalClusterSelectorAnnotationError(fake.Cluster(), "legacy"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(fake.ClusterSelector(), "legacy"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(fake.NamespaceSelector(), "legacy"),
			),
		},
		{
			name: "Non-selectable objects with inline cluster selectors",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Cluster(inlineClusterSelectorAnnotation),
					fake.ClusterSelector(inlineClusterSelectorAnnotation),
					fake.NamespaceSelector(inlineClusterSelectorAnnotation),
				},
			},
			wantErrs: status.Append(nil,
				nonhierarchical.IllegalClusterSelectorAnnotationError(fake.Cluster(), "inline"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(fake.ClusterSelector(), "inline"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(fake.NamespaceSelector(), "inline"),
			),
		},
		{
			name: "Cluster and legacy cluster selector in wrong directory",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterAtPath("system/cluster.yaml"),
					fake.ClusterSelectorAtPath("cluster/cs.yaml"),
				},
			},
			wantErrs: status.Append(nil,
				validation.ShouldBeInClusterRegistryError(fake.Cluster()),
				validation.ShouldBeInClusterRegistryError(fake.ClusterSelector()),
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ClusterSelectorsForHierarchical(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got ClusterSelectorsForHierarchical() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}

func TestClusterSelectorsForUnstructured(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *objects.Raw
		wantErrs status.MultiError
	}{
		// We really just need to verify that unstructured does not care about the
		// directory of the files.
		{
			name: "Cluster and legacy cluster selector in wrong directory",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.ClusterAtPath("system/cluster.yaml"),
					fake.ClusterSelectorAtPath("cluster/cs.yaml"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ClusterSelectorsForUnstructured(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got ClusterSelectorsForUnstructured() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}
