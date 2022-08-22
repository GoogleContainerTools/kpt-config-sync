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
	"sync"
	"testing"

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
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/syncer/reconcile"
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
	mapper  meta.RESTMapper
	Objects map[core.ID]client.Object
	eventCh chan watch.Event
	Now     func() metav1.Time

	lock     sync.RWMutex
	watchers map[chan watch.Event]struct{}
}

var _ client.Client = &Client{}

// RealNow is the default Now function used by NewClient.
func RealNow() metav1.Time {
	return metav1.Now()
}

// NewClient instantiates a new fake.Client pre-populated with the specified
// objects.
//
// Calls t.Fatal if unable to properly instantiate Client.
func NewClient(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) *Client {
	t.Helper()

	result := Client{
		scheme:   scheme,
		Objects:  make(map[core.ID]client.Object),
		eventCh:  make(chan watch.Event),
		watchers: make(map[chan watch.Event]struct{}),
		Now:      RealNow,
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
	result.mapper = testutil.NewFakeRESTMapper(result.allKnownGVKs()...)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Start event fan-out
	go result.handleEvents(ctx)

	for _, o := range objs {
		err = result.Create(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
	}

	return &result
}

// allKnownGVKs returns an unsorted list of GVKs known by the scheme and mapper.
func (c *Client) allKnownGVKs() []schema.GroupVersionKind {
	typeMap := c.scheme.AllKnownTypes()
	gvkList := make([]schema.GroupVersionKind, 0, len(typeMap))
	for gvk := range typeMap {
		gvkList = append(gvkList, gvk)
	}
	return gvkList
}

func (c *Client) handleEvents(ctx context.Context) {
	doneCh := ctx.Done()
	defer close(c.eventCh)
	for {
		select {
		case <-doneCh:
			// Context is cancelled or timed out.
			return
		case event, ok := <-c.eventCh:
			if !ok {
				// Input channel is closed.
				return
			}
			// Input event recieved.
			c.sendEventToWatchers(ctx, event)
		}
	}
}

func (c *Client) addWatcher(eventCh chan watch.Event) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.watchers[eventCh] = struct{}{}
}

func (c *Client) removeWatcher(eventCh chan watch.Event) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.watchers, eventCh)
}

func (c *Client) sendEventToWatchers(ctx context.Context, event watch.Event) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	klog.V(5).Infof("Broadcasting %s event for %T", event.Type, event.Object)

	doneCh := ctx.Done()
	for watcher := range c.watchers {
		klog.V(5).Infof("Narrowcasting %s event for %T", event.Type, event.Object)
		watcher := watcher
		go func() {
			select {
			case <-doneCh:
				// Context is cancelled or timed out.
			case watcher <- event:
				// Event recieved or channel closed
			}
		}()
	}
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

	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return err
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	id := core.IDOf(obj)
	cachedObj, ok := c.Objects[id]
	if !ok {
		return newNotFound(id)
	}

	// Convert to a typed object, optionally convert between versions
	tObj, matches, err := c.convertToVersion(cachedObj, gvk)
	if err != nil {
		return err
	}
	if !matches {
		// Someone lied about their GVK...
		return errors.Errorf("fake.Client.Get failed to convert object %s from %q to %q",
			key, cachedObj.GetObjectKind().GroupVersionKind(), gvk)
	}

	err = c.scheme.Convert(tObj, obj, nil)
	if err != nil {
		return err
	}
	// TODO: Does the normal client.Get always populate GVK on typed objects?
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	klog.V(5).Infof("Getting %T %s (ResourceVersion: %q): %#v",
		obj, gvk.Kind, obj.GetResourceVersion(), obj)
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

	// Lookup desired List type
	listGVK, err := kinds.Lookup(list, c.scheme)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(listGVK.Kind, "List") {
		return errors.Errorf("fake.Client.List(UnstructuredList) called with non-List GVK %q", listGVK.String())
	}

	// Get the List results from the cache as an UnstructuredList.
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(listGVK)

	// Get the item GVK
	gvk := listGVK.GroupVersion().WithKind(strings.TrimSuffix(listGVK.Kind, "List"))

	for _, obj := range c.list(gvk.GroupKind()) {
		// Convert to a typed object, optionally convert between versions
		tObj, matches, err := c.convertToVersion(obj, gvk)
		if err != nil {
			return err
		}
		if !matches {
			// Someone lied about their GVK...
			return errors.Errorf("fake.Client.List failed to convert object %s from %q to %q",
				client.ObjectKeyFromObject(obj), obj.GetObjectKind().GroupVersionKind(), gvk)
		}
		// Convert typed object to unstructured
		uObj, err := c.toUnstructured(tObj)
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

	err = c.scheme.Convert(uList, list, nil)
	if err != nil {
		return err
	}
	// TODO: Does the normal client.List always populate GVK on typed objects?
	// Some of the code seems to require this...
	list.GetObjectKind().SetGroupVersionKind(listGVK)

	klog.V(5).Infof("Listing %T %s (ResourceVersion: %q): %#v",
		list, gvk.Kind, list.GetResourceVersion(), list)
	return nil
}

func (c *Client) fromUnstructured(obj client.Object) (client.Object, error) {
	// If possible, we want to deal with the non-Unstructured form of objects.
	// Unstructureds are prone to declare a bunch of empty maps we don't care
	// about, and can't easily tell cmp.Diff to ignore.

	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return nil, err
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	if _, isUnstructured := obj.(*unstructured.Unstructured); !isUnstructured {
		// Already not unstructured
		return obj, nil
	}

	tObj, err := c.Scheme().New(gvk)
	if err != nil {
		return nil, fmt.Errorf("type not registered with scheme: %s", gvk)
	}

	err = c.scheme.Convert(obj, tObj, nil)

	// Copy GVK to make avoid diff between types
	tObj.GetObjectKind().SetGroupVersionKind(gvk)

	return tObj.(client.Object), err
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
	createOpts := &client.CreateOptions{}
	createOpts.ApplyOptions(opts)
	err := validateCreateOptions(createOpts)
	if err != nil {
		return err
	}

	tObj, err := c.fromUnstructured(obj)
	if err != nil {
		return err
	}

	id := core.IDOf(tObj)
	_, found := c.Objects[id]
	if found {
		return newAlreadyExists(id)
	}

	initResourceVersion(tObj)
	klog.V(5).Infof("Creating %T %s (ResourceVersion: %q)",
		tObj, client.ObjectKeyFromObject(tObj), tObj.GetResourceVersion())

	// Copy ResourceVersion change back to input object
	if obj != tObj {
		obj.SetResourceVersion(tObj.GetResourceVersion())
	}

	c.Objects[id] = tObj
	c.eventCh <- watch.Event{
		Type:   watch.Added,
		Object: tObj,
	}
	return nil
}

func validateDeleteOptions(opts []client.DeleteOption) error {
	var unsupported []client.DeleteOption
	for _, opt := range opts {
		switch opt {
		case client.PropagationPolicy(metav1.DeletePropagationForeground):
		case client.PropagationPolicy(metav1.DeletePropagationBackground):
		default:
			unsupported = append(unsupported, opt)
		}
	}
	if len(unsupported) > 0 {
		jsn, _ := json.MarshalIndent(opts, "", "  ")
		return errors.Errorf("fake.Client.Delete does not yet support opts, but got: %v", string(jsn))
	}

	return nil
}

// Delete implements client.Client.
func (c *Client) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	err := validateDeleteOptions(opts)
	options := client.DeleteOptions{}
	options.ApplyOptions(opts)
	if err != nil {
		return err
	}

	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return err
	}
	// Copy obj to avoid mutation
	obj = obj.DeepCopyObject().(client.Object)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	id := core.IDOf(obj)
	cachedObj, found := c.Objects[id]
	if !found {
		return newNotFound(id)
	}

	// Simulate apiserver delayed deletion for finalizers
	if cachedObj.GetDeletionTimestamp() == nil && len(cachedObj.GetFinalizers()) > 0 {
		klog.V(5).Infof("Found %d finalizers: Adding deleteTimestamp to %T %s (ResourceVersion: %q)",
			len(cachedObj.GetFinalizers()), cachedObj, client.ObjectKeyFromObject(cachedObj), cachedObj.GetResourceVersion())
		newObj := cachedObj.DeepCopyObject().(client.Object)
		now := c.Now()
		newObj.SetDeletionTimestamp(&now)
		err := c.Update(ctx, newObj)
		if err != nil {
			return err
		}
		return nil
	}

	klog.V(5).Infof("Deleting %T %s (ResourceVersion: %q)", cachedObj, client.ObjectKeyFromObject(cachedObj), cachedObj.GetResourceVersion())

	// Default to background deletion propagation, if unspecified
	if options.PropagationPolicy == nil {
		pp := metav1.DeletePropagationBackground
		options.PropagationPolicy = &pp
	}

	switch *options.PropagationPolicy {
	case metav1.DeletePropagationForeground:
		c.deleteManagedObjects(id)
		delete(c.Objects, id)
	case metav1.DeletePropagationBackground:
		// TODO: impliment thread-safe background deletion propagation.
		// For now, just emulate foreground deletion propagation instead.
		c.deleteManagedObjects(id)
		delete(c.Objects, id)
	default:
		return errors.Errorf("unsupported PropagationPolicy: %v", *options.PropagationPolicy)
	}

	c.eventCh <- watch.Event{
		Type:   watch.Deleted,
		Object: cachedObj,
	}

	return nil
}

// deleteManagedObjects deletes objects whose ownerRef is the specified obj.
func (c *Client) deleteManagedObjects(id core.ID) {
	for _, o := range c.Objects {
		for _, ownerRef := range o.GetOwnerReferences() {
			if ownerRef.Name == id.Name && ownerRef.Kind == id.Kind {
				delete(c.Objects, core.IDOf(o))
			}
		}
	}
}

func (c *Client) toUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return nil, err
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	if uObj, isUnstructured := obj.(*unstructured.Unstructured); isUnstructured {
		// Already unstructured
		return uObj, nil
	}

	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)
	err = c.scheme.Convert(obj, uObj, nil)
	if err != nil {
		return nil, err
	}
	uObj.SetGroupVersionKind(gvk)
	return uObj, nil
}

func (c *Client) getStatusFromObject(obj client.Object) (map[string]interface{}, bool, error) {
	u, err := c.toUnstructured(obj)
	if err != nil {
		return nil, false, err
	}
	return unstructured.NestedMap(u.Object, "status")
}

func (c *Client) updateObjectStatus(obj client.Object, status map[string]interface{}) (client.Object, error) {
	uObj, err := c.toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	if err = unstructured.SetNestedMap(uObj.Object, status, "status"); err != nil {
		return obj, err
	}
	return c.fromUnstructured(uObj)
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

// Update implements client.Client. It does not update the status field.
func (c *Client) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	updateOpts := &client.UpdateOptions{}
	updateOpts.ApplyOptions(opts)
	err := validateUpdateOptions(updateOpts)
	if err != nil {
		return err
	}

	tObj, err := c.fromUnstructured(obj)
	if err != nil {
		return err
	}

	id := core.IDOf(tObj)
	cachedObj, found := c.Objects[id]
	if !found {
		return newNotFound(id)
	}

	oldStatus, hasStatus, err := c.getStatusFromObject(cachedObj)
	if err != nil {
		return err
	}
	if len(updateOpts.DryRun) > 0 {
		// don't merge or store the result
		return nil
	}

	tObj.SetResourceVersion(cachedObj.GetResourceVersion())
	err = incrementResourceVersion(tObj)
	if err != nil {
		return fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	klog.V(5).Infof("Updating %T %s (ResourceVersion: %q)",
		tObj, client.ObjectKeyFromObject(tObj), tObj.GetResourceVersion())

	// Copy ResourceVersion change back to input object
	if obj != tObj {
		obj.SetResourceVersion(tObj.GetResourceVersion())
	}

	if hasStatus {
		tObj, err = c.updateObjectStatus(tObj, oldStatus)
		if err != nil {
			return err
		}
	}
	c.Objects[id] = tObj
	c.eventCh <- watch.Event{
		Type:   watch.Modified,
		Object: tObj,
	}

	// Simulate apiserver garbage collection
	if tObj.GetDeletionTimestamp() != nil && len(tObj.GetFinalizers()) == 0 {
		klog.V(5).Infof("Found deleteTimestamp and 0 finalizers: Deleting %T %s (ResourceVersion: %q)",
			tObj, client.ObjectKeyFromObject(tObj), tObj.GetResourceVersion())
		err := c.Delete(ctx, tObj)
		if err != nil {
			return err
		}
	}
	return nil
}

// Patch implements client.Client.
func (c *Client) Patch(ctx context.Context, obj client.Object, _ client.Patch, opts ...client.PatchOption) error {
	// Currently re-using the Update implementation for Patch since it fits the use-case where this is used for unit tests.
	// Please use this with caution for your use-case.
	var updateOpts []client.UpdateOption
	for _, opt := range opts {
		if uOpt, ok := opt.(client.UpdateOption); ok {
			updateOpts = append(updateOpts, uOpt)
		} else if opt == client.ForceOwnership {
			// Ignore, for now.
			// TODO: Simulate FieldManagement and Force updates.
		} else {
			return errors.Errorf("invalid patch option: %+v", opt)
		}
	}
	return c.Update(ctx, obj, updateOpts...)
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

	tObj, err := s.Client.fromUnstructured(obj)
	if err != nil {
		return err
	}

	id := core.IDOf(tObj)
	cachedObj, found := s.Client.Objects[id]
	if !found {
		return newNotFound(id)
	}

	newStatus, hasStatus, err := s.Client.getStatusFromObject(tObj)
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

	err = incrementResourceVersion(cachedObj)
	if err != nil {
		return fmt.Errorf("failed to increment resourceVersion: %w", err)
	}
	klog.V(5).Infof("Updating Status %T %s (ResourceVersion: %q)", cachedObj, client.ObjectKeyFromObject(cachedObj), cachedObj.GetResourceVersion())

	// Copy ResourceVersion change back to input object
	if obj != cachedObj {
		obj.SetResourceVersion(cachedObj.GetResourceVersion())
	}

	u, err := s.Client.updateObjectStatus(cachedObj, newStatus)
	if err != nil {
		return err
	}

	s.Client.Objects[id] = u
	return nil
}

// Patch implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Patch(ctx context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return s.Update(ctx, obj)
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
		obj, err := c.fromUnstructured(obj)
		if err != nil {
			// This is a test precondition, and if it fails the following error
			// messages will be garbage.
			t.Fatal(err)
		}
		wantMap[core.IDOf(obj)] = obj
	}

	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())
	checked := make(map[core.ID]bool)
	for id, want := range wantMap {
		checked[id] = true
		actual, found := c.Objects[id]
		if !found {
			t.Errorf("fake.Client missing %s", id.String())
			continue
		}

		_, wantUnstructured := want.(*unstructured.Unstructured)
		_, actualUnstructured := actual.(*unstructured.Unstructured)
		if wantUnstructured != actualUnstructured {
			// If you see this error, you should register the type so the code can
			// compare them properly.
			t.Errorf("got want.(type)=%T and actual.(type)=%T for two objects of type %s, want equal",
				want, actual, want.GetObjectKind().GroupVersionKind().String())
			continue
		}
		asserter.Equal(t, want, actual, "expected object (%s) to be equal", id.String())
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

// Watch implements client.WithWatch.
func (c *Client) Watch(ctx context.Context, objList client.ObjectList, opts ...client.ListOption) (watch.Interface, error) {
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	ctx, cancel := context.WithCancel(ctx)
	fw := &fakeWatcher{
		inCh:        make(chan watch.Event),
		outCh:       make(chan watch.Event),
		cancel:      cancel,
		objList:     objList,
		options:     options,
		convertFunc: c.convertToListItemType,
		matchFunc:   c.matchesListFilters,
	}
	c.addWatcher(fw.inCh)

	go func() {
		defer func() {
			cancel()
			c.removeWatcher(fw.inCh)
		}()
		fw.handleEvents(ctx)
	}()

	return fw, nil
}

type fakeWatcher struct {
	inCh        chan watch.Event
	outCh       chan watch.Event
	cancel      context.CancelFunc
	objList     client.ObjectList
	options     *client.ListOptions
	convertFunc func(runtime.Object, client.ObjectList) (obj runtime.Object, matchesGK bool, err error)
	matchFunc   func(runtime.Object, *client.ListOptions) (matches bool, err error)
}

func (fw *fakeWatcher) handleEvents(ctx context.Context) {
	doneCh := ctx.Done()
	defer close(fw.outCh)
	for {
		select {
		case <-doneCh:
			// Context is cancelled or timed out.
			return
		case event, ok := <-fw.inCh:
			if !ok {
				// Input channel is closed.
				return
			}
			// Input event recieved.
			fw.sendEvent(ctx, event)
		}
	}
}

func (fw *fakeWatcher) sendEvent(ctx context.Context, event watch.Event) {
	klog.V(5).Infof("Filtering %s event for %T", event.Type, event.Object)

	// Convert input object type to desired object type and version, if possible
	obj, matches, err := fw.convertFunc(event.Object, fw.objList)
	if err != nil {
		fw.sendEvent(ctx, watch.Event{
			Type:   watch.Error,
			Object: &apierrors.NewInternalError(err).ErrStatus,
		})
		return
	}
	if !matches {
		// No match
		return
	}
	event.Object = obj

	// Check if input object matches list option filters
	matches, err = fw.matchFunc(event.Object, fw.options)
	if err != nil {
		fw.sendEvent(ctx, watch.Event{
			Type:   watch.Error,
			Object: &apierrors.NewInternalError(err).ErrStatus,
		})
		return
	}
	if !matches {
		// No match
		return
	}

	klog.V(5).Infof("Sending %s event for %T", event.Type, event.Object)

	doneCh := ctx.Done()
	select {
	case <-doneCh:
		// Context is cancelled or timed out.
		return
	case fw.outCh <- event:
		// Event recieved or channel closed
	}
}

func (fw *fakeWatcher) Stop() {
	fw.cancel()
}

func (fw *fakeWatcher) ResultChan() <-chan watch.Event {
	return fw.outCh
}

// Applier returns a fake.Applier wrapping this fake.Client. Callers using the
// resulting Applier will read from/write to the original fake.Client.
func (c *Client) Applier() reconcile.Applier {
	return &applier{Client: c}
}

// Scheme implements client.Client.
func (c *Client) Scheme() *runtime.Scheme {
	return c.scheme
}

// RESTMapper implements client.Client.
func (c *Client) RESTMapper() meta.RESTMapper {
	return c.mapper
}

// convertToVersion converts the object to a typed object of the targetGVK.
// Does both object type conversion and verion conversion.
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

// convertToListItemType converts the object to the type of an item in the
// specified collection. Does both object type conversion and verion conversion.
func (c *Client) convertToListItemType(obj runtime.Object, objListType client.ObjectList) (runtime.Object, bool, error) {
	// Lookup the List type from the scheme
	listGVK, err := kinds.Lookup(objListType, c.scheme)
	if err != nil {
		return nil, false, err
	}
	// Convert the List type to the Item type
	targetKind := strings.TrimSuffix(listGVK.Kind, "List")
	if targetKind == listGVK.Kind {
		return nil, false, fmt.Errorf("collection kind does not have required List suffix: %s", listGVK.Kind)
	}
	targetGVK := listGVK.GroupVersion().WithKind(targetKind)
	// Convert to a typed object, optionally convert between versions
	tObj, matches, err := c.convertToVersion(obj, targetGVK)
	if err != nil {
		return nil, false, err
	}
	if !matches {
		return nil, false, nil
	}
	// Convert to unstructured, if needed
	if _, ok := objListType.(*unstructured.UnstructuredList); ok {
		if uObj, ok := tObj.(*unstructured.Unstructured); ok {
			return uObj, true, nil
		}
		uObj, err := c.toUnstructured(tObj)
		if err != nil {
			return nil, false, err
		}
		return uObj, true, nil
	}
	return tObj, true, nil
}

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

	uObj, err := c.toUnstructured(obj)
	if err != nil {
		return nil, nil, nil, err
	}
	uFields := &unstructuredFields{obj: uObj}

	return labelSet, uFields, accessor, nil
}

type unstructuredFields struct {
	obj *unstructured.Unstructured
}

func (uf *unstructuredFields) fields(field string) []string {
	field = strings.TrimPrefix(field, ".")
	return strings.Split(field, ".")
}

// Has returns whether the provided field exists.
func (uf *unstructuredFields) Has(field string) (exists bool) {
	_, found, err := unstructured.NestedString(uf.obj.Object, uf.fields(field)...)
	return err == nil && found
}

// Get returns the value for the provided field.
func (uf *unstructuredFields) Get(field string) (value string) {
	val, found, err := unstructured.NestedString(uf.obj.Object, uf.fields(field)...)
	if err != nil || !found {
		return ""
	}
	return val
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
	rv := obj.GetResourceVersion()
	if rv == "" {
		obj.SetResourceVersion("1")
	}
}
