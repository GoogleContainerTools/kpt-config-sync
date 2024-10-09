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

package watch

import (
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	utilwatch "kpt.dev/configsync/pkg/util/watch"
)

// Lister is any object that performs listing of a resource.
type Lister interface {
	// List returns a list of unstructured objects.
	// List can be cancelled with the input context.
	List(ctx context.Context, options metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

// Watcher is any object that performs watching of a resource.
type Watcher interface {
	// Watch starts watching at the specified version.
	// List can be cancelled with the input context.
	Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error)
}

// ListerWatcher is any object can perform both lists and watches.
// This is similar to the ListerWatcher in client-go, except it uses the
// dynamic client to return unstructured objects without requiring a local
// scheme.
type ListerWatcher interface {
	Lister
	Watcher
}

// ListerWatcherFactory knows how to build ListerWatchers for the specified
// GroupVersionKind and Namespace.
type ListerWatcherFactory func(gvk schema.GroupVersionKind, namespace string) ListerWatcher

// DynamicListerWatcherFactory provides a ListerWatcher method that implements
// the ListerWatcherFactory interface. It uses the specified DynamicClient and
// ResettableRESTMapper.
type DynamicListerWatcherFactory struct {
	DynamicClient *dynamic.DynamicClient
	Mapper        utilwatch.ResettableRESTMapper
}

// ListerWatcher constructs a ListerWatcher for the specified GroupVersionKind
// and Namespace.
func (dlwf *DynamicListerWatcherFactory) ListerWatcher(gvk schema.GroupVersionKind, namespace string) ListerWatcher {
	return NewListWatchFromClient(dlwf.DynamicClient, dlwf.Mapper, gvk, namespace)
}

// ListFunc knows how to list resources.
type ListFunc func(ctx context.Context, options metav1.ListOptions) (*unstructured.UnstructuredList, error)

// WatchFunc knows how to watch resources.
//
//nolint:revive // stuttering ok for consistency with client-go
type WatchFunc func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error)

// ListWatch implements the ListerWatcher interface.
// ListFunc and WatchFunc must not be nil
type ListWatch struct {
	ListFunc  ListFunc
	WatchFunc WatchFunc
}

// NewListWatchFromClient creates a new ListWatch.
func NewListWatchFromClient(dynamicClient dynamic.Interface, mapper meta.RESTMapper, gvk schema.GroupVersionKind, namespace string) *ListWatch {
	return NewFilteredListWatchFromClient(dynamicClient, mapper, gvk, namespace, func(*metav1.ListOptions) {})
}

// NewFilteredListWatchFromClient creates a new ListWatch that can dynamically
// modify ListOptions. This allows specifying option defaults and overrides.
func NewFilteredListWatchFromClient(dynamicClient dynamic.Interface, mapper meta.RESTMapper, gvk schema.GroupVersionKind, namespace string, optionsModifier func(options *metav1.ListOptions)) *ListWatch {
	listFunc := func(ctx context.Context, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		optionsModifier(&options)
		resourceClient, err := DynamicResourceClient(dynamicClient, mapper, gvk, namespace)
		if err != nil {
			return nil, fmt.Errorf("building lister: %w", err)
		}
		return resourceClient.List(ctx, options)
	}
	watchFunc := func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
		options.Watch = true
		optionsModifier(&options)
		resourceClient, err := DynamicResourceClient(dynamicClient, mapper, gvk, namespace)
		if err != nil {
			return nil, fmt.Errorf("building watcher: %w", err)
		}
		return resourceClient.Watch(ctx, options)
	}
	return &ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}

// List a set of apiserver resources
func (lw *ListWatch) List(ctx context.Context, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	// ListWatch is used in Reflector, which already supports pagination.
	// Don't paginate here to avoid duplication.
	return lw.ListFunc(ctx, options)
}

// Watch a set of apiserver resources
func (lw *ListWatch) Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	return lw.WatchFunc(ctx, options)
}

type listAndWatch struct {
	stopOnce sync.Once
	stopCh   chan struct{}
	eventCh  chan watch.Event
}

func (ww *listAndWatch) Stop() {
	ww.stopOnce.Do(func() {
		close(ww.stopCh)
	})
	<-ww.eventCh
}

func (ww *listAndWatch) ResultChan() <-chan watch.Event {
	return ww.eventCh
}

// ListAndWatch wraps a list and watch and returns a watch interface.
// This way, you can watch from "now" and get all the existing objects, not just
// new changes to objects.
//
// If ResourceVersion is specified, ListAndWatch acts like a normal Watch.
// Otherwise, a List is performed first and `Added` events are simulated,
// followed by a Watch, where subsequent `Added` events for pre-existing objects
// are converted to `Modified` events, unless that object is deleted first.
func ListAndWatch(ctx context.Context, lw ListerWatcher, opts metav1.ListOptions) (watch.Interface, error) {
	if opts.ResourceVersion != "" {
		// Just watch!
		return lw.Watch(ctx, opts)
	}

	opts.AllowWatchBookmarks = false // not allowed for List
	opts.Watch = false               // not allowed for List

	// TODO: Use a short timeout for List and longer for Watch

	cObjList, err := lw.List(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Specify ResourceVersion to ensure the Watch starts where the List stopped.
	// watch.Until will update the ListOptions for us.
	opts.ResourceVersion = cObjList.GetResourceVersion()

	// Enable bookmarks to ensure retries start from the latest ResourceVersion,
	// even if there haven't been any updates to the object being watched.
	opts.AllowWatchBookmarks = true
	opts.Watch = true

	wrapper := &listAndWatch{
		stopCh:  make(chan struct{}),
		eventCh: make(chan watch.Event),
	}

	// Spawn background thread to handle watching and pre-processing events.
	go func() {
		defer close(wrapper.eventCh)

		// De-dupe Added events to Modified events by caching the IDs of pre-existing objects.
		ids := make(map[core.ID]struct{}, len(cObjList.Items))

		// Send Added event for all pre-existing objects
		for i := range cObjList.Items {
			obj := cObjList.Items[i]
			ids[core.IDOf(&obj)] = struct{}{}
			event := watch.Event{Type: watch.Added, Object: &obj}
			select {
			case <-wrapper.stopCh:
				return
			case wrapper.eventCh <- event:
			}
		}

		// Start watching from the ResourceVersion received from List
		uWatch, err := lw.Watch(ctx, opts)
		if err != nil {
			statusErr, ok := err.(*apierrors.StatusError)
			if !ok {
				statusErr = NewClientError(fmt.Errorf("failed to start watching: %w", err))
			}
			event := watch.Event{Type: watch.Error, Object: &statusErr.ErrStatus}
			select {
			case <-wrapper.stopCh:
			case wrapper.eventCh <- event:
			}
			return
		}
		defer uWatch.Stop()

		// Pre-process events to de-dupe Added to Modified for pre-existing objects
		for e := range uWatch.ResultChan() {
			switch e.Type {
			case watch.Added:
				cObj, err := kinds.ObjectAsClientObject(e.Object)
				if err != nil {
					event := watch.Event{
						Type:   watch.Error,
						Object: &NewClientError(err).ErrStatus,
					}
					select {
					case <-wrapper.stopCh:
					case wrapper.eventCh <- event:
					}
					return
				}
				if _, found := ids[core.IDOf(cObj)]; found {
					// Convert from Added to Modified, because we already sent Added
					e.Type = watch.Modified
				}
			case watch.Deleted:
				cObj, err := kinds.ObjectAsClientObject(e.Object)
				if err != nil {
					event := watch.Event{
						Type:   watch.Error,
						Object: &NewClientError(err).ErrStatus,
					}
					select {
					case <-wrapper.stopCh:
					case wrapper.eventCh <- event:
					}
					return
				}
				// Remove from cache so that subsequent Added events are allowed
				delete(ids, core.IDOf(cObj))
			}
			select {
			case <-wrapper.stopCh:
				return
			case wrapper.eventCh <- e:
			}
		}
	}()

	return wrapper, nil
}

// StatusClientError is a "fake" HTTP status code used to indicate a client-side
// error. Normal status codes go from 1xx-5xx, so using 600 should be safe,
// unless Kubernetes starts using it.
const StatusClientError = 600

// StatusReasonClientError is the reason used for client-side errors.
// It's the human readable form of the `StatusClientError` status code.
const StatusReasonClientError metav1.StatusReason = "ClientError"

// NewClientError returns an error indicating that there was a client-side
// error. Unfortunately, the watch.Interface does not include a way to return
// client-side errors that preserves their error type. But at least this way
// we can tell the difference between a client-side error and a server-side
// error.
func NewClientError(err error) *apierrors.StatusError {
	return &apierrors.StatusError{
		ErrStatus: metav1.Status{
			Status: metav1.StatusFailure,
			Code:   StatusClientError,
			Reason: StatusReasonClientError,
			Details: &metav1.StatusDetails{
				Causes: []metav1.StatusCause{{Message: err.Error()}},
			},
			Message: fmt.Sprintf("Client error occurred (%T): %v", err, err),
		},
	}
}

// DynamicResourceClient uses a generic dynamic.Interface to build a
// resource-specific client.
//
//   - For cluster-scoped resources, namespace must be empty.
//   - For namespace-scoped resources, namespace may optionally be empty, to
//     include resources in all namespaces.
func DynamicResourceClient(dynamicClient dynamic.Interface, mapper meta.RESTMapper, gvk schema.GroupVersionKind, namespace string) (dynamic.ResourceInterface, error) {
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}
	switch mapping.Scope.Name() {
	case meta.RESTScopeNameRoot:
		if namespace != "" {
			return nil, fmt.Errorf("cannot query cluster-scoped resource %q in namespace %q", mapping.Resource, namespace)
		}
		// cluster-scope
		return dynamicClient.Resource(mapping.Resource), nil
	case meta.RESTScopeNameNamespace:
		if namespace != "" {
			return dynamicClient.Resource(mapping.Resource).Namespace(namespace), nil
		}
		// all namespaces
		return dynamicClient.Resource(mapping.Resource), nil
	default:
		return nil, fmt.Errorf("invalid resource scope %q for resource %q", mapping.Scope, mapping.Resource)
	}
}
