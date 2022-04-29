// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// ResourceStatus wraps an unstructured resource object, combined with the
// computed status (whether the status matches the spec).
type ResourceStatus struct {
	// Resource is the last known value retrieved from the cluster
	Resource *unstructured.Unstructured
	// Status of the resource
	Status status.Status
	// StatusMessage is the human readable reason for the status
	StatusMessage string
}

// ResourceCache stores CachedResource objects
type ResourceCache interface {
	ResourceCacheReader
	// Load one or more resources into the cache, generating the ObjMetadata
	// from the objects.
	Load(...ResourceStatus)
	// Put the resource into the cache using the specified ID.
	Put(object.ObjMetadata, ResourceStatus)
	// Remove the resource associated with the ID from the cache.
	Remove(object.ObjMetadata)
	// Clear the cache.
	Clear()
}

// ResourceCacheReader retrieves CachedResource objects
type ResourceCacheReader interface {
	// Get the resource associated with the ID from the cache.
	// If not cached, status will be Unknown and resource will be nil.
	Get(object.ObjMetadata) ResourceStatus
}
