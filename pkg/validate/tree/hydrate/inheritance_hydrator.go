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
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/syntax"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

type inheritanceSpecs map[schema.GroupKind]transform.InheritanceSpec

// Inheritance hydrates the given Tree objects by copying inherited objects from
// abstract namespaces down into child namespaces.
func Inheritance(objs *objects.Tree) status.MultiError {
	if objs.Tree == nil {
		return nil
	}
	specs, err := buildInheritanceSpecs(objs.HierarchyConfigs)
	if err != nil {
		return err
	}
	return specs.visitTreeNode(objs.Tree, nil)
}

// buildInheritanceSpecs populates the InheritanceHydrator with InheritanceSpecs
// based upon the HierarchyConfigs in the system directory.
func buildInheritanceSpecs(objs []ast.FileObject) (inheritanceSpecs, status.Error) {
	specs := make(map[schema.GroupKind]transform.InheritanceSpec)
	for _, obj := range objs {
		s, err := obj.Structured()
		if err != nil {
			return nil, err
		}
		hc := s.(*v1.HierarchyConfig)
		for _, r := range hc.Spec.Resources {
			effectiveMode := r.HierarchyMode
			if r.HierarchyMode == v1.HierarchyModeDefault {
				effectiveMode = v1.HierarchyModeInherit
			}

			for _, k := range r.Kinds {
				gk := schema.GroupKind{Group: r.Group, Kind: k}
				specs[gk] = transform.InheritanceSpec{Mode: effectiveMode}
			}
		}
	}
	return specs, nil
}

// visitTreeNode recursively hydrates Namespaces by copying inherited resource
// objects down into child Namespaces.
func (i inheritanceSpecs) visitTreeNode(node *ast.TreeNode, inherited []ast.FileObject) status.MultiError {
	var nodeObjs []ast.FileObject
	isNamespace := false
	for _, o := range node.Objects {
		if o.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
			isNamespace = true
		} else if o.GetObjectKind().GroupVersionKind() != kinds.NamespaceSelector() {
			// Don't copy down NamespaceSelectors.
			nodeObjs = append(nodeObjs, o)
		}
	}

	if isNamespace {
		return hydrateNamespace(node, inherited)
	}

	err := i.validateAbstractObjects(nodeObjs)
	inherited = append(inherited, nodeObjs...)
	for _, c := range node.Children {
		err = status.Append(err, i.visitTreeNode(c, inherited))
	}
	return err
}

// validateAbstractObjects returns an error if any invalid objects are declared
// in an abstract namespace.
func (i inheritanceSpecs) validateAbstractObjects(objs []ast.FileObject) status.MultiError {
	var err status.MultiError
	for _, o := range objs {
		gvk := o.GetObjectKind().GroupVersionKind()
		spec, found := i[gvk.GroupKind()]
		if (found && spec.Mode == v1.HierarchyModeNone) && !transform.IsEphemeral(gvk) && !syntax.IsSystemOnly(gvk) {
			err = status.Append(err, validation.IllegalAbstractNamespaceObjectKindError(o))
		}
	}
	return err
}

func hydrateNamespace(node *ast.TreeNode, inherited []ast.FileObject) status.MultiError {
	var err status.MultiError
	for _, child := range node.Children {
		err = status.Append(err, validation.IllegalNamespaceSubdirectoryError(child, node))
	}
	for _, obj := range inherited {
		node.Objects = append(node.Objects, obj.DeepCopy())
	}
	return err
}

// TODO: Move IllegalAbstractNamespaceObjectKindError and  IllegalNamespaceSubdirectoryError here.
