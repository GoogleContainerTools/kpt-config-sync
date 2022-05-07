// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/common"
)

// PreventRemoveFilter implements ValidationFilter interface to determine
// if an object should not be pruned (deleted) because of a
// "prevent remove" annotation.
type PreventRemoveFilter struct{}

const PreventRemoveFilterName = "PreventRemoveFilter"

// Name returns the preferred name for the filter. Usually
// used for logging.
func (prf PreventRemoveFilter) Name() string {
	return PreventRemoveFilterName
}

// Filter returns a AnnotationPreventedDeletionError if the object prune/delete
// should be skipped.
func (prf PreventRemoveFilter) Filter(obj *unstructured.Unstructured) error {
	for annotation, value := range obj.GetAnnotations() {
		if common.NoDeletion(annotation, value) {
			return &AnnotationPreventedDeletionError{
				Annotation: annotation,
				Value:      value,
			}
		}
	}
	return nil
}

type AnnotationPreventedDeletionError struct {
	Annotation string
	Value      string
}

func (e *AnnotationPreventedDeletionError) Error() string {
	return fmt.Sprintf("annotation prevents deletion (%q: %q)", e.Annotation, e.Value)
}

func (e *AnnotationPreventedDeletionError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*AnnotationPreventedDeletionError)
	if !ok {
		return false
	}
	return e.Annotation == tErr.Annotation &&
		e.Value == tErr.Value
}
