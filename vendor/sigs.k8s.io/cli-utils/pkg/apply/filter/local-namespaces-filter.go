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

// Filter returns true if the passed object should NOT be pruned (deleted)
// because the it is a namespace that objects still reside in; otherwise
// returns false. This filter should not be added to the list of filters
// for "destroying", since every object is being delete. Never returns an error.
func (lnf LocalNamespacesFilter) Filter(obj *unstructured.Unstructured) (bool, string, error) {
	id := object.UnstructuredToObjMetadata(obj)
	if id.GroupKind == namespaceGK &&
		lnf.LocalNamespaces.Has(id.Name) {
		reason := fmt.Sprintf("namespace still in use (namespace: %q)", id.Name)
		return true, reason, nil
	}
	return false, "", nil
}
