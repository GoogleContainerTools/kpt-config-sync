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

	"github.com/GoogleContainerTools/kpt/pkg/live"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
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
	"kpt.dev/configsync/pkg/util"
	nomosutil "kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
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
	// Errors and inventory events are sent to the superEventHandler.
	// Returns the status of the applied objects and statistics of the sync.
	// This is called by the reconciler when changes are detected in the
	// source of truth (git, OCI, helm) and periodically.
	Apply(ctx context.Context, superEventHandler func(SuperEvent), desiredResources []client.Object) (ObjectStatusMap, *stats.SyncStats)
}

// Destroyer is a bulk client for deleting all the managed resource objects
// tracked in a single ResourceGroup inventory.
type Destroyer interface {
	// Destroy deletes all managed resources.
	// Errors and inventory events are sent to the superEventHandler.
	// Returns the status of the destroyed objects and statistics of the sync.
	// This is called by the reconciler finalizer when deletion propagation is
	// enabled.
	Destroy(ctx context.Context, superEventHandler func(SuperEvent)) (ObjectStatusMap, *stats.SyncStats)
}

// SuperEventType is the type used by SuperEvent.Type
type SuperEventType string

const (
	// SuperErrorEventType is the type of the SuperErrorEvent
	SuperErrorEventType SuperEventType = "SuperErrorEvent"
	// SuperInventoryEventType is the type of the SuperInventoryEvent
	SuperInventoryEventType SuperEventType = "SuperInventoryEvent"
)

// SuperEvent is sent to the superEventHandler by the supervisor.
type SuperEvent interface {
	// Type returns the type of the event.
	Type() SuperEventType
}

// SuperErrorEvent is sent after the supervisor has errored.
// Generally, the supervisor will still continue until success or timeout.
type SuperErrorEvent struct {
	Error status.Error
}

// Type returns the type of the event.
func (e SuperErrorEvent) Type() SuperEventType {
	return SuperErrorEventType
}

// SuperInventoryEvent is sent after the inventory is updated.
// It may or may not have actually changed.
type SuperInventoryEvent struct {
	Inventory *v1alpha1.ResourceGroup
}

// Type returns the type of the event.
func (e SuperInventoryEvent) Type() SuperEventType {
	return SuperInventoryEventType
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
}

// supervisor is the default implementation of the Supervisor interface.
type supervisor struct {
	// inventory policy for configuring the inventory status
	policy inventory.Policy
	// inventory ResourceGroup used to track managed objects
	inventory *live.InventoryResourceGroup
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
func NewSupervisor(cs *ClientSet, scope declared.Scope, syncName string, reconcileTimeout time.Duration) (Supervisor, error) {
	if scope == declared.RootReconciler {
		return NewRootSupervisor(cs, syncName, reconcileTimeout)
	}
	return NewNamespaceSupervisor(cs, scope, syncName, reconcileTimeout)
}

// NewNamespaceSupervisor constructs a Supervisor that can manage resource
// objects in a single namespace.
func NewNamespaceSupervisor(cs *ClientSet, namespace declared.Scope, syncName string, reconcileTimeout time.Duration) (Supervisor, error) {
	syncKind := configsync.RepoSyncKind
	invObj := newInventoryUnstructured(syncKind, syncName, string(namespace), cs.StatusMode)
	// If the ResourceGroup object exists, annotate the status mode on the
	// existing object.
	if err := annotateStatusMode(context.TODO(), cs.Client, invObj, cs.StatusMode); err != nil {
		klog.Errorf("failed to annotate the ResourceGroup object with the status mode %s", cs.StatusMode)
		return nil, err
	}
	klog.Infof("successfully annotate the ResourceGroup object with the status mode %s", cs.StatusMode)
	inv, err := wrapInventoryObj(invObj)
	if err != nil {
		return nil, err
	}
	a := &supervisor{
		inventory:        inv,
		clientSet:        cs,
		policy:           inventory.PolicyAdoptIfNoInventory,
		syncKind:         syncKind,
		syncName:         syncName,
		syncNamespace:    string(namespace),
		reconcileTimeout: reconcileTimeout,
	}
	klog.V(4).Infof("Namespace Supervisor %s/%s is initialized", namespace, syncName)
	return a, nil
}

// NewRootSupervisor constructs a Supervisor that can manage both cluster-level
// and namespace-level resource objects in a single cluster.
func NewRootSupervisor(cs *ClientSet, syncName string, reconcileTimeout time.Duration) (Supervisor, error) {
	syncKind := configsync.RootSyncKind
	u := newInventoryUnstructured(syncKind, syncName, configmanagement.ControllerNamespace, cs.StatusMode)
	// If the ResourceGroup object exists, annotate the status mode on the
	// existing object.
	if err := annotateStatusMode(context.TODO(), cs.Client, u, cs.StatusMode); err != nil {
		klog.Errorf("failed to annotate the ResourceGroup object with the status mode %s", cs.StatusMode)
		return nil, err
	}
	klog.Infof("successfully annotate the ResourceGroup object with the status mode %s", cs.StatusMode)
	inv, err := wrapInventoryObj(u)
	if err != nil {
		return nil, err
	}
	a := &supervisor{
		inventory:        inv,
		clientSet:        cs,
		policy:           inventory.PolicyAdoptAll,
		syncKind:         syncKind,
		syncName:         syncName,
		syncNamespace:    string(configmanagement.ControllerNamespace),
		reconcileTimeout: reconcileTimeout,
	}
	klog.V(4).Infof("Root Supervisor %s is initialized and synced with the API server", syncName)
	return a, nil
}

func wrapInventoryObj(obj *unstructured.Unstructured) (*live.InventoryResourceGroup, error) {
	inv, ok := live.WrapInventoryObj(obj).(*live.InventoryResourceGroup)
	if !ok {
		return nil, errors.New("failed to create an ResourceGroup object")
	}
	return inv, nil
}

type eventHandler struct {
	// isDestroy indicates whether the events being processed are for a Destroy
	isDestroy bool
	// clientSet is used for accessing various k8s clients
	clientSet *ClientSet
}

func (h *eventHandler) processApplyEvent(ctx context.Context, e event.ApplyEvent, s *stats.ApplyEventStats, objectStatusMap ObjectStatusMap, unknownTypeResources map[core.ID]struct{}, resourceMap map[core.ID]client.Object) status.Error {
	id := idFrom(e.Identifier)
	s.Add(e.Status)

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
		return h.handleApplySkippedEvent(e.Resource, id, e.Error)

	default:
		return ErrorForResource(fmt.Errorf("unexpected prune event status: %v", e.Status), id)
	}
}

func (h *eventHandler) processWaitEvent(e event.WaitEvent, s *stats.WaitEventStats, objectStatusMap ObjectStatusMap) error {
	id := idFrom(e.Identifier)
	s.Add(e.Status)

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
		if h.isDestroy {
			return WaitErrorForResource(fmt.Errorf("reconcile failed"), id)
		}
	case event.ReconcileTimeout:
		objectStatus.Reconcile = actuation.ReconcileTimeout
		// ReconcileTimeout is treated as an error for destroy
		if h.isDestroy {
			return WaitErrorForResource(fmt.Errorf("reconcile timeout"), id)
		}
	default:
		return ErrorForResource(fmt.Errorf("unexpected wait event status: %v", e.Status), id)
	}
	return nil
}

// handleApplySkippedEvent translates from apply skipped event into resource error.
func (h *eventHandler) handleApplySkippedEvent(obj *unstructured.Unstructured, id core.ID, err error) status.Error {
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
func (h *eventHandler) processPruneEvent(ctx context.Context, e event.PruneEvent, s *stats.PruneEventStats, objectStatusMap ObjectStatusMap) status.Error {
	id := idFrom(e.Identifier)
	s.Add(e.Status)

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
		return nil

	case event.PruneFailed:
		objectStatus.Actuation = actuation.ActuationFailed
		handleMetrics(ctx, "delete", e.Error)
		return PruneErrorForResource(e.Error, id)

	case event.PruneSkipped:
		objectStatus.Actuation = actuation.ActuationSkipped
		// Skip event always includes an error with the reason
		return h.handleDeleteSkippedEvent(ctx, event.PruneType, e.Object, id, e.Error)

	default:
		return PruneErrorForResource(fmt.Errorf("unexpected prune event status: %v", e.Status), id)
	}
}

// processDeleteEvent handles DeleteEvents from the Destroyer
func (h *eventHandler) processDeleteEvent(ctx context.Context, e event.DeleteEvent, s *stats.DeleteEventStats, objectStatusMap ObjectStatusMap) status.Error {
	id := idFrom(e.Identifier)
	s.Add(e.Status)

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
		return h.handleDeleteSkippedEvent(ctx, event.DeleteType, e.Object, id, e.Error)

	default:
		return DeleteErrorForResource(fmt.Errorf("unexpected delete event status: %v", e.Status), id)
	}
}

// handleDeleteSkippedEvent translates from prune skip or delete skip event into
// a resource error.
func (h *eventHandler) handleDeleteSkippedEvent(ctx context.Context, eventType event.Type, obj *unstructured.Unstructured, id core.ID, err error) status.Error {
	// Disable protected namespaces that were removed from the desired object set.
	if isNamespace(obj) && differ.SpecialNamespaces[obj.GetName()] {
		// the `client.lifecycle.config.k8s.io/deletion: detach` annotation is not a part of the Config Sync metadata, and will not be removed here.
		err := h.abandonObject(ctx, obj)
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
		err := h.abandonObject(ctx, obj)
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
func (a *supervisor) checkInventoryObjectSize(ctx context.Context, c client.Client) {
	u := newInventoryUnstructured(a.syncKind, a.syncName, a.syncNamespace, a.clientSet.StatusMode)
	err := c.Get(ctx, client.ObjectKey{Namespace: a.syncNamespace, Name: a.syncName}, u)
	if err == nil {
		size, err := getObjectSize(u)
		if err != nil {
			klog.Warningf("Failed to marshal ResourceGroup %s/%s to get its size: %s", a.syncNamespace, a.syncName, err)
		}
		if int64(size) > maxRequestBytes/2 {
			klog.Warningf("ResourceGroup %s/%s is close to the maximum object size limit (size: %d, max: %s). "+
				"There are too many resources being synced than Config Sync can handle! Please split your repo into smaller repos "+
				"to avoid future failure.", a.syncNamespace, a.syncName, size, maxRequestBytesStr)
		}
	}
}

// applyInner triggers a kpt live apply library call to apply a set of resources.
func (a *supervisor) applyInner(ctx context.Context, superEventHandler func(SuperEvent), objs []client.Object) (ObjectStatusMap, *stats.SyncStats) {
	a.checkInventoryObjectSize(ctx, a.clientSet.Client)
	eh := eventHandler{
		isDestroy: false,
		clientSet: a.clientSet,
	}

	syncStats := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	// disabledObjs are objects for which the management are disabled
	// through annotation.
	enabledObjs, disabledObjs := partitionObjs(objs)
	if len(disabledObjs) > 0 {
		klog.Infof("%v objects to be disabled: %v", len(disabledObjs), core.GKNNs(disabledObjs))
		disabledCount, err := eh.handleDisabledObjects(ctx, a.inventory, disabledObjs)
		if err != nil {
			a.sendErrorEvent(err, superEventHandler)
			return objStatusMap, syncStats
		}
		syncStats.DisableObjs = &stats.DisabledObjStats{
			Total:     uint64(len(disabledObjs)),
			Succeeded: disabledCount,
		}
	}
	klog.Infof("%v objects to be applied: %v", len(enabledObjs), core.GKNNs(enabledObjs))
	resources, err := toUnstructured(enabledObjs)
	if err != nil {
		a.sendErrorEvent(err, superEventHandler)
		return objStatusMap, syncStats
	}

	unknownTypeResources := make(map[core.ID]struct{})
	options := apply.ApplierOptions{
		ServerSideOptions: common.ServerSideOptions{
			ServerSideApply: true,
			ForceConflicts:  true,
			FieldManager:    configsync.FieldManager,
		},
		InventoryPolicy: a.policy,
		// Leaving ReconcileTimeout and PruneTimeout unset may cause a WaitTask to wait forever.
		// ReconcileTimeout defines the timeout for a wait task after an apply task.
		// ReconcileTimeout is a task-level setting instead of an object-level setting.
		ReconcileTimeout: a.reconcileTimeout,
		// PruneTimeout defines the timeout for a wait task after a prune task.
		// PruneTimeout is a task-level setting instead of an object-level setting.
		PruneTimeout: a.reconcileTimeout,
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
	meta.MaybeResetRESTMapper(a.clientSet.Mapper)

	// Builds a map of id -> resource
	resourceMap := make(map[core.ID]client.Object)
	for _, obj := range resources {
		resourceMap[idFrom(object.UnstructuredToObjMetadata(obj))] = obj
	}

	events := a.clientSet.KptApplier.Run(ctx, a.inventory, object.UnstructuredSet(resources), options)
	for e := range events {
		switch e.Type {
		case event.InitType:
			for _, ag := range e.InitEvent.ActionGroups {
				klog.Info("InitEvent", ag)
			}
		case event.ActionGroupType:
			klog.Info(e.ActionGroupEvent)
			if e.ActionGroupEvent.Action == event.InventoryAction && e.ActionGroupEvent.Status == event.Finished {
				inventory, err := a.getInventory(ctx)
				if err != nil {
					a.sendErrorEvent(err, superEventHandler)
				}
				a.sendInventoryEvent(inventory, superEventHandler)
			}
		case event.ErrorType:
			klog.Info(e.ErrorEvent)
			err := e.ErrorEvent.Err
			if util.IsRequestTooLargeError(err) {
				err = largeResourceGroupError(err, idFromInventory(a.inventory))
			}
			a.sendErrorEvent(err, superEventHandler)
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
			if err := eh.processWaitEvent(e.WaitEvent, syncStats.WaitEvent, objStatusMap); err != nil {
				a.sendErrorEvent(err, superEventHandler)
			}
		case event.ApplyType:
			if e.ApplyEvent.Error != nil {
				klog.Info(e.ApplyEvent)
			} else {
				klog.V(1).Info(e.ApplyEvent)
			}
			if err := eh.processApplyEvent(ctx, e.ApplyEvent, syncStats.ApplyEvent, objStatusMap, unknownTypeResources, resourceMap); err != nil {
				a.sendErrorEvent(err, superEventHandler)
			}
		case event.PruneType:
			if e.PruneEvent.Error != nil {
				klog.Info(e.PruneEvent)
			} else {
				klog.V(1).Info(e.PruneEvent)
			}
			if err := eh.processPruneEvent(ctx, e.PruneEvent, syncStats.PruneEvent, objStatusMap); err != nil {
				a.sendErrorEvent(err, superEventHandler)
			}
		default:
			klog.Infof("Unhandled event (%s): %v", e.Type, e)
		}
	}

	return objStatusMap, syncStats
}

func (a *supervisor) sendErrorEvent(err error, superEventHandler func(SuperEvent)) {
	superEventHandler(SuperErrorEvent{Error: wrapError(err)})
}

func wrapError(err error) status.Error {
	if statusErr, ok := err.(status.Error); ok {
		return statusErr
	}
	// Wrap as an applier.Error to indicate the source of the error
	return Error(err)
}

func (a *supervisor) sendInventoryEvent(inventory *v1alpha1.ResourceGroup, superEventHandler func(SuperEvent)) {
	superEventHandler(SuperInventoryEvent{Inventory: inventory})
}

func (a *supervisor) getInventory(ctx context.Context) (*v1alpha1.ResourceGroup, error) {
	rg := &v1alpha1.ResourceGroup{}
	rgRef := types.NamespacedName{
		Name:      a.inventory.Name(),
		Namespace: a.inventory.Namespace(),
	}
	if err := a.clientSet.Client.Get(ctx, rgRef, rg); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, status.APIServerError(err, "reading inventory")
	}
	return rg, nil
}

// destroyInner triggers a kpt live destroy library call to destroy a set of resources.
func (a *supervisor) destroyInner(ctx context.Context, superEventHandler func(SuperEvent)) (ObjectStatusMap, *stats.SyncStats) {
	syncStats := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	eh := eventHandler{
		isDestroy: true,
		clientSet: a.clientSet,
	}

	options := apply.DestroyerOptions{
		InventoryPolicy: a.policy,
		// DeleteTimeout defines the timeout for a wait task after a delete task.
		// DeleteTimeout is a task-level setting instead of an object-level setting.
		DeleteTimeout: a.reconcileTimeout,
		// DeletePropagationPolicy defines what policy to use when deleting
		// managed objects.
		// Use "Foreground" to ensure owned resources are deleted in the right
		// order. This ensures things like a Deployment's ReplicaSets and Pods
		// are deleted before the Namespace that contains them.
		DeletePropagationPolicy: metav1.DeletePropagationForeground,
	}

	// Reset shared mapper before each destroy to invalidate the discovery cache.
	// This allows for picking up CRD changes.
	meta.MaybeResetRESTMapper(a.clientSet.Mapper)

	events := a.clientSet.KptDestroyer.Run(ctx, a.inventory, options)
	for e := range events {
		switch e.Type {
		case event.InitType:
			for _, ag := range e.InitEvent.ActionGroups {
				klog.Info("InitEvent", ag)
			}
		case event.ActionGroupType:
			klog.Info(e.ActionGroupEvent)
			if e.ActionGroupEvent.Action == event.InventoryAction && e.ActionGroupEvent.Status == event.Finished {
				inventory, err := a.getInventory(ctx)
				if err != nil {
					a.sendErrorEvent(err, superEventHandler)
				}
				a.sendInventoryEvent(inventory, superEventHandler)
			}
		case event.ErrorType:
			klog.Info(e.ErrorEvent)
			err := e.ErrorEvent.Err
			if util.IsRequestTooLargeError(err) {
				err = largeResourceGroupError(err, idFromInventory(a.inventory))
			}
			a.sendErrorEvent(err, superEventHandler)
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
			if err := eh.processWaitEvent(e.WaitEvent, syncStats.WaitEvent, objStatusMap); err != nil {
				a.sendErrorEvent(err, superEventHandler)
			}
		case event.DeleteType:
			if e.DeleteEvent.Error != nil {
				klog.Info(e.DeleteEvent)
			} else {
				klog.V(1).Info(e.DeleteEvent)
			}
			if err := eh.processDeleteEvent(ctx, e.DeleteEvent, syncStats.DeleteEvent, objStatusMap); err != nil {
				a.sendErrorEvent(err, superEventHandler)
			}
		default:
			klog.Infof("Unhandled event (%s): %v", e.Type, e)
		}
	}
	return objStatusMap, syncStats
}

// Apply all managed resource objects and return their status.
// Apply implements the Applier interface.
func (a *supervisor) Apply(ctx context.Context, superEventHandler func(SuperEvent), desiredResource []client.Object) (ObjectStatusMap, *stats.SyncStats) {
	a.execMux.Lock()
	defer a.execMux.Unlock()

	return a.applyInner(ctx, superEventHandler, desiredResource)
}

// Destroy all managed resource objects and return their status.
// Destroy implements the Destroyer interface.
func (a *supervisor) Destroy(ctx context.Context, superEventHandler func(SuperEvent)) (ObjectStatusMap, *stats.SyncStats) {
	a.execMux.Lock()
	defer a.execMux.Unlock()

	return a.destroyInner(ctx, superEventHandler)
}

// newInventoryUnstructured creates an inventory object as an unstructured.
func newInventoryUnstructured(kind, name, namespace string, statusMode InventoryStatusMode) *unstructured.Unstructured {
	id := InventoryID(name, namespace)
	u := resourcegroup.Unstructured(name, namespace, id)
	core.SetLabel(u, metadata.ManagedByKey, metadata.ManagedByValue)
	core.SetLabel(u, metadata.SyncNamespaceLabel, namespace)
	core.SetLabel(u, metadata.SyncNameLabel, name)
	core.SetLabel(u, metadata.SyncKindLabel, kind)
	core.SetAnnotation(u, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
	core.SetAnnotation(u, StatusModeKey, statusMode.String())
	return u
}

// InventoryID returns the inventory id of an inventory object.
// The inventory object generated by ConfigSync is in the same namespace as RootSync or RepoSync.
// The inventory ID is assigned as <NAMESPACE>_<NAME>.
func InventoryID(syncName, syncNamespace string) string {
	return fmt.Sprintf("%s_%s", syncNamespace, syncName)
}

// handleDisabledObjects removes the specified objects from the inventory, and
// then disables them, one by one, by removing the ConfigSync metadata.
// Returns the number of objects which are disabled successfully, and any errors
// encountered.
func (h *eventHandler) handleDisabledObjects(ctx context.Context, rg *live.InventoryResourceGroup, objs []client.Object) (uint64, status.MultiError) {
	// disabledCount tracks the number of objects which are disabled successfully
	var disabledCount uint64
	err := h.removeFromInventory(rg, objs)
	if err != nil {
		if nomosutil.IsRequestTooLargeError(err) {
			return disabledCount, largeResourceGroupError(err, idFromInventory(rg))
		}
		return disabledCount, Error(err)
	}
	var errs status.MultiError
	for _, obj := range objs {
		id := core.IDOf(obj)
		err := h.abandonObject(ctx, obj)
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
func (h *eventHandler) removeFromInventory(rg *live.InventoryResourceGroup, objs []client.Object) error {
	clusterInv, err := h.clientSet.InvClient.GetClusterInventoryInfo(rg)
	if err != nil {
		return err
	}
	if clusterInv == nil {
		// If inventory does not exist, there is nothing to remove
		return nil
	}
	wrappedInv, err := wrapInventoryObj(clusterInv)
	if err != nil {
		return err
	}
	oldObjs, err := wrappedInv.Load()
	if err != nil {
		return err
	}
	newObjs := removeFrom(oldObjs, objs)
	err = rg.Store(newObjs, nil)
	if err != nil {
		return err
	}
	return h.clientSet.InvClient.Replace(rg, newObjs, nil, common.DryRunNone)
}

// abandonObject removes ConfigSync labels and annotations from an object,
// disabling management.
func (h *eventHandler) abandonObject(ctx context.Context, obj client.Object) error {
	gvk, err := kinds.Lookup(obj, h.clientSet.Client.Scheme())
	if err != nil {
		return err
	}
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)
	err = h.clientSet.Client.Get(ctx, client.ObjectKeyFromObject(obj), uObj)
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
		updated := metadata.RemoveConfigSyncMetadata(toObj)
		if !updated {
			return nil
		}
		// Use merge-patch instead of server-side-apply, because we don't have
		// the object's source of truth handy and don't want to take ownership
		// of all the fields managed by other clients.
		return h.clientSet.Client.Patch(ctx, toObj, client.MergeFrom(fromObj),
			client.FieldOwner(configsync.FieldManager))
	}
	return nil
}
