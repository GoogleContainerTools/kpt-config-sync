// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package mutator

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Interface decouples apply-time-mutation
// from the concrete structs used for applying.
type Interface interface {
	// Name returns a filter name (usually for logging).
	Name() string
	// Mutate returns true if the object was mutated.
	// This allows the mutator to decide if mutation is needed.
	// If mutated, a reason string is returned.
	// If an error happens during mutation, it is returned.
	Mutate(ctx context.Context, obj *unstructured.Unstructured) (bool, string, error)
}
