// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/cli-utils/pkg/object"
)

var (
	namespaceGK = schema.GroupKind{Group: "", Kind: "Namespace"}
)

// LocalNamespacesFilter encapsulates the set of namespaces
// that are currently in use. Used to ensure we do not delete
// namespaces with currently applied objects in them.
type LocalNamespacesFilter struct {
	LocalNamespaces sets.String
}

// Name returns a filter identifier for logging.
func (lnf LocalNamespacesFilter) Name() string {
	return "LocalNamespacesFilter"
}

// Filter returns a NamespaceInUseError if the object prune/delete should be
// skipped.
func (lnf LocalNamespacesFilter) Filter(obj *unstructured.Unstructured) error {
	id := object.UnstructuredToObjMetadata(obj)
	if id.GroupKind == namespaceGK &&
		lnf.LocalNamespaces.Has(id.Name) {
		return &NamespaceInUseError{
			Namespace: id.Name,
		}
	}
	return nil
}

type NamespaceInUseError struct {
	Namespace string
}

func (e *NamespaceInUseError) Error() string {
	return fmt.Sprintf("namespace still in use: %s", e.Namespace)
}

func (e *NamespaceInUseError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*NamespaceInUseError)
	if !ok {
		return false
	}
	return e.Namespace == tErr.Namespace
}
