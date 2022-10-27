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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Copying strategy from k8s.io/client-go/tools/cache/reflector.go
	// We try to spread the load on apiserver by setting timeouts for
	// watch requests - it is random in [minWatchTimeout, 2*minWatchTimeout].
	minWatchTimeout = 5 * time.Minute

	// RESTConfigTimeout sets the REST config timeout for the remediator to 1 hour.
	//
	// RESTConfigTimeout should be longer than 2*minWatchTimeout to respect
	// the watch timeout set by `ListOptions.TimeoutSeconds` in the watch
	// create requests.
	RESTConfigTimeout = time.Hour
)

// maxWatchRetryFactor is used to determine when the next retry should happen.
// 2^^18 * time.Millisecond = 262,144 ms, which is about 4.36 minutes.
const maxWatchRetryFactor = 18

// Runnable defines the custom watch interface.
type Runnable interface {
	Stop()
	Run(ctx context.Context) status.Error
	ManagementConflict() bool
	SetManagementConflict(object client.Object)
	ClearManagementConflict()
	removeManagementConflictError(object client.Object)
	removeAllManagementConflictErrorsWithGVK(gvk schema.GroupVersionKind)
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
// - either present in the declared resources,
// - or managed by the same reconciler.
type filteredWatcher struct {
	gvk        string
	startWatch startWatchFunc
	resources  *declared.Resources
	queue      *queue.ObjectQueue
	scope      declared.Scope
	syncName   string
	// errorTracker maps an error to the time when the same error happened last time.
	errorTracker map[string]time.Time

	// The following fields are guarded by the mutex.
	mux                     sync.Mutex
	base                    watch.Interface
	stopped                 bool
	managementConflict      bool
	conflictErrMap          map[queue.GVKNN]status.ManagementConflictError
	addConflictErrorFunc    func(status.ManagementConflictError)
	removeConflictErrorFunc func(status.ManagementConflictError)
}

// filteredWatcher implements the Runnable interface.
var _ Runnable = &filteredWatcher{}

// NewFiltered returns a new filtered watch initialized with the given options.
func NewFiltered(_ context.Context, cfg watcherConfig) Runnable {
	return &filteredWatcher{
		gvk:                     cfg.gvk.String(),
		startWatch:              cfg.startWatch,
		resources:               cfg.resources,
		queue:                   cfg.queue,
		scope:                   cfg.scope,
		syncName:                cfg.syncName,
		base:                    watch.NewEmptyWatch(),
		errorTracker:            make(map[string]time.Time),
		conflictErrMap:          make(map[queue.GVKNN]status.ManagementConflictError),
		addConflictErrorFunc:    cfg.addConflictErrorFunc,
		removeConflictErrorFunc: cfg.removeConflictErrorFunc,
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

func (w *filteredWatcher) ManagementConflict() bool {
	w.mux.Lock()
	defer w.mux.Unlock()
	return w.managementConflict
}

func (w *filteredWatcher) SetManagementConflict(object client.Object) {
	w.mux.Lock()
	defer w.mux.Unlock()

	// The resource should be managed by a namespace reconciler, but now is updated.
	// Most likely the resource is now managed by a root reconciler.
	// In this case, set `managementConflict` true to trigger the namespace reconciler's
	// parse-apply-watch loop. The kpt_applier should report the ManagementConflictError (KNV1060).
	// No need to add the conflictError to the remediator because it will be surfaced by kpt_applier.
	if w.scope != declared.RootReconciler {
		w.managementConflict = true
		return
	}

	manager, found := object.GetAnnotations()[metadata.ResourceManagerKey]
	// There should be no conflict if the resource manager key annotation is not found
	// because any reconciler can manage a non-ConfigSync managed resource.
	if !found {
		klog.Warningf("No management conflict as the object %q is not managed by Config Sync", core.IDOf(object))
		return
	}
	// Root reconciler can override resource managed by namespace reconciler.
	// It is not a conflict in this case.
	if !declared.IsRootManager(manager) {
		klog.Warningf("No management conflict as the root reconciler %q should update the object %q that is managed by the namespace reconciler %q",
			w.syncName, core.IDOf(object), manager)
		return
	}

	// The remediator detects conflict between two root reconcilers.
	// Add the conflict error to the remediator, and the updateStatus goroutine will surface the ManagementConflictError (KNV1060).
	// It also sets `managementConflict` true to keep retrying the parse-apply-watch loop
	// so that the error can auto-resolve if the resource is removed from the conflicting manager's repository.
	w.managementConflict = true
	newManager := declared.ResourceManager(w.scope, w.syncName)
	klog.Warningf("The remediator detects a management conflict for object %q between root reconcilers: %q and %q",
		core.GKNN(object), newManager, manager)
	gvknn := queue.GVKNNOf(object)
	w.conflictErrMap[gvknn] = status.ManagementConflictErrorWrap(object, newManager)
	w.addConflictErrorFunc(w.conflictErrMap[gvknn])
	metrics.RecordResourceConflict(context.Background(), object.GetObjectKind().GroupVersionKind())
}

func (w *filteredWatcher) ClearManagementConflict() {
	w.mux.Lock()
	w.managementConflict = false
	w.mux.Unlock()
}

func (w *filteredWatcher) removeManagementConflictError(object client.Object) {
	w.mux.Lock()
	gvknn := queue.GVKNNOf(object)
	if conflictError, found := w.conflictErrMap[gvknn]; found {
		w.removeConflictErrorFunc(conflictError)
		delete(w.conflictErrMap, gvknn)
	}
	w.mux.Unlock()
}

func (w *filteredWatcher) removeAllManagementConflictErrorsWithGVK(gvk schema.GroupVersionKind) {
	w.mux.Lock()
	for gvknn, conflictError := range w.conflictErrMap {
		if gvknn.GroupVersionKind() == gvk {
			w.removeConflictErrorFunc(conflictError)
			delete(w.conflictErrMap, gvknn)
		}
	}
	w.mux.Unlock()
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

// This function is borrowed from https://github.com/kubernetes/client-go/blob/master/tools/cache/reflector.go.
func isExpiredError(err error) bool {
	// In Kubernetes 1.17 and earlier, the api server returns both apierrors.StatusReasonExpired and
	// apierrors.StatusReasonGone for HTTP 410 (Gone) status code responses. In 1.18 the kube server is more consistent
	// and always returns apierrors.StatusReasonExpired. For backward compatibility we can only remove the apierrors.IsGone
	// check when we fully drop support for Kubernetes 1.17 servers from reflectors.
	return apierrors.IsResourceExpired(err) || apierrors.IsGone(err)
}

// TODO: Use wait.ExponentialBackoff in the watch retry logic
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
func (w *filteredWatcher) Run(ctx context.Context) status.Error {
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
		klog.V(2).Infof("(Re)starting watch for %s at resource version %q", w.gvk, resourceVersion)
		for event := range w.base.ResultChan() {
			w.pruneErrors()
			newVersion, ignoreEvent, err := w.handle(ctx, event)
			eventCount++
			if ignoreEvent {
				ignoredEventCount++
			}
			if err != nil {
				if isExpiredError(err) {
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
		klog.V(2).Infof("Ending watch for %s at resource version %q (total events: %d, ignored events: %d)",
			w.gvk, resourceVersion, eventCount, ignoredEventCount)
	}
	klog.Infof("Watch stopped for %s", w.gvk)
	return nil
}

// start initiates a new base watch at the given resource version in a
// threadsafe manner and returns true if the new base watch was created. Returns
// false if the filteredWatcher is already stopped and returns error if the base
// watch could not be started.
func (w *filteredWatcher) start(resourceVersion string) (bool, status.Error) {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.stopped {
		return false, nil
	}
	w.base.Stop()

	// We want to avoid situations of hanging watchers. Stop any watchers that
	// do not receive any events within the timeout window.
	timeoutSeconds := int64(minWatchTimeout.Seconds() * (rand.Float64() + 1.0))
	options := metav1.ListOptions{
		AllowWatchBookmarks: true,
		ResourceVersion:     resourceVersion,
		TimeoutSeconds:      &timeoutSeconds,
		Watch:               true,
	}

	base, err := w.startWatch(options)
	if err != nil {
		return false, status.APIServerErrorf(err, "failed to start watch for %s", w.gvk)
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
// and an error indicating that a watch.Error event type is encountered and the
// watch should be restarted.
func (w *filteredWatcher) handle(ctx context.Context, event watch.Event) (string, bool, error) {
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
	object, ok := event.Object.(client.Object)
	if !ok {
		klog.Warningf("Received non client.Object in watch event: %T", object)
		metrics.RecordInternalError(ctx, "remediator")
		return "", false, nil
	}
	// filter objects.
	if !w.shouldProcess(object) {
		klog.V(4).Infof("Ignoring event for object: %v", object)
		return object.GetResourceVersion(), true, nil
	}

	if deleted {
		klog.V(2).Infof("Received watch event for deleted object %q", core.IDOf(object))
		object = queue.MarkDeleted(ctx, object)
	} else {
		klog.V(2).Infof("Received watch event for created/updated object %q", core.IDOf(object))
	}

	klog.V(3).Infof("Received object: %v", object)
	w.queue.Add(object)
	return object.GetResourceVersion(), false, nil
}

// shouldProcess returns true if the given object should be enqueued by the
// watcher for processing.
func (w *filteredWatcher) shouldProcess(object client.Object) bool {
	// Process the resource if we are the manager regardless if it is declared or not.
	if diff.IsManager(w.scope, w.syncName, object) {
		w.removeManagementConflictError(object)
		return true
	}
	id := core.IDOf(object)
	decl, ok := w.resources.Get(id)
	if !ok {
		// The resource is neither declared nor managed by the same reconciler, so don't manage it.
		return false
	}

	// If the object is declared, we only process it if it has the same GVK as
	// its declaration. Otherwise we expect to get another event for the same
	// object but with a matching GVK so we can actually compare it to its
	// declaration.
	if object.GetObjectKind().GroupVersionKind() != decl.GroupVersionKind() {
		return false
	}

	if !diff.CanManage(w.scope, w.syncName, object, diff.OperationManage) {
		w.SetManagementConflict(object)
		return false
	}
	w.removeManagementConflictError(object)
	return true
}
