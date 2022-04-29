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

package objects

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/tree"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

// TreeVisitor is a function that validates or hydrates Raw objects.
type TreeVisitor func(t *Tree) status.MultiError

// Tree contains a collection of FileObjects that are organized based upon the
// structure of a hierarchical repo. This includes system-level objects like
// HierarchyConfigs as well as a hierarchical tree of namespaces and namespace-
// scoped objects.
type Tree struct {
	Repo               ast.FileObject
	HierarchyConfigs   []ast.FileObject
	NamespaceSelectors map[string]ast.FileObject
	Cluster            []ast.FileObject
	Tree               *ast.TreeNode
}

// treeBuilder is a helper type that specifically helps group Namespace objects
// together from several sources in the Scoped object before they are used to
// build the hierarchical Tree.
type treeBuilder struct {
	Repo               ast.FileObject
	HierarchyConfigs   []ast.FileObject
	NamespaceSelectors map[string]ast.FileObject
	Cluster            []ast.FileObject
	Namespace          []ast.FileObject
}

func (t *treeBuilder) addObject(obj ast.FileObject, dir string) status.Error {
	switch dir {
	case repo.SystemDir:
		return t.addSystemObject(obj)
	case repo.ClusterDir:
		t.Cluster = append(t.Cluster, obj)
	case repo.NamespacesDir:
		if obj.GetObjectKind().GroupVersionKind() == kinds.NamespaceSelector() {
			// We have already verified that all NamespaceSelectors have a unique name.
			t.NamespaceSelectors[obj.GetName()] = obj
		} else {
			t.Namespace = append(t.Namespace, obj)
		}
	default:
		return status.InternalErrorf("unhandled top level directory: %q", dir)
	}

	return nil
}

func (t *treeBuilder) addSystemObject(obj ast.FileObject) status.Error {
	gk := obj.GetObjectKind().GroupVersionKind().GroupKind()
	switch gk {
	case kinds.HierarchyConfig().GroupKind():
		t.HierarchyConfigs = append(t.HierarchyConfigs, obj)
	case kinds.Repo().GroupKind():
		// We have already validated that there is only one Repo object.
		t.Repo = obj
	default:
		return status.InternalErrorf("unhandled system object: %v", obj)
	}
	return nil
}

// Objects returns all FileObjects in the Tree collection.
func (t *Tree) Objects() []ast.FileObject {
	return append(t.Cluster, t.Tree.Flatten()...)
}

// BuildTree builds a Tree collection of objects from the given Scoped objects.
func BuildTree(scoped *Scoped) (*Tree, status.MultiError) {
	var errs status.MultiError
	b := &treeBuilder{
		NamespaceSelectors: make(map[string]ast.FileObject),
	}

	// First process cluster-scoped resources.
	for _, obj := range scoped.Cluster {
		dir, err := topLevelDirectory(obj, repo.ClusterDir)
		if err != nil {
			errs = status.Append(errs, err)
			continue
		}
		errs = status.Append(errs, b.addObject(obj, dir))
	}

	// Next, do our best to process unknown-scoped resources.
	for _, obj := range scoped.Unknown {
		sourcePath := obj.OSPath()
		dir := cmpath.RelativeSlash(sourcePath).Split()[0]
		errs = status.Append(errs, b.addObject(obj, dir))
	}

	// Finally, process namespace-scoped resources.
	for _, obj := range scoped.Namespace {
		_, err := topLevelDirectory(obj, repo.NamespacesDir)
		if err != nil {
			errs = status.Append(errs, err)
		}
	}
	v := tree.NewBuilder(append(b.Namespace, scoped.Namespace...))
	treeRoot := v.VisitRoot(&ast.Root{}).Tree
	if errs != nil {
		return nil, errs
	}

	if treeRoot == nil {
		treeRoot = &ast.TreeNode{
			Relative: cmpath.RelativeSlash(""),
			Type:     node.AbstractNamespace,
		}
	}
	return &Tree{
		Repo:               b.Repo,
		HierarchyConfigs:   b.HierarchyConfigs,
		NamespaceSelectors: b.NamespaceSelectors,
		Cluster:            b.Cluster,
		Tree:               treeRoot,
	}, nil
}

var topLevelDirectoryOverrides = map[schema.GroupVersionKind]string{
	kinds.Repo():            repo.SystemDir,
	kinds.HierarchyConfig(): repo.SystemDir,

	kinds.Namespace():         repo.NamespacesDir,
	kinds.NamespaceSelector(): repo.NamespacesDir,
}

func topLevelDirectory(obj ast.FileObject, expectedDir string) (string, status.Error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if override, hasOverride := topLevelDirectoryOverrides[gvk]; hasOverride {
		expectedDir = override
	}

	if obj.Split()[0] == expectedDir {
		return expectedDir, nil
	}

	switch expectedDir {
	case repo.SystemDir:
		return "", validation.ShouldBeInSystemError(obj)
	case repo.ClusterDir:
		return "", validation.ShouldBeInClusterError(obj)
	case repo.NamespacesDir:
		return "", validation.ShouldBeInNamespacesError(obj)
	default:
		return "", status.InternalErrorf("unhandled top level directory: %q", expectedDir)
	}
}
