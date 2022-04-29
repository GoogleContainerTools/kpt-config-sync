// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

// CurrentUIDFilter implements ValidationFilter interface to determine
// if an object should not be pruned (deleted) because it has recently
// been applied.
type CurrentUIDFilter struct {
	CurrentUIDs sets.String
}

// Name returns a filter identifier for logging.
func (cuf CurrentUIDFilter) Name() string {
	return "CurrentUIDFilter"
}

// Filter returns true if the passed object should NOT be pruned (deleted)
// because it has recently been applied.
func (cuf CurrentUIDFilter) Filter(obj *unstructured.Unstructured) (bool, string, error) {
	uid := string(obj.GetUID())
	if cuf.CurrentUIDs.Has(uid) {
		reason := fmt.Sprintf("resource just applied (UID: %q)", uid)
		return true, reason, nil
	}
	return false, "", nil
}
