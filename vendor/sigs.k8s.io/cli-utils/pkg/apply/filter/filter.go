// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ValidationFilter interface decouples apply/prune validation
// from the concrete structs used for validation. The apply/prune
// functionality will run validation filters to remove objects
// which should not be applied or pruned.
type ValidationFilter interface {
	// Name returns a filter name (usually for logging).
	Name() string
	// Filter returns true if validation fails. If true a
	// reason string is included in the return. If an error happens
	// during filtering it is returned.
	Filter(obj *unstructured.Unstructured) (bool, string, error)
}
