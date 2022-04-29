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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// NamespaceSelectors hydrates the given Scoped objects by performing namespace
// selection to copy objects into namespaces which match their selector. It also
// sets a default namespace on any namespace-scoped object that does not already
// have a namespace or namespace selector set.
func NamespaceSelectors(objs *objects.Scoped) status.MultiError {
	nsSelectors, errs := buildSelectorMap(objs)
	if errs != nil {
		return errs
	}
	var result []ast.FileObject
	for _, obj := range objs.Namespace {
		_, hasSelector := obj.GetAnnotations()[metadata.NamespaceSelectorAnnotationKey]
		if hasSelector {
			copies, err := makeNamespaceCopies(obj, nsSelectors)
			if err != nil {
				errs = status.Append(errs, err)
			} else {
				result = append(result, copies...)
			}
		} else {
			if obj.GetNamespace() == "" {
				obj.SetNamespace(objs.DefaultNamespace)
			}
			result = append(result, obj)
		}
	}
	if errs != nil {
		return errs
	}
	objs.Namespace = result
	return nil
}

// buildSelectorMap processes the given cluster-scoped objects to return a map
// of NamespaceSelector names to the namespaces that are selected by each one.
// Note that this modifies the Scoped objects to filter out the
// NamespaceSelectors since they are no longer needed after this point.
func buildSelectorMap(objs *objects.Scoped) (map[string][]string, status.MultiError) {
	var namespaces, nsSelectors, others []ast.FileObject
	for _, obj := range objs.Cluster {
		switch obj.GetObjectKind().GroupVersionKind() {
		case kinds.Namespace():
			namespaces = append(namespaces, obj)
		case kinds.NamespaceSelector():
			nsSelectors = append(nsSelectors, obj)
		default:
			others = append(others, obj)
		}
	}

	var errs status.MultiError
	selectorMap := make(map[string][]string)

	for _, obj := range nsSelectors {
		var selected []string
		selector, err := labelSelector(obj)
		if err != nil {
			errs = status.Append(errs, err)
			continue
		}

		for _, namespace := range namespaces {
			if selector.Matches(labels.Set(namespace.GetLabels())) {
				selected = append(selected, namespace.GetName())
			}
		}

		selectorMap[obj.GetName()] = selected
	}

	if errs != nil {
		return nil, errs
	}

	// We are done with NamespaceSelectors so we can filter them out now.
	objs.Cluster = append(namespaces, others...)
	return selectorMap, nil
}

func labelSelector(obj ast.FileObject) (labels.Selector, status.Error) {
	s, sErr := obj.Structured()
	if sErr != nil {
		return nil, sErr
	}
	nss := s.(*v1.NamespaceSelector)

	selector, err := metav1.LabelSelectorAsSelector(&nss.Spec.Selector)
	if err != nil {
		return nil, selectors.InvalidSelectorError(obj, err)
	}
	if selector.Empty() {
		return nil, selectors.EmptySelectorError(obj)
	}
	return selector, nil
}

// makeNamespaceCopies uses the given object's namespace selector to make a copy
// of it into each namespace that is selected by it.
func makeNamespaceCopies(obj ast.FileObject, nsSelectors map[string][]string) ([]ast.FileObject, status.Error) {
	selector := obj.GetAnnotations()[metadata.NamespaceSelectorAnnotationKey]
	selected, exists := nsSelectors[selector]
	if !exists {
		return nil, selectors.ObjectHasUnknownNamespaceSelector(obj, selector)
	}

	var result []ast.FileObject
	for _, ns := range selected {
		objCopy := obj.DeepCopy()
		objCopy.SetNamespace(ns)
		result = append(result, objCopy)
	}
	return result, nil
}
