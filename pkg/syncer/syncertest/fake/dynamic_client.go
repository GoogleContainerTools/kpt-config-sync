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
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DynamicClient is a fake implementation of dynamic.Interface
type DynamicClient struct {
	*dynamicfake.FakeDynamicClient
	test    *testing.T
	mapper  meta.RESTMapper
	scheme  *runtime.Scheme
	storage *MemoryStorage
}

// Prove DynamicClient satisfies the dynamic.Interface interface
var _ dynamic.Interface = &DynamicClient{}

// NewDynamicClient instantiates a new fake.DynamicClient
func NewDynamicClient(t *testing.T, scheme *runtime.Scheme) *DynamicClient {
	t.Helper()

	gvks := prioritizedGVKsAllGroups(scheme)
	mapper := testutil.NewFakeRESTMapper(gvks...)
	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	watchSupervisor := NewWatchSupervisor(scheme)
	storage := NewInMemoryStorage(scheme, watchSupervisor)
	dc := &DynamicClient{
		test:              t,
		FakeDynamicClient: fakeClient,
		mapper:            mapper,
		scheme:            scheme,
		storage:           storage,
	}
	dc.registerHandlers(gvks)

	StartWatchSupervisor(t, watchSupervisor)

	return dc
}

func (dc *DynamicClient) registerHandlers(gvks []schema.GroupVersionKind) {
	resources := map[string]struct{}{}
	for _, gvk := range gvks {
		// Technically "unsafe", but this is what the FakeRESTMapper uses.
		// So it's safe as long as the test uses the same mapper.
		plural, singular := meta.UnsafeGuessKindToResource(gvk)
		resources[plural.Resource] = struct{}{}
		resources[singular.Resource] = struct{}{}
	}
	for resource := range resources {
		dc.FakeDynamicClient.PrependReactor("create", resource, dc.create)
		dc.FakeDynamicClient.PrependReactor("get", resource, dc.get)
		dc.FakeDynamicClient.PrependReactor("list", resource, dc.list)
		dc.FakeDynamicClient.PrependReactor("update", resource, dc.update)
		dc.FakeDynamicClient.PrependReactor("patch", resource, dc.patch)
		dc.FakeDynamicClient.PrependReactor("delete", resource, dc.delete)
		dc.FakeDynamicClient.PrependReactor("delete-collection", resource, dc.deleteAllOf)
		dc.FakeDynamicClient.PrependWatchReactor(resource, dc.watch)
	}
}

// PutAll loops through the objects and adds them to the cache, with metadata
// validation.
//
// Use for test initialization to simulate exact cluster state.
func (dc *DynamicClient) PutAll(t *testing.T, objs ...*unstructured.Unstructured) {
	for _, obj := range objs {
		dc.Put(t, obj)
	}
}

// Put adds the object to the cache, with metadata validation.
//
// Use for test initialization to simulate exact cluster state.
// Does not trigger events or garbage collection.
func (dc *DynamicClient) Put(t *testing.T, obj *unstructured.Unstructured) {
	// don't modify original or allow later modification of the cache
	obj = obj.DeepCopy()
	gvk := obj.GroupVersionKind()
	id := genID(obj.GetNamespace(), obj.GetName(), gvk.GroupKind())
	mapping, err := dc.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		t.Fatal(err)
	}
	if mapping.Scope == meta.RESTScopeNamespace {
		if obj.GetNamespace() == "" {
			t.Fatalf("namespace-scoped object must have a namespace: %s %s",
				gvk.Kind, id.ObjectKey)
		}
	} else {
		if obj.GetNamespace() != "" {
			t.Fatalf("cluster-scoped object must not have a namespace: %s %s",
				gvk.Kind, id.ObjectKey)
		}
	}
	if obj.GetUID() == "" {
		obj.SetUID("1")
	}
	if obj.GetResourceVersion() == "" {
		obj.SetResourceVersion("1")
	}
	if obj.GetGeneration() == 0 {
		obj.SetGeneration(1)
	}
	klog.V(5).Infof("Putting %s (ResourceVersion: %q): Diff (- Old, + New):\n%s",
		id, obj.GetResourceVersion(),
		log.AsYAMLDiffWithScheme(nil, obj, dc.scheme))

	err = dc.storage.TestPut(obj)
	if err != nil {
		t.Fatalf("failed to put object into storage: %s %s: %v",
			gvk.Kind, id.ObjectKey, err)
	}
}

func (dc *DynamicClient) create(action clienttesting.Action) (bool, runtime.Object, error) {
	createAction := action.(clienttesting.CreateAction)
	if createAction.GetSubresource() != "" {
		// TODO: add support for subresource create, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.create: resource=%q subresource=%q: not yet implemented",
			createAction.GetResource().Resource, createAction.GetSubresource())
	}
	gvk, err := dc.mapper.KindFor(createAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	rObj := createAction.GetObject()
	uObj, err := kinds.ToUnstructured(rObj, dc.scheme)
	if err != nil {
		return true, nil, err
	}
	if uObj.GetNamespace() != "" && uObj.GetNamespace() != createAction.GetNamespace() {
		return true, nil, fmt.Errorf("invalid metadata.namespace: expected %q but found %q",
			createAction.GetNamespace(), uObj.GetNamespace())
	}
	if gvk != uObj.GroupVersionKind() {
		return true, nil, fmt.Errorf("invalid GVK for resource %q: expected %q but found %q",
			createAction.GetResource(),
			gvk, uObj.GroupVersionKind())
	}
	err = dc.storage.Create(context.Background(), uObj, &client.CreateOptions{
		// TODO: Pass through FieldManager from CreateOption (requires client-go & client-gen changes)
		FieldManager: FieldManager,
	})
	return true, uObj, err
}

func (dc *DynamicClient) get(action clienttesting.Action) (bool, runtime.Object, error) {
	getAction := action.(clienttesting.GetAction)
	if getAction.GetSubresource() != "" {
		// TODO: add support for subresource get, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.get: resource=%q subresource=%q: not yet implemented",
			getAction.GetResource().Resource, getAction.GetSubresource())
	}
	gvk, err := dc.mapper.KindFor(getAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	id := genID(getAction.GetNamespace(), getAction.GetName(), gvk.GroupKind())
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)
	err = dc.storage.Get(context.Background(), id.ObjectKey, uObj, &client.GetOptions{})
	return true, uObj, err
}

func (dc *DynamicClient) list(action clienttesting.Action) (bool, runtime.Object, error) {
	listAction := action.(clienttesting.ListAction)
	if listAction.GetSubresource() != "" {
		// TODO: add support for subresource list, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.list: resource=%q subresource=%q: not yet implemented",
			listAction.GetResource().Resource, listAction.GetSubresource())
	}
	restrictions := listAction.GetListRestrictions()
	opts := &client.ListOptions{
		Namespace:     listAction.GetNamespace(),
		LabelSelector: restrictions.Labels,
		FieldSelector: restrictions.Fields,
	}
	gvk, err := dc.mapper.KindFor(listAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	uObjList := kinds.NewUnstructuredListForItemGVK(gvk)
	err = dc.storage.List(context.Background(), uObjList, opts)
	return true, uObjList, err
}

func (dc *DynamicClient) update(action clienttesting.Action) (bool, runtime.Object, error) {
	updateAction := action.(clienttesting.UpdateAction)
	if updateAction.GetSubresource() != "" {
		// TODO: add support for subresource updates, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.update: resource=%q subresource=%q: not yet implemented",
			updateAction.GetResource().Resource, updateAction.GetSubresource())
	}
	gvk, err := dc.mapper.KindFor(updateAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	rObj := updateAction.GetObject()
	uObj, err := kinds.ToUnstructured(rObj, dc.scheme)
	if err != nil {
		return true, nil, err
	}
	if uObj.GetNamespace() != "" && uObj.GetNamespace() != updateAction.GetNamespace() {
		return true, nil, fmt.Errorf("invalid metadata.namespace: expected %q but found %q",
			updateAction.GetNamespace(), uObj.GetNamespace())
	}
	if gvk != uObj.GroupVersionKind() {
		return true, nil, fmt.Errorf("invalid GVK for resource %q: expected %q but found %q",
			updateAction.GetResource(),
			gvk, uObj.GroupVersionKind())
	}
	err = dc.storage.Update(context.Background(), uObj, &client.UpdateOptions{
		// TODO: Pass through FieldManager from UpdateOption (requires client-go & client-gen changes)
		FieldManager: FieldManager,
	})
	return true, uObj, err
}

func (dc *DynamicClient) patch(action clienttesting.Action) (bool, runtime.Object, error) {
	patchAction := action.(clienttesting.PatchAction)
	if patchAction.GetSubresource() != "" {
		// TODO: add support for subresource patch, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.patch: resource=%q subresource=%q: not yet implemented",
			patchAction.GetResource().Resource, patchAction.GetSubresource())
	}
	gvk, err := dc.mapper.KindFor(patchAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)
	uObj.SetNamespace(patchAction.GetNamespace())
	uObj.SetName(patchAction.GetName())
	patch := client.RawPatch(patchAction.GetPatchType(), patchAction.GetPatch())
	err = dc.storage.Patch(context.Background(), uObj, patch, &client.PatchOptions{
		// TODO: Pass through FieldManager from PatchOptions (requires client-go & client-gen changes)
		FieldManager: FieldManager,
	})
	return true, uObj, err
}

func (dc *DynamicClient) delete(action clienttesting.Action) (bool, runtime.Object, error) {
	deleteAction := action.(clienttesting.DeleteAction)
	if deleteAction.GetSubresource() != "" {
		// TODO: add support for subresource delete, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.delete: resource=%q subresource=%q: not yet implemented",
			deleteAction.GetResource().Resource, deleteAction.GetSubresource())
	}
	gvk, err := dc.mapper.KindFor(deleteAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)
	uObj.SetNamespace(deleteAction.GetNamespace())
	uObj.SetName(deleteAction.GetName())
	err = dc.storage.Delete(context.Background(), uObj, &client.DeleteOptions{})
	return true, uObj, err
}

func (dc *DynamicClient) deleteAllOf(action clienttesting.Action) (bool, runtime.Object, error) {
	deleteAction := action.(clienttesting.DeleteCollectionAction)
	if deleteAction.GetSubresource() != "" {
		// TODO: add support for subresource delete-collection, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.deleteAllOf: resource=%q subresource=%q: not yet implemented",
			deleteAction.GetResource().Resource, deleteAction.GetSubresource())
	}
	restrictions := deleteAction.GetListRestrictions()
	opts := &client.DeleteAllOfOptions{
		ListOptions: client.ListOptions{
			Namespace:     deleteAction.GetNamespace(),
			LabelSelector: restrictions.Labels,
			FieldSelector: restrictions.Fields,
		},
	}
	gvk, err := dc.mapper.KindFor(deleteAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	uObjList := kinds.NewUnstructuredListForItemGVK(gvk)
	err = dc.storage.DeleteAllOf(context.Background(), uObjList, opts)
	return true, uObjList, err
}

func (dc *DynamicClient) watch(action clienttesting.Action) (bool, watch.Interface, error) {
	watchAction := action.(clienttesting.WatchAction)
	if watchAction.GetSubresource() != "" {
		// TODO: add support for subresource watch, if needed
		return true, nil, fmt.Errorf("fake.DynamicClient.watch: resource=%q subresource=%q: not yet implemented",
			watchAction.GetResource().Resource, watchAction.GetSubresource())
	}
	gvk, err := dc.mapper.KindFor(watchAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	listGVK := kinds.ListGVKForItemGVK(gvk)
	restrictions := watchAction.GetWatchRestrictions()
	opts := &client.ListOptions{
		Namespace:     watchAction.GetNamespace(),
		LabelSelector: restrictions.Labels,
		FieldSelector: restrictions.Fields,
		Raw: &metav1.ListOptions{
			ResourceVersion: restrictions.ResourceVersion,
		},
	}
	exampleList := &unstructured.UnstructuredList{}
	exampleList.SetGroupVersionKind(listGVK)
	watcher, err := dc.storage.Watch(context.Background(), exampleList, opts)
	if err != nil {
		return true, nil, err
	}

	// Stop the watcher when the test ends, if not already stopped
	dc.test.Cleanup(watcher.Stop)
	return true, watcher, nil
}

func genID(namespace, name string, gk schema.GroupKind) core.ID {
	return core.ID{
		GroupKind: gk,
		ObjectKey: types.NamespacedName{Namespace: namespace, Name: name},
	}
}

// Check reports an error to `t` if the passed objects in wants do not match the
// expected set of objects in the fake.Client, and only the passed updates to
// Status fields were recorded.
func (dc *DynamicClient) Check(t *testing.T, wants ...client.Object) {
	t.Helper()
	dc.storage.Check(t, wants...)
}

// Scheme returns the backing Scheme.
func (dc *DynamicClient) Scheme() *runtime.Scheme {
	return dc.scheme
}

// RESTMapper returns the backing RESTMapper.
func (dc *DynamicClient) RESTMapper() meta.RESTMapper {
	return dc.mapper
}

// Storage returns the backing Storage layer
func (dc *DynamicClient) Storage() *MemoryStorage {
	return dc.storage
}
