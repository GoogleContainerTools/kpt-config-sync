// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package validation

//go:generate stringer -type=Policy
type Policy int

const (
	// ExitEarly policy errors and exits if any objects are invalid, before
	// apply/delete of any objects.
	ExitEarly Policy = iota

	// SkipInvalid policy skips the apply/delete of invalid objects.
	SkipInvalid
)
