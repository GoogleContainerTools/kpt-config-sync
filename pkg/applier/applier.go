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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configmanagement"
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

// applier is the default implimentation of the Applier interface.
type applier struct {
	// inventory policy for the applier.
	policy inventory.Policy
	// inventory is the inventory ResourceGroup for current Applier.
	inventory *live.InventoryResourceGroup
	// clientSet is a wrapper around multiple clients
	clientSet *ClientSet
	// syncKind is the Kind of the RSync object: RootSync or RepoSync
	syncKind string
	// syncName is the name of RSync object
	syncName string
	// syncNamespace is the namespace of RSync object
	syncNamespace string
	// reconcileTimeout controls the reconcile and prune timeout
	reconcileTimeout time.Duration

	// applyMux prevents concurrent Apply() calls
	applyMux sync.Mutex
	// errorMux prevents concurrent modifications to the cached set of errors
	errorMux sync.RWMutex
	// errs tracks all the errors the applier encounters.
	// This field is cleared at the start of the `Applier.Apply` method
	errs status.MultiError
}

// Applier is a bulk client for applying a set of desired resource objects and
// tracking them in a ResourceGroup inventory. This enables pruning objects
// by removing them from the list of desired resource objects and re-applying.
type Applier interface {
	// Apply updates the resource API server with the latest parsed git resource.
	// This is called when a new change in the git resource is detected. It also
	// returns a map of the GVKs which were successfully applied by the Applier.
	Apply(ctx context.Context, desiredResources []client.Object) (map[schema.GroupVersionKind]struct{}, status.MultiError)
	// Errors returns the errors encountered during apply.
	Errors() status.MultiError
}

var _ Applier = &applier{}

// NewNamespaceApplier initializes an applier that fetches a certain namespace's resources from
// the API server.
func NewNamespaceApplier(cs *ClientSet, namespace declared.Scope, syncName string, reconcileTimeout time.Duration) (Applier, error) {
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
	a := &applier{
		inventory:        inv,
		clientSet:        cs,
		policy:           inventory.PolicyAdoptIfNoInventory,
		syncKind:         syncKind,
		syncName:         syncName,
		syncNamespace:    string(namespace),
		reconcileTimeout: reconcileTimeout,
	}
	klog.V(4).Infof("Applier %s/%s is initialized", namespace, syncName)
	return a, nil
}

// NewRootApplier initializes an applier that can fetch all resources from the API server.
func NewRootApplier(cs *ClientSet, syncName string, reconcileTimeout time.Duration) (Applier, error) {
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
	a := &applier{
		inventory:        inv,
		clientSet:        cs,
		policy:           inventory.PolicyAdoptAll,
		syncKind:         syncKind,
		syncName:         syncName,
		syncNamespace:    string(configmanagement.ControllerNamespace),
		reconcileTimeout: reconcileTimeout,
	}
	klog.V(4).Infof("Root applier %s is initialized and synced with the API server", syncName)
	return a, nil
}

func wrapInventoryObj(obj *unstructured.Unstructured) (*live.InventoryResourceGroup, error) {
	inv, ok := live.WrapInventoryObj(obj).(*live.InventoryResourceGroup)
	if !ok {
		return nil, errors.New("failed to create an ResourceGroup object")
	}
	return inv, nil
}

func processApplyEvent(ctx context.Context, e event.ApplyEvent, s *stats.ApplyEventStats, objectStatusMap ObjectStatusMap, unknownTypeResources map[core.ID]struct{}) status.Error {
	id := idFrom(e.Identifier)
	klog.V(4).Infof("apply %v for object: %v", e.Status, id)
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
		handleMetrics(ctx, "update", e.Error, id.WithVersion(""))
		return nil

	case event.ApplyFailed:
		objectStatus.Actuation = actuation.ActuationFailed
		handleMetrics(ctx, "update", e.Error, id.WithVersion(""))
		switch e.Error.(type) {
		case *applyerror.UnknownTypeError:
			unknownTypeResources[id] = struct{}{}
			return ErrorForResource(e.Error, id)
		default:
			return ErrorForResource(e.Error, id)
		}

	case event.ApplySkipped:
		objectStatus.Actuation = actuation.ActuationSkipped
		// Skip event always includes an error with the reason
		return handleApplySkippedEvent(e.Resource, id, e.Error)

	default:
		return ErrorForResource(fmt.Errorf("unexpected prune event status: %v", e.Status), id)
	}
}

func processWaitEvent(e event.WaitEvent, s *stats.WaitEventStats, objectStatusMap ObjectStatusMap) error {
	id := idFrom(e.Identifier)
	if e.Status != event.ReconcilePending {
		// Don't log pending. It's noisy and only fires in certain conditions.
		klog.V(4).Infof("Reconcile %v: %v", e.Status, id)
	}
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
	case event.ReconcileTimeout:
		objectStatus.Reconcile = actuation.ReconcileTimeout
	default:
		return ErrorForResource(fmt.Errorf("unexpected wait event status: %v", e.Status), id)
	}
	return nil
}

// handleApplySkippedEvent translates from apply skipped event into resource error.
func handleApplySkippedEvent(obj *unstructured.Unstructured, id core.ID, err error) status.Error {
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

func (a *applier) processPruneEvent(ctx context.Context, e event.PruneEvent, s *stats.PruneEventStats, objectStatusMap ObjectStatusMap) status.Error {
	id := idFrom(e.Identifier)
	klog.V(4).Infof("prune %v for object: %v", e.Status, id)
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
		handleMetrics(ctx, "delete", e.Error, id.WithVersion(""))
		return nil

	case event.PruneFailed:
		objectStatus.Actuation = actuation.ActuationFailed
		handleMetrics(ctx, "delete", e.Error, id.WithVersion(""))
		return PruneErrorForResource(e.Error, id)

	case event.PruneSkipped:
		objectStatus.Actuation = actuation.ActuationSkipped
		// Skip event always includes an error with the reason
		return a.handlePruneSkippedEvent(ctx, e.Object, id, e.Error)

	default:
		return PruneErrorForResource(fmt.Errorf("unexpected prune event status: %v", e.Status), id)
	}
}

// handlePruneSkippedEvent translates from prune skipped event into resource error.
func (a *applier) handlePruneSkippedEvent(ctx context.Context, obj *unstructured.Unstructured, id core.ID, err error) status.Error {
	// Disable protected namespaces that were removed from the desired object set.
	if isNamespace(obj) && differ.SpecialNamespaces[obj.GetName()] {
		// the `client.lifecycle.config.k8s.io/deletion: detach` annotation is not a part of the Config Sync metadata, and will not be removed here.
		err := a.disableObject(ctx, obj)
		handleMetrics(ctx, "unmanage", err, id.WithVersion(""))
		if err != nil {
			errorMsg := "failed to remove the Config Sync metadata from %v (which is a special namespace): %v"
			klog.Errorf(errorMsg, id, err)
			return applierErrorBuilder.Wrap(fmt.Errorf(errorMsg, id, err)).Build()
		}
		klog.V(4).Infof("removed the Config Sync metadata from %v (which is a special namespace)", id)
	}

	var depErr *filter.DependencyPreventedActuationError
	if errors.As(err, &depErr) {
		return SkipErrorForResource(err, id, depErr.Strategy)
	}

	var depMismatchErr *filter.DependencyActuationMismatchError
	if errors.As(err, &depMismatchErr) {
		return SkipErrorForResource(err, id, depMismatchErr.Strategy)
	}

	var applyDeleteErr *filter.ApplyPreventedDeletionError
	if errors.As(err, &applyDeleteErr) {
		return SkipErrorForResource(err, id, actuation.ActuationStrategyDelete)
	}

	var policyErr *inventory.PolicyPreventedActuationError
	if errors.As(err, &policyErr) {
		// For prunes, this is desired behavior, not a fatal error.
		klog.Infof("Resource object removed from inventory,  but not deleted: %v: %v", id, err)
		return nil
	}

	var namespaceErr *filter.NamespaceInUseError
	if errors.As(err, &namespaceErr) {
		return SkipErrorForResource(err, id, actuation.ActuationStrategyDelete)
	}

	var abandonErr *filter.AnnotationPreventedDeletionError
	if errors.As(err, &abandonErr) {
		// For prunes, this is desired behavior, not a fatal error.
		klog.Infof("Resource object removed from inventory, but not deleted: %v: %v", id, err)
		return nil
	}

	return SkipErrorForResource(err, id, actuation.ActuationStrategyDelete)
}

func isNamespace(obj *unstructured.Unstructured) bool {
	return obj.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind()
}

func handleMetrics(ctx context.Context, operation string, err error, gvk schema.GroupVersionKind) {
	// TODO capture the apply duration in the kpt apply library.
	start := time.Now()

	m.RecordAPICallDuration(ctx, operation, m.StatusTagKey(err), gvk, start)
	metrics.Operations.WithLabelValues(operation, gvk.Kind, metrics.StatusLabel(err)).Inc()
	m.RecordApplyOperation(ctx, m.ApplierController, operation, m.StatusTagKey(err), gvk)
}

// checkInventoryObjectSize checks the inventory object size limit.
// If it is close to the size limit 1M, log a warning.
func (a *applier) checkInventoryObjectSize(ctx context.Context, c client.Client) {
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
func (a *applier) applyInner(ctx context.Context, objs []client.Object) (map[schema.GroupVersionKind]struct{}, status.MultiError) {
	a.checkInventoryObjectSize(ctx, a.clientSet.Client)

	s := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	// disabledObjs are objects for which the management are disabled
	// through annotation.
	enabledObjs, disabledObjs := partitionObjs(objs)
	if len(disabledObjs) > 0 {
		klog.Infof("%v objects to be disabled: %v", len(disabledObjs), core.GKNNs(disabledObjs))
		disabledCount, err := a.handleDisabledObjects(ctx, a.inventory, disabledObjs)
		if err != nil {
			a.addError(err)
			return nil, a.Errors()
		}
		s.DisableObjs = &stats.DisabledObjStats{
			Total:     uint64(len(disabledObjs)),
			Succeeded: disabledCount,
		}
	}
	klog.Infof("%v objects to be applied: %v", len(enabledObjs), core.GKNNs(enabledObjs))
	resources, err := toUnstructured(enabledObjs)
	if err != nil {
		a.addError(err)
		return nil, a.Errors()
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
	}

	// Reset shared mapper before each apply to invalidate the discovery cache.
	// This allows for picking up CRD changes.
	meta.MaybeResetRESTMapper(a.clientSet.Mapper)

	events := a.clientSet.KptApplier.Run(ctx, a.inventory, object.UnstructuredSet(resources), options)
	for e := range events {
		switch e.Type {
		case event.InitType:
			for _, ag := range e.InitEvent.ActionGroups {
				klog.Info("InitEvent", ag)
			}
		case event.ActionGroupType:
			klog.Info(e.ActionGroupEvent)
		case event.ErrorType:
			klog.V(4).Info(e.ErrorEvent)
			if util.IsRequestTooLargeError(e.ErrorEvent.Err) {
				a.addError(largeResourceGroupError(e.ErrorEvent.Err, idFromInventory(a.inventory)))
			} else {
				a.addError(e.ErrorEvent.Err)
			}
			s.ErrorTypeEvents++
		case event.WaitType:
			// Log WaitEvent at the verbose level of 4 due to the number of WaitEvent.
			// For every object which is skipped to apply/prune, there will be one ReconcileSkipped WaitEvent.
			// For every object which is not skipped to apply/prune, there will be at least two WaitEvent:
			// one ReconcilePending WaitEvent and one Reconciled/ReconcileFailed/ReconcileTimeout WaitEvent. In addition,
			// a reconciled object may become pending before a wait task times out.
			// Record the objs that have been reconciled.
			klog.V(4).Info(e.WaitEvent)
			a.addError(processWaitEvent(e.WaitEvent, s.WaitEvent, objStatusMap))
		case event.ApplyType:
			logEvent := event.ApplyEvent{
				GroupName:  e.ApplyEvent.GroupName,
				Identifier: e.ApplyEvent.Identifier,
				Status:     e.ApplyEvent.Status,
				// nil Resource to reduce log noise
				Error: e.ApplyEvent.Error,
			}
			klog.V(4).Info(logEvent)
			a.addError(processApplyEvent(ctx, e.ApplyEvent, s.ApplyEvent, objStatusMap, unknownTypeResources))
		case event.PruneType:
			logEvent := event.PruneEvent{
				GroupName:  e.PruneEvent.GroupName,
				Identifier: e.PruneEvent.Identifier,
				Status:     e.PruneEvent.Status,
				// nil Resource to reduce log noise
				Error: e.PruneEvent.Error,
			}
			klog.V(4).Info(logEvent)
			a.addError(a.processPruneEvent(ctx, e.PruneEvent, s.PruneEvent, objStatusMap))
		default:
			klog.V(4).Infof("skipped %v event", e.Type)
		}
	}

	gvks := make(map[schema.GroupVersionKind]struct{})
	for _, resource := range objs {
		id := core.IDOf(resource)
		if _, found := unknownTypeResources[id]; found {
			continue
		}
		gvks[resource.GetObjectKind().GroupVersionKind()] = struct{}{}
	}

	errs := a.Errors()
	if errs == nil {
		klog.V(4).Infof("all resources are up to date.")
	}
	if s.Empty() {
		klog.V(4).Infof("The applier made no new progress")
	} else {
		klog.Infof("The applier made new progress: %s", s.String())
		objStatusMap.Log(klog.V(0))
	}
	return gvks, errs
}

// Errors returns the errors encountered during the last apply or current apply
// if still running.
// Errors implements Interface.
func (a *applier) Errors() status.MultiError {
	a.errorMux.RLock()
	defer a.errorMux.RUnlock()

	// Return a copy to avoid persisting caller modifications
	return status.Append(nil, a.errs)
}

func (a *applier) addError(err error) {
	a.errorMux.Lock()
	defer a.errorMux.Unlock()

	if _, ok := err.(status.Error); !ok {
		// Wrap as an applier.Error to indidate the source of the error
		err = Error(err)
	}

	a.errs = status.Append(a.errs, err)
}

func (a *applier) invalidateErrors() {
	a.errorMux.Lock()
	defer a.errorMux.Unlock()

	a.errs = nil
}

// Apply implements Interface.
func (a *applier) Apply(ctx context.Context, desiredResource []client.Object) (map[schema.GroupVersionKind]struct{}, status.MultiError) {
	a.applyMux.Lock()
	defer a.applyMux.Unlock()

	// Ideally we want to avoid invalidating errors that will continue to happen,
	// but for now, invalidate all errors until they recur.
	// TODO: improve error cache invalidation to make rsync status more stable
	a.invalidateErrors()
	return a.applyInner(ctx, desiredResource)
}

// newInventoryUnstructured creates an inventory object as an unstructured.
func newInventoryUnstructured(kind, name, namespace, statusMode string) *unstructured.Unstructured {
	id := InventoryID(name, namespace)
	u := resourcegroup.Unstructured(name, namespace, id)
	core.SetLabel(u, metadata.ManagedByKey, metadata.ManagedByValue)
	core.SetLabel(u, metadata.SyncNamespaceLabel, namespace)
	core.SetLabel(u, metadata.SyncNameLabel, name)
	core.SetLabel(u, metadata.SyncKindLabel, kind)
	core.SetAnnotation(u, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
	core.SetAnnotation(u, StatusModeKey, statusMode)
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
func (a *applier) handleDisabledObjects(ctx context.Context, rg *live.InventoryResourceGroup, objs []client.Object) (uint64, status.MultiError) {
	// disabledCount tracks the number of objects which are disabled successfully
	var disabledCount uint64
	err := a.removeFromInventory(rg, objs)
	if err != nil {
		if nomosutil.IsRequestTooLargeError(err) {
			return disabledCount, largeResourceGroupError(err, idFromInventory(rg))
		}
		return disabledCount, Error(err)
	}
	var errs status.MultiError
	for _, obj := range objs {
		err := a.disableObject(ctx, obj)
		handleMetrics(ctx, "unmanage", err, obj.GetObjectKind().GroupVersionKind())
		if err != nil {
			klog.Warningf("failed to disable object %v", core.IDOf(obj))
			errs = status.Append(errs, Error(err))
		} else {
			klog.V(4).Infof("disabled object %v", core.IDOf(obj))
			disabledCount++
		}
	}
	return disabledCount, errs
}

// removeFromInventory removes the specified objects from the inventory, if it
// exists.
func (a *applier) removeFromInventory(rg *live.InventoryResourceGroup, objs []client.Object) error {
	clusterInv, err := a.clientSet.InvClient.GetClusterInventoryInfo(rg)
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
	return a.clientSet.InvClient.Replace(rg, newObjs, nil, common.DryRunNone)
}

// disableObject disables the management for a single object by removing the
// ConfigSync labels and annotations.
func (a *applier) disableObject(ctx context.Context, obj client.Object) error {
	meta := ObjMetaFromObject(obj)
	mapping, err := a.clientSet.Mapper.RESTMapping(meta.GroupKind)
	if err != nil {
		return err
	}
	uObj, err := a.clientSet.DynamicClient.Resource(mapping.Resource).
		Namespace(meta.Namespace).
		Get(ctx, meta.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if metadata.HasConfigSyncMetadata(uObj) {
		updated := metadata.RemoveConfigSyncMetadata(uObj)
		if !updated {
			return nil
		}
		uObj.SetManagedFields(nil)
		return a.clientSet.Client.Patch(ctx, uObj, client.Apply, client.FieldOwner(configsync.FieldManager), client.ForceOwnership)
	}
	return nil
}
