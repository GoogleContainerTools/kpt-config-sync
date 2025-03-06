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

// updateStatusKptGroup updates the ResourceGroup status.
// To avoid unnecessary updates, it sorts the conditions and checks for a diff.
//
// This method explicitly does not retry on conflict errors, to allow the
// controller manager to handle retry backoff, which it does for all errors,
// not just conflicts. This allows the reconciler to pick up new changes between
// update attempts, instead of getting stuck here retrying endlessly.
func (r *reconciler) updateStatusKptGroup(ctx context.Context, resgroup *v1alpha1.ResourceGroup, newStatus v1alpha1.ResourceGroupStatus) error {
	newStatus.Conditions = adjustConditionOrder(newStatus.Conditions)
	if apiequality.Semantic.DeepEqual(resgroup.Status, newStatus) {
		return nil
	}
	resgroup.Status = newStatus
	// Use `r.Status().Update()` here instead of `r.Update()` to update only resgroup.Status.
	return r.client.Status().Update(ctx, resgroup, client.FieldOwner(FieldManager))
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
		// Updating the ObservedGeneration should be a no-op, because the
		// applier should have updated it already.
		newStatus.ObservedGeneration = generation
		newStatus.Conditions = updateCondition(newStatus.Conditions,
			aggregateResourceStatuses(newStatus.ResourceStatuses))
	case <-time.After(reconcileTimeout):
		newStatus.Conditions = updateCondition(newStatus.Conditions,
			newStalledCondition(v1alpha1.TrueConditionStatus, ExceedTimeout, exceedTimeoutMsg))
	}

	// Previously, the Reconciling condition toggled between True & False while
	// this controller was reconciling, but this caused unnecessary churn and
	// confusion, because it didn't reflect the reconciliation status of the
	// managed objects in status.resourceStatuses. So now it's always set to
	// False, for reverse compatibility, in case any clients were waiting it.
	newStatus.Conditions = updateCondition(newStatus.Conditions,
		newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg))

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

		// Keep the existing object statuses written by cli-utils
		aStatus, exists := actuationStatuses[resStatus.ObjMetadata]
		if exists {
			resStatus.Actuation = aStatus.Actuation
			resStatus.Strategy = aStatus.Strategy
			resStatus.Reconcile = aStatus.Reconcile

			// Update the status field to reflect source -> spec -> status,
			// not just spec -> status.
			resStatus.Status = UpdateStatusToReflectActuation(resStatus)
		}

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

// UpdateStatusToReflectActuation updates the latest computed kstatus to reflect
// the desired state in the source of truth, as much as possible, not just the
// desired state that has been persisted to the Kubernetes API.
//
// This is necessary, because kstatus only reflects whether the object status
// reflects the object spec. But in Config Sync, the desired state is actually
// in the source or truth. So if the latest desired state has not yet been
// successfully actuated (applied or deleted), then the current status is
// Unknown, because neither Kubernetes nor this ResourceGroup controller knows
// what the desired state is.
func UpdateStatusToReflectActuation(s v1alpha1.ResourceStatus) v1alpha1.Status {
	// When actuation is unknown, return the latest computed status.
	// This should only ever happen when upgrading from an old version.
	// It means the applier was last run with code that predates the addition
	// of the strategy, actuation, and reconcile fields.
	if s.Actuation == "" {
		return s.Status
	}

	// When actuation has succeeded, we know the object spec is up to date with
	// the latest source, so the latest computed status should be correct.
	if s.Actuation == v1alpha1.ActuationSucceeded {
		return s.Status
	}

	// When the latest computed status is Terminating or NotFound, keep it,
	// even if actuation is Pending, Skipped, or Failed, because Terminating and
	// NotFound are independent of the desired state in the source of truth,
	// while InProgress, Current, and Failed depend on actuation having
	// successfully updated the spec on the cluster.
	if s.Status == v1alpha1.NotFound || s.Status == v1alpha1.Terminating {
		return s.Status
	}

	// When actuation is not successful (Pending, Skipped, Failed), that means
	// the object spec has not been updated to the desired spec from source yet,
	// so the computed status can be ignored and the real status is Unknown,
	// unless the latest computed status is NotFound or Terminating.
	return v1alpha1.Unknown
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
