// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"sort"

	"sigs.k8s.io/cli-utils/pkg/object"
)

// Edge encapsulates a pair of vertices describing a
// directed edge.
type Edge struct {
	From object.ObjMetadata
	To   object.ObjMetadata
}

// SortableEdges sorts a list of edges alphanumerically by From and then To.
type SortableEdges []Edge

var _ sort.Interface = SortableEdges{}

func (a SortableEdges) Len() int      { return len(a) }
func (a SortableEdges) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortableEdges) Less(i, j int) bool {
	if a[i].From != a[j].From {
		return metaIsLessThan(a[i].From, a[j].From)
	}
	return metaIsLessThan(a[i].To, a[j].To)
}

func metaIsLessThan(i, j object.ObjMetadata) bool {
	if i.GroupKind.Group != j.GroupKind.Group {
		return i.GroupKind.Group < j.GroupKind.Group
	}
	if i.GroupKind.Kind != j.GroupKind.Kind {
		return i.GroupKind.Kind < j.GroupKind.Kind
	}
	if i.Namespace != j.Namespace {
		return i.Namespace < j.Namespace
	}
	return i.Name < j.Name
}
