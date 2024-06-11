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
	"errors"
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
	"kpt.dev/configsync/pkg/remediator/conflict"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/log"
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

	// ClientWatchDecodingCause is the status cause returned for client errors during response decoding.
	// https://github.com/kubernetes/client-go/blob/v0.26.7/rest/request.go#L785
	ClientWatchDecodingCause metav1.CauseType = "ClientWatchDecoding"

	// ContextCancelledCauseMessage is the error message from the DynamicClient
	// when the Watch method errors due to context cancellation.
	// https://github.com/kubernetes/apimachinery/blob/v0.26.7/pkg/watch/streamwatcher.go#L120
	ContextCancelledCauseMessage = "unable to decode an event from the watch stream: context canceled"
)

// maxWatchRetryFactor is used to determine when the next retry should happen.
// 2^^18 * time.Millisecond = 262,144 ms, which is about 4.36 minutes.
const maxWatchRetryFactor = 18

// Runnable defines the custom watch interface.
type Runnable interface {
	Stop()
	Run(ctx context.Context) status.Error
	ManagementConflict() bool
	SetManagementConflict(object client.Object, commit string)
	ClearManagementConflicts()
	ClearManagementConflictsWithKind(gk schema.GroupKind)
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
	startWatch WatchFunc
	resources  *declared.Resources
	queue      *queue.ObjectQueue
	scope      declared.Scope
	syncName   string
	// errorTracker maps an error to the time when the same error happened last time.
	errorTracker map[string]time.Time

	// The following fields are guarded by the mutex.
	mux                sync.Mutex
	base               watch.Interface
	stopped            bool
	managementConflict bool
	conflictHandler    conflict.Handler
}

// filteredWatcher implements the Runnable interface.
var _ Runnable = &filteredWatcher{}

// NewFiltered returns a new filtered watcher initialized with the given options.
func NewFiltered(cfg watcherConfig) Runnable {
	return &filteredWatcher{
		gvk:             cfg.gvk.String(),
		startWatch:      cfg.startWatch,
		resources:       cfg.resources,
		queue:           cfg.queue,
		scope:           cfg.scope,
		syncName:        cfg.syncName,
		base:            watch.NewEmptyWatch(),
		errorTracker:    make(map[string]time.Time),
		conflictHandler: cfg.conflictHandler,
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

func (w *filteredWatcher) SetManagementConflict(object client.Object, commit string) {
	w.mux.Lock()
	defer w.mux.Unlock()

	// The resource should be managed by a namespace reconciler, but now is updated.
	// Most likely the resource is now managed by a root reconciler.
	// In this case, set `managementConflict` true to trigger the namespace reconciler's
	// parse-apply-watch loop. The kpt_applier should report the ManagementConflictError (KNV1060).
	// No need to add the conflictError to the remediator because it will be surfaced by kpt_applier.
	if w.scope != declared.RootScope {
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
	w.conflictHandler.AddConflictError(core.IDOf(object), status.ManagementConflictErrorWrap(object, newManager))
	metrics.RecordResourceConflict(context.Background(), commit)
}

func (w *filteredWatcher) ClearManagementConflicts() {
	w.mux.Lock()
	w.managementConflict = false
	w.mux.Unlock()
}

func (w *filteredWatcher) ClearManagementConflictsWithKind(gk schema.GroupKind) {
	w.conflictHandler.ClearConflictErrorsWithKind(gk)
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

// isContextCancelledStatusError returns true if the error is a *StatusError and
// one of the detail cause reasons is `ClientWatchDecoding` with the message
// `unable to decode an event from the watch stream: context canceled`.
// StatusError doesn't implement Is or Unwrap methods, and all http client
// errors are returned as decoding errors, so we have to test the cause message
// to detect context cancellation.
// This explicitly does not test for context timeout, because that's used for
// http client timeout, which we want to retry, and we don't currently have any
// timeout on the parent context.
func isContextCancelledStatusError(err error) bool {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		if message, found := findStatusErrorCauseMessage(statusErr, ClientWatchDecodingCause); found {
			if message == ContextCancelledCauseMessage {
				return true
			}
		}
	}
	return false
}

// findStatusErrorCauseMessage returns the message and true, if the StatusError
// has a .detail.cause[] entry that matches the specified type, otherwise
// returns false.
func findStatusErrorCauseMessage(statusErr *apierrors.StatusError, causeType metav1.CauseType) (string, bool) {
	if statusErr == nil || statusErr.ErrStatus.Details == nil {
		return "", false
	}
	for _, cause := range statusErr.ErrStatus.Details.Causes {
		if cause.Type == causeType {
			return cause.Message, true
		}
	}
	return "", false
}

// Run reads the event from the base watch interface,
// filters the event and pushes the object contained
// in the event to the controller work queue.
func (w *filteredWatcher) Run(ctx context.Context) status.Error {
	klog.Infof("Watch started for %s", w.gvk)
	var resourceVersion string
	var retriesForWatchError int
	var runErr status.Error

Watcher:
	for {
		// There are three ways start can return:
		// 1. false, error -> We were unable to start the watch, so exit Run().
		// 2. false, nil   -> We have been stopped via Stop(), so exit Run().
		// 3. true,  nil   -> We have not been stopped and we started a new watch.
		started, err := w.start(ctx, resourceVersion)
		if err != nil {
			return err
		}
		if !started {
			break
		}

		eventCount := 0
		ignoredEventCount := 0
		klog.V(2).Infof("(Re)starting watch for %s at resource version %q", w.gvk, resourceVersion)
		eventCh := w.base.ResultChan()
	EventHandler:
		for {
			select {
			case <-ctx.Done():
				runErr = status.InternalWrapf(ctx.Err(), "remediator watch stopped for %s", w.gvk)
				// Stop the watcher & return the status error
				break Watcher
			case event, ok := <-eventCh:
				if !ok { // channel closed
					// Restart the watcher
					break EventHandler
				}
				w.pruneErrors()
				newVersion, ignoreEvent, err := w.handle(ctx, event)
				eventCount++
				if ignoreEvent {
					ignoredEventCount++
				}
				if err != nil {
					if errors.Is(err, context.Canceled) || isContextCancelledStatusError(err) {
						// The error wrappers are especially confusing for
						// users, so just return context.Canceled.
						runErr = status.InternalWrapf(context.Canceled, "remediator watch stopped for %s", w.gvk)
						// Stop the watcher & return the status error
						break Watcher
					}
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
					// Restart the watcher
					break EventHandler
				}
				retriesForWatchError = 0
				if newVersion != "" {
					resourceVersion = newVersion
				}
			}
		}
		klog.V(2).Infof("Ending watch for %s at resource version %q (total events: %d, ignored events: %d)",
			w.gvk, resourceVersion, eventCount, ignoredEventCount)
	}
	klog.Infof("Watch stopped for %s", w.gvk)
	return runErr
}

// start initiates a new base watch at the given resource version in a
// threadsafe manner and returns true if the new base watch was created. Returns
// false if the filteredWatcher is already stopped and returns error if the base
// watch could not be started.
func (w *filteredWatcher) start(ctx context.Context, resourceVersion string) (bool, status.Error) {
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

	base, err := w.startWatch(ctx, options)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, status.InternalWrapf(err, "failed to start watch for %s", w.gvk)
		}
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
	klog.Infof("Handling watch event %v %v", event.Type, w.gvk)
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

	if klog.V(5).Enabled() {
		klog.V(5).Infof("Received watch event for object: %q (generation: %d): %s",
			core.IDOf(object), object.GetGeneration(), log.AsJSON(object))
	} else {
		klog.V(3).Infof("Received watch event for object: %q (generation: %d)",
			core.IDOf(object), object.GetGeneration())
	}

	// filter objects.
	if !w.shouldProcess(object) {
		klog.V(4).Infof("Ignoring event for object: %q (generation: %d)",
			core.IDOf(object), object.GetGeneration())
		return object.GetResourceVersion(), true, nil
	}

	if deleted {
		klog.V(2).Infof("Received watch event for deleted object %q (generation: %d)",
			core.IDOf(object), object.GetGeneration())
		object = queue.MarkDeleted(ctx, object)
	} else {
		klog.V(2).Infof("Received watch event for created/updated object %q (generation: %d)",
			core.IDOf(object), object.GetGeneration())
	}

	w.queue.Add(object)
	return object.GetResourceVersion(), false, nil
}

// shouldProcess returns true if the given object should be enqueued by the
// watcher for processing.
func (w *filteredWatcher) shouldProcess(object client.Object) bool {
	id := core.IDOf(object)
	// Process the resource if we are the manager regardless if it is declared or not.
	if diff.IsManager(w.scope, w.syncName, object) {
		w.conflictHandler.RemoveConflictError(id)
		return true
	}
	decl, commit, found := w.resources.Get(id)
	if !found {
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
		w.SetManagementConflict(object, commit)
		return false
	}
	w.conflictHandler.RemoveConflictError(id)
	return true
}
