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
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RealNow is the default Now function used by NewClient.
func RealNow() metav1.Time {
	return metav1.Now()
}

// MemoryStorage is an in-memory simulation of the Kubernetes APIServer.
//
// Many resource-agnostic features are implemented to facilitate testing
// controllers and other libraries.
// No resource-specific controller behaviors are implemented.
type MemoryStorage struct {
	scheme          *runtime.Scheme
	watchSupervisor *WatchSupervisor

	// Now is a hook to replace time.Now for testing.
	// Default impl is RealNow.
	Now func() metav1.Time

	lock sync.RWMutex
	// objects caches the stored objects in memory.
	// The map is indexed by ID.
	// TODO: The version stored should be the storage version according to the scheme.
	objects map[core.ID]*unstructured.Unstructured
}

// NewInMemoryStorage constructs a new MemoryStorage
func NewInMemoryStorage(scheme *runtime.Scheme, watchSupervisor *WatchSupervisor) *MemoryStorage {
	return &MemoryStorage{
		scheme:          scheme,
		watchSupervisor: watchSupervisor,
		Now:             RealNow,
		objects:         make(map[core.ID]*unstructured.Unstructured),
	}
}

// TestGet gets an object from storage, without validation or type conversion.
// Use for testing what exactly is in storage.
// The object is NOT a copy. Use with caution.
func (ms *MemoryStorage) TestGet(id core.ID) (*unstructured.Unstructured, bool) {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	uObj, found := ms.objects[id]
	return uObj, found
}

// TestGetAll gets all objects from storage, without validation or type conversion.
// Use for testing what exactly is in storage.
// The map is a copy, but the objects are NOT copies. Use with caution.
func (ms *MemoryStorage) TestGetAll() map[core.ID]*unstructured.Unstructured {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	mapCopy := make(map[core.ID]*unstructured.Unstructured, len(ms.objects))
	for id, uObj := range ms.objects {
		mapCopy[id] = uObj
	}
	return mapCopy
}

// TestPut adds or replaces an object in storage without sending events or
// triggering garbage collection.
// Use for initializing or replacing what exactly is in storage.
func (ms *MemoryStorage) TestPut(obj client.Object) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	id, err := lookupObjectID(obj, ms.scheme)
	if err != nil {
		return err
	}
	err = ms.putWithoutLock(id, obj)
	if err != nil {
		return errors.Wrapf(err, "failed to put object into storage: %s %s",
			id.Kind, id.ObjectKey)
	}
	return nil
}

// TestPutAll adds or replaces multiple objects in storage without sending
// events or triggering garbage collection.
// Use for initializing or replacing what exactly is in storage.
func (ms *MemoryStorage) TestPutAll(objs ...client.Object) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	for _, obj := range objs {
		id, err := lookupObjectID(obj, ms.scheme)
		if err != nil {
			return err
		}
		err = ms.putWithoutLock(id, obj)
		if err != nil {
			return errors.Wrapf(err, "failed to put object into storage: %s %s",
				id.Kind, id.ObjectKey)
		}
	}
	return nil
}

func (ms *MemoryStorage) putWithoutLock(id core.ID, obj client.Object) error {
	cachedObj := ms.objects[id]
	uObj, err := ms.prepareObject(obj)
	if err != nil {
		return err
	}
	klog.V(5).Infof("MemoryStorage.Put (%s): Diff (- Old, + New):\n%s",
		id, log.AsYAMLDiffWithScheme(cachedObj, uObj, ms.scheme))
	ms.objects[id] = uObj
	return nil
}

// prepareObject converts the object to the scheme-preferred version, converts
// to unstructured, and removes all nil fields recursively.
func (ms *MemoryStorage) prepareObject(obj client.Object) (*unstructured.Unstructured, error) {
	storageGVK, err := ms.storageGVK(obj)
	if err != nil {
		return nil, err
	}

	// Convert to Unstructured with the scheme-preferred version
	uObj, err := kinds.ToUnstructuredWithVersion(obj, storageGVK, ms.scheme)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert to scheme-preferred version")
	}

	MinimizeUnstructured(uObj)

	return uObj, nil
}

// storageGVK returns the preferred GVK for storage, according to the scheme.
// Note: This may not match upstream Kubernetes, because k8s.io/api does not
// include all conversion code or all internal versions.
// As long as this is consistent, and not lossy, it should be fine for testing.
func (ms *MemoryStorage) storageGVK(obj client.Object) (schema.GroupVersionKind, error) {
	gvk, err := kinds.Lookup(obj, ms.scheme)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}

	// Store as highest-priority version with the kind registered
	for _, gv := range ms.scheme.PrioritizedVersionsForGroup(gvk.Group) {
		priorityGVK := gv.WithKind(gvk.Kind)
		if ms.scheme.Recognizes(gv.WithKind(gvk.Kind)) {
			return priorityGVK, nil
		}
	}
	// Kind not found in any of the prioritized versions for this group.
	// Probably means SetVersionPriority wasn't called with this GVK.
	// This is true for most groups with only one version.
	return gvk, nil
}

// sendPutEvent sends added/modified events and triggers (foreground) garbage collection.
func (ms *MemoryStorage) sendPutEvent(ctx context.Context, id core.ID, eventType watch.EventType) error {
	if eventType != watch.Added && eventType != watch.Modified {
		return errors.Errorf("sendPutEvent: invalid EventType: %v", eventType)
	}

	uObj := ms.objects[id]

	// Send event to watchers
	// TODO: send the event asynchronously, even if the caller cancelled the context.
	ms.watchSupervisor.Send(ctx, id.GroupKind, watch.Event{
		Type:   eventType,
		Object: uObj,
	})

	// TODO: support background garbage collection & orphan

	// Simulate apiserver garbage collection
	if uObj.GetDeletionTimestamp() != nil && len(uObj.GetFinalizers()) == 0 {
		klog.V(5).Infof("Found deleteTimestamp and 0 finalizers: Deleting %s (ResourceVersion: %q)",
			kinds.ObjectSummary(uObj), uObj.GetResourceVersion())
		err := ms.deleteWithoutLock(ctx, uObj, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ms *MemoryStorage) listObjects(gk schema.GroupKind) []*unstructured.Unstructured {
	var result []*unstructured.Unstructured
	for _, o := range ms.objects {
		if o.GetObjectKind().GroupVersionKind().GroupKind() != gk {
			continue
		}
		result = append(result, o)
	}
	return result
}

// Get an object from storage
func (ms *MemoryStorage) Get(_ context.Context, gvk schema.GroupVersionKind, key client.ObjectKey, obj client.Object, opts *client.GetOptions) error {
	ms.lock.RLock()
	defer ms.lock.RUnlock()

	err := ms.validateGetOptions(opts)
	if err != nil {
		return err
	}
	id := core.ID{
		GroupKind: gvk.GroupKind(),
		ObjectKey: key,
	}
	cachedObj, ok := ms.objects[id]
	if !ok {
		return newNotFound(id)
	}
	klog.V(6).Infof("Getting(cached) %s (Generation: %v, ResourceVersion: %q): %s",
		kinds.ObjectSummary(cachedObj),
		cachedObj.GetGeneration(), cachedObj.GetResourceVersion(),
		log.AsJSON(cachedObj))

	// Convert to a typed object, optionally convert between versions
	tObj, err := kinds.ToTypedWithVersion(cachedObj, gvk, ms.scheme)
	if err != nil {
		return err
	}

	// Convert from the typed object to whatever type the caller asked for.
	// If it's the same, it'll just do a DeepCopyInto.
	err = ms.scheme.Convert(tObj, obj, nil)
	if err != nil {
		return err
	}
	// Conversion sometimes drops the GVK, so add it back in.
	// TODO: Does the real client do this for typed objects or just Unstructured?
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	klog.V(6).Infof("Getting %s (Generation: %v, ResourceVersion: %q): %s",
		kinds.ObjectSummary(obj),
		obj.GetGeneration(), obj.GetResourceVersion(),
		log.AsJSON(obj))
	return nil
}

func (ms *MemoryStorage) validateGetOptions(opts *client.GetOptions) error {
	if opts == nil {
		return nil
	}
	if opts.Raw != nil && opts.Raw.ResourceVersion != "" {
		return errors.Errorf("fake.MemoryStorage.List: opts.Raw.ResourceVersion=%q: not yet implemented", opts.Raw.ResourceVersion)
	}
	return nil
}

// List all the objects in storage that match the resource type and list objects.
func (ms *MemoryStorage) List(_ context.Context, list client.ObjectList, opts *client.ListOptions) error {
	ms.lock.RLock()
	defer ms.lock.RUnlock()

	err := ms.validateListOptions(opts)
	if err != nil {
		return err
	}

	_, isList := list.(meta.List)
	if !isList {
		return errors.Errorf("called fake.MemoryStorage.List on non-List %s",
			kinds.ObjectSummary(list))
	}

	// Populate the GVK, if not populated.
	// The normal rest client populates it from the apiserver response anyway.
	listGVK, err := kinds.Lookup(list, ms.scheme)
	if err != nil {
		return err
	}
	gvk := kinds.ItemGVKForListGVK(listGVK)
	if gvk.Kind == listGVK.Kind {
		return errors.Errorf("fake.MemoryStorage.List called with non-List GVK %q", listGVK.String())
	}
	list.GetObjectKind().SetGroupVersionKind(listGVK)

	// Get the List results from the cache as an UnstructuredList.
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(listGVK)

	// Populate the items
	for _, obj := range ms.listObjects(gvk.GroupKind()) {
		// Convert to a unstructured object with optional version conversion
		uObj, err := kinds.ToUnstructuredWithVersion(obj, gvk, ms.scheme)
		if err != nil {
			return err
		}
		// Skip objects that don't match the ListOptions filters
		ok, err := matchesListFilters(uObj, opts, ms.scheme)
		if err != nil {
			return err
		}
		if !ok {
			// No match
			continue
		}
		uList.Items = append(uList.Items, *uObj)
	}

	// Convert from the UnstructuredList to whatever type the caller asked for.
	// If it's the same, it'll just do a DeepCopyInto.
	err = ms.scheme.Convert(uList, list, nil)
	if err != nil {
		return err
	}
	// TODO: Does the normal client.List always populate GVK on typed objects?
	// Some of the code seems to require this...
	list.GetObjectKind().SetGroupVersionKind(listGVK)

	klog.V(6).Infof("Listing %s (ResourceVersion: %q, Items: %d): %s",
		kinds.ObjectSummary(list),
		list.GetResourceVersion(), len(uList.Items),
		log.AsJSON(list))
	return nil
}

func (ms *MemoryStorage) validateListOptions(opts *client.ListOptions) error {
	if opts == nil {
		return nil
	}
	if opts.Continue != "" {
		return errors.Errorf("fake.MemoryStorage.List: opts.Continue=%q: not yet implemented", opts.Continue)
	}
	if opts.Limit != 0 {
		// TODO: Implement limit for List & Watch calls.
		// Our tests don't need it yet, but watchtools.UntilWithSync passes it,
		// so just return the whole set of objects for now.
		// Since we don't return a Continue token, it won't retry, and should
		// just handle all the return values, even if there's more than the
		// requested limit.
		klog.Warningf("fake.MemoryStorage.List: opts.Limit=%d: not yet implemented (no limit)", opts.Limit)
	}
	return nil
}

// Create an object in storage
func (ms *MemoryStorage) Create(ctx context.Context, obj client.Object, opts *client.CreateOptions) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	err := ms.validateCreateOptions(opts)
	if err != nil {
		return err
	}

	// Convert to a typed object for storage, with GVK populated.
	tObj, err := toTypedClientObject(obj, ms.scheme)
	if err != nil {
		return err
	}

	// Set defaults (must be passed a typed object registered with the scheme)
	ms.scheme.Default(tObj)

	id, err := lookupObjectID(tObj, ms.scheme)
	if err != nil {
		return err
	}

	_, found := ms.objects[id]
	if found {
		return newAlreadyExists(id)
	}

	initResourceVersion(tObj)
	initGeneration(tObj)
	initUID(tObj)
	klog.V(5).Infof("Creating %s (Generation: %v, ResourceVersion: %q)",
		kinds.ObjectSummary(tObj),
		tObj.GetGeneration(), tObj.GetResourceVersion())

	// Copy ResourceVersion change back to input object
	obj.SetResourceVersion(tObj.GetResourceVersion())
	// Copy Generation change back to input object
	obj.SetGeneration(tObj.GetGeneration())

	err = ms.putWithoutLock(id, tObj)
	if err != nil {
		return err
	}

	return ms.sendPutEvent(ctx, id, watch.Added)
}

func (ms *MemoryStorage) validateCreateOptions(opts *client.CreateOptions) error {
	if opts == nil {
		return nil
	}
	if len(opts.DryRun) > 0 {
		if len(opts.DryRun) > 1 || opts.DryRun[0] != metav1.DryRunAll {
			return errors.Errorf("invalid dry run option: %+v", opts.DryRun)
		}
	}
	if opts.FieldManager != "" && opts.FieldManager != configsync.FieldManager {
		return errors.Errorf("invalid field manager option: %v", opts.FieldManager)
	}
	return nil
}

// Delete an object in storage
func (ms *MemoryStorage) Delete(ctx context.Context, obj client.Object, opts *client.DeleteOptions) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	return ms.deleteWithoutLock(ctx, obj, opts)
}

func (ms *MemoryStorage) deleteWithoutLock(ctx context.Context, obj client.Object, opts *client.DeleteOptions) error {
	err := ms.validateDeleteOptions(opts)
	if err != nil {
		return err
	}

	id, err := lookupObjectID(obj, ms.scheme)
	if err != nil {
		return err
	}

	cachedObj, found := ms.objects[id]
	if !found {
		return newNotFound(id)
	}

	if obj.GetUID() != "" && obj.GetUID() != cachedObj.GetUID() {
		return newConflictingUID(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}
	if obj.GetResourceVersion() != "" && obj.GetResourceVersion() != cachedObj.GetResourceVersion() {
		return newConflictingResourceVersion(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}

	// Simulate apiserver delayed deletion for finalizers
	if cachedObj.GetDeletionTimestamp() == nil && len(cachedObj.GetFinalizers()) > 0 {
		klog.V(5).Infof("Found %d finalizers: Adding deleteTimestamp to %s (ResourceVersion: %q)",
			len(cachedObj.GetFinalizers()), kinds.ObjectSummary(cachedObj), cachedObj.GetResourceVersion())
		newObj := cachedObj.DeepCopyObject().(client.Object)
		now := ms.Now()
		newObj.SetDeletionTimestamp(&now)
		// TODO: propagate DeleteOptions -> UpdateOptions
		err := ms.updateWithoutLock(ctx, newObj, nil)
		if err != nil {
			return err
		}
		return nil
	}

	// Delete method in real typed client(https://github.com/kubernetes-sigs/controller-runtime/blob/v0.14.1/pkg/client/typed_client.go#L84)
	// does not copy the latest values back to input object which is different from other methods.

	klog.V(5).Infof("Deleting %s (Generation: %v, ResourceVersion: %q): Diff (- Old, + New):\n%s",
		kinds.ObjectSummary(cachedObj),
		cachedObj.GetGeneration(), cachedObj.GetResourceVersion(),
		log.AsYAMLDiffWithScheme(cachedObj, nil, ms.scheme))

	// Default to background deletion propagation, if unspecified
	if opts.PropagationPolicy == nil {
		pp := metav1.DeletePropagationBackground
		opts.PropagationPolicy = &pp
	}

	switch *opts.PropagationPolicy {
	case metav1.DeletePropagationForeground:
		// Delete managed objects beforehand
		err = ms.deleteManagedObjectsWithoutLock(ctx, id)
		if err != nil {
			return err
		}
		delete(ms.objects, id)
		// TODO: send the event asynchronously, even if the caller cancelled the context.
		ms.watchSupervisor.Send(ctx, id.GroupKind, watch.Event{
			Type:   watch.Deleted,
			Object: cachedObj,
		})
	case metav1.DeletePropagationBackground:
		// Delete managed objects afterwards
		delete(ms.objects, id)
		// TODO: send the event asynchronously, even if the caller cancelled the context.
		ms.watchSupervisor.Send(ctx, id.GroupKind, watch.Event{
			Type:   watch.Deleted,
			Object: cachedObj,
		})
		// TODO: implement thread-safe background deletion propagation with retry & back-off.
		// For now, just delete before returning. This should help catch GC concurrency errors.
		err = ms.deleteManagedObjectsWithoutLock(ctx, id)
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("fake.MemoryStorage.Delete: DeleteOptions.PropagationPolicy=%q: not yet implemented", *opts.PropagationPolicy)
	}
	return nil
}

func (ms *MemoryStorage) validateDeleteOptions(opts *client.DeleteOptions) error {
	if opts == nil {
		return nil
	}
	if opts.DryRun != nil {
		return errors.Errorf("fake.MemoryStorage.Delete: DeleteOptions.DryRun=%+v: not yet implemented",
			opts.DryRun)
	}
	if opts.GracePeriodSeconds != nil {
		return errors.Errorf("fake.MemoryStorage.Delete: DeleteOptions.GracePeriodSeconds=%d: not yet implemented",
			*opts.GracePeriodSeconds)
	}
	if opts.Preconditions != nil {
		return errors.Errorf("fake.MemoryStorage.Delete: DeleteOptions.Preconditions=%+v: not yet implemented",
			opts.Preconditions)
	}
	if opts.PropagationPolicy != nil {
		switch *opts.PropagationPolicy {
		case metav1.DeletePropagationForeground:
		case metav1.DeletePropagationBackground:
		default:
			return errors.Errorf("fake.MemoryStorage.Delete: DeleteOptions.PropagationPolicy=%q: not yet implemented",
				*opts.PropagationPolicy)
		}
	}
	return nil
}

// deleteManagedObjectsWithoutLock deletes objects whose ownerRef is the specified obj.
// TODO: retry on error (probably can't actually fail)
func (ms *MemoryStorage) deleteManagedObjectsWithoutLock(ctx context.Context, parentID core.ID) error {
	// find then delete, to avoid concurrent map read/write
	var childObjs []*unstructured.Unstructured

	for storedID, storedObj := range ms.objects {
		// owned objects must be in the same namespace
		if storedID.Namespace != parentID.Namespace {
			continue
		}
		for _, parentRef := range storedObj.GetOwnerReferences() {
			parentRefGVK := schema.FromAPIVersionAndKind(parentRef.APIVersion, parentRef.Kind)
			if parentRef.Name != parentID.Name || parentRefGVK.GroupKind() != parentID.GroupKind {
				continue
			}
			klog.V(5).Infof("Garbage collecting child object %s of deleted object %s",
				kinds.ObjectSummary(storedObj), parentID)
			childObjs = append(childObjs, storedObj)
			break
		}
	}

	if len(childObjs) == 0 {
		return nil
	}

	policy := metav1.DeletePropagationForeground
	deleteOpts := &client.DeleteOptions{PropagationPolicy: &policy}

	for _, childObj := range childObjs {
		// Shallow copy the object to avoid DeepCopy or modifying the cache
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(childObj.GroupVersionKind())
		obj.SetName(childObj.GetName())
		obj.SetNamespace(childObj.GetNamespace())
		// don't delete if the object has been replaced since lookup (unlikely due to lock)
		obj.SetUID(childObj.GetUID())
		// don't delete if the object has been modified since lookup (unlikely due to lock)
		// TODO: when async, this could cause a conflict, which will require re-lookup to verify the owner ref hasn't been removed.
		obj.SetResourceVersion(childObj.GetResourceVersion())

		if err := ms.deleteWithoutLock(ctx, obj, deleteOpts); err != nil {
			if apierrors.IsNotFound(err) && meta.IsNoMatchError(err) {
				// Already deleted (unlikely due to lock)
				break
			}
			return errors.Wrapf(err, "failed to garbage collect object %s owned by deleted object %v",
				kinds.ObjectSummary(obj), parentID)
		}
	}
	return nil
}

// Update an object in storage
func (ms *MemoryStorage) Update(ctx context.Context, obj client.Object, opts *client.UpdateOptions) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	return ms.updateWithoutLock(ctx, obj, opts)
}

// updateWithoutLock implements Update, but without the lock.
// This allows it to be called by Delete when finalizers are present.
func (ms *MemoryStorage) updateWithoutLock(ctx context.Context, obj client.Object, opts *client.UpdateOptions) error {
	err := ms.validateUpdateOptions(opts)
	if err != nil {
		return err
	}

	// Convert to a typed object for storage, with GVK populated.
	tObj, err := toTypedClientObject(obj, ms.scheme)
	if err != nil {
		return err
	}

	// Set defaults (must be passed a typed object registered with the scheme)
	ms.scheme.Default(tObj)

	id, err := lookupObjectID(obj, ms.scheme)
	if err != nil {
		return err
	}

	cachedObj, found := ms.objects[id]
	if !found {
		return newNotFound(id)
	}

	oldStatus, hasStatus, err := ms.getStatusFromObject(cachedObj)
	if err != nil {
		return err
	}
	if len(opts.DryRun) > 0 {
		// don't merge or store the result
		return nil
	}

	if obj.GetUID() == "" {
		tObj.SetUID(cachedObj.GetUID())
	} else if obj.GetUID() != cachedObj.GetUID() {
		return newConflictingUID(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}
	if obj.GetResourceVersion() == "" {
		tObj.SetResourceVersion(cachedObj.GetResourceVersion())
	} else if obj.GetResourceVersion() != cachedObj.GetResourceVersion() {
		return newConflictingResourceVersion(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}

	if err = incrementResourceVersion(tObj); err != nil {
		return errors.Wrap(err, "failed to increment resourceVersion")
	}
	if err = ms.updateGeneration(cachedObj, tObj); err != nil {
		return errors.Wrap(err, "failed to update generation")
	}

	if hasStatus {
		tObj, err = ms.updateObjectStatus(tObj, oldStatus)
		if err != nil {
			return err
		}
	}

	// Copy latest values back to input object
	obj.SetUID(tObj.GetUID())
	obj.SetResourceVersion(tObj.GetResourceVersion())
	obj.SetGeneration(tObj.GetGeneration())

	klog.V(5).Infof("Updating %s (Generation: %v, ResourceVersion: %q)",
		kinds.ObjectSummary(tObj),
		tObj.GetGeneration(), tObj.GetResourceVersion())

	err = ms.putWithoutLock(id, tObj)
	if err != nil {
		return err
	}

	return ms.sendPutEvent(ctx, id, watch.Modified)
}

func (ms *MemoryStorage) validateUpdateOptions(opts *client.UpdateOptions) error {
	if opts == nil {
		return nil
	}
	if len(opts.DryRun) > 0 {
		if len(opts.DryRun) > 1 || opts.DryRun[0] != metav1.DryRunAll {
			return errors.Errorf("invalid dry run option: %+v", opts.DryRun)
		}
	}
	if opts.FieldManager != "" && opts.FieldManager != configsync.FieldManager {
		return errors.Errorf("invalid field manager option: %v", opts.FieldManager)
	}
	return nil
}

func (ms *MemoryStorage) getStatusFromObject(obj client.Object) (map[string]interface{}, bool, error) {
	uObj, err := kinds.ToUnstructured(obj, ms.scheme)
	if err != nil {
		return nil, false, err
	}
	return unstructured.NestedMap(uObj.Object, "status")
}

func (ms *MemoryStorage) updateObjectStatus(obj client.Object, status map[string]interface{}) (client.Object, error) {
	uObj, err := kinds.ToUnstructured(obj, ms.scheme)
	if err != nil {
		return nil, err
	}
	if err = unstructured.SetNestedMap(uObj.Object, status, "status"); err != nil {
		return obj, err
	}
	return toTypedClientObject(uObj, ms.scheme)
}

// Patch an object in storage
func (ms *MemoryStorage) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts *client.PatchOptions) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()

	err := ms.validatePatchOptions(opts, patch)
	if err != nil {
		return err
	}

	gvk, err := kinds.Lookup(obj, ms.scheme)
	if err != nil {
		return errors.Wrap(err, "failed to lookup GVK from scheme")
	}

	patchData, err := patch.Data(obj)
	if err != nil {
		return errors.Wrap(err, "failed to build patch")
	}

	id, err := lookupObjectID(obj, ms.scheme)
	if err != nil {
		return err
	}

	cachedObj, found := ms.objects[id]
	// TODO: What do we do if the patch version isn't the same as the preferred/cached version???
	// Do we need to convert the stored version to the patch version? What if that's lossy?
	// Is it possible to convert a patch version without patching the object first?
	// Probably not...

	var mergedData []byte
	switch patch.Type() {
	case types.ApplyPatchType:
		// WARNING: If you need to test SSA with multiple managers, do it in e2e tests!
		// Unfortunately, there's no good way to replicate the field-manager
		// behavior of Server-Side Apply, because some of the code used by the
		// apiserver is internal and can't be imported.
		// So we're using Update behavior here instead, since that's effectively
		// how SSA acts when there's only one field manager.
		// Since we're treating the patch data as the full intent, we don't need
		// to merge with the cached object, just preserve the ResourceVersion.
		mergedData = patchData
	case types.MergePatchType:
		if found {
			oldData, err := json.Marshal(cachedObj)
			if err != nil {
				return err
			}
			mergedData, err = jsonpatch.MergePatch(oldData, patchData)
			if err != nil {
				return err
			}
		} else {
			mergedData = patchData
		}
	case types.StrategicMergePatchType:
		if found {
			rObj, err := kinds.NewClientObjectForGVK(gvk, ms.scheme)
			if err != nil {
				return err
			}
			oldData, err := json.Marshal(cachedObj)
			if err != nil {
				return err
			}
			mergedData, err = strategicpatch.StrategicMergePatch(oldData, patchData, rObj)
			if err != nil {
				return err
			}
		} else {
			mergedData = patchData
		}
	case types.JSONPatchType:
		patch, err := jsonpatch.DecodePatch(patchData)
		if err != nil {
			return err
		}
		oldData, err := json.Marshal(cachedObj)
		if err != nil {
			return err
		}
		mergedData, err = patch.Apply(oldData)
		if err != nil {
			return err
		}
	default:
		return errors.Errorf("fake.MemoryStorage.Patch: Patch.Type=%q: not yet implemented", patch.Type())
	}
	// Use a new object, instead of updating the cached object,
	// because unmarshal doesn't always delete unspecified fields.
	uObj := &unstructured.Unstructured{}
	if err = uObj.UnmarshalJSON(mergedData); err != nil {
		return errors.Wrap(err, "failed to unmarshal patch data")
	}
	if found {
		uObj.SetResourceVersion(cachedObj.GetResourceVersion())
	} else {
		uObj.SetResourceVersion("0") // init to allow incrementing
	}
	if err := incrementResourceVersion(uObj); err != nil {
		return errors.Wrap(err, "failed to increment resourceVersion")
	}
	if found {
		// Update generation if spec changed
		if err = ms.updateGeneration(cachedObj, uObj); err != nil {
			return errors.Wrap(err, "failed to update generation")
		}
	} else {
		uObj.SetGeneration(1)
	}
	klog.V(5).Infof("Patching %s (Found: %v, Generation: %v, ResourceVersion: %q)",
		id, found,
		uObj.GetGeneration(), uObj.GetResourceVersion())

	err = ms.putWithoutLock(id, uObj)
	if err != nil {
		return err
	}

	return ms.sendPutEvent(ctx, id, watch.Modified)
}

func (ms *MemoryStorage) validatePatchOptions(opts *client.PatchOptions, patch client.Patch) error {
	if len(opts.DryRun) > 0 {
		if len(opts.DryRun) > 1 || opts.DryRun[0] != metav1.DryRunAll {
			return errors.Errorf("invalid dry run option: %+v", opts.DryRun)
		}
	}
	if opts.FieldManager != "" && opts.FieldManager != configsync.FieldManager {
		return errors.Errorf("invalid field manager option: %v", opts.FieldManager)
	}
	if patch != client.Apply && opts.Force != nil {
		return errors.Errorf("invalid force option: Forbidden: may not be specified for non-apply patch")
	}
	return nil
}

func (ms *MemoryStorage) updateGeneration(oldObj, newObj client.Object) error {
	uOldObj, err := ms.prepareObject(oldObj)
	if err != nil {
		return err
	}
	oldCopyContent := uOldObj.UnstructuredContent()

	uNewObj, err := ms.prepareObject(newObj)
	if err != nil {
		return err
	}
	newCopyContent := uNewObj.UnstructuredContent()

	// ignore metadata
	delete(newCopyContent, "metadata")
	delete(oldCopyContent, "metadata")
	// Assume all objects have a status sub-resource, and ignore it.
	// TODO: figure out how to detect if this resource has a status sub-resource.
	delete(newCopyContent, "status")
	delete(oldCopyContent, "status")
	if !equality.Semantic.DeepEqual(newCopyContent, oldCopyContent) {
		newObj.SetGeneration(oldObj.GetGeneration() + 1)
	} else {
		newObj.SetGeneration(oldObj.GetGeneration())
	}
	return nil
}

// DeleteAllOf deletes all the objects of the specified resource.
func (ms *MemoryStorage) DeleteAllOf(_ context.Context, _ client.ObjectList, _ *client.DeleteAllOfOptions) error {
	// TODO: Implement DeleteAllOf, if needed
	return errors.New("fake.MemoryStorage.DeleteAllOf: not yet implemented")
}

// Watch the specified objects.
// The returned watcher will stream events to the ResultChan until Stop is called.
func (ms *MemoryStorage) Watch(_ context.Context, exampleList client.ObjectList, opts *client.ListOptions) (watch.Interface, error) {
	err := ms.validateListOptions(opts)
	if err != nil {
		return nil, err
	}
	listGVK, err := kinds.Lookup(exampleList, ms.scheme)
	if err != nil {
		return nil, errors.Wrap(err, "failed to lookup GVK from scheme")
	}
	gvk := kinds.ItemGVKForListGVK(listGVK)
	if gvk.Kind == listGVK.Kind {
		return nil, errors.Errorf("fake.MemoryStorage.Watch called with non-List GVK: %v", listGVK)
	}
	klog.V(6).Infof("Watching %s (Options: %+v)",
		kinds.ObjectSummary(exampleList), opts)
	watcher := NewWatcher(ms.watchSupervisor, gvk.GroupKind(), exampleList, opts)
	// TODO: Should Client.Watch's context.Done cancel the background stream or just the initial request?
	// If yes, StartWatcher needs to take a context.
	// client-go's FakeDynamicClient.Watch seems to just ignore the context, so that's what we're doing here too.
	go func() {
		// Run watcher until watch.Interface.Stop is called
		watcher.Run(context.Background())
	}()
	return watcher, nil
}

func initResourceVersion(obj client.Object) {
	if obj.GetResourceVersion() == "" {
		obj.SetResourceVersion("1")
	}
}

func initGeneration(obj client.Object) {
	if obj.GetGeneration() == 0 {
		obj.SetGeneration(1)
	}
}

func initUID(obj client.Object) {
	if obj.GetUID() == "" {
		obj.SetUID("1")
	}
}

func incrementResourceVersion(obj client.Object) error {
	rv := obj.GetResourceVersion()
	rvInt, err := strconv.Atoi(rv)
	if err != nil {
		return errors.Wrap(err, "failed to parse resourceVersion")
	}
	obj.SetResourceVersion(strconv.Itoa(rvInt + 1))
	return nil
}

// Subresource returns a new SubresourceStorage, which can be used to update
// the sub-resource field, without updating any other fields.
func (ms *MemoryStorage) Subresource(field string) *SubresourceStorage {
	return &SubresourceStorage{
		Storage: ms,
		Field:   field,
	}
}

// Check reports a test error if the passed objects (wants) do not match the
// expected set of objects in storage. Objects will be converted to the
// scheme-preferred version and minimized before comparison.
func (ms *MemoryStorage) Check(t *testing.T, wants ...client.Object) {
	t.Helper()

	wantMap := make(map[core.ID]client.Object)

	for _, obj := range wants {
		// Convert to typed first, so we can set the defaults on the specified
		// version. Then prep for storage & minimize before comparison.
		tObj, err := toTypedClientObject(obj, ms.scheme)
		if err != nil {
			t.Fatalf("failed to convert expected object: %v", err)
		}
		ms.scheme.Default(tObj)
		uObj, err := ms.prepareObject(tObj)
		if err != nil {
			t.Fatalf("failed to prepare expected object for comparison with objects in storage: %v", err)
		}
		wantMap[core.IDOf(obj)] = uObj
	}

	asserter := testutil.NewAsserter(
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(metav1.Time{}, "Time"),
		cmpopts.IgnoreMapEntries(func(k string, _ interface{}) bool {
			return k == "lastUpdateTime" || k == "lastTransitionTime"
		}),
	)
	checked := make(map[core.ID]bool)
	for id, want := range wantMap {
		checked[id] = true
		actual, found := ms.TestGet(id)
		if !found {
			t.Errorf("MemoryStorage missing %s", id.String())
			continue
		}
		asserter.Equal(t, want, actual, "expected object (%s) to be equal", id)
	}
	for id := range ms.TestGetAll() {
		if !checked[id] {
			t.Errorf("MemoryStorage unexpectedly contains %s", id.String())
		}
	}
}
