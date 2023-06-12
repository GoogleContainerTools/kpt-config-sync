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
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These constants are used for log field keys.
// They mirror the keys used by the controller-manager.
const (
	logFieldObjectRef  = "object"
	logFieldObjectKind = "objectKind"
)

// DeleteAndWait deletes the object (if not already deleted) and blocks until it
// is fully deleted (NotFound, not just deleteTimestamp). This allows for any
// finalizers to complete.
//
// Exits early if the object is already deleted or doesn't exist.
//
// The Object contents will be replaced with updated state for each new event
// received while waiting for deletion.
//
// If you want to time out the full function, use a context.WithTimeout.
// If you want to time out just the waiting after the delete, use reconcileTimeout.
func DeleteAndWait(ctx context.Context, c client.WithWatch, obj client.Object, reconcileTimeout time.Duration) error {
	gvk, err := kinds.Lookup(obj, c.Scheme())
	if err != nil {
		return err
	}
	id := core.ID{
		ObjectKey: client.ObjectKeyFromObject(obj),
		GroupKind: gvk.GroupKind(),
	}

	// Use cancellable context to delay starting the timer until after the
	// watcher has synchronized and the delete call has completed.
	watchCtx, watchCancel := context.WithCancel(ctx)
	// Stop early, if done or errored before timeout.
	defer watchCancel()
	// Delay timer start until after Sync and Delete.
	startTimer := func() {
		timerCtx, timerCancel := context.WithTimeout(watchCtx, reconcileTimeout)
		go func() {
			<-timerCtx.Done()
			watchCancel()
			timerCancel()
		}()
	}

	klog.V(3).Infof("Deletion watcher starting %q=%q, %q=%q",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)

	// Wait until the object is actually deleted.
	// Delete may be delayed by finalizers.
	// Perform the Delete in the precondition, which executes after the list and
	// watch calls have started, to ensure the delete event is received by the watch.
	err = UntilDeletedWithSync(watchCtx, c, obj, func(store cache.Store) (bool, error) {
		// Start timer after predicate returns.
		// The timer will be cancelled eventually, whether there's an error,
		// early exit, or timeout.
		defer startTimer()

		klog.V(3).Infof("Deletion watcher synced %q=%q, %q=%q",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)

		klog.V(3).Infof("Deletion watcher cache keys: %v", store.ListKeys())

		// Synced - test for existence
		lastKnownObj, exists, err := store.Get(obj)
		if err != nil {
			// Stop waiting
			return false, err
		}
		if !exists {
			// Stop waiting - object already deleted or never existed
			klog.V(3).Infof("Object already deleted %q=%q, %q=%q",
				logFieldObjectRef, id.ObjectKey.String(),
				logFieldObjectKind, id.Kind)
			klog.V(3).Infof("Cache Keys: %#v", store.ListKeys())
			return true, nil
		}

		// UntilDeletedWithSync->UntilWithSync->NewIndexerInformerWatcher->NewIndexerInformer.
		// NewIndexerInformer returns cache.cache as the indexer that implements
		// the cache.Store, but the cache is populated by DeltaFIFO, which
		// wraps objects with DeletedFinalStateUnknown sometimes.
		staleObj, stale := lastKnownObj.(cache.DeletedFinalStateUnknown)
		if stale {
			// Object was deleted but the watch deletion event was missed while
			// disconnected from apiserver. So the last known object may be
			// stale, but metadata.deletionTimestamp may still be available, if
			// that update was received before the disconnect.
			lastKnownObj = staleObj.Obj
		}
		if lastKnownObj == nil {
			// The object either never existed or was deleted before the initial
			// List or both created and deleted after List but while Watch was
			// disconnected from the apiserver.
			// Alternatively, the cache/Indexer.Get may have errored
			// (errors are swallowed by DeltaFIFO.Replace).

			// Stop waiting - object already deleted or never existed
			klog.V(3).Infof("Object already deleted %q=%q, %q=%q",
				logFieldObjectRef, id.ObjectKey.String(),
				logFieldObjectKind, id.Kind)
			return true, nil
		}
		// Since we supplied a client.Object, we know the objects returned
		// will be client.Object.
		cObj, ok := lastKnownObj.(client.Object)
		if !ok {
			return false, errors.Errorf("unexpected watch cache object %T for object %s %s: %+v",
				lastKnownObj, id.Kind, id.ObjectKey, lastKnownObj)
		}
		// Check if the object was already deleted but still terminating
		if cObj.GetDeletionTimestamp() != nil {
			// Continue waiting until fully deleted - already deleting
			klog.V(3).Infof("Object already deleting %q=%q, %q=%q",
				logFieldObjectRef, id.ObjectKey.String(),
				logFieldObjectKind, id.Kind)
			// Note: Whoever initiated the delete chose what deletion
			// propagation policy to use. We can't change that now.
			// If Foreground was used, the metav1.FinalizerDeleteDependents
			// finalizer will have been added to enforce it.
			return false, nil
		}

		klog.V(1).Infof("Deleting object %q=%q, %q=%q",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)

		// Object exists - safe to delete now
		// Use foreground deletion to ensure all managed objects are garbage
		// collected by the apiserver before continuing.
		if err := c.Delete(ctx, cObj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(3).Infof("Object already deleted %q=%q, %q=%q",
					logFieldObjectRef, id.ObjectKey.String(),
					logFieldObjectKind, id.Kind)
				// Stop waiting - object already deleted
				return true, nil
			}
			// Stop waiting
			return false, err
		}
		klog.V(3).Infof("Object delete successful %q=%q, %q=%q",
			logFieldObjectRef, id.ObjectKey.String(),
			logFieldObjectKind, id.Kind)

		// Continue waiting until fully deleted
		return false, nil
	})
	if err != nil {
		return errors.Wrapf(err, "failed to watch for deletion of %s: %s", id.Kind, id.ObjectKey)
	}
	klog.V(1).Infof("Object delete confirmed %q=%q, %q=%q",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)
	return nil
}
