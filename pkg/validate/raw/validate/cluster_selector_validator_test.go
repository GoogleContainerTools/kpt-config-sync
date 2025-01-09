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
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	csmetadata "kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

var (
	legacyClusterSelectorAnnotation = core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-selector")
	inlineClusterSelectorAnnotation = core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod-cluster")
)

func TestClusterSelectorsForHierarchical(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *fileobjects.Raw
		wantErrs status.MultiError
	}{
		{
			name: "No objects",
			objs: &fileobjects.Raw{},
		},
		{
			name: "One ClusterSelector",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterSelector(core.Name("first")),
				},
			},
		},
		{
			name: "Two ClusterSelectors",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterSelector(core.Name("first")),
					k8sobjects.ClusterSelector(core.Name("second")),
				},
			},
		},
		{
			name: "Duplicate ClusterSelectors",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterSelector(core.Name("first")),
					k8sobjects.ClusterSelector(core.Name("first")),
				},
			},
			wantErrs: nonhierarchical.SelectorMetadataNameCollisionError(kinds.ClusterSelector().Kind, "first", k8sobjects.ClusterSelector()),
		},
		{
			name: "Objects with no cluster selector",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterRole(),
					k8sobjects.AnvilCRDv1AtPath("crd.yaml"),
					k8sobjects.AnvilCRDv1beta1AtPath("crd.yaml"),
				},
			},
		},
		{
			name: "Objects with legacy cluster selector",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterRole(legacyClusterSelectorAnnotation),
					k8sobjects.AnvilCRDv1AtPath("crd.yaml", legacyClusterSelectorAnnotation),
					k8sobjects.AnvilCRDv1beta1AtPath("crd.yaml", legacyClusterSelectorAnnotation),
				},
			},
		},
		{
			name: "Objects with inline cluster selector",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterRole(inlineClusterSelectorAnnotation),
					k8sobjects.AnvilCRDv1AtPath("crd.yaml", inlineClusterSelectorAnnotation),
					k8sobjects.AnvilCRDv1beta1AtPath("crd.yaml", inlineClusterSelectorAnnotation),
				},
			},
		},
		{
			name: "Non-selectable objects with no cluster selectors",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Cluster(),
					k8sobjects.ClusterSelector(),
					k8sobjects.NamespaceSelector(),
				},
			},
		},
		{
			name: "Non-selectable objects with legacy cluster selectors",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Cluster(legacyClusterSelectorAnnotation),
					k8sobjects.ClusterSelector(legacyClusterSelectorAnnotation),
					k8sobjects.NamespaceSelector(legacyClusterSelectorAnnotation),
				},
			},
			wantErrs: status.Wrap(
				nonhierarchical.IllegalClusterSelectorAnnotationError(k8sobjects.Cluster(), "legacy"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(k8sobjects.ClusterSelector(), "legacy"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(k8sobjects.NamespaceSelector(), "legacy"),
			),
		},
		{
			name: "Non-selectable objects with inline cluster selectors",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Cluster(inlineClusterSelectorAnnotation),
					k8sobjects.ClusterSelector(inlineClusterSelectorAnnotation),
					k8sobjects.NamespaceSelector(inlineClusterSelectorAnnotation),
				},
			},
			wantErrs: status.Wrap(
				nonhierarchical.IllegalClusterSelectorAnnotationError(k8sobjects.Cluster(), "inline"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(k8sobjects.ClusterSelector(), "inline"),
				nonhierarchical.IllegalClusterSelectorAnnotationError(k8sobjects.NamespaceSelector(), "inline"),
			),
		},
		{
			name: "Cluster and legacy cluster selector in wrong directory",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterAtPath("system/cluster.yaml"),
					k8sobjects.ClusterSelectorAtPath("cluster/cs.yaml"),
				},
			},
			wantErrs: status.Wrap(
				validation.ShouldBeInClusterRegistryError(k8sobjects.Cluster()),
				validation.ShouldBeInClusterRegistryError(k8sobjects.ClusterSelector()),
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
		objs     *fileobjects.Raw
		wantErrs status.MultiError
	}{
		// We really just need to verify that unstructured does not care about the
		// directory of the files.
		{
			name: "Cluster and legacy cluster selector in wrong directory",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterAtPath("system/cluster.yaml"),
					k8sobjects.ClusterSelectorAtPath("cluster/cs.yaml"),
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
