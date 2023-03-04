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

package fake

import (
	"encoding/json"
	"fmt"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DynamicClient is a fake dynamic client which contains a map to store unstructured objects
type DynamicClient struct {
	*dynamicfake.FakeDynamicClient
	objects map[core.ID]*unstructured.Unstructured
	mapper  meta.RESTMapper
	scheme  *runtime.Scheme
}

// NewDynamicClient instantiates a new fake.DynamicClient
func NewDynamicClient(t *testing.T, scheme *runtime.Scheme) *DynamicClient {
	t.Helper()

	gvks := allKnownGVKs(scheme)
	mapper := testutil.NewFakeRESTMapper(gvks...)
	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	unObjs := make(map[core.ID]*unstructured.Unstructured)
	dc := &DynamicClient{
		FakeDynamicClient: fakeClient,
		objects:           unObjs,
		mapper:            mapper,
		scheme:            scheme,
	}
	resources := map[string]struct{}{}
	for _, gvk := range gvks {
		// Technically "unsafe", but this is what the FakeRESTMapper uses.
		// So it's safe as long as the test uses the same mapper.
		plural, singular := meta.UnsafeGuessKindToResource(gvk)
		resources[plural.Resource] = struct{}{}
		resources[singular.Resource] = struct{}{}
	}
	for resource := range resources {
		fakeClient.PrependReactor("create", resource, dc.create)
		fakeClient.PrependReactor("get", resource, dc.get)
		fakeClient.PrependReactor("update", resource, dc.update)
		fakeClient.PrependReactor("patch", resource, dc.patch)
		fakeClient.PrependReactor("delete", resource, dc.delete)
		// TODO: add support for list, delete-collection, and watch, if needed
	}
	return dc
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
	dc.objects[id] = obj
}

func (dc *DynamicClient) create(action clienttesting.Action) (bool, runtime.Object, error) {
	createAction := action.(clienttesting.CreateAction)
	if createAction.GetSubresource() != "" {
		// TODO: add support for subresource updates, if needed
		return true, nil, fmt.Errorf("subresource updates not supported by fake.DynamicClient for %s of %s",
			createAction.GetSubresource(), createAction.GetResource().Resource)
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
	if uObj.IsList() {
		// TODO: add support for list creates, if needed
		return true, nil, fmt.Errorf("list creates not supported by fake.DynamicClient for %s",
			createAction.GetResource().Resource)
	}
	if uObj.GetNamespace() != "" && uObj.GetNamespace() != createAction.GetNamespace() {
		return true, nil, fmt.Errorf("invalid metadata.namespace: expected %q but found %q",
			createAction.GetNamespace(), uObj.GetNamespace())
	}
	id := genID(createAction.GetNamespace(), uObj.GetName(), gvk.GroupKind())
	_, found := dc.objects[id]
	if found {
		return true, nil, newAlreadyExists(id)
	}
	uObj.SetUID("1")
	uObj.SetResourceVersion("1")
	uObj.SetGeneration(1)
	klog.V(5).Infof("Creating %s (ResourceVersion: %q): Diff (- Old, + New):\n%s",
		id, uObj.GetResourceVersion(),
		log.AsYAMLDiffWithScheme(nil, uObj, dc.scheme))
	dc.objects[id] = uObj
	// return a copy to make sure the caller can't modify the cached object
	return true, uObj.DeepCopy(), nil
}

func (dc *DynamicClient) get(action clienttesting.Action) (bool, runtime.Object, error) {
	getAction := action.(clienttesting.GetAction)
	gvk, err := dc.mapper.KindFor(getAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	id := genID(getAction.GetNamespace(), getAction.GetName(), gvk.GroupKind())
	cachedObj, found := dc.objects[id]
	if !found {
		return true, nil, newNotFound(id)
	}
	klog.V(5).Infof("Getting %s (ResourceVersion: %q):\n%s",
		id, cachedObj.GetResourceVersion(), log.AsYAML(cachedObj))
	return true, cachedObj.DeepCopy(), nil
}

func (dc *DynamicClient) update(action clienttesting.Action) (bool, runtime.Object, error) {
	updateAction := action.(clienttesting.UpdateAction)
	if updateAction.GetSubresource() != "" {
		// TODO: add support for subresource updates, if needed
		return true, nil, fmt.Errorf("subresource updates not supported by fake.DynamicClient for %s of %s",
			updateAction.GetSubresource(), updateAction.GetResource().Resource)
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
	if uObj.IsList() {
		// TODO: add support for list updates, if needed
		return true, nil, fmt.Errorf("list updates not supported by fake.DynamicClient for %s",
			updateAction.GetResource().Resource)
	}
	if uObj.GetNamespace() != "" && uObj.GetNamespace() != updateAction.GetNamespace() {
		return true, nil, fmt.Errorf("invalid metadata.namespace: expected %q but found %q",
			updateAction.GetNamespace(), uObj.GetNamespace())
	}
	id := genID(updateAction.GetNamespace(), uObj.GetName(), gvk.GroupKind())
	cachedObj, found := dc.objects[id]
	if !found {
		return true, nil, newNotFound(id)
	}
	if uObj.GetResourceVersion() != "" && uObj.GetResourceVersion() != cachedObj.GetResourceVersion() {
		// TODO: return a real conflict error
		return true, nil, fmt.Errorf("invalid spec.resourceVersion: expected %q but found %q",
			updateAction.GetNamespace(), uObj.GetNamespace())
	}
	// Copy cached ResourceVersion to updated object & increment
	uObj.SetResourceVersion(cachedObj.GetResourceVersion())
	if err := incrementResourceVersion(uObj); err != nil {
		return true, nil, fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	if err = updateGeneration(cachedObj, uObj, dc.scheme); err != nil {
		return true, nil, fmt.Errorf("failed to update generation: %w", err)
	}
	klog.V(5).Infof("Updating %s (ResourceVersion: %q): Diff (- Old, + New):\n%s",
		id, uObj.GetResourceVersion(),
		log.AsYAMLDiffWithScheme(cachedObj, uObj, dc.scheme))
	dc.objects[id] = uObj
	// return a copy to make sure the caller can't modify the cached object
	return true, uObj.DeepCopy(), nil
}

func (dc *DynamicClient) patch(action clienttesting.Action) (bool, runtime.Object, error) {
	patchAction := action.(clienttesting.PatchAction)
	gvk, err := dc.mapper.KindFor(patchAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	id := genID(patchAction.GetNamespace(), patchAction.GetName(), gvk.GroupKind())
	cachedObj, found := dc.objects[id]
	var mergedData []byte
	switch patchAction.GetPatchType() {
	case types.ApplyPatchType:
		// WARNING: If you need to test SSA with multiple managers, do it in e2e tests!
		// Unfortunately, there's no good way to replicate the field-manager
		// behavior of Server-Side Apply, because some of the code used by the
		// apiserver is internal and can't be imported.
		// So we're using Update behavior here instead, since that's effectively
		// how SSA acts when there's only one field manager.
		// Since we're treating the patch data as the full intent, we don't need
		// to merge with the cached object, just preserve the ResourceVersion.
		mergedData = patchAction.GetPatch()
	case types.MergePatchType:
		if found {
			oldData, err := json.Marshal(cachedObj)
			if err != nil {
				return true, nil, err
			}
			mergedData, err = jsonpatch.MergePatch(oldData, patchAction.GetPatch())
			if err != nil {
				return true, nil, err
			}
		} else {
			mergedData = patchAction.GetPatch()
		}
	case types.StrategicMergePatchType:
		if found {
			rObj, err := dc.scheme.New(gvk)
			if err != nil {
				return true, nil, err
			}
			oldData, err := json.Marshal(cachedObj)
			if err != nil {
				return true, nil, err
			}
			mergedData, err = strategicpatch.StrategicMergePatch(oldData, patchAction.GetPatch(), rObj)
			if err != nil {
				return true, nil, err
			}
		} else {
			mergedData = patchAction.GetPatch()
		}
	case types.JSONPatchType:
		patch, err := jsonpatch.DecodePatch(patchAction.GetPatch())
		if err != nil {
			return true, nil, err
		}
		oldData, err := json.Marshal(cachedObj)
		if err != nil {
			return true, nil, err
		}
		mergedData, err = patch.Apply(oldData)
		if err != nil {
			return true, nil, err
		}
	default:
		return true, nil, fmt.Errorf("patch type not supported: %q", patchAction.GetPatchType())
	}
	// Use a new object, instead of updating the cached object,
	// because unmarshal doesn't always delete unspecified fields.
	patchedObj := &unstructured.Unstructured{}
	if err = patchedObj.UnmarshalJSON(mergedData); err != nil {
		return true, nil, fmt.Errorf("failed to unmarshal patch data: %w", err)
	}
	if found {
		patchedObj.SetResourceVersion(cachedObj.GetResourceVersion())
	} else {
		patchedObj.SetResourceVersion("0") // init to allow incrementing
	}
	if err := incrementResourceVersion(patchedObj); err != nil {
		return true, nil, fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	if found {
		if err = updateGeneration(cachedObj, patchedObj, dc.scheme); err != nil {
			return true, nil, fmt.Errorf("failed to update generation: %w", err)
		}
	} else {
		patchedObj.SetGeneration(1)
	}
	klog.V(5).Infof("Patching %s (Found: %v, ResourceVersion: %q): Diff (- Old, + New):\n%s",
		id, found, patchedObj.GetResourceVersion(),
		log.AsYAMLDiffWithScheme(cachedObj, patchedObj, dc.scheme))
	dc.objects[id] = patchedObj
	// return a copy to make sure the caller can't modify the cached object
	return true, patchedObj.DeepCopy(), nil
}

func (dc *DynamicClient) delete(action clienttesting.Action) (bool, runtime.Object, error) {
	deleteAction := action.(clienttesting.DeleteAction)
	gvk, err := dc.mapper.KindFor(deleteAction.GetResource())
	if err != nil {
		return true, nil, fmt.Errorf("failed to lookup kind for resource: %w", err)
	}
	id := genID(deleteAction.GetNamespace(), deleteAction.GetName(), gvk.GroupKind())
	cachedObj, found := dc.objects[id]
	if !found {
		return true, nil, newNotFound(id)
	}
	// TODO: Handle DeletionTimestamp when Finalizers exist
	klog.V(5).Infof("Deleting %s (ResourceVersion: %q): %s",
		id, cachedObj.GetResourceVersion(),
		log.AsYAMLDiffWithScheme(cachedObj, nil, dc.scheme))
	delete(dc.objects, id)
	return true, nil, nil
}

func genID(namespace, name string, gk schema.GroupKind) core.ID {
	return core.ID{
		GroupKind: gk,
		ObjectKey: types.NamespacedName{Namespace: namespace, Name: name},
	}
}

func updateGeneration(oldObj, newObj client.Object, scheme *runtime.Scheme) error {
	var oldCopyContent, newCopyContent map[string]interface{}
	if uObj, ok := oldObj.(*unstructured.Unstructured); ok {
		oldCopyContent = uObj.DeepCopy().UnstructuredContent()
	} else {
		uObj, err := kinds.ToUnstructured(oldObj, scheme)
		if err != nil {
			return fmt.Errorf("failed to convert old object to unstructured: %w", err)
		}
		oldCopyContent = uObj.UnstructuredContent()
	}
	if uObj, ok := newObj.(*unstructured.Unstructured); ok {
		newCopyContent = uObj.DeepCopy().UnstructuredContent()
	} else {
		uObj, err := kinds.ToUnstructured(newObj, scheme)
		if err != nil {
			return fmt.Errorf("failed to convert new object to unstructured: %w", err)
		}
		newCopyContent = uObj.UnstructuredContent()
	}
	// ignore metadata
	delete(newCopyContent, "metadata")
	delete(oldCopyContent, "metadata")
	// Assume all objects have a status subresource, and ignore it.
	// TODO: figure out how to detect if this resource has a status subresource.
	delete(newCopyContent, "status")
	delete(oldCopyContent, "status")
	if !equality.Semantic.DeepEqual(newCopyContent, oldCopyContent) {
		newObj.SetGeneration(oldObj.GetGeneration() + 1)
	} else {
		newObj.SetGeneration(oldObj.GetGeneration())
	}
	return nil
}

// Check reports an error to `t` if the passed objects in wants do not match the
// expected set of objects in the fake.Client, and only the passed updates to
// Status fields were recorded.
func (dc *DynamicClient) Check(t *testing.T, wants ...client.Object) {
	t.Helper()

	wantMap := make(map[core.ID]*unstructured.Unstructured)

	for _, obj := range wants {
		uObj, err := kinds.ToUnstructured(obj, dc.scheme)
		if err != nil {
			// This is a test precondition, not a validation failure
			t.Fatal(err)
		}
		wantMap[core.IDOf(uObj)] = uObj
	}

	asserter := testutil.NewAsserter(
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(metav1.Time{}, "Time"),
	)
	checked := make(map[core.ID]bool)
	for id, want := range wantMap {
		checked[id] = true
		actual, found := dc.objects[id]
		if !found {
			t.Errorf("fake.DynamicClient missing %s", id.String())
			continue
		}
		asserter.Equal(t, want, actual, "expected object (%s) to be equal", id)
	}
	for id := range dc.objects {
		if !checked[id] {
			t.Errorf("fake.DynamicClient unexpectedly contains %s", id.String())
		}
	}
}
