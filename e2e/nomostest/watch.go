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

package nomostest

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/util/log"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchOption is an optional parameter for Watch
type WatchOption func(watch *watchSpec)

type watchSpec struct {
	timeout         time.Duration
	errorOnNotFound bool
}

// WatchTimeout provides the timeout option to Watch.
func WatchTimeout(timeout time.Duration) WatchOption {
	return func(watch *watchSpec) {
		watch.timeout = timeout
	}
}

// watchErrorOnNotFound configures Watch to error if the object is deleted or
// not found.
// This is a private option. You probably want to use WatchForNotFound instead.
func watchErrorOnNotFound() WatchOption {
	return func(watch *watchSpec) {
		watch.errorOnNotFound = true
	}
}

// WatchObjectNotFoundError is returned by WatchObject when the watched object
// is deleted or not found, if WatchErrorOnDelete is enabled.
type WatchObjectNotFoundError struct {
	GroupVersionKind schema.GroupVersionKind
	Name             string
	Namespace        string
}

func (wde *WatchObjectNotFoundError) Error() string {
	return fmt.Sprintf("expected %s %s/%s to exist and continue to exist, but it was deleted or not found",
		wde.GroupVersionKind.Kind, wde.Namespace, wde.Name)
}

// WatchObject watches the specified object util all predicates return nil,
// or the timeout is reached. Object does not need to exist yet, as long as the
// resource type exists.
func WatchObject(nt *NT, gvk schema.GroupVersionKind, name, namespace string, predicates []Predicate, opts ...WatchOption) error {
	errPrefix := fmt.Sprintf("WatchObject(%s %s/%s)", gvk.Kind, namespace, name)

	startTime := time.Now()
	defer func() {
		took := time.Since(startTime)
		nt.T.Logf("%s watched for %v", errPrefix, took)
	}()

	spec := watchSpec{
		timeout: nt.DefaultWaitTimeout,
	}
	for _, opt := range opts {
		opt(&spec)
	}

	cObjList, err := newObjectListForObjectKind(gvk, nt.scheme)
	if err != nil {
		return err
	}

	var listOpts []client.ListOption
	listOpts = append(listOpts, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector("metadata.name", name),
	})
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	ctx, cancel := context.WithTimeout(nt.Context, spec.timeout)
	defer cancel()

	// Use LIST with a single-object selector, instead of GET, because we want
	// the resource version of the LIST response to start the watch with, so we
	// don't miss any updates.

	// Use the Client, instead of the WatchClient, because it has a shorter timeout.
	err = nt.Client.List(ctx, cObjList, listOpts...)
	if err != nil {
		return err
	}
	// The Items field of a ObjectList is a typed array.
	// So we have to use reflection to get the items out.
	items, err := meta.ExtractList(cObjList)
	if err != nil {
		return errors.Wrapf(err, "%s unable to understand list result %#v",
			errPrefix, cObjList)
	}
	// Iterate through the list to find the desired object, if it exists
	var prevObj client.Object
	for _, rObj := range items {
		cObj, ok := rObj.(client.Object)
		if !ok {
			return errors.Wrapf(
				WrongTypeErr(rObj, client.Object(nil)),
				"%s unexpected list item type", errPrefix)
		}
		if cObj.GetName() != name {
			// Ignore other objects, if any.
			// Should never happen, due to name selector.
			continue
		}
		prevObj = cObj
		break
	}
	if prevObj == nil {
		if spec.errorOnNotFound {
			return &WatchObjectNotFoundError{
				GroupVersionKind: gvk,
				Name:             name,
				Namespace:        namespace,
			}
		}
		nt.DebugLogf("%s GET Not Found", errPrefix)
	} else {
		// Log the initial/current state as a diff to make it easy to compare with update diffs.
		// Use diff.Diff because it doesn't truncate like cmp.Diff does.
		// Use log.AsYAMLWithScheme to get the full scheme-consistent YAML with GVK.
		nt.DebugLogf("%s GET Diff (+ Current):\n%s",
			errPrefix, log.AsYAMLDiffWithScheme(nil, prevObj, nt.scheme))
	}

	rv := cObjList.GetResourceVersion()

	// Create a new empty list to use with WATCH
	cObjList, err = newObjectListForObjectKind(gvk, nt.scheme)
	if err != nil {
		return err
	}

	// Specify ResourceVersion to ensure the Watch starts where the List stopped.
	cObjList.SetResourceVersion(rv)

	// Use the WatchClient, instead of the Client, because it has a longer timeout.
	result, err := nt.WatchClient.Watch(ctx, cObjList, listOpts...)
	defer result.Stop()
	if err != nil {
		return err
	}

	// Save the predicate errors from the last state change and return them
	// if the watch closes before the predicates all pass.
	var prevErrs []error

	for event := range result.ResultChan() {
		rObj := event.Object
		cObj, ok := rObj.(client.Object)
		if !ok {
			return errors.Errorf("%s expected event object of type client.Object but got %T",
				errPrefix, rObj)
		}
		if cObj.GetName() != name {
			// Ignore events for other objects, if any.
			// Should never happen, due to name selector.
			continue
		}

		switch event.Type {
		case watch.Bookmark:
			// do nothing
		case watch.Deleted:
			nt.DebugLogf("%s %s", errPrefix, event.Type)
			prevObj = nil
			if spec.errorOnNotFound {
				return &WatchObjectNotFoundError{
					GroupVersionKind: gvk,
					Name:             name,
					Namespace:        namespace,
				}
			} // Else, continue watching. The object may be re-created.
		case watch.Added, watch.Modified:
			eType := event.Type
			if eType == watch.Added && prevObj != nil {
				// Added to cache for the first time, but not to k8s
				eType = watch.Modified
			}
			nt.DebugLogf("%s %s Diff (- Removed, + Added):\n%s",
				errPrefix, eType,
				log.AsYAMLDiffWithScheme(prevObj, cObj, nt.scheme))
			var errs []error
			for _, predicate := range predicates {
				if err := predicate(cObj); err != nil {
					errs = append(errs, err)
				}
			}
			if len(errs) == 0 {
				// Success! passed all predicates!
				return nil
			}
			prevErrs = errs
			prevObj = cObj
		case watch.Error:
			return errors.Errorf("%s received error event: %#v",
				errPrefix, event)
		default:
			return errors.Errorf("%s received unexpected event: %#v",
				errPrefix, event)
		}
	}

	if len(prevErrs) > 0 {
		// watch exited with predicate errors
		return multierr.Combine(prevErrs...)
	}
	return errors.Errorf("%s exited before any add/update events were received",
		errPrefix)
}

// WatchForCurrentStatus watches the object until it reconciles (Current).
func WatchForCurrentStatus(nt *NT, gvk schema.GroupVersionKind, name, namespace string, opts ...WatchOption) error {
	return WatchObject(nt, gvk, name, namespace,
		[]Predicate{StatusEquals(nt, kstatus.CurrentStatus)},
		opts...)
}

// WatchForNotFound waits for the passed object to be fully deleted or not found.
// Returns an error if the object is not deleted within the timeout.
func WatchForNotFound(nt *NT, gvk schema.GroupVersionKind, name, namespace string, opts ...WatchOption) error {
	opts = append(opts, watchErrorOnNotFound())
	err := WatchObject(nt, gvk, name, namespace,
		[]Predicate{objectNotFoundPredicate},
		opts...)
	if err == nil {
		// Should never happen!
		panic("WatchForNotFound exited without error or timeout")
	}
	if nfe := (&WatchObjectNotFoundError{}); errors.As(err, &nfe) {
		// yay!
		return nil
	}
	return err
}

// objectNotFound always returns an error.
// This is a dummy predicate which will cause WatchOject to continue watching
// until another error or timeout is reached.
// If you see this error, the timeout was probably reached.
func objectNotFoundPredicate(o client.Object) error {
	return errors.Errorf("expected %T object %s to be not found",
		o, core.ObjectNamespacedName(o))
}

func newObjectListForObjectKind(gvk schema.GroupVersionKind, scheme *runtime.Scheme) (client.ObjectList, error) {
	listGVK := gvk.GroupVersion().WithKind(gvk.Kind + "List")
	rObjList, err := scheme.New(listGVK)
	if err != nil {
		return nil, err
	}
	cObjList, ok := rObjList.(client.ObjectList)
	if !ok {
		return nil, errors.Wrapf(
			WrongTypeErr(rObjList, client.ObjectList(nil)),
			"list type is invalid: %s", listGVK)
	}
	return cObjList, nil
}
