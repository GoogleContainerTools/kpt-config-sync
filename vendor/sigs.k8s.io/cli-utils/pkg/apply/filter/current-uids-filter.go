// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
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

// Filter returns a ApplyPreventedDeletionError if the object prune/delete
// should be skipped.
func (cuf CurrentUIDFilter) Filter(obj *unstructured.Unstructured) error {
	uid := obj.GetUID()
	if cuf.CurrentUIDs.Has(string(uid)) {
		return &ApplyPreventedDeletionError{UID: uid}
	}
	return nil
}

type ApplyPreventedDeletionError struct {
	UID types.UID
}

func (e *ApplyPreventedDeletionError) Error() string {
	return fmt.Sprintf("object just applied (UID: %q)", e.UID)
}

func (e *ApplyPreventedDeletionError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*ApplyPreventedDeletionError)
	if !ok {
		return false
	}
	return e.UID == tErr.UID
}
