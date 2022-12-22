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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusWriter is a fake implementation of client.StatusWriter.
type statusWriter struct {
	Client *Client
}

var _ client.StatusWriter = &statusWriter{}

// Client is a fake implementation of client.Client.
type Client struct {
	scheme  *runtime.Scheme
	codecs  serializer.CodecFactory
	mapper  meta.RESTMapper
	Objects map[core.ID]client.Object
}

var _ client.Client = &Client{}

// NewClient instantiates a new fake.Client pre-populated with the specified
// objects.
//
// Calls t.Fatal if unable to properly instantiate Client.
func NewClient(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) *Client {
	t.Helper()

	result := Client{
		scheme:  scheme,
		codecs:  serializer.NewCodecFactory(scheme),
		Objects: make(map[core.ID]client.Object),
	}

	err := v1.AddToScheme(result.scheme)
	if err != nil {
		t.Fatal(errors.Wrap(err, "unable to create fake Client"))
	}

	err = configsyncv1beta1.AddToScheme(result.scheme)
	if err != nil {
		t.Fatal(errors.Wrap(err, "unable to create fake Client"))
	}

	err = corev1.AddToScheme(result.scheme)
	if err != nil {
		t.Fatal(errors.Wrap(err, "unable to create fake Client"))
	}

	// Build mapper using known GVKs from the scheme
	result.mapper = testutil.NewFakeRESTMapper(allKnownGVKs(result.scheme)...)

	for _, o := range objs {
		err = result.Create(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
	}

	return &result
}

// allKnownGVKs returns an unsorted list of GVKs known by the scheme and mapper.
func allKnownGVKs(scheme *runtime.Scheme) []schema.GroupVersionKind {
	typeMap := scheme.AllKnownTypes()
	gvkList := make([]schema.GroupVersionKind, 0, len(typeMap))
	for gvk := range typeMap {
		gvkList = append(gvkList, gvk)
	}
	return gvkList
}

func toGR(gk schema.GroupKind) schema.GroupResource {
	return schema.GroupResource{
		Group:    gk.Group,
		Resource: gk.Kind,
	}
}

// Get implements client.Client.
func (c *Client) Get(_ context.Context, key client.ObjectKey, obj client.Object) error {
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)

	// Populate the GVK, if not populated. We need it to build the ID.
	// The normal rest client populates it from the apiserver response anyway.
	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return err
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	id := c.idFromObject(obj)
	cachedObj, ok := c.Objects[id]
	if !ok {
		return newNotFound(id)
	}
	klog.V(5).Infof("Getting(cached) %T %s (Generation: %v, ResourceVersion: %q): %s",
		cachedObj, gvk.Kind,
		cachedObj.GetGeneration(), cachedObj.GetResourceVersion(),
		log.AsJSON(cachedObj))

	// Convert to a typed object, optionally convert between versions
	tObj, matches, err := c.convertToVersion(cachedObj, gvk)
	if err != nil {
		return err
	}
	if !matches {
		// Either the typed object specified the wrong GVK
		// or the desired GVK isn't in the scheme.
		return errors.Errorf("fake.Client.Get failed to convert object %s from %q to %q",
			key, cachedObj.GetObjectKind().GroupVersionKind(), gvk)
	}

	// Convert from the typed object to whatever type the caller asked for.
	// If it's the same, it'll just do a DeepCopyInto.
	err = c.scheme.Convert(tObj, obj, nil)
	if err != nil {
		return err
	}
	// Conversion sometimes drops the GVK, so add it back in.
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	klog.V(5).Infof("Getting %T %s (Generation: %v, ResourceVersion: %q): %s",
		obj, gvk.Kind,
		obj.GetGeneration(), obj.GetResourceVersion(),
		log.AsJSON(obj))
	return nil
}

func validateListOptions(opts client.ListOptions) error {
	if opts.Continue != "" {
		return errors.Errorf("fake.Client.List does not yet support the Continue option, but got: %+v", opts)
	}
	if opts.Limit != 0 {
		return errors.Errorf("fake.Client.List does not yet support the Limit option, but got: %+v", opts)
	}

	return nil
}

// List implements client.Client.
//
// Does not paginate results.
func (c *Client) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	options := client.ListOptions{}
	options.ApplyOptions(opts)
	err := validateListOptions(options)
	if err != nil {
		return err
	}

	_, isList := list.(meta.List)
	if !isList {
		return errors.Errorf("called fake.Client.List on non-List type %T", list)
	}

	// Populate the GVK, if not populated.
	// The normal rest client populates it from the apiserver response anyway.
	listGVK, err := kinds.Lookup(list, c.scheme)
	if err != nil {
		return err
	}
	// Get the item GVK
	gvk := listGVK.GroupVersion().WithKind(strings.TrimSuffix(listGVK.Kind, "List"))
	// Validate the list is a list
	if gvk.Kind == listGVK.Kind {
		return errors.Errorf("fake.Client.List(UnstructuredList) called with non-List GVK %q", listGVK.String())
	}
	list.GetObjectKind().SetGroupVersionKind(listGVK)

	// Get the List results from the cache as an UnstructuredList.
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(listGVK)

	// Populate the items
	for _, obj := range c.list(gvk.GroupKind()) {
		// Convert to a typed object of the right version
		tObj, matches, err := c.convertToVersion(obj, gvk)
		if err != nil {
			return err
		}
		if !matches {
			// Either the typed object specified the wrong GVK
			// or the desired GVK isn't in the scheme.
			return errors.Errorf("fake.Client.List failed to convert object %s from %q to %q",
				client.ObjectKeyFromObject(obj), tObj.GetObjectKind().GroupVersionKind(), gvk)
		}
		// Convert typed object to unstructured, since we're using an UnstructuredList.
		uObj, err := kinds.ToUnstructured(tObj, c.Scheme())
		if err != nil {
			return err
		}
		// TODO: Is GVK set for unversioned List items? typed List items?
		uObj.SetGroupVersionKind(gvk)
		// Skip objects that don't match the ListOptions filters
		ok, err := c.matchesListFilters(uObj, &options)
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
	err = c.scheme.Convert(uList, list, nil)
	if err != nil {
		return err
	}
	// Conversion sometimes drops the GVK, so add it back in.
	list.GetObjectKind().SetGroupVersionKind(gvk)

	klog.V(5).Infof("Listing %T %s (ResourceVersion: %q, Items: %d): %s",
		list, gvk.Kind, list.GetResourceVersion(), len(uList.Items), log.AsJSON(list))
	return nil
}

func (c *Client) convertFromUnstructured(obj client.Object) (client.Object, error) {
	tObj, err := kinds.ToTypedObject(obj, c.Scheme())
	if err != nil {
		return nil, err
	}
	cObj, ok := tObj.(client.Object)
	if !ok {
		return nil, fmt.Errorf("failed to convert patched %T to client.Object", tObj)
	}
	return cObj, nil
}

func validateCreateOptions(opts *client.CreateOptions) error {
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

// Create implements client.Client.
func (c *Client) Create(_ context.Context, obj client.Object, opts ...client.CreateOption) error {
	options := &client.CreateOptions{}
	options.ApplyOptions(opts)
	err := validateCreateOptions(options)
	if err != nil {
		return err
	}

	// Convert to a typed object for storage, with GVK populated.
	tObj, err := c.convertFromUnstructured(obj)
	if err != nil {
		return err
	}

	// Set defaults
	c.scheme.Default(tObj)

	id := c.idFromObject(tObj)
	_, found := c.Objects[id]
	if found {
		return newAlreadyExists(id)
	}

	initResourceVersion(tObj)
	initGeneration(tObj)
	initUID(tObj)
	klog.V(5).Infof("Creating %T %s (Generation: %v, ResourceVersion: %q): %s",
		tObj, client.ObjectKeyFromObject(tObj),
		tObj.GetGeneration(), tObj.GetResourceVersion(),
		log.AsJSON(tObj))

	// Copy ResourceVersion change back to input object
	obj.SetResourceVersion(tObj.GetResourceVersion())
	// Copy Generation change back to input object
	obj.SetGeneration(tObj.GetGeneration())

	c.Objects[id] = tObj
	return nil
}

func validateDeleteOptions(opts client.DeleteOptions) error {
	if opts.DryRun != nil {
		return errors.Errorf("fake.Client.Delete does not yet support DryRun, but got: %+v", opts)
	}
	if opts.GracePeriodSeconds != nil {
		return errors.Errorf("fake.Client.Delete does not yet support GracePeriodSeconds, but got: %+v", opts)
	}
	if opts.Preconditions != nil {
		return errors.Errorf("fake.Client.Delete does not yet support Preconditions, but got: %+v", opts)
	}
	if opts.PropagationPolicy != nil &&
		*opts.PropagationPolicy != metav1.DeletePropagationBackground {
		return errors.Errorf("fake.Client.Delete does not yet support PropagationPolicy %q",
			*opts.PropagationPolicy)
	}
	return nil
}

// Delete implements client.Client.
func (c *Client) Delete(_ context.Context, obj client.Object, opts ...client.DeleteOption) error {
	options := client.DeleteOptions{}
	options.ApplyOptions(opts)
	err := validateDeleteOptions(options)
	if err != nil {
		return err
	}

	// Populate the GVK, if not populated.
	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return err
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	id := c.idFromObject(obj)

	cachedObj, found := c.Objects[id]
	if !found {
		return newNotFound(id)
	}

	if obj.GetUID() != "" && obj.GetUID() != cachedObj.GetUID() {
		return newConflictingUID(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}
	if obj.GetResourceVersion() != "" && obj.GetResourceVersion() != cachedObj.GetResourceVersion() {
		return newConflictingResourceVersion(id, obj.GetResourceVersion(), cachedObj.GetResourceVersion())
	}

	// Delete method in real typed client(https://github.com/kubernetes-sigs/controller-runtime/blob/v0.14.1/pkg/client/typed_client.go#L84)
	// does not copy the latest values back to input object which is different from other methods.

	klog.V(5).Infof("Deleting %T %s (Generation: %v, ResourceVersion: %q): %s",
		cachedObj, client.ObjectKeyFromObject(cachedObj),
		cachedObj.GetGeneration(), cachedObj.GetResourceVersion(),
		log.AsJSON(cachedObj))

	c.deleteManagedObjects(id)
	delete(c.Objects, id)
	return nil
}

// deleteManagedObjects deletes objects whose ownerRef is the specified obj.
func (c *Client) deleteManagedObjects(id core.ID) {
	for _, o := range c.Objects {
		for _, ownerRef := range o.GetOwnerReferences() {
			if ownerRef.Name == id.Name && ownerRef.Kind == id.Kind {
				delete(c.Objects, c.idFromObject(o))
			}
		}
	}
}

func (c *Client) getStatusFromObject(obj client.Object) (map[string]interface{}, bool, error) {
	uObj, err := kinds.ToUnstructured(obj, c.Scheme())
	if err != nil {
		return nil, false, err
	}
	return unstructured.NestedMap(uObj.Object, "status")
}

func (c *Client) updateObjectStatus(obj client.Object, status map[string]interface{}) (client.Object, error) {
	uObj, err := kinds.ToUnstructured(obj, c.Scheme())
	if err != nil {
		return nil, err
	}
	if err = unstructured.SetNestedMap(uObj.Object, status, "status"); err != nil {
		return obj, err
	}
	return c.convertFromUnstructured(uObj)
}

func validateUpdateOptions(opts *client.UpdateOptions) error {
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

func validatePatchOptions(opts *client.PatchOptions) error {
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

// Update implements client.Client. It does not update the status field.
func (c *Client) Update(_ context.Context, obj client.Object, opts ...client.UpdateOption) error {
	updateOpts := &client.UpdateOptions{}
	updateOpts.ApplyOptions(opts)
	err := validateUpdateOptions(updateOpts)
	if err != nil {
		return err
	}

	tObj, err := c.convertFromUnstructured(obj)
	if err != nil {
		return err
	}

	// Set defaults
	c.scheme.Default(tObj)

	id := c.idFromObject(tObj)
	cachedObj, found := c.Objects[id]
	if !found {
		return newNotFound(id)
	}

	oldStatus, hasStatus, err := c.getStatusFromObject(c.Objects[id])
	if err != nil {
		return err
	}
	if len(updateOpts.DryRun) > 0 {
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
		return fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	if err = updateGeneration(cachedObj, tObj, c.scheme); err != nil {
		return fmt.Errorf("failed to update generation: %w", err)
	}

	if hasStatus {
		tObj, err = c.updateObjectStatus(tObj, oldStatus)
		if err != nil {
			return err
		}
	}

	// Copy latest values back to input object
	obj.SetUID(tObj.GetUID())
	obj.SetResourceVersion(tObj.GetResourceVersion())
	obj.SetGeneration(tObj.GetGeneration())

	klog.V(5).Infof("Updating %T %s (Generation: %v, ResourceVersion: %q): %s\nDiff (- Old, + New):\n%s",
		tObj, id,
		tObj.GetGeneration(), tObj.GetResourceVersion(),
		log.AsJSON(tObj), cmp.Diff(cachedObj, tObj))

	c.Objects[id] = tObj
	return nil
}

// Patch implements client.Client.
func (c *Client) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	patchOpts := &client.PatchOptions{}
	patchOpts.ApplyOptions(opts)
	err := validatePatchOptions(patchOpts)
	if err != nil {
		return err
	}

	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return fmt.Errorf("failed to lookup GVK from scheme: %w", err)
	}

	patchData, err := patch.Data(obj)
	if err != nil {
		return fmt.Errorf("failed to build patch: %w", err)
	}

	id := c.idFromObject(obj)
	cachedObj, found := c.Objects[id]
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
			rObj, err := c.scheme.New(gvk)
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
		return fmt.Errorf("patch type not supported: %q", patch.Type())
	}
	// Use a new object, instead of updating the cached object,
	// because unmarshal doesn't always delete unspecified fields.
	uObj := &unstructured.Unstructured{}
	if err = uObj.UnmarshalJSON(mergedData); err != nil {
		return fmt.Errorf("failed to unmarshal patch data: %w", err)
	}
	tObj, err := c.convertFromUnstructured(uObj)
	if err != nil {
		return err
	}
	if found {
		tObj.SetResourceVersion(cachedObj.GetResourceVersion())
	} else {
		tObj.SetResourceVersion("0") // init to allow incrementing
	}
	if err := incrementResourceVersion(tObj); err != nil {
		return fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	if found {
		// Update generation if spec changed
		if err = updateGeneration(cachedObj, tObj, c.scheme); err != nil {
			return fmt.Errorf("failed to update generation: %w", err)
		}
	} else {
		tObj.SetGeneration(1)
	}
	klog.V(5).Infof("Patching %s (Found: %v, Generation: %v, ResourceVersion: %q): %s\nDiff (- Old, + New):\n%s",
		id, found,
		tObj.GetGeneration(), tObj.GetResourceVersion(),
		log.AsJSON(tObj), cmp.Diff(cachedObj, tObj))
	c.Objects[id] = tObj
	return nil
}

// DeleteAllOf implements client.Client.
func (c *Client) DeleteAllOf(_ context.Context, _ client.Object, _ ...client.DeleteAllOfOption) error {
	return errors.New("fake.Client does not support DeleteAllOf()")
}

// Update implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Update(_ context.Context, obj client.Object, opts ...client.UpdateOption) error {
	updateOpts := &client.UpdateOptions{}
	updateOpts.ApplyOptions(opts)
	err := validateUpdateOptions(updateOpts)
	if err != nil {
		return err
	}

	tObj, err := s.Client.convertFromUnstructured(obj)
	if err != nil {
		return err
	}

	id := s.Client.idFromObject(tObj)
	cachedObj, found := s.Client.Objects[id]
	if !found {
		return newNotFound(id)
	}

	newStatus, hasStatus, err := s.Client.getStatusFromObject(obj)
	if err != nil {
		return err
	}

	if !hasStatus {
		return errors.Errorf("the object %q/%q does not have a status field",
			tObj.GetObjectKind().GroupVersionKind(), tObj.GetName())
	}

	if len(updateOpts.DryRun) > 0 {
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
	updatedObj := cachedObj.DeepCopyObject().(client.Object)

	err = incrementResourceVersion(updatedObj)
	if err != nil {
		return fmt.Errorf("failed to increment resourceVersion: %w", err)
	}

	// Assume status doesn't affect generation (don't increment).
	// TODO: Figure out how to check if status is a sub-resource. If not, increment generation.

	updatedObj, err = s.Client.updateObjectStatus(updatedObj, newStatus)
	if err != nil {
		return err
	}

	// Copy latest values back to input object
	obj.SetUID(updatedObj.GetUID())
	obj.SetResourceVersion(updatedObj.GetResourceVersion())
	obj.SetGeneration(updatedObj.GetGeneration())

	klog.V(5).Infof("Updating Status %T %s (ResourceVersion: %q): %s\nDiff (- Old, + New):\n%s",
		updatedObj, id.ObjectKey, updatedObj.GetResourceVersion(),
		log.AsJSON(updatedObj), cmp.Diff(cachedObj, updatedObj))

	s.Client.Objects[id] = updatedObj
	return nil
}

// Patch implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Patch(ctx context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
	// TODO: Impliment status patch, if needed
	panic("fakeClient.Status().Patch() not yet implimented")
}

// Status implements client.Client.
func (c *Client) Status() client.StatusWriter {
	return &statusWriter{
		Client: c,
	}
}

// Check reports an error to `t` if the passed objects in wants do not match the
// expected set of objects in the fake.Client, and only the passed updates to
// Status fields were recorded.
func (c *Client) Check(t *testing.T, wants ...client.Object) {
	t.Helper()

	wantMap := make(map[core.ID]client.Object)

	for _, obj := range wants {
		obj, err := c.convertFromUnstructured(obj)
		if err != nil {
			// This is a test precondition, and if it fails the following error
			// messages will be garbage.
			t.Fatal(err)
		}
		wantMap[c.idFromObject(obj)] = obj
	}

	asserter := testutil.NewAsserter(
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(metav1.Time{}, "Time"),
	)
	checked := make(map[core.ID]bool)
	for id, want := range wantMap {
		checked[id] = true
		actual, found := c.Objects[id]
		if !found {
			t.Errorf("fake.Client missing %s", id.String())
			continue
		}
		asserter.Equal(t, want, actual, "expected object (%s) to be equal", id)
	}
	for id := range c.Objects {
		if !checked[id] {
			t.Errorf("fake.Client unexpectedly contains %s", id.String())
		}
	}
}

func newNotFound(id core.ID) error {
	return apierrors.NewNotFound(toGR(id.GroupKind), id.ObjectKey.String())
}

func newAlreadyExists(id core.ID) error {
	return apierrors.NewAlreadyExists(toGR(id.GroupKind), id.ObjectKey.String())
}

func newConflict(id core.ID, err error) error {
	return apierrors.NewConflict(toGR(id.GroupKind), id.ObjectKey.String(), err)
}

func newConflictingUID(id core.ID, expectedUID, foundUID string) error {
	return newConflict(id,
		fmt.Errorf("UID conflict: expected %q but found %q",
			expectedUID, foundUID))
}

func newConflictingResourceVersion(id core.ID, expectedRV, foundRV string) error {
	return newConflict(id,
		fmt.Errorf("ResourceVersion conflict: expected %q but found %q",
			expectedRV, foundRV))
}

func (c *Client) list(gk schema.GroupKind) []client.Object {
	var result []client.Object
	for _, o := range c.Objects {
		if o.GetObjectKind().GroupVersionKind().GroupKind() != gk {
			continue
		}
		result = append(result, o)
	}
	return result
}

// Applier returns a fake.Applier wrapping this fake.Client. Callers using the
// resulting Applier will read from/write to the original fake.Client.
func (c *Client) Applier() reconcile.Applier {
	return &Applier{Client: c}
}

// Scheme implements client.Client.
func (c *Client) Scheme() *runtime.Scheme {
	return c.scheme
}

// RESTMapper implements client.Client.
func (c *Client) RESTMapper() meta.RESTMapper {
	return c.mapper
}

// idFromObject returns the object's ID.
// If the GK isn't set, the Scheme is used to look it up by object type.
func (c *Client) idFromObject(obj client.Object) core.ID {
	id := core.IDOf(obj)
	if id.GroupKind.Empty() {
		gvk, err := kinds.Lookup(obj, c.scheme)
		if err != nil {
			panic(fmt.Sprintf("Failed to lookup object GVK: %v", err))
		}
		id.GroupKind = gvk.GroupKind()
	}
	return id
}

// convertToVersion converts the object to a typed object of the targetGVK.
// Does both object type conversion and version conversion.
func (c *Client) convertToVersion(obj runtime.Object, targetGVK schema.GroupVersionKind) (runtime.Object, bool, error) {
	objGVK, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return nil, false, err
	}
	if objGVK.GroupKind() != targetGVK.GroupKind() {
		// No match
		return nil, false, nil
	}
	if objGVK.Version != targetGVK.Version {
		// Version conversion goes through the unversioned internal type first.
		// This avoids all versions needing to know how to convert to all other versions.
		untypedObj, err := c.scheme.New(targetGVK.GroupKind().WithVersion(runtime.APIVersionInternal))
		if err != nil {
			return nil, false, err
		}
		err = c.scheme.Convert(obj, untypedObj, nil)
		if err != nil {
			return nil, false, err
		}
		obj = untypedObj
	}
	// Convert to the desired typed object
	tObj, err := c.scheme.New(targetGVK)
	if err != nil {
		return nil, false, err
	}
	err = c.scheme.Convert(obj, tObj, nil)
	if err != nil {
		return nil, false, err
	}
	return tObj, true, nil
}

// matchesListFilters returns true if the object matches the constaints
// specified by the ListOptions: Namespace, LabelSelector, and FieldSelector.
func (c *Client) matchesListFilters(obj runtime.Object, opts *client.ListOptions) (bool, error) {
	labels, fields, accessor, err := c.getAttrs(obj)
	if err != nil {
		return false, err
	}
	if opts.Namespace != "" && opts.Namespace != accessor.GetNamespace() {
		// No match
		return false, nil
	}
	if opts.LabelSelector != nil && !opts.LabelSelector.Matches(labels) {
		// No match
		return false, nil
	}
	if opts.FieldSelector != nil && !opts.FieldSelector.Matches(fields) {
		// No match
		return false, nil
	}
	// Match!
	return true, nil
}

// getAttrs returns the label set and field set from an object that can be used
// for query filtering. This is roughly equivelent to what's in the apiserver,
// except only supporting the few metadata fields that are supported by CRDs.
func (c *Client) getAttrs(obj runtime.Object) (labels.Set, fields.Fields, metav1.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, nil, err
	}
	labelSet := labels.Set(accessor.GetLabels())

	uObj, err := kinds.ToUnstructured(obj, c.scheme)
	if err != nil {
		return nil, nil, nil, err
	}
	uFields := &UnstructuredFields{Object: uObj}

	return labelSet, uFields, accessor, nil
}

func incrementResourceVersion(obj client.Object) error {
	rv := obj.GetResourceVersion()
	rvInt, err := strconv.Atoi(rv)
	if err != nil {
		return fmt.Errorf("failed to parse resourceVersion: %w", err)
	}
	obj.SetResourceVersion(strconv.Itoa(rvInt + 1))
	return nil
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
