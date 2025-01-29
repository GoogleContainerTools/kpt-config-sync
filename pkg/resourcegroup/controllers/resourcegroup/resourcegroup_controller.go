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

package resourcegroup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/handler"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/metrics"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	controllerstatus "kpt.dev/configsync/pkg/resourcegroup/controllers/status"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/typeresolver"
	"sigs.k8s.io/cli-utils/pkg/common"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//nolint:revive // TODO: add comments for public constants and enable linting
const (
	StartReconciling         = "StartReconciling"
	startReconcilingMsg      = "start reconciling"
	FinishReconciling        = "FinishReconciling"
	finishReconcilingMsg     = "finish reconciling"
	ComponentFailed          = "ComponentFailed"
	componentFailedMsgPrefix = "The following components failed:"
	ExceedTimeout            = "ExceedTimeout"
	exceedTimeoutMsg         = "Exceed timeout, the .status.observedGeneration and .status.resourceStatuses fields are old."
	owningInventoryKey       = "config.k8s.io/owning-inventory"
	readinessComponent       = "readiness"
)

// DefaultDuration is the default throttling duration
var DefaultDuration = 30 * time.Second

// reconciler reconciles a ResourceGroup object
type reconciler struct {
	*controllers.LoggingController

	// Client is to get and update ResourceGroup object.
	client client.Client

	// resolver is the type resolver to find the server preferred
	// GVK for a GK
	resolver *typeresolver.TypeResolver

	// resMap is the resourcemap for storing the resource status and conditions.
	resMap *resourcemap.ResourceMap
}

// +kubebuilder:rbac:groups=kpt.dev,resources=resourcegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kpt.dev,resources=resourcegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = r.SetLoggerValues(ctx, "resourcegroup", req.NamespacedName)
	r.Logger(ctx).V(3).Info("Reconcile starting")

	// obtain the ResourceGroup CR
	rgObj := &v1alpha1.ResourceGroup{}
	err := r.client.Get(ctx, req.NamespacedName, rgObj)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if rgObj.DeletionTimestamp != nil {
		r.Logger(ctx).V(3).Info("Skipping ResourceGroup status update: ResourceGroup being deleted")
		return ctrl.Result{}, nil
	}

	// Special gated behavior for ResourceGroups managed by Config Sync.
	// This way the ResourceGroupController still works with kpt managed ResourceGroups.
	if isManagedByConfigSync(rgObj) {
		// Skip ResourceGroup status updates if the applier hasn't updated the
		// status to reflect the spec yet. This usually happens back to back in the
		// inventory client, and will trigger another watch event. Otherwise, if we
		// update the status first, the status update in the applier may fail due to
		// ResourceVersion conflict
		if rgObj.Status.ObservedGenerations.Reconciler != rgObj.Generation {
			r.Logger(ctx).V(3).Info("Skipping ResourceGroup status update: ResourceGroup status update pending by Reconciler")
			return ctrl.Result{}, nil
		}
	}

	// Since this controller doesn't perform any writes in any external systems,
	// it doesn't need to set the Reconciling condition status to True before
	// building the new status, because there are no possible side-effects that
	// the user might need to know about when reconciling fails.
	// So skip initializing the Reconciling & Stalled conditions and only set
	// them when reconciling succeeds or fails.

	id := getInventoryID(rgObj.Labels)
	newStatus := r.endReconcilingStatus(ctx, id, req.NamespacedName, rgObj.Spec, rgObj.Status, rgObj.Generation)
	if err := r.updateStatusKptGroup(ctx, rgObj, newStatus); err != nil {
		r.Logger(ctx).Error(err, "failed to update ResourceGroup")
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{}, nil
}

func isManagedByConfigSync(obj client.Object) bool {
	return core.GetLabel(obj, metadata.ManagedByKey) == metadata.ManagedByValue
}

// currentStatusCount counts the number of `Current` statuses.
func currentStatusCount(statuses []v1alpha1.ResourceStatus) int {
	count := 0
	for _, status := range statuses {
		if status.Status == v1alpha1.Current {
			count++
		}
	}
	return count
}

// updateResourceMetrics updates the resource metrics.
func updateResourceMetrics(ctx context.Context, nn types.NamespacedName, statuses []v1alpha1.ResourceStatus) {
	metrics.RecordReadyResourceCount(ctx, nn, int64(currentStatusCount(statuses)))
	metrics.RecordResourceCount(ctx, nn, int64(len(statuses)))
	namespaces := make(map[string]bool)
	clusterScopedCount := 0
	kccCount := 0
	crds := make(map[string]bool)
	for _, res := range statuses {
		namespaces[res.Namespace] = true
		if res.Namespace == "" {
			clusterScopedCount++
		}
		if res.Kind == "CustomResourceDefinition" {
			crds[res.Name] = true
		}
		if strings.Contains(res.Group, "cnrm.cloud.google.com") {
			kccCount++
		}
	}
	metrics.RecordNamespaceCount(ctx, nn, int64(len(namespaces)))
	metrics.RecordClusterScopedResourceCount(ctx, nn, int64(clusterScopedCount))
	metrics.RecordCRDCount(ctx, nn, int64(len(crds)))
	metrics.RecordKCCResourceCount(ctx, nn, int64(kccCount))
}

func (r *reconciler) updateStatusKptGroup(ctx context.Context, rgObj *v1alpha1.ResourceGroup, newStatus v1alpha1.ResourceGroupStatus) error {
	// Sort the conditions
	newStatus.Conditions = adjustConditionOrder(newStatus.Conditions)

	// Skip API call if no status changes
	if apiequality.Semantic.DeepEqual(rgObj.Status, newStatus) {
		return nil
	}

	// Keep the current metadata & spec, but update the status.
	rgObj.Status = newStatus

	// Use `r.Status().Update()` here instead of `r.Update()` to update only resgroup.Status.
	// Don't retry here on conflict!
	// Let the controller-manager handle retry with backoff to make sure all the
	// early-exit checks are performed and the spec & metadata are up to date.
	return r.client.Status().Update(ctx, rgObj, client.FieldOwner(FieldManager))
}

func (r *reconciler) endReconcilingStatus(
	ctx context.Context,
	id string,
	namespacedName types.NamespacedName,
	spec v1alpha1.ResourceGroupSpec,
	status v1alpha1.ResourceGroupStatus,
	generation int64,
) v1alpha1.ResourceGroupStatus {
	// Preserve existing status and only update fields that need to change.
	// This avoids repeatedly updating the status just to update a timestamp.
	// It also prevents fighting with other controllers updating the status.
	newStatus := *status.DeepCopy()
	startTime := time.Now()
	reconcileTimeout := getReconcileTimeOut(len(spec.Subgroups) + len(spec.Resources))

	finish := make(chan struct{})
	go func() {
		defer close(finish)
		newStatus.ResourceStatuses = r.computeResourceStatuses(ctx, id, status, spec.Resources, namespacedName)
		newStatus.SubgroupStatuses = r.computeSubGroupStatuses(ctx, id, status, spec.Subgroups, namespacedName)
	}()
	select {
	case <-finish:
		// Update conditions, if they've changed
		newStatus.Conditions = updateCondition(newStatus.Conditions,
			newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg))
		newStatus.Conditions = updateCondition(newStatus.Conditions,
			aggregateResourceStatuses(newStatus.ResourceStatuses))
	case <-time.After(reconcileTimeout):
		// Update conditions, if they've changed
		newStatus.Conditions = updateCondition(newStatus.Conditions,
			newReconcilingCondition(v1alpha1.FalseConditionStatus, ExceedTimeout, exceedTimeoutMsg))
		newStatus.Conditions = updateCondition(newStatus.Conditions,
			newStalledCondition(v1alpha1.TrueConditionStatus, ExceedTimeout, exceedTimeoutMsg))
	}

	// Always update this controller's ObservedGenerations entry
	newStatus.ObservedGenerations.ResourceGroupController = generation
	// It's also safe to always update the aggregate ObservedGeneration,
	// because we know all the other controllers already updated theirs,
	// due to checks in the Reconcile method.
	newStatus.ObservedGeneration = generation

	metrics.RecordReconcileDuration(ctx, newStatus.Conditions[1].Reason, startTime)
	updateResourceMetrics(ctx, namespacedName, newStatus.ResourceStatuses)
	return newStatus
}

func (r *reconciler) computeResourceStatuses(
	ctx context.Context,
	id string,
	existingStatus v1alpha1.ResourceGroupStatus,
	metas []v1alpha1.ObjMetadata,
	nn types.NamespacedName,
) []v1alpha1.ResourceStatus {
	return r.computeStatus(ctx, id, existingStatus, metas, nn, true)
}

func (r *reconciler) computeSubGroupStatuses(
	ctx context.Context,
	id string,
	existingStatus v1alpha1.ResourceGroupStatus,
	groupMetas []v1alpha1.GroupMetadata,
	nn types.NamespacedName,
) []v1alpha1.GroupStatus {
	objMetadata := v1alpha1.ToObjMetadata(groupMetas)
	statuses := r.computeStatus(ctx, id, existingStatus, objMetadata, nn, false)
	return v1alpha1.ToGroupStatuses(statuses)
}

// getResourceStatus returns a map of resource statuses indexed on the resource's object meta.
func (r *reconciler) getResourceStatus(status v1alpha1.ResourceGroupStatus) map[v1alpha1.ObjMetadata]v1alpha1.ResourceStatus {
	statuses := make(map[v1alpha1.ObjMetadata]v1alpha1.ResourceStatus, len(status.ResourceStatuses))

	for _, res := range status.ResourceStatuses {
		statuses[res.ObjMetadata] = res
	}

	return statuses
}

// isResource flag indicates if the compute is for resource when true. If false, the compute is for SubGroup (or others).
func (r *reconciler) computeStatus(
	ctx context.Context,
	id string,
	existingStatus v1alpha1.ResourceGroupStatus,
	metas []v1alpha1.ObjMetadata,
	nn types.NamespacedName,
	isResource bool,
) []v1alpha1.ResourceStatus {
	actuationStatuses := r.getResourceStatus(existingStatus)
	statuses := []v1alpha1.ResourceStatus{}
	hasErr := false
	for _, res := range metas {
		resStatus := v1alpha1.ResourceStatus{
			ObjMetadata: res,
		}
		// New temporary logger for each object to add object ID fields
		log := r.Logger(ctx).WithValues("inventory.object", res)

		cachedStatus := r.resMap.GetStatus(res)

		// Add status to cache, if not present.
		switch {
		case cachedStatus != nil:
			log.V(4).Info("Resource object status found in the cache")
			setResStatus(id, &resStatus, cachedStatus)
		default:
			log.V(4).Info("Resource object status not found in the cache")
			resObj := new(unstructured.Unstructured)
			gvk, gvkFound := r.resolver.Resolve(schema.GroupKind(res.GroupKind))
			if !gvkFound {
				// If the resolver cache does not contain the server preferred GVK, then GVK returned
				// will be empty resulting in a GET error. An instance of this occurring is when the
				// resource type (CRD) does not exist.
				log.V(4).Info("Resource type unknown")
				resStatus.Status = v1alpha1.NotFound
				break // Breaks out of switch statement, not the loop.
			}
			resObj.SetGroupVersionKind(gvk)
			log.V(4).Info("Fetching object from API server")
			err := r.client.Get(ctx, types.NamespacedName{
				Namespace: res.Namespace,
				Name:      res.Name,
			}, resObj)
			if err != nil {
				switch {
				case meta.IsNoMatchError(err):
					log.V(4).Info("Resource type unknown")
					resStatus.Status = v1alpha1.NotFound
				case errors.IsNotFound(err):
					log.V(4).Info("Resource object not found")
					resStatus.Status = v1alpha1.NotFound
				default:
					log.Error(err, "Resource object status unknown: failed to get resource object from API server to compute status")
					resStatus.Status = v1alpha1.Unknown
				}
				break // Breaks out of switch statement, not the loop.
			}
			// get the resource status using the kstatus library
			cachedStatus = controllerstatus.ComputeStatus(resObj)
			// save the computed status and condition in memory.
			r.resMap.SetStatus(res, cachedStatus)
			// Update the new resource status.
			setResStatus(id, &resStatus, cachedStatus)
		}

		if resStatus.Status == v1alpha1.Failed || controllerstatus.IsCNRMResource(resStatus.Group) && resStatus.Status != v1alpha1.Current {
			hasErr = true
		}

		// If the inventory status was already populated...
		if aStatus, exists := actuationStatuses[resStatus.ObjMetadata]; exists {
			// Start with the existing status values
			resStatus.Strategy = aStatus.Strategy
			resStatus.Actuation = aStatus.Actuation
			resStatus.Reconcile = aStatus.Reconcile

			// Update the reconcile status based on the existing Strategy and
			// Actuation status and latest kstatus.
			resStatus.Reconcile = KstatusToReconcileStatus(resStatus)
		}

		log.V(5).Info("Resource object status computed", "status", resStatus)

		// add the resource status into resgroup
		statuses = append(statuses, resStatus)
	}
	// Only record pipeline_error_observed when computing status for resources
	// Since the subgroup is always 0 and can overwrite the resource status
	if isResource {
		metrics.RecordPipelineError(ctx, nn, readinessComponent, hasErr)
	}
	return statuses
}

// KstatusToReconcileStatus uses the current Strategy and Actuation status from
// the applier and the newly computed kstatus to compute the Reconcile status.
func KstatusToReconcileStatus(status v1alpha1.ResourceStatus) v1alpha1.Reconcile {
	switch status.Strategy {
	case v1alpha1.Apply:
		switch status.Actuation {
		case v1alpha1.ActuationSucceeded:
			switch status.Status {
			case v1alpha1.Current:
				return v1alpha1.ReconcileSucceeded
			case v1alpha1.InProgress:
				return v1alpha1.ReconcilePending
			case v1alpha1.Failed:
				return v1alpha1.ReconcileFailed
			case v1alpha1.Terminating:
				return v1alpha1.ReconcileFailed
			case v1alpha1.NotFound:
				return v1alpha1.ReconcileFailed
			default: // Invalid kstatus
				return status.Reconcile // Keep the old Reconcile status
			}
		case v1alpha1.ActuationPending:
			return v1alpha1.ReconcilePending
		case v1alpha1.ActuationSkipped:
			return v1alpha1.ReconcileSkipped
		case v1alpha1.ActuationFailed:
			return v1alpha1.ReconcileSkipped
		default: // Invalid Actuation status
			return "" // Unknown Reconcile status
		}

	case v1alpha1.Delete:
		switch status.Actuation {
		case v1alpha1.ActuationSucceeded:
			switch status.Status {
			case v1alpha1.Current:
				return v1alpha1.ReconcileFailed
			case v1alpha1.InProgress:
				return v1alpha1.ReconcileFailed
			case v1alpha1.Failed:
				return v1alpha1.ReconcileFailed
			case v1alpha1.Terminating:
				return v1alpha1.ReconcilePending
			case v1alpha1.NotFound:
				return v1alpha1.ReconcileSucceeded
			default: // Invalid kstatus
				return status.Reconcile // Keep the old Reconcile status
			}
		case v1alpha1.ActuationPending:
			return v1alpha1.ReconcilePending
		case v1alpha1.ActuationSkipped:
			return v1alpha1.ReconcileSkipped
		case v1alpha1.ActuationFailed:
			return v1alpha1.ReconcileSkipped
		default: // Invalid Actuation status
			return "" // Unknown Reconcile status
		}

	default: // Invalid Strategy
		return "" // Unknown Reconcile status
	}
}

// setResStatus updates a resource status struct using values within the cached status struct.
func setResStatus(id string, resStatus *v1alpha1.ResourceStatus, cachedStatus *resourcemap.CachedStatus) {
	resStatus.Status = cachedStatus.Status
	resStatus.Conditions = make([]v1alpha1.Condition, len(cachedStatus.Conditions))
	copy(resStatus.Conditions, cachedStatus.Conditions)
	resStatus.SourceHash = cachedStatus.SourceHash
	cond := ownershipCondition(id, cachedStatus.InventoryID)
	if cond != nil {
		resStatus.Conditions = append(resStatus.Conditions, *cond)
	}
}

func aggregateResourceStatuses(statuses []v1alpha1.ResourceStatus) v1alpha1.Condition {
	failedResources := []string{}
	for _, status := range statuses {
		if status.Status == v1alpha1.Failed {
			res := status.ObjMetadata
			resStr := fmt.Sprintf("%s/%s/%s/%s", res.Group, res.Kind, res.Namespace, res.Name)
			failedResources = append(failedResources, resStr)
		}
	}
	if len(failedResources) > 0 {
		return newStalledCondition(v1alpha1.TrueConditionStatus, ComponentFailed,
			componentFailedMsgPrefix+strings.Join(failedResources, ", "))
	}
	return newStalledCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg)
}

// NewRGController creates a new ResourceGroup controller and registers it with
// the provided manager.
func NewRGController(mgr ctrl.Manager, channel chan event.GenericEvent, logger logr.Logger,
	resolver *typeresolver.TypeResolver, resMap *resourcemap.ResourceMap, duration time.Duration) error {
	r := &reconciler{
		LoggingController: controllers.NewLoggingController(logger),
		client:            mgr.GetClient(),
		resolver:          resolver,
		resMap:            resMap,
	}

	c, err := controller.New(v1alpha1.ResourceGroupKind, mgr, controller.Options{
		Reconciler: reconcile.Func(r.Reconcile),
	})

	if err != nil {
		return err
	}

	err = c.Watch(source.Channel(channel, handler.NewThrottler(duration)))
	if err != nil {
		return err
	}
	return nil
}

func getInventoryID(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	return labels[common.InventoryLabel]
}

func ownershipCondition(id, inv string) *v1alpha1.Condition {
	if id == inv {
		return nil
	}
	c := &v1alpha1.Condition{
		Type:               v1alpha1.Ownership,
		LastTransitionTime: metav1.Now(),
	}
	if inv != "" {
		c.Status = v1alpha1.TrueConditionStatus
		c.Reason = v1alpha1.OwnershipUnmatch
		c.Message = fmt.Sprintf("This resource is owned by another ResourceGroup %s. "+
			"The status only reflects the specification for the current object in ResourceGroup %s.", inv, inv)
	} else {
		c.Status = v1alpha1.UnknownConditionStatus
		c.Reason = v1alpha1.OwnershipEmpty
		c.Message = "This object is not owned by any inventory object. " +
			"The status for the current object may not reflect the specification for it in current ResourceGroup."
	}
	return c
}

// maxTimeout is the largest timeout it may take
// to reconcile a ResourceGroup CR with the assumption
// that there are at most 5000 resources GKNN inside the
// ResourceGroup CR.
var maxTimeout = 5 * time.Minute

// getReconcileTimeOut returns the timeout based on how many resources
// that are listed in the ResourceGroup spec.
// The rule for setting the timeout is that every 500 resources
// get 30 seconds.
func getReconcileTimeOut(count int) time.Duration {
	q := count/500 + 1
	timeout := time.Duration(q) * 30 * time.Second
	if timeout > maxTimeout {
		return maxTimeout
	}
	return timeout
}
