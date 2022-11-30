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
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/meta"
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
		fakeClient.PrependReactor("get", resource, dc.get)
		fakeClient.PrependReactor("update", resource, dc.update)
		fakeClient.PrependReactor("patch", resource, dc.patch)
		fakeClient.PrependReactor("delete", resource, dc.delete)
		// TODO: add support for list, delete-collection, and watch, if needed
	}
	return dc
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
	klog.V(5).Infof("Getting %s (ResourceVersion: %q): %s",
		id, cachedObj.GetResourceVersion(), log.AsJSON(cachedObj))
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
	uObj.SetResourceVersion(cachedObj.GetResourceVersion())
	if err := incrementResourceVersion(uObj); err != nil {
		return true, nil, fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	klog.V(5).Infof("Updating %s (ResourceVersion: %q): %s\nDiff (- Old, + New):\n%s",
		id, found, uObj.GetResourceVersion(),
		log.AsJSON(uObj), cmp.Diff(cachedObj, uObj))
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
	klog.V(5).Infof("Patching %s (Found: %v, ResourceVersion: %q): %s\nDiff (- Old, + New):\n%s",
		id, found, patchedObj.GetResourceVersion(),
		log.AsJSON(patchedObj), cmp.Diff(cachedObj, patchedObj))
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
		id, cachedObj.GetResourceVersion(), log.AsJSON(cachedObj))
	delete(dc.objects, id)
	return true, nil, nil
}

func genID(namespace, name string, gk schema.GroupKind) core.ID {
	return core.ID{
		GroupKind: gk,
		ObjectKey: types.NamespacedName{Namespace: namespace, Name: name},
	}
}
