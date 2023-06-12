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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	jserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
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
// The optional Precondition will be called after the Informer is Synced,
// meaning that both the List and Watch call have been made, after which it's
// safe to Delete the object and assume the watch will receive the delete event.
//
// The Precondition Store argument uses "<namespace>/<name>" or "<name>" keys
// and returns either a cache.DeletedFinalStateUnknown or an object of the type
// specified by the `items` field of the provided ObjectList (Kind without the
// "List" suffix). For example, UnstructuredList caches Unstructured objects.
//
// The Object ResourceVersion of the input object is ignored, because a new
// Informer is created which will do a ListAndWatch to determine which
// ResourceVersion to start watching from.
//
// The Object contents will be replaced with updated state for each new event
// received while waiting for deletion.
//
// For timeout, use:
// watchCtx, cancel := context.WithTimeout(ctx, 1*time.Minute)
// defer cancel()
//
// If you want to make sure something is deleted, delete it if it's not yet
// deleted, and then wait for it to be fully deleted, use DeleteAndWait instead.
func UntilDeletedWithSync(ctx context.Context, c client.WithWatch, obj client.Object, precondition watchtools.PreconditionFunc) error {
	itemGVK, err := kinds.Lookup(obj, c.Scheme())
	if err != nil {
		return err
	}
	var objList client.ObjectList
	if _, ok := obj.(*unstructured.Unstructured); ok {
		objList = kinds.NewUnstructuredListForItemGVK(itemGVK)
	} else {
		objList, err = kinds.NewTypedListForItemGVK(itemGVK, c.Scheme())
		if err != nil {
			return err
		}
	}
	watcher := &ClientListerWatcher{
		Context:     ctx,
		Client:      c,
		Key:         client.ObjectKeyFromObject(obj),
		Labels:      obj.GetLabels(),
		ExampleList: objList,
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
				// Stop waiting - precondition errored
				return true, err
			}
			if met {
				// Stop waiting - precondition met
				return true, nil
			}
		}

		_, exists, err := store.Get(obj)
		if err != nil {
			// Stop waiting - storage retrieval errored
			return true, err
		}
		if !exists {
			// Stop waiting - object already deleted
			return true, nil
		}

		// Continue waiting until deleted
		return false, nil
	}
	yamlSerializer := jserializer.NewYAMLSerializer(jserializer.DefaultMetaFactory, c.Scheme(), c.Scheme())
	decoder := serializer.NewCodecFactory(c.Scheme()).UniversalDeserializer()
	_, err = watchtools.UntilWithSync(ctx, watcher, obj, wrappedPrecondition, func(e watchapi.Event) (bool, error) {
		klog.V(5).Infof("Received %s event for %T", e.Type, e.Object)
		switch e.Type {
		case watchapi.Error:
			// Stop waiting - UntilWithSync uses UntilWithoutRetry
			return true, errors.Wrapf(apierrors.FromObject(e.Object),
				"error event while watching: %s", kinds.ObjectSummary(obj))
		case watchapi.Added, watchapi.Modified, watchapi.Bookmark:
			// Simulate DeepCopyInto by encoding the new object into the object
			// passed in by the caller.
			yamlBytes, err := runtime.Encode(yamlSerializer, e.Object)
			if err != nil {
				// Stop waiting - invalid object
				return true, errors.Wrapf(err, "encoding event object to YAML while watching: %s",
					kinds.ObjectSummary(obj))
			}
			_, _, err = decoder.Decode(yamlBytes, nil, obj)
			if err != nil {
				// Stop waiting - invalid object
				return true, errors.Wrapf(err, "decoding event object YAML to %T while watching: %s",
					obj, kinds.ObjectSummary(obj))
			}
			// Keep waiting - object still exists
			return false, nil
		case watchapi.Deleted:
			// Stop waiting - object successfully deleted
			return true, nil
		default:
			// Stop waiting - invalid event type
			return true, errors.Errorf("unexpected event type %s while watching: %s: %v",
				e.Type, kinds.ObjectSummary(obj), e)
		}
	})
	return err
}
