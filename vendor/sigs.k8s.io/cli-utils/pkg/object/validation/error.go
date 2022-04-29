// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"strings"

	"sigs.k8s.io/cli-utils/pkg/object"
)

func NewError(cause error, ids ...object.ObjMetadata) *Error {
	return &Error{
		ids:   object.ObjMetadataSet(ids),
		cause: cause,
	}
}

// Error wraps an error with the object or objects it applies to.
type Error struct {
	ids   object.ObjMetadataSet
	cause error
}

// Identifiers returns zero or more object IDs which are invalid.
func (ve *Error) Identifiers() object.ObjMetadataSet {
	return ve.ids
}

// Unwrap returns the cause of the error.
// This may be useful when printing the cause without printing the identifiers.
func (ve *Error) Unwrap() error {
	return ve.cause
}

// Error stringifies the the error.
func (ve *Error) Error() string {
	switch {
	case len(ve.ids) == 0:
		return fmt.Sprintf("validation error: %v", ve.cause.Error())
	case len(ve.ids) == 1:
		return fmt.Sprintf("invalid object: %q: %v", ve.ids[0], ve.cause.Error())
	default:
		var b strings.Builder
		_, _ = fmt.Fprintf(&b, "invalid objects: [%q", ve.ids[0])
		for _, id := range ve.ids[1:] {
			_, _ = fmt.Fprintf(&b, ", %q", id)
		}
		_, _ = fmt.Fprintf(&b, "] %v", ve.cause)
		return b.String()
	}
}
