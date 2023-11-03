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
	"math"
	"math/rand"
	"reflect"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sevent "sigs.k8s.io/controller-runtime/pkg/event"

	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"kpt.dev/resourcegroup/controllers/resourcemap"
	"kpt.dev/resourcegroup/controllers/status"
)

const (
	// Copying strategy from k8s.io/client-go/tools/cache/reflector.go
	// We try to spread the load on apiserver by setting timeouts for
	// watch requests - it is random in [minWatchTimeout, 2*minWatchTimeout].
	minWatchTimeout = 5 * time.Minute
)

// maxWatchRetryFactor is used to determine when the next retry should happen.
// 2^^18 * time.Millisecond = 262,144 ms, which is about 4.36 minutes.
const maxWatchRetryFactor = 18

// Runnable defines the custom watch interface.
type Runnable interface {
	Stop()
	Run(ctx context.Context) error
}

const (
	watchEventBookmarkType    = "Bookmark"
	watchEventErrorType       = "Error"
	watchEventUnsupportedType = "Unsupported"
)

// errorLoggingInterval specifies the minimal time interval two errors related to the same object
// and having the same errorType should be logged.
const errorLoggingInterval = time.Second

// filteredWatcher is wrapper around a watch interface.
// It only keeps the events for objects that are
// listed in a ResourceGroup CR.
type filteredWatcher struct {
	gvk        string
	startWatch startWatchFunc
	resources  *resourcemap.ResourceMap
	// errorTracker maps an error to the time when the same error happened last time.
	errorTracker map[string]time.Time

	// channel is the channel for ResourceGroup generic events.
	channel chan k8sevent.GenericEvent

	// The following fields are guarded by the mutex.
	mux     sync.Mutex
	base    watch.Interface
	stopped bool
}

// filteredWatcher implements the Runnable interface.
var _ Runnable = &filteredWatcher{}

// NewFiltered returns a new filtered watch initialized with the given options.
func NewFiltered(_ context.Context, cfg watcherConfig) Runnable {
	return &filteredWatcher{
		gvk:          cfg.gvk.String(),
		startWatch:   cfg.startWatch,
		resources:    cfg.resources,
		base:         watch.NewEmptyWatch(),
		errorTracker: make(map[string]time.Time),
		channel:      cfg.channel,
	}
}

// pruneErrors removes the errors happened before errorLoggingInterval from w.errorTracker.
// This is to save the memory usage for tracking errors.
func (w *filteredWatcher) pruneErrors() {
	for errName, lastErrorTime := range w.errorTracker {
		if time.Since(lastErrorTime) >= errorLoggingInterval {
			delete(w.errorTracker, errName)
		}
	}
}

// addError checks whether an error identified by the errorID has been tracked,
// and handles it in one of the following ways:
//   - tracks it if it has not yet been tracked;
//   - updates the time for this error to time.Now() if `errorLoggingInterval` has passed
//     since the same error happened last time;
//   - ignore the error if `errorLoggingInterval` has NOT passed since it happened last time.
//
// addError returns false if the error is ignored, and true if it is not ignored.
func (w *filteredWatcher) addError(errorID string) bool {
	lastErrorTime, ok := w.errorTracker[errorID]
	if !ok || time.Since(lastErrorTime) >= errorLoggingInterval {
		w.errorTracker[errorID] = time.Now()
		return true
	}
	return false
}

// Stop fully stops the filteredWatcher in a threadsafe manner. This means that
// it stops the underlying base watch and prevents the filteredWatcher from
// restarting it (like it does if the API server disconnects the base watch).
func (w *filteredWatcher) Stop() {
	w.mux.Lock()
	defer w.mux.Unlock()

	w.base.Stop()
	w.stopped = true
}

func waitUntilNextRetry(retries int) {
	if retries > maxWatchRetryFactor {
		retries = maxWatchRetryFactor
	}
	milliseconds := int64(math.Pow(2, float64(retries)))
	duration := time.Duration(milliseconds) * time.Millisecond
	time.Sleep(duration)
}

// Run reads the event from the base watch interface,
// filters the event and pushes the object contained
// in the event to the controller work queue.
func (w *filteredWatcher) Run(context.Context) error {
	klog.Infof("Watch started for %s", w.gvk)
	var resourceVersion string
	var retriesForWatchError int

	for {
		// There are three ways this function can return:
		// 1. false, error -> We were unable to start the watch, so exit Run().
		// 2. false, nil   -> We have been stopped via Stop(), so exit Run().
		// 3. true,  nil   -> We have not been stopped and we started a new watch.
		started, err := w.start(resourceVersion)
		if err != nil {
			return err
		}
		if !started {
			break
		}

		eventCount := 0
		ignoredEventCount := 0
		klog.Infof("(Re)starting watch for %s at resource version %q", w.gvk, resourceVersion)
		for event := range w.base.ResultChan() {
			w.pruneErrors()
			newVersion, ignoreEvent, err := w.handle(event)
			eventCount++
			if ignoreEvent {
				ignoredEventCount++
			}
			if err != nil {
				if apierrors.IsResourceExpired(err) {
					klog.Infof("Watch for %s at resource version %q closed with: %v", w.gvk, resourceVersion, err)
					// `w.handle` may fail because we try to watch an old resource version, setting
					// a watch on an old resource version will always fail.
					// Reset `resourceVersion` to an empty string here so that we can start a new
					// watch at the most recent resource version.
					resourceVersion = ""
				} else if w.addError(watchEventErrorType + errorID(err)) {
					klog.Errorf("Watch for %s at resource version %q ended with: %v", w.gvk, resourceVersion, err)
				}
				retriesForWatchError++
				waitUntilNextRetry(retriesForWatchError)
				// Call `break` to restart the watch.
				break
			}
			retriesForWatchError = 0
			if newVersion != "" {
				resourceVersion = newVersion
			}
		}
		klog.Infof("Ending watch for %s at resource version %q (total events: %d, ignored events: %d)",
			w.gvk, resourceVersion, eventCount, ignoredEventCount)
	}
	klog.Infof("Watch stopped for %s", w.gvk)
	return nil
}

// start initiates a new base watch at the given resource version in a
// threadsafe manner and returns true if the new base watch was created. Returns
// false if the filteredWatcher is already stopped and returns error if the base
// watch could not be started.
func (w *filteredWatcher) start(resourceVersion string) (bool, error) {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.stopped {
		return false, nil
	}
	w.base.Stop()

	// We want to avoid situations of hanging watchers. Stop any watchers that
	// do not receive any events within the timeout window.
	//nolint:gosec // don't need cryptographic randomness here
	timeoutSeconds := int64(minWatchTimeout.Seconds() * (rand.Float64() + 1.0))
	options := metav1.ListOptions{
		AllowWatchBookmarks: true,
		ResourceVersion:     resourceVersion,
		TimeoutSeconds:      &timeoutSeconds,
		Watch:               true,
	}

	base, err := w.startWatch(options)
	if err != nil {
		return false, fmt.Errorf("failed to start watch for %s: %v", w.gvk, err)
	}
	w.base = base
	return true, nil
}

func errorID(err error) string {
	errTypeName := reflect.TypeOf(err).String()

	var s string
	switch t := err.(type) {
	case *apierrors.StatusError:
		if t == nil {
			break
		}
		if t.ErrStatus.Details != nil {
			s = t.ErrStatus.Details.Name
		}
		if s == "" {
			s = fmt.Sprintf("%s-%s-%d", t.ErrStatus.Status, t.ErrStatus.Reason, t.ErrStatus.Code)
		}
	}
	return errTypeName + s
}

// handle reads the event from the base watch interface,
// filters the event and pushes the object contained
// in the event to the controller work queue.
//
// handle returns the new resource version, whether the event should be ignored,
// and an error indicating that a watch.Error event type was encountered and the
// watch should be restarted.
func (w *filteredWatcher) handle(event watch.Event) (string, bool, error) {
	var deleted bool
	switch event.Type {
	case watch.Added, watch.Modified:
		deleted = false
	case watch.Deleted:
		deleted = true
	case watch.Bookmark:
		m, err := meta.Accessor(event.Object)
		if err != nil {
			// For watch.Bookmark, only the ResourceVersion field of event.Object is set.
			// Therefore, set the second argument of w.addError to watchEventBookmarkType.
			if w.addError(watchEventBookmarkType) {
				klog.Errorf("Unable to access metadata of Bookmark event: %v", event)
			}
			return "", false, nil
		}
		return m.GetResourceVersion(), false, nil
	case watch.Error:
		return "", false, apierrors.FromObject(event.Object)
	// Keep the default case to catch any new watch event types added in the future.
	default:
		if w.addError(watchEventUnsupportedType) {
			klog.Errorf("Unsupported watch event: %#v", event)
		}
		return "", false, nil
	}

	// get client.Object from the runtime object.
	object, ok := event.Object.(*unstructured.Unstructured)
	if !ok {
		klog.Infof("Received non unstructured object in watch event: %T", object)
		return "", false, nil
	}
	// filter objects.
	id := getID(object)
	if !w.shouldProcess(object) {
		klog.V(4).Infof("Ignoring event for object: %v", id)
		return object.GetResourceVersion(), true, nil
	}

	if deleted {
		klog.Infof("updating the reconciliation status: %v: %v", id, v1alpha1.NotFound)
		w.resources.SetStatus(id, &resourcemap.CachedStatus{Status: v1alpha1.NotFound})
	} else {
		klog.Infof("Received watch event for created/updated object %q", id)
		resStatus := status.ComputeStatus(object)
		if resStatus != nil {
			klog.Infof("updating the reconciliation status: %v: %v", id, resStatus.Status)
			w.resources.SetStatus(id, resStatus)
		}
	}

	for _, r := range w.resources.Get(id) {
		resgroup := &v1alpha1.ResourceGroup{}
		resgroup.SetNamespace(r.Namespace)
		resgroup.SetName(r.Name)

		klog.Infof("sending a generic event from watcher for %v", resgroup.GetObjectMeta())
		w.channel <- k8sevent.GenericEvent{Object: resgroup}
	}

	return object.GetResourceVersion(), false, nil
}

// shouldProcess returns true if the given object should be enqueued by the
// watcher for processing.
func (w *filteredWatcher) shouldProcess(object client.Object) bool {
	if w.resources == nil {
		klog.V(4).Infof("The resources are empty")
	}
	id := getID(object)
	return w.resources.HasResource(id)
}

func getID(object client.Object) v1alpha1.ObjMetadata {
	gvk := object.GetObjectKind().GroupVersionKind()
	id := v1alpha1.ObjMetadata{
		Name:      object.GetName(),
		Namespace: object.GetNamespace(),
		GroupKind: v1alpha1.GroupKind{
			Group: gvk.Group,
			Kind:  gvk.Kind,
		},
	}
	return id
}
