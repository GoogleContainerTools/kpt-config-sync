// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package declared

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kpt.dev/configsync/pkg/core"
)

// Resources is a threadsafe container for a set of resources declared in a Git
// repo.
type Resources struct {
	mutex sync.RWMutex
	// objectSet is a map of object IDs to the unstructured format of those
	// objects. Note that the pointer to this map is threadsafe but the map itself
	// is not threadsafe. This map should never be returned from a function
	// directly. The map should never be written to once it has been assigned to
	// this reference; it should be treated as read-only from then on.
	objectSet map[core.ID]*unstructured.Unstructured
	// commit of the source in which the resources were declared
	commit string
}

// Update performs an atomic update on the resource declaration set.
func (r *Resources) Update(ctx context.Context, objects []client.Object, commit string) ([]client.Object, status.Error) {
	// First build up the new map using a local pointer/reference.
	newSet := make(map[core.ID]*unstructured.Unstructured)
	newObjects := []client.Object{}
	for _, obj := range objects {
		if obj == nil {
			klog.Warning("Resources received nil declared resource")
			metrics.RecordInternalError(ctx, "parser")
			continue
		}
		id := core.IDOf(obj)
		u, err := reconcile.AsUnstructuredSanitized(obj)
		if err != nil {
			// This should never happen.
			return nil, status.InternalErrorBuilder.Wrap(err).
				Sprintf("converting %v to unstructured.Unstructured", id).Build()
		}
		newSet[id] = u
		newObjects = append(newObjects, obj)
	}

	// Record the declared_resources metric, after parsing but before validation.
	metrics.RecordDeclaredResources(ctx, commit, len(newObjects))

	previousSet, _ := r.getObjectSet()
	if err := deletesAllNamespaces(previousSet, newSet); err != nil {
		return nil, err
	}

	// Now assign the pointer for the new map to the struct reference in a
	// threadsafe context. From now on, this map is read-only.
	r.setObjectSet(newSet, commit)
	return newObjects, nil
}

// Get returns a copy of the resource declaration as read from Git
func (r *Resources) Get(id core.ID) (*unstructured.Unstructured, string, bool) {
	objSet, commit := r.getObjectSet()

	// A local reference to the map is threadsafe since only the struct reference
	// is replaced on update.
	u, found := objSet[id]
	// We return a copy of the Unstructured, as
	// 1) client.Client methods mutate the objects passed into them.
	// 2) We don't want to persist any changes made to an object we retrieved
	//  from a declared.Resources.
	return u.DeepCopy(), commit, found
}

// DeclaredUnstructureds returns all resource objects declared in the source,
// along with the source commit.
func (r *Resources) DeclaredUnstructureds() ([]*unstructured.Unstructured, string) {
	var objects []*unstructured.Unstructured
	objSet, commit := r.getObjectSet()

	// A local reference to the map is threadsafe since only the struct reference
	// is replaced on update.
	for _, obj := range objSet {
		objects = append(objects, obj)
	}
	return objects, commit
}

// DeclaredObjects returns all resource objects declared in the source, along
// with the source commit.
func (r *Resources) DeclaredObjects() ([]client.Object, string) {
	var objects []client.Object
	objSet, commit := r.getObjectSet()

	// A local reference to the map is threadsafe since only the struct reference
	// is replaced on update.
	for _, obj := range objSet {
		objects = append(objects, obj)
	}
	return objects, commit
}

// DeclaredGVKs returns the set of all GroupVersionKind found in the source,
// along with the source commit.
func (r *Resources) DeclaredGVKs() (map[schema.GroupVersionKind]struct{}, string) {
	gvkSet := make(map[schema.GroupVersionKind]struct{})
	objSet, commit := r.getObjectSet()

	// A local reference to the objSet map is threadsafe since only the pointer to
	// the map is replaced on update.
	for _, obj := range objSet {
		gvkSet[obj.GroupVersionKind()] = struct{}{}
	}
	return gvkSet, commit
}

func (r *Resources) getObjectSet() (map[core.ID]*unstructured.Unstructured, string) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.objectSet, r.commit
}

func (r *Resources) setObjectSet(objectSet map[core.ID]*unstructured.Unstructured, commit string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.objectSet = objectSet
	r.commit = commit
}
