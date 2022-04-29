// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package manifestreader

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/kustomize/kyaml/kio/filters"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// UnknownTypesError captures information about unknown types encountered.
type UnknownTypesError struct {
	GroupVersionKinds []schema.GroupVersionKind
}

func (e *UnknownTypesError) Error() string {
	var gvks []string
	for _, gvk := range e.GroupVersionKinds {
		gvks = append(gvks, fmt.Sprintf("%s/%s/%s",
			gvk.Group, gvk.Version, gvk.Kind))
	}
	return fmt.Sprintf("unknown resource types: %s", strings.Join(gvks, ","))
}

// NamespaceMismatchError is returned if all resources must be in a specific
// namespace, and resources are found using other namespaces.
type NamespaceMismatchError struct {
	RequiredNamespace string
	Namespace         string
}

func (e *NamespaceMismatchError) Error() string {
	return fmt.Sprintf("found namespace %q, but all resources must be in namespace %q",
		e.Namespace, e.RequiredNamespace)
}

// SetNamespaces verifies that every namespaced resource has the namespace
// set, and if one does not, it will set the namespace to the provided
// defaultNamespace.
// This implementation will check each resource (that doesn't already have
// the namespace set) on whether it is namespace or cluster scoped. It does
// this by first checking the RESTMapper, and it there is not match there,
// it will look for CRDs in the provided Unstructureds.
func SetNamespaces(mapper meta.RESTMapper, objs []*unstructured.Unstructured,
	defaultNamespace string, enforceNamespace bool) error {
	var crdObjs []*unstructured.Unstructured

	// find any crds in the set of resources.
	for _, obj := range objs {
		if object.IsCRD(obj) {
			crdObjs = append(crdObjs, obj)
		}
	}

	var unknownGVKs []schema.GroupVersionKind
	for _, obj := range objs {
		// Exclude any inventory objects here since we don't want to change
		// their namespace.
		if inventory.IsInventoryObject(obj) {
			continue
		}

		// Look up the scope of the resource so we know if the resource
		// should have a namespace set or not.
		scope, err := object.LookupResourceScope(obj, crdObjs, mapper)
		if err != nil {
			var unknownTypeError *object.UnknownTypeError
			if errors.As(err, &unknownTypeError) {
				// If no scope was found, just add the resource type to the list
				// of unknown types.
				unknownGVKs = append(unknownGVKs, unknownTypeError.GroupVersionKind)
				continue
			} else {
				// If something went wrong when looking up the scope, just
				// give up.
				return err
			}
		}

		switch scope {
		case meta.RESTScopeNamespace:
			if obj.GetNamespace() == "" {
				obj.SetNamespace(defaultNamespace)
			} else {
				ns := obj.GetNamespace()
				if enforceNamespace && ns != defaultNamespace {
					return &NamespaceMismatchError{
						Namespace:         ns,
						RequiredNamespace: defaultNamespace,
					}
				}
			}
		case meta.RESTScopeRoot:
			if ns := obj.GetNamespace(); ns != "" {
				return fmt.Errorf("resource is cluster-scoped but has a non-empty namespace %q", ns)
			}
		default:
			return fmt.Errorf("unknown RESTScope %q", scope.Name())
		}
	}
	if len(unknownGVKs) > 0 {
		return &UnknownTypesError{
			GroupVersionKinds: unknownGVKs,
		}
	}
	return nil
}

// FilterLocalConfig returns a new slice of Unstructured where all resources
// with the LocalConfig annotation is filtered out.
func FilterLocalConfig(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	var filteredObjs []*unstructured.Unstructured
	for _, obj := range objs {
		// Ignoring the value of the LocalConfigAnnotation here. This is to be
		// consistent with the behavior in the kyaml library:
		// https://github.com/kubernetes-sigs/kustomize/blob/30b58e90a39485bc5724b2278651c5d26b815cb2/kyaml/kio/filters/local.go#L29
		if _, found := obj.GetAnnotations()[filters.LocalConfigAnnotation]; !found {
			filteredObjs = append(filteredObjs, obj)
		}
	}
	return filteredObjs
}

// RemoveAnnotations removes the specified kioutil annotations from the resource.
func RemoveAnnotations(n *yaml.RNode, annotations ...kioutil.AnnotationKey) error {
	for _, a := range annotations {
		err := n.PipeE(yaml.ClearAnnotation(a))
		if err != nil {
			return err
		}
	}
	return nil
}

// KyamlNodeToUnstructured take a resource represented as a kyaml RNode and
// turns it into an Unstructured object.
func KyamlNodeToUnstructured(n *yaml.RNode) (*unstructured.Unstructured, error) {
	b, err := n.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var m map[string]interface{}
	err = json.Unmarshal(b, &m)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{
		Object: m,
	}, nil
}
