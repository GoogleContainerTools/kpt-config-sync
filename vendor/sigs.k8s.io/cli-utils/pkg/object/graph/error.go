// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"bytes"
	"fmt"

	"sigs.k8s.io/cli-utils/pkg/multierror"
	"sigs.k8s.io/cli-utils/pkg/object/mutation"
)

// ExternalDependencyError represents an invalid graph edge caused by an
// object that is not in the object set.
type ExternalDependencyError struct {
	Edge Edge
}

func (ede ExternalDependencyError) Error() string {
	return fmt.Sprintf("external dependency: %s -> %s",
		mutation.ResourceReferenceFromObjMetadata(ede.Edge.From),
		mutation.ResourceReferenceFromObjMetadata(ede.Edge.To))
}

// CyclicDependencyError represents a cycle in the graph, making topological
// sort impossible.
type CyclicDependencyError struct {
	Edges []Edge
}

func (cde CyclicDependencyError) Error() string {
	var errorBuf bytes.Buffer
	errorBuf.WriteString("cyclic dependency:")
	for _, edge := range cde.Edges {
		errorBuf.WriteString(fmt.Sprintf("\n%s%s -> %s", multierror.Prefix,
			mutation.ResourceReferenceFromObjMetadata(edge.From),
			mutation.ResourceReferenceFromObjMetadata(edge.To)))
	}
	return errorBuf.String()
}

// DuplicateDependencyError represents an invalid depends-on annotation with
// duplicate references.
type DuplicateDependencyError struct {
	Edge Edge
}

func (dde DuplicateDependencyError) Error() string {
	return fmt.Sprintf("duplicate dependency: %s -> %s",
		mutation.ResourceReferenceFromObjMetadata(dde.Edge.From),
		mutation.ResourceReferenceFromObjMetadata(dde.Edge.To))
}
