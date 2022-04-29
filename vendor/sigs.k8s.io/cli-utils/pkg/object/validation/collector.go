// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"errors"

	"sigs.k8s.io/cli-utils/pkg/multierror"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Collector simplifies collecting validation errors from multiple sources and
// extracting the IDs of the invalid objects.
type Collector struct {
	Errors     []error
	InvalidIds object.ObjMetadataSet
}

// Collect unwraps MultiErrors, adds them to Errors, extracts invalid object
// IDs from validation.Error, and adds them to InvalidIds.
func (c *Collector) Collect(err error) {
	errs := multierror.Unwrap(err)
	c.InvalidIds = c.InvalidIds.Union(extractInvalidIds(errs))
	c.Errors = append(c.Errors, errs...)
}

// ToError returns the list of errors as a single error.
func (c *Collector) ToError() error {
	return multierror.Wrap(c.Errors...)
}

// FilterInvalidObjects returns a set of objects that does not contain any
// invalid objects, based on the collected InvalidIds.
func (c *Collector) FilterInvalidObjects(objs object.UnstructuredSet) object.UnstructuredSet {
	var diff object.UnstructuredSet
	for _, obj := range objs {
		if !c.InvalidIds.Contains(object.UnstructuredToObjMetadata(obj)) {
			diff = append(diff, obj)
		}
	}
	return diff
}

// FilterInvalidIds returns a set of object ID that does not contain any
// invalid IDs, based on the collected InvalidIds.
func (c *Collector) FilterInvalidIds(ids object.ObjMetadataSet) object.ObjMetadataSet {
	return ids.Diff(c.InvalidIds)
}

// extractInvalidIds extracts invalid object IDs from a list of possible
// validation.Error.
func extractInvalidIds(errs []error) object.ObjMetadataSet {
	var invalidIds object.ObjMetadataSet
	for _, err := range errs {
		// unwrap recursively looking for a validation.Error
		var vErr *Error
		if errors.As(err, &vErr) {
			invalidIds = invalidIds.Union(vErr.Identifiers())
		}
	}
	return invalidIds
}
