// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

// This package provides a graph data struture
// and graph functionality using ObjMetadata as
// vertices in the graph.
package graph

import (
	"sort"

	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/validation"
	"sigs.k8s.io/cli-utils/pkg/ordering"
)

// Graph is contains a directed set of edges, implemented as
// an adjacency list (map key is "from" vertex, slice are "to"
// vertices).
type Graph struct {
	// map "from" vertex -> list of "to" vertices
	edges map[object.ObjMetadata]object.ObjMetadataSet
	// map "to" vertex -> list of "from" vertices
	reverseEdges map[object.ObjMetadata]object.ObjMetadataSet
}

// New returns a pointer to an empty Graph data structure.
func New() *Graph {
	g := &Graph{}
	g.edges = make(map[object.ObjMetadata]object.ObjMetadataSet)
	g.reverseEdges = make(map[object.ObjMetadata]object.ObjMetadataSet)
	return g
}

// AddVertex adds an ObjMetadata vertex to the graph, with
// an initial empty set of edges from added vertex.
func (g *Graph) AddVertex(v object.ObjMetadata) {
	if _, exists := g.edges[v]; !exists {
		g.edges[v] = object.ObjMetadataSet{}
	}
	if _, exists := g.reverseEdges[v]; !exists {
		g.reverseEdges[v] = object.ObjMetadataSet{}
	}
}

// edgeMapKeys returns a sorted set of unique vertices in the graph.
func edgeMapKeys(edgeMap map[object.ObjMetadata]object.ObjMetadataSet) object.ObjMetadataSet {
	keys := make(object.ObjMetadataSet, len(edgeMap))
	i := 0
	for k := range edgeMap {
		keys[i] = k
		i++
	}
	sort.Sort(ordering.SortableMetas(keys))
	return keys
}

// AddEdge adds a edge from one ObjMetadata vertex to another. The
// direction of the edge is "from" -> "to".
func (g *Graph) AddEdge(from object.ObjMetadata, to object.ObjMetadata) {
	// Add "from" vertex if it doesn't already exist.
	if _, exists := g.edges[from]; !exists {
		g.edges[from] = object.ObjMetadataSet{}
	}
	if _, exists := g.reverseEdges[from]; !exists {
		g.reverseEdges[from] = object.ObjMetadataSet{}
	}
	// Add "to" vertex if it doesn't already exist.
	if _, exists := g.edges[to]; !exists {
		g.edges[to] = object.ObjMetadataSet{}
	}
	if _, exists := g.reverseEdges[to]; !exists {
		g.reverseEdges[to] = object.ObjMetadataSet{}
	}
	// Add edge "from" -> "to" if it doesn't already exist
	// into the adjacency list.
	if !g.isAdjacent(from, to) {
		g.edges[from] = append(g.edges[from], to)
		g.reverseEdges[to] = append(g.reverseEdges[to], from)
	}
}

// edgeMapToList returns a sorted slice of directed graph edges (vertex pairs).
func edgeMapToList(edgeMap map[object.ObjMetadata]object.ObjMetadataSet) []Edge {
	edges := []Edge{}
	for from, toList := range edgeMap {
		for _, to := range toList {
			edge := Edge{From: from, To: to}
			edges = append(edges, edge)
		}
	}
	sort.Sort(SortableEdges(edges))
	return edges
}

// isAdjacent returns true if an edge "from" vertex -> "to" vertex exists;
// false otherwise.
func (g *Graph) isAdjacent(from object.ObjMetadata, to object.ObjMetadata) bool {
	// If "from" vertex does not exist, it is impossible edge exists; return false.
	if _, exists := g.edges[from]; !exists {
		return false
	}
	// Iterate through adjacency list to see if "to" vertex is adjacent.
	for _, vertex := range g.edges[from] {
		if vertex == to {
			return true
		}
	}
	return false
}

// Size returns the number of vertices in the graph.
func (g *Graph) Size() int {
	return len(g.edges)
}

// removeVertex removes the passed vertex as well as any edges into the vertex.
func removeVertex(edges map[object.ObjMetadata]object.ObjMetadataSet, r object.ObjMetadata) {
	// First, remove the object from all adjacency lists.
	for v, adj := range edges {
		edges[v] = adj.Remove(r)
	}
	// Finally, remove the vertex
	delete(edges, r)
}

// Dependencies returns the objects that this object depends on.
func (g *Graph) Dependencies(from object.ObjMetadata) object.ObjMetadataSet {
	edgesFrom, exists := g.edges[from]
	if !exists {
		return nil
	}
	c := make(object.ObjMetadataSet, len(edgesFrom))
	copy(c, edgesFrom)
	return c
}

// Dependents returns the objects that depend on this object.
func (g *Graph) Dependents(to object.ObjMetadata) object.ObjMetadataSet {
	edgesTo, exists := g.reverseEdges[to]
	if !exists {
		return nil
	}
	c := make(object.ObjMetadataSet, len(edgesTo))
	copy(c, edgesTo)
	return c
}

// Sort returns the ordered set of vertices after a topological sort.
func (g *Graph) Sort() ([]object.ObjMetadataSet, error) {
	// deep copy edge map to avoid destructive sorting
	edges := make(map[object.ObjMetadata]object.ObjMetadataSet, len(g.edges))
	for vertex, deps := range g.edges {
		c := make(object.ObjMetadataSet, len(deps))
		copy(c, deps)
		edges[vertex] = c
	}

	sorted := []object.ObjMetadataSet{}
	for len(edges) > 0 {
		// Identify all the leaf vertices.
		leafVertices := object.ObjMetadataSet{}
		for v, adj := range edges {
			if len(adj) == 0 {
				leafVertices = append(leafVertices, v)
			}
		}
		// No leaf vertices means cycle in the directed graph,
		// where remaining edges define the cycle.
		if len(leafVertices) == 0 {
			// Error can be ignored, so return the full set list
			return sorted, validation.NewError(CyclicDependencyError{
				Edges: edgeMapToList(edges),
			}, edgeMapKeys(edges)...)
		}
		// Remove all edges to leaf vertices.
		for _, v := range leafVertices {
			removeVertex(edges, v)
		}
		sorted = append(sorted, leafVertices)
	}
	return sorted, nil
}
