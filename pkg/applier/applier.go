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

package applier

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/applier/stats"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	m "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/resourcegroup"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/syncer/metrics"
	nomosutil "kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// maxRequestBytesStr defines the max request bytes on the etcd server.
	// It is defined in https://github.com/etcd-io/etcd/blob/release-3.4/embed/config.go#L56
	maxRequestBytesStr = "1.5M"

	// maxRequestBytes defines the max request bytes on the etcd server.
	// It is defined in https://github.com/etcd-io/etcd/blob/release-3.4/embed/config.go#L56
	maxRequestBytes = int64(1.5 * 1024 * 1024)
)

// Applier is a bulk client for applying a set of desired resource objects and
// tracking them in a ResourceGroup inventory. This enables pruning objects
// by removing them from the list of desired resource objects and re-applying.
type Applier interface {
	// Apply creates, updates, or prunes all managed resources, depending on
	// the new desired resource objects.
	// Error events are sent to the eventHandler.
	// Returns the status of the applied objects and statistics of the sync.
	// This is called by the reconciler when changes are detected in the
	// source of truth (git, OCI, helm) and periodically.
	Apply(ctx context.Context, eventHandler func(Event), resources *declared.Resources) (ObjectStatusMap, *stats.SyncStats)
}

// Destroyer is a bulk client for deleting all the managed resource objects
// tracked in a single ResourceGroup inventory.
type Destroyer interface {
	// Destroy deletes all managed resources.
	// Error events are sent to the eventHandler.
	// Returns the status of the destroyed objects and statistics of the sync.
	// This is called by the reconciler finalizer when deletion propagation is
	// enabled.
	Destroy(ctx context.Context, eventHandler func(Event)) (ObjectStatusMap, *stats.SyncStats)
}

// EventType is the type used by SuperEvent.Type
type EventType string

const (
	// ErrorEventType is the type of the ErrorEvent
	ErrorEventType EventType = "ErrorEvent"
)

// Event is sent to the eventHandler by the supervisor.
type Event interface {
	// Type returns the type of the event.
	Type() EventType
}

// ErrorEvent is sent after the supervisor has errored.
// Generally, the supervisor will still continue until success or timeout.
type ErrorEvent struct {
	Error status.Error
}

// Type returns the type of the event.
func (e ErrorEvent) Type() EventType {
	return ErrorEventType
}

// Supervisor is a bulk client for applying and deleting a mutable set of
// resource objects. Managed objects are tracked in a ResourceGroup inventory
// object.
//
// Supervisor satisfies both the Applier and Destroyer interfaces, with a shared
// lock, preventing Apply and Destroy from running concurrently.
//
// The Applier and Destroyer share an error cache. So the Errors method will
// return the last errors from Apply or the Destroy, whichever came last.
type Supervisor interface {
	Applier
	Destroyer
	UpdateStatusMode(ctx context.Context) error
}

// supervisor is the default implementation of the Supervisor interface.
type supervisor struct {
	// inventory policy for configuring the inventory status
	policy inventory.Policy
	// invInfo is the metadata for the ResourceGroup used to track managed objects
	invInfo *inventory.SingleObjectInfo
	// clientSet wraps multiple API server clients
	clientSet *ClientSet
	// syncKind is the Kind of the RSync object: RootSync or RepoSync
	syncKind string
	// syncName is the name of RSync object
	syncName string
	// syncNamespace is the namespace of RSync object
	syncNamespace string
	// reconcileTimeout controls the reconcile and prune timeout
	reconcileTimeout time.Duration

	// execMux prevents concurrent Apply/Destroy calls
	execMux sync.Mutex
}

var _ Applier = &supervisor{}
var _ Destroyer = &supervisor{}
var _ Supervisor = &supervisor{}

// NewSupervisor constructs either a cluster-level or namespace-level Supervisor,
// based on the specified scope.
func NewSupervisor(cs *ClientSet, scope declared.Scope, syncName string, reconcileTimeout time.Duration) Supervisor {
	syncKind := scope.SyncKind()
	syncNamespace := scope.SyncNamespace()
	invInfo := inventory.NewSingleObjectInfo(
		inventory.ID(InventoryID(syncName, syncNamespace)),
		types.NamespacedName{
			Name:      syncName,
			Namespace: syncNamespace,
		},
	)
	policy := inventory.PolicyAdoptIfNoInventory
	if scope == declared.RootScope {
		policy = inventory.PolicyAdoptAll
	}
	a := &supervisor{
		invInfo:          invInfo,
		clientSet:        cs,
		policy:           policy,
		syncKind:         syncKind,
		syncName:         syncName,
		syncNamespace:    syncNamespace,
		reconcileTimeout: reconcileTimeout,
	}
	klog.V(4).Infof("%s Supervisor %s/%s is initialized", syncKind, syncNamespace, syncName)
	return a
}

// UpdateStatusMode sets the status mode annotation on the ResourceGroup object
// if it exists.
// This is invoked once before the applier runs so that ResourceGroups which are
// too large can have their status cleared by the resource-group-controller before
// trying to add any new resources from source.
func (s *supervisor) UpdateStatusMode(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		u := newInventoryUnstructured(s.syncName, s.syncNamespace)
		key := client.ObjectKeyFromObject(u)
		err := s.clientSet.Client.Get(ctx, key, u)
		if err != nil {
			// RG doesn't exist, it will be created by applier with appropriate status mode
			if apierrors.IsNotFound(err) {
				return nil
			}
			return status.APIServerErrorf(err, "failed to get %s: %s", u.GetKind(), key)
		}
		if core.SetAnnotation(u, metadata.StatusModeAnnotationKey, s.clientSet.StatusMode.String()) {
			klog.V(3).Infof("Updating %s annotation: %s: %s", u.GetKind(), metadata.StatusModeAnnotationKey, s.clientSet.StatusMode)
			err := s.clientSet.Client.Update(ctx, u, client.FieldOwner(configsync.FieldManager))
			if err != nil {
				return status.APIServerErrorf(err, "failed to update %s: %s", u.GetKind(), key)
			}
		}
		return nil
	})
}

func (s *supervisor) processApplyEvent(ctx context.Context, e event.ApplyEvent, syncStats *stats.ApplyEventStats, objectStatusMap ObjectStatusMap, unknownTypeResources map[core.ID]struct{}, resourceMap map[core.ID]client.Object) status.Error {
	id := idFrom(e.Identifier)
	syncStats.Add(e.Status)

	objectStatus, ok := objectStatusMap[id]
	if !ok || objectStatus == nil {
		objectStatus = &ObjectStatus{}
		objectStatusMap[id] = objectStatus
	}
	objectStatus.Strategy = actuation.ActuationStrategyApply

	switch e.Status {
	case event.ApplyPending:
		objectStatus.Actuation = actuation.ActuationPending
		return nil

	case event.ApplySuccessful:
		objectStatus.Actuation = actuation.ActuationSucceeded
		handleMetrics(ctx, "update", e.Error)
		return nil

	case event.ApplyFailed:
		objectStatus.Actuation = actuation.ActuationFailed
		handleMetrics(ctx, "update", e.Error)
		switch e.Error.(type) {
		case *applyerror.UnknownTypeError:
			unknownTypeResources[id] = struct{}{}
		}

		if resourceMap[id] != nil {
			return ErrorForResourceWithResource(e.Error, id, resourceMap[id])
		}

		return ErrorForResource(e.Error, id)

	case event.ApplySkipped:
		objectStatus.Actuation = actuation.ActuationSkipped
		// Skip event always includes an error with the reason
		return s.handleApplySkippedEvent(e.Resource, id, e.Error)

	default:
		return ErrorForResource(fmt.Errorf("unexpected prune event status: %v", e.Status), id)
	}
}

func (s *supervisor) processWaitEvent(e event.WaitEvent, syncStats *stats.WaitEventStats, objectStatusMap ObjectStatusMap, isDestroy bool) error {
	id := idFrom(e.Identifier)
	syncStats.Add(e.Status)

	objectStatus, ok := objectStatusMap[id]
	if !ok || objectStatus == nil {
		objectStatus = &ObjectStatus{}
		objectStatusMap[id] = objectStatus
	}

	switch e.Status {
	case event.ReconcilePending:
		objectStatus.Reconcile = actuation.ReconcilePending
	case event.ReconcileSkipped:
		objectStatus.Reconcile = actuation.ReconcileSkipped
	case event.ReconcileSuccessful:
		objectStatus.Reconcile = actuation.ReconcileSucceeded
	case event.ReconcileFailed:
		objectStatus.Reconcile = actuation.ReconcileFailed
		// ReconcileFailed is treated as an error for destroy
		if isDestroy {
			return WaitErrorForResource(fmt.Errorf("reconcile failed"), id)
		}
	case event.ReconcileTimeout:
		objectStatus.Reconcile = actuation.ReconcileTimeout
		// ReconcileTimeout is treated as an error for destroy
		if isDestroy {
			return WaitErrorForResource(fmt.Errorf("reconcile timeout"), id)
		}
	default:
		return ErrorForResource(fmt.Errorf("unexpected wait event status: %v", e.Status), id)
	}
	return nil
}

// handleApplySkippedEvent translates from apply skipped event into resource error.
func (s *supervisor) handleApplySkippedEvent(obj *unstructured.Unstructured, id core.ID, err error) status.Error {
	var depErr *filter.DependencyPreventedActuationError
	if errors.As(err, &depErr) {
		return SkipErrorForResource(err, id, depErr.Strategy)
	}

	var depMismatchErr *filter.DependencyActuationMismatchError
	if errors.As(err, &depMismatchErr) {
		return SkipErrorForResource(err, id, depMismatchErr.Strategy)
	}

	var policyErr *inventory.PolicyPreventedActuationError
	if errors.As(err, &policyErr) {
		// TODO: return ManagementConflictError with the conflicting manager if
		// cli-utils supports reporting the conflicting manager in
		// PolicyPreventedActuationError.
		// return SkipErrorForResource(err, id, policyErr.Strategy)
		return KptManagementConflictError(obj)
	}

	return SkipErrorForResource(err, id, actuation.ActuationStrategyApply)
}

// processPruneEvent handles PruneEvents from the Applier
func (s *supervisor) processPruneEvent(ctx context.Context, e event.PruneEvent, syncStats *stats.PruneEventStats, objectStatusMap ObjectStatusMap, declaredResources *declared.Resources) status.Error {
	id := idFrom(e.Identifier)
	syncStats.Add(e.Status)

	objectStatus, ok := objectStatusMap[id]
	if !ok || objectStatus == nil {
		objectStatus = &ObjectStatus{}
		objectStatusMap[id] = objectStatus
	}
	objectStatus.Strategy = actuation.ActuationStrategyDelete

	switch e.Status {
	case event.PrunePending:
		objectStatus.Actuation = actuation.ActuationPending
		return nil

	case event.PruneSuccessful:
		objectStatus.Actuation = actuation.ActuationSucceeded
		handleMetrics(ctx, "delete", e.Error)

		iObj, found := declaredResources.GetIgnored(id)
		if found {
			klog.V(3).Infof("Deleting object '%v' from the ignore cache", core.GKNN(iObj))
			declaredResources.DeleteIgnored(id)
		}

		return nil

	case event.PruneFailed:
		objectStatus.Actuation = actuation.ActuationFailed
		handleMetrics(ctx, "delete", e.Error)
		return PruneErrorForResource(e.Error, id)

	case event.PruneSkipped:
		objectStatus.Actuation = actuation.ActuationSkipped
		// Skip event always includes an error with the reason
		return s.handleDeleteSkippedEvent(ctx, event.PruneType, e.Object, id, e.Error)

	default:
		return PruneErrorForResource(fmt.Errorf("unexpected prune event status: %v", e.Status), id)
	}
}

// processDeleteEvent handles DeleteEvents from the Destroyer
func (s *supervisor) processDeleteEvent(ctx context.Context, e event.DeleteEvent, syncStats *stats.DeleteEventStats, objectStatusMap ObjectStatusMap) status.Error {
	id := idFrom(e.Identifier)
	syncStats.Add(e.Status)

	objectStatus, ok := objectStatusMap[id]
	if !ok || objectStatus == nil {
		objectStatus = &ObjectStatus{}
		objectStatusMap[id] = objectStatus
	}
	objectStatus.Strategy = actuation.ActuationStrategyDelete

	switch e.Status {
	case event.DeletePending:
		objectStatus.Actuation = actuation.ActuationPending
		return nil

	case event.DeleteSuccessful:
		objectStatus.Actuation = actuation.ActuationSucceeded
		handleMetrics(ctx, "delete", e.Error)
		return nil

	case event.DeleteFailed:
		objectStatus.Actuation = actuation.ActuationFailed
		handleMetrics(ctx, "delete", e.Error)
		return DeleteErrorForResource(e.Error, id)

	case event.DeleteSkipped:
		objectStatus.Actuation = actuation.ActuationSkipped
		// Skip event always includes an error with the reason
		return s.handleDeleteSkippedEvent(ctx, event.DeleteType, e.Object, id, e.Error)

	default:
		return DeleteErrorForResource(fmt.Errorf("unexpected delete event status: %v", e.Status), id)
	}
}

// handleDeleteSkippedEvent translates from prune skip or delete skip event into
// a resource error.
func (s *supervisor) handleDeleteSkippedEvent(ctx context.Context, eventType event.Type, obj *unstructured.Unstructured, id core.ID, err error) status.Error {
	// Disable protected namespaces that were removed from the desired object set.
	if isNamespace(obj) && differ.SpecialNamespaces[obj.GetName()] {
		// the `client.lifecycle.config.k8s.io/deletion: detach` annotation is not a part of the Config Sync metadata, and will not be removed here.
		err := s.abandonObject(ctx, obj)
		handleMetrics(ctx, "unmanage", err)
		if err != nil {
			err = fmt.Errorf("failed to remove the Config Sync metadata from %v (protected namespace): %v",
				id, err)
			klog.Error(err)
			return applierErrorBuilder.Wrap(err).Build()
		}
		klog.V(4).Infof("removed the Config Sync metadata from %v (protected namespace)", id)
	}

	var depErr *filter.DependencyPreventedActuationError
	if errors.As(err, &depErr) {
		return SkipErrorForResource(err, id, depErr.Strategy)
	}

	var depMismatchErr *filter.DependencyActuationMismatchError
	if errors.As(err, &depMismatchErr) {
		return SkipErrorForResource(err, id, depMismatchErr.Strategy)
	}

	// ApplyPreventedDeletionError is only sent by the Applier in a PruneEvent,
	// not by the Destroyer in a DeleteEvent, because the Destroyer doesn't
	// perform any applies before deleting.
	if eventType == event.PruneType {
		var applyDeleteErr *filter.ApplyPreventedDeletionError
		if errors.As(err, &applyDeleteErr) {
			return SkipErrorForResource(err, id, actuation.ActuationStrategyDelete)
		}
	}

	var policyErr *inventory.PolicyPreventedActuationError
	if errors.As(err, &policyErr) {
		// For prunes/deletes, this is desired behavior, not a fatal error.
		klog.Infof("Resource object removed from inventory,  but not deleted: %v: %v", id, err)
		return nil
	}

	var namespaceErr *filter.NamespaceInUseError
	if errors.As(err, &namespaceErr) {
		return SkipErrorForResource(err, id, actuation.ActuationStrategyDelete)
	}

	var abandonErr *filter.AnnotationPreventedDeletionError
	if errors.As(err, &abandonErr) {
		// For prunes/deletes, this is desired behavior, not a fatal error.
		klog.Infof("Resource object removed from inventory, but not deleted: %v: %v", id, err)
		// The `client.lifecycle.config.k8s.io/deletion: detach` annotation is not a part of the Config Sync metadata, and will not be removed here.
		err := s.abandonObject(ctx, obj)
		handleMetrics(ctx, "unmanage", err)
		if err != nil {
			err = fmt.Errorf("failed to remove the Config Sync metadata from %v (%s: %s): %v",
				id, abandonErr.Annotation, abandonErr.Value, err)
			klog.Error(err)
			return applierErrorBuilder.Wrap(err).Build()
		}
		klog.V(4).Infof("removed the Config Sync metadata from %v (%s: %s)", id, abandonErr.Annotation, abandonErr.Value)
		return nil
	}

	return SkipErrorForResource(err, id, actuation.ActuationStrategyDelete)
}

func isNamespace(obj *unstructured.Unstructured) bool {
	return obj.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind()
}

func handleMetrics(ctx context.Context, operation string, err error) {
	// TODO capture the apply duration in the kpt apply library.
	start := time.Now()

	m.RecordAPICallDuration(ctx, operation, m.StatusTagKey(err), start)
	metrics.Operations.WithLabelValues(operation, metrics.StatusLabel(err)).Inc()
	m.RecordApplyOperation(ctx, m.ApplierController, operation, m.StatusTagKey(err))
}

// checkInventoryObjectSize checks the inventory object size limit.
// If it is close to the size limit 1M, log a warning.
func (s *supervisor) checkInventoryObjectSize(ctx context.Context, c client.Client) {
	u := newInventoryUnstructured(s.syncName, s.syncNamespace)
	err := c.Get(ctx, client.ObjectKey{Namespace: s.syncNamespace, Name: s.syncName}, u)
	if err == nil {
		size, err := getObjectSize(u)
		if err != nil {
			klog.Warningf("Failed to marshal ResourceGroup %s/%s to get its size: %s", s.syncNamespace, s.syncName, err)
		}
		if int64(size) > maxRequestBytes/2 {
			klog.Warningf("ResourceGroup %s/%s is close to the maximum object size limit (size: %d, max: %s). "+
				"There are too many resources being synced than Config Sync can handle! Please split your repo into smaller repos "+
				"to avoid future failure.", s.syncNamespace, s.syncName, size, maxRequestBytesStr)
		}
	}
}

// applyInner triggers a kpt live apply library call to apply a set of resources.
func (s *supervisor) applyInner(ctx context.Context, eventHandler func(Event), declaredResources *declared.Resources) (ObjectStatusMap, *stats.SyncStats) {
	s.checkInventoryObjectSize(ctx, s.clientSet.Client)
	isDestroy := false

	syncStats := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	objs := declaredResources.DeclaredObjects()

	if err := s.cacheIgnoreMutationObjects(ctx, declaredResources); err != nil {
		sendErrorEvent(err, eventHandler)
		return objStatusMap, syncStats
	}

	if len(declaredResources.IgnoredObjects()) > 0 {
		klog.Infof("%v mutation-ignored objects: %v", len(declaredResources.IgnoredObjects()), core.GKNNs(declaredResources.IgnoredObjects()))
	}

	// disabledObjs are objects for which the management are disabled
	// through annotation.
	enabledObjs, disabledObjs := partitionObjs(objs)
	if len(disabledObjs) > 0 {
		klog.Infof("%v objects to be disabled: %v", len(disabledObjs), core.GKNNs(disabledObjs))
		disabledCount, err := s.handleDisabledObjects(ctx, disabledObjs)
		if err != nil {
			sendErrorEvent(err, eventHandler)
			return objStatusMap, syncStats
		}
		syncStats.DisableObjs = &stats.DisabledObjStats{
			Total:     uint64(len(disabledObjs)),
			Succeeded: disabledCount,
		}
	}

	objsToApply := handleIgnoredObjects(enabledObjs, declaredResources)

	klog.Infof("%v objects to be applied: %v", len(objsToApply), core.GKNNs(objsToApply))
	resources, err := toUnstructured(objsToApply)
	if err != nil {
		sendErrorEvent(err, eventHandler)
		return objStatusMap, syncStats
	}

	unknownTypeResources := make(map[core.ID]struct{})
	options := apply.ApplierOptions{
		ServerSideOptions: common.ServerSideOptions{
			ServerSideApply: true,
			ForceConflicts:  true,
			FieldManager:    configsync.FieldManager,
		},
		InventoryPolicy: s.policy,
		// Leaving ReconcileTimeout and PruneTimeout unset may cause a WaitTask to wait forever.
		// ReconcileTimeout defines the timeout for a wait task after an apply task.
		// ReconcileTimeout is a task-level setting instead of an object-level setting.
		ReconcileTimeout: s.reconcileTimeout,
		// PruneTimeout defines the timeout for a wait task after a prune task.
		// PruneTimeout is a task-level setting instead of an object-level setting.
		PruneTimeout: s.reconcileTimeout,
		// PrunePropagationPolicy defines what policy to use when pruning
		// managed objects.
		// Use "Background" for now, otherwise managed RootSyncs cannot be
		// deleted, because the reconciler-manager configures the dependencies
		// to be garbage collected as owned resources.
		// TODO: Switch to "Foreground" after the reconciler-manager finalizer is added.
		PrunePropagationPolicy: metav1.DeletePropagationBackground,
	}

	// Reset shared mapper before each apply to invalidate the discovery cache.
	// This allows for picking up CRD changes.
	meta.MaybeResetRESTMapper(s.clientSet.Mapper)

	// Builds a map of id -> resource
	resourceMap := make(map[core.ID]client.Object)
	for _, obj := range resources {
		resourceMap[idFrom(ObjMetaFromUnstructured(obj))] = obj
	}

	events := s.clientSet.KptApplier.Run(ctx, s.invInfo, resources, options)
	for e := range events {
		switch e.Type {
		case event.InitType:
			for _, ag := range e.InitEvent.ActionGroups {
				klog.Info("InitEvent", ag)
			}
		case event.ActionGroupType:
			klog.Info(e.ActionGroupEvent)
		case event.ErrorType:
			klog.Info(e.ErrorEvent)
			err := e.ErrorEvent.Err
			if nomosutil.IsRequestTooLargeError(err) {
				err = largeResourceGroupError(err, coreIDFromInventoryInfo(s.invInfo))
			}
			sendErrorEvent(err, eventHandler)
			syncStats.ErrorTypeEvents++
		case event.WaitType:
			// Pending events are sent for any objects that haven't reconciled
			// when the WaitEvent starts. They're not very useful to the user.
			// So log them at a higher verbosity.
			if e.WaitEvent.Status == event.ReconcilePending {
				klog.V(4).Info(e.WaitEvent)
			} else {
				klog.V(1).Info(e.WaitEvent)
			}
			if err := s.processWaitEvent(e.WaitEvent, syncStats.WaitEvent, objStatusMap, isDestroy); err != nil {
				sendErrorEvent(err, eventHandler)
			}
		case event.ApplyType:
			if e.ApplyEvent.Error != nil {
				klog.Info(e.ApplyEvent)
			} else {
				klog.V(1).Info(e.ApplyEvent)
			}
			if err := s.processApplyEvent(ctx, e.ApplyEvent, syncStats.ApplyEvent, objStatusMap, unknownTypeResources, resourceMap); err != nil {
				sendErrorEvent(err, eventHandler)
			}
		case event.PruneType:
			if e.PruneEvent.Error != nil {
				klog.Info(e.PruneEvent)
			} else {
				klog.V(1).Info(e.PruneEvent)
			}
			if err := s.processPruneEvent(ctx, e.PruneEvent, syncStats.PruneEvent, objStatusMap, declaredResources); err != nil {
				sendErrorEvent(err, eventHandler)
			}
		default:
			klog.Infof("Unhandled event (%s): %v", e.Type, e)
		}
	}

	return objStatusMap, syncStats
}

func sendErrorEvent(err error, eventHandler func(Event)) {
	eventHandler(ErrorEvent{Error: wrapError(err)})
}

func wrapError(err error) status.Error {
	if statusErr, ok := err.(status.Error); ok {
		return statusErr
	}
	// Wrap as an applier.Error to indicate the source of the error
	return Error(err)
}

// destroyInner triggers a kpt live destroy library call to destroy a set of resources.
func (s *supervisor) destroyInner(ctx context.Context, eventHandler func(Event)) (ObjectStatusMap, *stats.SyncStats) {
	syncStats := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	isDestroy := true

	options := apply.DestroyerOptions{
		InventoryPolicy: s.policy,
		// DeleteTimeout defines the timeout for a wait task after a delete task.
		// DeleteTimeout is a task-level setting instead of an object-level setting.
		DeleteTimeout: s.reconcileTimeout,
		// DeletePropagationPolicy defines what policy to use when deleting
		// managed objects.
		// Use "Foreground" to ensure owned resources are deleted in the right
		// order. This ensures things like a Deployment's ReplicaSets and Pods
		// are deleted before the Namespace that contains them.
		DeletePropagationPolicy: metav1.DeletePropagationForeground,
	}

	// Reset shared mapper before each destroy to invalidate the discovery cache.
	// This allows for picking up CRD changes.
	meta.MaybeResetRESTMapper(s.clientSet.Mapper)

	events := s.clientSet.KptDestroyer.Run(ctx, s.invInfo, options)
	for e := range events {
		switch e.Type {
		case event.InitType:
			for _, ag := range e.InitEvent.ActionGroups {
				klog.Info("InitEvent", ag)
			}
		case event.ActionGroupType:
			klog.Info(e.ActionGroupEvent)
		case event.ErrorType:
			klog.Info(e.ErrorEvent)
			err := e.ErrorEvent.Err
			if nomosutil.IsRequestTooLargeError(err) {
				err = largeResourceGroupError(err, coreIDFromInventoryInfo(s.invInfo))
			}
			sendErrorEvent(err, eventHandler)
			syncStats.ErrorTypeEvents++
		case event.WaitType:
			// Pending events are sent for any objects that haven't reconciled
			// when the WaitEvent starts. They're not very useful to the user.
			// So log them at a higher verbosity.
			if e.WaitEvent.Status == event.ReconcilePending {
				klog.V(4).Info(e.WaitEvent)
			} else {
				klog.V(1).Info(e.WaitEvent)
			}
			if err := s.processWaitEvent(e.WaitEvent, syncStats.WaitEvent, objStatusMap, isDestroy); err != nil {
				sendErrorEvent(err, eventHandler)
			}
		case event.DeleteType:
			if e.DeleteEvent.Error != nil {
				klog.Info(e.DeleteEvent)
			} else {
				klog.V(1).Info(e.DeleteEvent)
			}
			if err := s.processDeleteEvent(ctx, e.DeleteEvent, syncStats.DeleteEvent, objStatusMap); err != nil {
				sendErrorEvent(err, eventHandler)
			}
		default:
			klog.Infof("Unhandled event (%s): %v", e.Type, e)
		}
	}

	return objStatusMap, syncStats
}

// Apply all managed resource objects and return their status.
// Apply implements the Applier interface.
func (s *supervisor) Apply(ctx context.Context, eventHandler func(Event), resources *declared.Resources) (ObjectStatusMap, *stats.SyncStats) {
	s.execMux.Lock()
	defer s.execMux.Unlock()

	return s.applyInner(ctx, eventHandler, resources)
}

// Destroy all managed resource objects and return their status.
// Destroy implements the Destroyer interface.
func (s *supervisor) Destroy(ctx context.Context, eventHandler func(Event)) (ObjectStatusMap, *stats.SyncStats) {
	s.execMux.Lock()
	defer s.execMux.Unlock()

	return s.destroyInner(ctx, eventHandler)
}

// newInventoryUnstructured creates an inventory object as an unstructured.
func newInventoryUnstructured(name, namespace string) *unstructured.Unstructured {
	id := InventoryID(name, namespace)
	u := resourcegroup.Unstructured(name, namespace, id)
	return u
}

// InventoryID returns the inventory id of an inventory object.
// The inventory object generated by ConfigSync is in the same namespace as RootSync or RepoSync.
// The inventory ID is assigned as <NAMESPACE>_<NAME>.
func InventoryID(name, namespace string) string {
	return namespace + "_" + name
}

// handleDisabledObjects removes the specified objects from the inventory, and
// then disables them, one by one, by removing the ConfigSync metadata.
// Returns the number of objects which are disabled successfully, and any errors
// encountered.
func (s *supervisor) handleDisabledObjects(ctx context.Context, objs []client.Object) (uint64, status.MultiError) {
	// disabledCount tracks the number of objects which are disabled successfully
	var disabledCount uint64
	err := s.removeFromInventory(ctx, objs)
	if err != nil {
		if nomosutil.IsRequestTooLargeError(err) {
			return disabledCount, largeResourceGroupError(err, coreIDFromInventoryInfo(s.invInfo))
		}
		return disabledCount, Error(err)
	}
	var errs status.MultiError
	for _, obj := range objs {
		id := core.IDOf(obj)
		err := s.abandonObject(ctx, obj)
		handleMetrics(ctx, "unmanage", err)
		if err != nil {
			err = fmt.Errorf("failed to remove the Config Sync metadata from %v (%s: %s): %v",
				id, metadata.ResourceManagementKey, metadata.ResourceManagementDisabled, err)
			klog.Warning(err)
			errs = status.Append(errs, Error(err))
		} else {
			klog.V(4).Infof("removed the Config Sync metadata from %v (%s: %s)",
				id, metadata.ResourceManagementKey, metadata.ResourceManagementDisabled)
			disabledCount++
		}
	}
	return disabledCount, errs
}

// removeFromInventory removes the specified objects from the inventory, if it
// exists.
func (s *supervisor) removeFromInventory(ctx context.Context, objs []client.Object) error {
	inv, err := s.clientSet.InvClient.Get(ctx, s.invInfo, inventory.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	newObjs := removeFrom(inv.GetObjectRefs(), objs)
	inv.SetObjectRefs(newObjs)
	return s.clientSet.InvClient.CreateOrUpdate(ctx, inv, inventory.UpdateOptions{})
}

// abandonObject removes ConfigSync labels and annotations from an object,
// disabling management.
func (s *supervisor) abandonObject(ctx context.Context, obj client.Object) error {
	gvk, err := kinds.Lookup(obj, s.clientSet.Client.Scheme())
	if err != nil {
		return err
	}
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)

	err = s.clientSet.Client.Get(ctx, client.ObjectKeyFromObject(obj), uObj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Warningf("Failed to abandon object: object not found: %s", core.IDOf(obj))
			return nil
		}
		return err
	}
	klog.Infof("Abandoning object: %s", core.IDOf(obj))
	if metadata.HasConfigSyncMetadata(uObj) {
		// Use minimal before & after objects to simplify DeepCopy and building
		// the merge patch.
		fromObj := &unstructured.Unstructured{}
		fromObj.SetGroupVersionKind(uObj.GroupVersionKind())
		fromObj.SetNamespace(uObj.GetNamespace())
		fromObj.SetName(uObj.GetName())
		fromObj.SetAnnotations(uObj.GetAnnotations())
		fromObj.SetLabels(uObj.GetLabels())

		toObj := fromObj.DeepCopy()
		updated1 := metadata.RemoveConfigSyncMetadata(toObj)
		updated2 := metadata.RemoveApplySetPartOfLabel(toObj, s.clientSet.ApplySetID)
		if !updated1 && !updated2 {
			return nil
		}
		// Use merge-patch instead of server-side-apply, because we don't have
		// the object's source of truth handy and don't want to take ownership
		// of all the fields managed by other clients.
		return s.clientSet.Client.Patch(ctx, toObj, client.MergeFrom(fromObj),
			client.FieldOwner(configsync.FieldManager))
	}
	return nil
}

// cacheIgnoreMutationObjects gets the current cluster state of any declared objects with the ignore mutation annotation and puts it in the Resources ignore objects cache
// Returns any errors that occur
func (s *supervisor) cacheIgnoreMutationObjects(ctx context.Context, declaredResources *declared.Resources) error {
	var objsToUpdate []client.Object
	declaredObjs := declaredResources.DeclaredObjects()

	for _, obj := range declaredObjs {
		if obj.GetAnnotations()[metadata.LifecycleMutationAnnotation] == metadata.IgnoreMutation {

			if _, found := declaredResources.GetIgnored(core.IDOf(obj)); !found {
				// Fetch the cluster state of the object if not already in the cache
				uObj := &unstructured.Unstructured{}
				uObj.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
				err := s.clientSet.Client.Get(ctx, client.ObjectKeyFromObject(obj), uObj)

				// Object doesn't exist on the cluster
				if apierrors.IsNotFound(err) {
					continue
				}

				if err != nil {
					return err
				}

				objsToUpdate = append(objsToUpdate, uObj)
			}
		}
	}

	declaredResources.UpdateIgnored(objsToUpdate...)

	return nil
}
