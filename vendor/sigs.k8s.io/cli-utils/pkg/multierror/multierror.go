// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package multierror

import (
	"fmt"
	"strings"
)

const Prefix = "- "
const Indent = "  "

type Interface interface {
	Errors() []error
}

// New returns a new MultiError wrapping the specified error list.
func New(causes ...error) *MultiError {
	return &MultiError{
		Causes: causes,
	}
}

// MultiError wraps multiple errors and formats them for multi-line output.
type MultiError struct {
	Causes []error
}

func (mve *MultiError) Errors() []error {
	return mve.Causes
}

func (mve *MultiError) Error() string {
	if len(mve.Causes) == 1 {
		return mve.Causes[0].Error()
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%d errors:\n", len(mve.Causes))
	for _, err := range mve.Causes {
		_, _ = fmt.Fprintf(&b, "%s\n", formatError(err))
	}
	return b.String()
}

func formatError(err error) string {
	lines := strings.Split(err.Error(), "\n")
	return Prefix + strings.Join(lines, fmt.Sprintf("\n%s", Indent))
}

// Wrap merges zero or more errors and/or MultiErrors into one error.
// MultiErrors are recursively unwrapped to reduce depth.
// If only one error is received, that error is returned without a wrapper.
func Wrap(errs ...error) error {
	if len(errs) == 0 {
		return nil
	}
	errs = Unwrap(errs...)
	var err error
	switch {
	case len(errs) == 0:
		err = nil
	case len(errs) == 1:
		err = errs[0]
	case len(errs) > 1:
		err = &MultiError{
			Causes: errs,
		}
	}
	return err
}

// Unwrap flattens zero or more errors and/or MultiErrors into a list of errors.
// MultiErrors are recursively unwrapped to reduce depth.
func Unwrap(errs ...error) []error {
	if len(errs) == 0 {
		return nil
	}
	var errors []error
	for _, err := range errs {
		if mve, ok := err.(Interface); ok {
			// Recursively unwrap MultiErrors
			for _, cause := range mve.Errors() {
				errors = append(errors, Unwrap(cause)...)
			}
		} else {
			errors = append(errors, err)
		}
	}
	return errors
}
