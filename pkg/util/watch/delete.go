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
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These log fields
const (
	logFieldObject = "object"
	logFieldKind   = "kind"
)

// DeleteAndWait deletes the object (if not already deleted) and blocks until it
// is fully deleted (NotFound, not just deleteTimestamp). This allows for any
// finalizers to complete.
//
// Exits early if the object is already deleted or doesn't exist.
//
// If you want to time out the full function, use a context.WithTimeout.
// If you want to time out just the waiting after the delete, use reconcileTimeout.
func DeleteAndWait(ctx context.Context, c client.WithWatch, obj client.Object, reconcileTimeout time.Duration) error {
	key := client.ObjectKeyFromObject(obj)
	gvk, err := kinds.Lookup(obj, c.Scheme())
	if err != nil {
		return err
	}

	// Use cancellable context to delay starting the timer until after the
	// watcher has synchronized and the delete call has completed.
	watchCtx, cancel := context.WithCancel(ctx)
	// Stop early, if done or errored before timeout.
	defer cancel()
	// Delay timer start until after Sync and Delete.
	startTimer := func() {
		timerCtx, cancel2 := context.WithTimeout(watchCtx, reconcileTimeout)
		go func() {
			<-timerCtx.Done()
			cancel()
			cancel2()
		}()
	}

	klog.V(3).Infof("Deletion watcher starting %q=%q, %q=%q",
		logFieldObject, key.String(),
		logFieldKind, gvk.Kind)

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
			logFieldObject, key.String(),
			logFieldKind, gvk.Kind)

		klog.V(3).Infof("Deletion watcher cache keys: %v", store.ListKeys())

		// Synced - test for existance
		lastKnownObj, exists, err := store.Get(obj)
		if err != nil {
			// Stop waiting
			return false, err
		}
		if !exists {
			// Stop waiting - object already deleted or never existed
			klog.V(3).Infof("Object already deleted %q=%q, %q=%q",
				logFieldObject, key.String(),
				logFieldKind, gvk.Kind)
			klog.V(3).Infof("Cache Keys: %#v", store.ListKeys())
			return true, nil
		}

		// UntilDeletedWithSync->UntilWithSync->NewIndexerInformerWatcher->NewIndexerInformer.
		// NewIndexerInformer returns cache.cache as the indexer that impliments
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
				logFieldObject, key.String(),
				logFieldKind, gvk.Kind)
			return true, nil
		}
		// Since we supplied a client.Object, we know the objects returned
		// will be client.Object.
		cObj, ok := lastKnownObj.(client.Object)
		if !ok {
			return false, fmt.Errorf("unexpected watch cache object %T for object %s %s: %+v",
				lastKnownObj, gvk.Kind, key, lastKnownObj)
		}
		// Check if the object was already deleted but still terminating
		if cObj.GetDeletionTimestamp() != nil {
			// Continue waiting until fully deleted - already deleting
			klog.V(3).Infof("Object already deleting %q=%q, %q=%q",
				logFieldObject, key.String(),
				logFieldKind, gvk.Kind)
			// Note: In this case, we can't use Foreground deletion to wait for
			// managed resources to be garbage collected by the apiserver.
			// TODO: Confirm that Delete Foreground after Delete Background does not add GC finalizer.
			return false, nil
		}

		klog.V(1).Infof("Deleting object %q=%q, %q=%q",
			logFieldObject, key.String(),
			logFieldKind, gvk.Kind)

		// Object exists - safe to delete now
		// Use foreground deletion to ensure all managed objects are garbage
		// collected by the apiserver before continuing.
		if err := c.Delete(ctx, cObj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(3).Infof("Object already deleted %q=%q, %q=%q",
					logFieldObject, key.String(),
					logFieldKind, gvk.Kind)
				// Stop waiting - object already deleted
				return true, nil
			}
			// Stop waiting
			return false, err
		}
		klog.V(3).Infof("Object delete successful %q=%q, %q=%q",
			logFieldObject, key.String(),
			logFieldKind, gvk.Kind)

		// Continue waiting until fully deleted
		return false, nil
	})
	if err != nil {
		return errors.Wrapf(err, "failed to watch for deletion of %s: %s", gvk.Kind, key)
	}
	klog.V(1).Infof("Object delete confirmed %q=%q, %q=%q",
		logFieldObject, key.String(),
		logFieldKind, gvk.Kind)
	return nil
}
