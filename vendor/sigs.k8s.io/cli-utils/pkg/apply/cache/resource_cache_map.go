// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"

	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// ResourceCacheMap stores ResourceStatus objects in a map indexed by resource ID.
// ResourceCacheMap is thread-safe.
type ResourceCacheMap struct {
	mu    sync.RWMutex
	cache map[object.ObjMetadata]ResourceStatus
}

// NewResourceCacheMap returns a new empty ResourceCacheMap
func NewResourceCacheMap() *ResourceCacheMap {
	return &ResourceCacheMap{
		cache: make(map[object.ObjMetadata]ResourceStatus),
	}
}

// Load resources into the cache, generating the ID from the resource itself.
// Existing resources with the same ID will be replaced.
func (rc *ResourceCacheMap) Load(values ...ResourceStatus) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	for _, value := range values {
		id := object.UnstructuredToObjMetadata(value.Resource)
		rc.cache[id] = value
	}
}

// Put the resource into the cache using the supplied ID, replacing any
// existing resource with the same ID.
func (rc *ResourceCacheMap) Put(id object.ObjMetadata, value ResourceStatus) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.cache[id] = value
}

// Get retrieves the resource associated with the ID from the cache.
// Returns (nil, true) if not found in the cache.
func (rc *ResourceCacheMap) Get(id object.ObjMetadata) ResourceStatus {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	obj, found := rc.cache[id]
	if klog.V(4).Enabled() {
		if found {
			klog.Infof("resource cache hit: %s", id)
		} else {
			klog.Infof("resource cache miss: %s", id)
		}
	}
	if !found {
		return ResourceStatus{
			Resource:      nil,
			Status:        status.UnknownStatus,
			StatusMessage: "resource not cached",
		}
	}
	return obj
}

// Remove the resource associated with the ID from the cache.
func (rc *ResourceCacheMap) Remove(id object.ObjMetadata) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	delete(rc.cache, id)
}

// Clear the cache.
func (rc *ResourceCacheMap) Clear() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.cache = make(map[object.ObjMetadata]ResourceStatus)
}
