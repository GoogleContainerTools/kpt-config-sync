// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package taskrunner

import (
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/apply/cache"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/graph"
)

// NewTaskContext returns a new TaskContext
func NewTaskContext(eventChannel chan event.Event, resourceCache cache.ResourceCache) *TaskContext {
	return &TaskContext{
		taskChannel:      make(chan TaskResult),
		eventChannel:     eventChannel,
		resourceCache:    resourceCache,
		inventoryManager: inventory.NewManager(),
		abandonedObjects: make(map[object.ObjMetadata]struct{}),
		invalidObjects:   make(map[object.ObjMetadata]struct{}),
		graph:            graph.New(),
	}
}

// TaskContext defines a context that is passed between all
// the tasks that is in a taskqueue.
type TaskContext struct {
	taskChannel      chan TaskResult
	eventChannel     chan event.Event
	resourceCache    cache.ResourceCache
	inventoryManager *inventory.Manager
	abandonedObjects map[object.ObjMetadata]struct{}
	invalidObjects   map[object.ObjMetadata]struct{}
	graph            *graph.Graph
}

func (tc *TaskContext) TaskChannel() chan TaskResult {
	return tc.taskChannel
}

func (tc *TaskContext) EventChannel() chan event.Event {
	return tc.eventChannel
}

func (tc *TaskContext) ResourceCache() cache.ResourceCache {
	return tc.resourceCache
}

func (tc *TaskContext) InventoryManager() *inventory.Manager {
	return tc.inventoryManager
}

func (tc *TaskContext) Graph() *graph.Graph {
	return tc.graph
}

func (tc *TaskContext) SetGraph(g *graph.Graph) {
	tc.graph = g
}

// SendEvent sends an event on the event channel
func (tc *TaskContext) SendEvent(e event.Event) {
	klog.V(5).Infof("sending event: %s", e)
	tc.eventChannel <- e
}

// IsAbandonedObject returns true if the object is abandoned
func (tc *TaskContext) IsAbandonedObject(id object.ObjMetadata) bool {
	_, found := tc.abandonedObjects[id]
	return found
}

// AddAbandonedObject registers that the object is abandoned
func (tc *TaskContext) AddAbandonedObject(id object.ObjMetadata) {
	tc.abandonedObjects[id] = struct{}{}
}

// AbandonedObjects returns all the abandoned objects
func (tc *TaskContext) AbandonedObjects() object.ObjMetadataSet {
	return object.ObjMetadataSetFromMap(tc.abandonedObjects)
}

// IsInvalidObject returns true if the object is abandoned
func (tc *TaskContext) IsInvalidObject(id object.ObjMetadata) bool {
	_, found := tc.invalidObjects[id]
	return found
}

// AddInvalidObject registers that the object is abandoned
func (tc *TaskContext) AddInvalidObject(id object.ObjMetadata) {
	tc.invalidObjects[id] = struct{}{}
}

// InvalidObjects returns all the abandoned objects
func (tc *TaskContext) InvalidObjects() object.ObjMetadataSet {
	return object.ObjMetadataSetFromMap(tc.invalidObjects)
}
