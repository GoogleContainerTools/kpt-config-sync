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

package watch

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UntilDeletedWithSync watches the specified object and blocks until it is
// fully deleted (NotFound, not just deleteTimestamp). This allows for any
// finalizers to complete.
//
// If the object is not yet deleted, the delete call will use foreground
// deletion, waiting untile garbage collection completes too. However, if the
// object was already deleted
//
// If the object is Unstructured, the GVK must be set.
//
// The ResourceVersion of the input object is ignored, because a new Informer is
// created which will do a ListAndWatch, and List needs the collection
// ResourceVersion instead of the object ResourceVersion.
//
// The optional Precondition will be called after Informer is Synced,
// meaning that both the List and Watch call have been made, after which it's
// safe to Delete the object and assume the watch will receive the delete event.
//
// The Precondition Store argument uses "<namespace>/<name>" or "<name>" keys
// and returns either a cache.DeletedFinalStateUnknown or an object of the type
// specified by the `items` field of the provided ObjectList (Kind without the
// "List" suffix). For example, UnstructuredList caches Unstructured objects.
//
// For timeout, use:
// watchCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
// defer cancel()
//
// Given the complexities of using this, if you want to make sure something is
// deleted and delete it if it's not yet deleted and then wait for it to be
// fully deleted, use DeleteAndWait instead.
func UntilDeletedWithSync(ctx context.Context, c client.WithWatch, obj client.Object, precondition watchtools.PreconditionFunc) error {
	gvk, err := kinds.Lookup(obj, c.Scheme())
	if err != nil {
		return err
	}
	// Yes, "collections" all just add "List" to the kind...
	// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.12.3/pkg/cache/internal/informers_map.go#L280
	objListObj, err := c.Scheme().New(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
	if err != nil {
		return err
	}
	objList, ok := objListObj.(client.ObjectList)
	if !ok {
		return fmt.Errorf("collection type is not an object list: %T", objListObj)
	}

	watcher := &flexWatcher{
		Context: ctx,
		Client:  c,
		Key:     client.ObjectKeyFromObject(obj),
		Labels:  obj.GetLabels(),
		ObjList: objList,
	}
	// Precondition runs after Watch and Sync but before Watch events are handled.
	// So use it to confirm that the object still exists in the watch cache,
	// otherwise the watcher will never receive the deletion event.
	// In that case, just exit early.
	// Precondition returning true means exit early & cancel the Watch.
	wrappedPrecondition := func(store cache.Store) (bool, error) {
		// Run the caller's precondition first, if one was supplied.
		if precondition != nil {
			met, err := precondition(store)
			if err != nil {
				// Stop waiting
				return false, err
			}
			if met {
				// Stop waiting - precondition met
				return true, nil
			}
		}

		_, exists, err := store.Get(obj)
		if err != nil {
			// Stop waiting
			return false, err
		}
		if !exists {
			// Stop waiting - object already deleted
			return true, nil
		}

		// Continue waiting until deleted
		return false, nil
	}
	_, err = watchtools.UntilWithSync(ctx, watcher, obj, wrappedPrecondition, func(e watchapi.Event) (bool, error) {
		klog.V(5).Infof("Received %s event for %T", e.Type, e.Object)
		switch e.Type {
		case watchapi.Error:
			return false, fmt.Errorf("watch failed: %v", e.Object)
		case watchapi.Added, watchapi.Modified, watchapi.Bookmark:
			return false, nil
		case watchapi.Deleted:
			// Stop waiting - object successfully deleted
			return true, nil
		default:
			return false, fmt.Errorf("received unexpected event type %s while watching: %v", e.Type, e)
		}
	})
	return err
}

// flexWatcher wraps a generic client.WithWatch and impliments the
// resource-specific cache.ListerWatcher interface, with optional filters:
// Name, Namespace, and Labels. The filters are converted into ListOptions.
type flexWatcher struct {
	// Context to pass to client list and watch methods
	Context context.Context

	// Client to list and watch with
	Client client.WithWatch

	// Key is the name & namespace to watch (both optional)
	Key client.ObjectKey

	// Labels to filter with (optional)
	Labels map[string]string

	// ObjList is the collection type to list & watch
	ObjList client.ObjectList
}

// Watch satisfies the cache.Lister interface
func (fw *flexWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	objList, ok := fw.ObjList.DeepCopyObject().(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("failed to cast deep copy of %T to ObjectList", fw.ObjList)
	}
	opts := fw.listOptions()
	klog.V(5).Infof("Listing %T %s: %v", objList, objList.GetObjectKind().GroupVersionKind().Kind, opts)
	err := fw.Client.List(fw.Context, objList, opts...)
	return objList, err
}

// Watch satisfies the cache.Watcher interface
func (fw *flexWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	objList, ok := fw.ObjList.DeepCopyObject().(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("failed to cast deep copy of %T to ObjectList", fw.ObjList)
	}
	opts := fw.listOptions()
	klog.V(5).Infof("Watching %T %s: %v", objList, objList.GetObjectKind().GroupVersionKind().Kind, opts)
	return fw.Client.Watch(fw.Context, objList, opts...)
}

func (fw *flexWatcher) listOptions() []client.ListOption {
	var opts []client.ListOption
	if len(fw.Key.Namespace) > 0 {
		opts = append(opts, client.InNamespace(fw.Key.Namespace))
	}
	if len(fw.Key.Name) > 0 {
		opts = append(opts, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("metadata.name", fw.Key.Name),
		})
	}
	if len(fw.Labels) > 0 {
		opts = append(opts, client.MatchingLabels(fw.Labels))
	}
	return opts
}
