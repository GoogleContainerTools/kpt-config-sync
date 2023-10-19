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

package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator"
	"kpt.dev/configsync/pkg/remediator/cache"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// Remediator is a controller that watches for timer-based events.
// When an object is added to the remediate resource list, it enqueues the GKNN
// of the object as a remediate request. The reconciler dequeues the request
// and remediate the object to match the declared resource.
type Remediator struct {
	// ControllerCtx is set in reconciler/main.go and is canceled by the Finalizer.
	ControllerCtx context.Context
	// DoneCh indicates the Remediator stops processing new requests.
	// It is a signal for the Finalizer to continue to finalize managed resources.
	DoneCh chan struct{}
	// SyncScope is the scope of the reconciler, either a namespace or ':root'
	SyncScope declared.Scope
	// SyncName is the name of the RootSync or RepoSync.
	SyncName string
	// Applier is the client to correct drift.
	syncerreconcile.Applier
	// declared.Resources is the cache of the declared resource list. It is used
	// as the source of truth for drift correction.
	*declared.Resources
	// cache.RemediateResources is the cache of the remediate resource list. It is
	// used to get the actual Kubernetes object for a particular GKNN.
	*cache.RemediateResources
	// Remediator is the actor to check whether it is ready to start reconciliation.
	*remediator.Remediator
}

// Reconcile processes the remediate request to correct drift.
func (r *Remediator) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	if errors.Is(r.ControllerCtx.Err(), context.Canceled) {
		// Drain the request queue
		klog.V(3).Infof("Stop remediating new objects as the context is canceled..")
		return ctrl.Result{}, nil
	}

	klog.V(1).Infof("New remediator reconciliation for %q", req.Name)

	id, err := core.FromID(req.Name)
	if err != nil {
		klog.Errorf("invalid request: %v", err)
		return ctrl.Result{}, nil
	}

	// marks the Remediator is working to block the Applier if it can remediate.
	if !r.StartRemediating() {
		// Mark the object as done so that it can be requeued later when the Applier is not running.
		klog.V(1).Infof("Skip remediating object %s because the Remediator is paused", id)
		r.DoneObj(id)
		return ctrl.Result{}, nil
	}
	// Mark the Remediator as done before it exits, so the Syncer can start the apply work.
	defer r.DoneRemediating()

	obj, found := r.GetObjs()[id]
	if !found {
		klog.V(4).Infof("Skip remediating object %q because it is no longer on the remediate list", id)
		return ctrl.Result{}, nil
	}

	// Get the ResourceVersion of the object from the last remediation.
	lastRV := r.LastRemediateRV(id)
	// Get the current ResourceVersion.
	currentRV := obj.GetResourceVersion()

	var toRemediate client.Object
	if cache.WasDeleted(r.ControllerCtx, obj) {
		// Passing a nil Object to the reconciler signals that the accompanying ID
		// is for an Object that was deleted.
		toRemediate = nil
		// If the object was deleted, it has no ResourceVersion, use the constant.
		currentRV = cache.DeletedResourceVersion
	} else {
		toRemediate = obj
	}

	// If the current ResourceVersion was last remediated, skip remediation because
	// it should match the declared resource.
	if currentRV == lastRV {
		// If no fight errors, return directly and remove the remediate cache
		if len(r.FightErrors()) == 0 {
			r.RemoveObj(id)
			return ctrl.Result{}, nil
		}
		// Even though remediation is skipped, it still needs to detect fight to
		// report the fight status.
		_, fightErr := r.ResolveFight(time.Now(), obj)
		if fightErr == nil {
			// No more fight for the object, remove it from the queue.
			r.RemoveFightError(id)
			r.RemoveObj(id)
			return ctrl.Result{}, nil
		}

		r.AddFightError(id, fightErr)
		klog.Errorf("fight for %s still exists in the remediator", id)
		// Requeue the request to address the fight.
		r.DoneObj(id, currentRV)
		return ctrl.Result{}, nil
	}

	// If the current ResourceVersion doesn't match the last remediated one, it is
	// a new remediation.
	klog.V(3).Infof("Remediator processing object %q (generation: %d, resourceversion: %s)", id, obj.GetGeneration(), obj.GetResourceVersion())
	_, remErr := r.remediate(r.ControllerCtx, id, toRemediate)
	if remErr != nil {
		// To debug the set of events we've missed, you may need to comment out this
		// block. Specifically, this makes things smooth for production, but can
		// hide bugs (for example, if we don't properly process delete events).
		if remErr.Code() == syncerclient.ResourceConflictCode {
			// This means our cached version of the object isn't the same as the one
			// on the cluster. We need to refresh the cached version.
			if refreshErr := r.refresh(r.ControllerCtx, obj); refreshErr != nil {
				klog.Errorf("Unable to update cached version of %q: %v", id, refreshErr)
			}
		}
		klog.Errorf("Failed to remediate %q: %v", id, remErr)
		// The object is marked done in the queue so that it can be requeued next time.
		// If there is no error, the next reconciliation will succeed and clear the object from the queue.
		// If an error occurs, the next reconciliation will re-process the remediate request.
		return ctrl.Result{}, nil
	}

	klog.V(3).Infof("Successfully reconciled %q", id)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the Remediator Controller.
func (r *Remediator) SetupWithManager(mgr ctrl.Manager) error {
	// an event channel to trigger reconciliation
	timerEvents := make(chan event.GenericEvent)

	// run the timer as a goroutine to trigger the reconciliation periodically
	go func() {
		remediateTimer := time.NewTimer(time.Second)
		defer remediateTimer.Stop()

		for {
			select {
			case <-r.ControllerCtx.Done():
				// The controller is shutting down, close the timerEvents channel and exit
				close(timerEvents)
				// Stop enqueueing and executing new remediator requests.
				r.Remediator.Pause()
				// block and wait until the current remediation is done.
				r.Remediator.WaitRemediating()
				// clear remediate cache to abandon obsolete remediate requests.
				r.Remediator.ClearCache()
				close(r.DoneCh) // inform finalizer the remediator is done
				return

			case <-remediateTimer.C:
				if r.CanRemediate() {
					// Skip enqueuing remediator requests if the Applier is running.
					r.enqueueEvents(timerEvents)
				}
				remediateTimer.Reset(time.Second)
			}
		}
	}()

	return ctrl.NewControllerManagedBy(mgr).
		Named(RemediatorController).
		// watch for the timer events
		Watches(&source.Channel{Source: timerEvents},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}

// enqueueEvents gets the remediate objects and enqueues them as remediate requests
// if they are not enqueued yet.
func (r *Remediator) enqueueEvents(eventChannel chan event.GenericEvent) {
	for id, obj := range r.GetObjs() {
		// Only enqueue the request when the object queue doesn't include the ID.
		if r.EnqueueObj(obj) {
			req := corev1.Event{}
			req.Name = id.String()
			// Send the Event request to the event channel to trigger a new reconciliation.
			eventChannel <- event.GenericEvent{Object: &req}
		}
	}
}

// remediate takes a client.Object representing the object to update, and then
// ensures that the version on the server matches it.
// It returns the ResourceVersion of the new remediated object.
func (r *Remediator) remediate(ctx context.Context, id core.ID, obj client.Object) (string, status.Error) {
	start := time.Now()

	declU, commit, found := r.Get(id) // Get the declared resource
	// Yes, this if block is necessary because Go is pedantic about nil interfaces.
	// 1) var decl client.Object = declU results in a panic.
	// 2) Using declU as a client.Object results in a panic.
	var decl client.Object
	if found {
		decl = declU
	}
	objDiff := diff.Diff{
		Declared: decl,
		Actual:   obj,
	}

	remediatedRV, err := r.remediateDiff(ctx, id, objDiff)
	// Mark the remediation done without deleting it from the cache.
	// The updated object might have already been enqueued before the remediation is done.
	// Deleting from the cache might lead to loss of remediation request.
	defer r.DoneObj(id, remediatedRV)

	// Record duration, even if there's an error
	metrics.RecordRemediateDuration(ctx, metrics.StatusTagKey(err), start)

	if err != nil {
		switch err.Code() {
		case syncerclient.ResourceConflictCode:
			// Record conflict, if there was one
			metrics.RecordResourceConflict(ctx, commit)
		case status.FightErrorCode:
			operation := objDiff.Operation(r.SyncScope, r.SyncName)
			metrics.RecordResourceFight(ctx, string(operation))
			r.AddFightError(id, err)
		}
		return remediatedRV, err
	}

	r.RemoveFightError(id)
	return remediatedRV, nil
}

// remediateDiff takes diff (declared & actual) and ensures the server matches the
// declared state.
// It returns the ResourceVersion of the new remediated object.
func (r *Remediator) remediateDiff(ctx context.Context, id core.ID, objDiff diff.Diff) (string, status.Error) {
	switch t := objDiff.Operation(r.SyncScope, r.SyncName); t {
	case diff.NoOp:
		if objDiff.Actual != nil {
			return objDiff.Actual.GetResourceVersion(), nil
		}
		return cache.DeletedResourceVersion, nil
	case diff.Create:
		declaredUn, err := objDiff.UnstructuredDeclared()
		if err != nil {
			return "", err
		}
		klog.V(3).Infof("Remediator creating object: %v", id)
		return r.Applier.Create(ctx, declaredUn)
	case diff.Update:
		declaredUn, err := objDiff.UnstructuredDeclared()
		if err != nil {
			return "", err
		}
		actual, err := objDiff.UnstructuredActual()
		if err != nil {
			return "", err
		}
		klog.V(3).Infof("Remediator updating object: %v", id)
		return r.Applier.Update(ctx, declaredUn, actual)
	case diff.Delete:
		actual, err := objDiff.UnstructuredActual()
		if err != nil {
			return "", err
		}
		klog.V(3).Infof("Remediator deleting object: %v", id)
		return r.Applier.Delete(ctx, actual)
	case diff.Error:
		// This is the case where the annotation in the *repository* is invalid.
		// Should never happen as the Parser would have thrown an error.
		return "", nonhierarchical.IllegalManagementAnnotationError(
			objDiff.Declared,
			objDiff.Declared.GetAnnotations()[metadata.ResourceManagementKey],
		)
	case diff.Abandon:
		actual, err := objDiff.UnstructuredActual()
		if err != nil {
			return "", err
		}
		klog.V(3).Infof("Remediator abandoning object %v", id)
		return r.Applier.RemoveNomosMeta(ctx, actual, metrics.RemediatorController)
	default:
		// e.g. differ.DeleteNsConfig, which shouldn't be possible to get to any way.
		metrics.RecordInternalError(ctx, "remediator")
		return "", status.InternalErrorf("diff type not supported: %v", t)
	}
}

// refresh updates the cached version of the object.
func (r *Remediator) refresh(ctx context.Context, o client.Object) status.Error {
	c := r.Applier.GetClient()

	// Try to get an updated version of the object from the cluster.
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(o.GetObjectKind().GroupVersionKind())
	err := c.Get(ctx, client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, u)

	switch {
	case apierrors.IsNotFound(err):
		// The object no longer exists on the cluster, so mark it deleted.
		r.AddObj(cache.MarkDeleted(ctx, o))
	case err != nil:
		// We encountered some other error that we don't know how to solve, so
		// surface it.
		return status.APIServerError(err, "failed to get updated object for worker cache", o)
	default:
		// Update the cached version of the resource.
		r.AddObj(u)
	}
	return nil
}
