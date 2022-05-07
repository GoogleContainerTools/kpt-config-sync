// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

// FatalError is a wrapper for filters to indicate an error is unrecoverable,
// not just a reason to skip actuation.
type FatalError struct {
	Err error
}

func NewFatalError(err error) *FatalError {
	return &FatalError{Err: err}
}

func (e *FatalError) Error() string {
	return e.Err.Error()
}

func (e *FatalError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*FatalError)
	if !ok {
		return false
	}
	return e.Err == tErr.Err
}
