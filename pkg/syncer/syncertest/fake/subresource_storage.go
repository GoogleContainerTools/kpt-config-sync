// Copyright 2023 Google LLC
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

package fake

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SubresourceStorage is a wrapper around MemoryStorage that allows modifying
// a specific top-level field without updating any other fields.
type SubresourceStorage struct {
	// Storage is the backing store for full resource objects
	Storage *MemoryStorage
	// Field is the sub-resource field managed by this SubresourceStorage
	Field string
}

func (ss *SubresourceStorage) getSubresourceInterface(uObj *unstructured.Unstructured) (interface{}, bool, error) {
	return object.NestedField(uObj.Object, ss.Field)
}

func (ss *SubresourceStorage) setSubresourceInterface(uObj *unstructured.Unstructured, value interface{}) error {
	return unstructured.SetNestedField(uObj.Object, value, ss.Field)
}

// Update the sub-resource field. All other fields are ignored.
func (ss *SubresourceStorage) Update(ctx context.Context, obj client.Object, opts *client.UpdateOptions) error {
	ss.Storage.lock.Lock()
	defer ss.Storage.lock.Unlock()

	err := ss.Storage.validateUpdateOptions(opts)
	if err != nil {
		return err
	}

	id, err := lookupObjectID(obj, ss.Storage.scheme)
	if err != nil {
		return err
	}

	cachedObj, found := ss.Storage.objects[id]
	if !found {
		return newNotFound(id)
	}

	storageGVK, err := ss.Storage.storageGVK(obj)
	if err != nil {
		return err
	}

	// Convert to Unstructured and the storage version.
	// Don't use prepareObject, because we don't want to minimize yet.
	uObj, err := kinds.ToUnstructuredWithVersion(obj, storageGVK, ss.Storage.scheme)
	if err != nil {
		return err
	}

	newSubresourceValue, hasSubresource, err := ss.getSubresourceInterface(uObj)
	if err != nil {
		return err
	}

	// TODO: Figure out how to check if the resource in the scheme has this sub-resource.
	if !hasSubresource {
		return errors.Errorf("the %s object %s does not have a %q sub-resource field",
			id.GroupKind, id.ObjectKey, ss.Field)
	}

	if len(opts.DryRun) > 0 {
		// don't merge or store the result
		return nil
	}

	if obj.GetUID() != "" && obj.GetUID() != cachedObj.GetUID() {
		return newConflictingUID(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}
	if obj.GetResourceVersion() != "" && obj.GetResourceVersion() != cachedObj.GetResourceVersion() {
		return newConflictingResourceVersion(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}

	// Copy cached object so we can diff the changes later
	updatedObj := cachedObj.DeepCopy()

	err = incrementResourceVersion(updatedObj)
	if err != nil {
		return errors.Wrap(err, "failed to increment resourceVersion")
	}

	// Assume status doesn't affect generation (don't increment).

	err = ss.setSubresourceInterface(updatedObj, newSubresourceValue)
	if err != nil {
		return err
	}

	// Copy latest values back to input object
	obj.SetUID(updatedObj.GetUID())
	obj.SetResourceVersion(updatedObj.GetResourceVersion())
	obj.SetGeneration(updatedObj.GetGeneration())

	klog.V(5).Infof("Updating Status %s (ResourceVersion: %q)",
		kinds.ObjectSummary(updatedObj), updatedObj.GetResourceVersion())

	err = ss.Storage.putWithoutLock(id, updatedObj)
	if err != nil {
		return err
	}

	return ss.Storage.sendPutEvent(ctx, id, watch.Modified)
}

// Patch the sub-resource field. All other fields are ignored.
func (ss *SubresourceStorage) Patch(_ context.Context, _ client.Object, _ client.Patch, _ *client.PatchOptions) error {
	ss.Storage.lock.Lock()
	defer ss.Storage.lock.Unlock()

	// TODO: Implement sub-resource patch, if needed
	return errors.New("fake.SubresourceStorage.Patch: not yet implemented")
}
