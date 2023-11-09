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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cli-utils/pkg/common"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"kpt.dev/resourcegroup/controllers/handler"
	"kpt.dev/resourcegroup/controllers/metrics"
	"kpt.dev/resourcegroup/controllers/resourcemap"
	controllerstatus "kpt.dev/resourcegroup/controllers/status"
	"kpt.dev/resourcegroup/controllers/typeresolver"
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

// contextKey is a custom type for wrapping context values to make them unique
// to this package
type contextKey string

const contextLoggerKey = contextKey("logger")

// DefaultDuration is the default throttling duration
var DefaultDuration = 30 * time.Second

// reconciler reconciles a ResourceGroup object
type reconciler struct {
	// Client is to get and update ResourceGroup object.
	client.Client

	// log is the logger of the reconciler.
	log logr.Logger

	// TODO: check if scheme is needed
	scheme *runtime.Scheme

	// resolver is the type resolver to find the server preferred
	// GVK for a GK
	resolver *typeresolver.TypeResolver

	// resMap is the resourcemap for storing the resource status and conditions.
	resMap *resourcemap.ResourceMap
}

// +kubebuilder:rbac:groups=kpt.dev,resources=resourcegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kpt.dev,resources=resourcegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

func (r *reconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.log
	ctx := context.WithValue(c, contextLoggerKey, logger)
	logger.Info("starts reconciling")
	return r.reconcileKpt(ctx, req, logger)
}

func (r *reconciler) reconcileKpt(ctx context.Context, req ctrl.Request, logger logr.Logger) (ctrl.Result, error) {
	// obtain the ResourceGroup CR
	resgroup := &v1alpha1.ResourceGroup{}
	err := r.Get(ctx, req.NamespacedName, resgroup)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// ResourceGroup is in the process of being deleted
	if resgroup.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	newStatus := r.startReconcilingStatus(resgroup.Status)
	if err := r.updateStatusKptGroup(ctx, resgroup, newStatus); err != nil {
		logger.Error(err, "failed to update")
		return ctrl.Result{Requeue: true}, err
	}

	id := getInventoryID(resgroup.Labels)
	newStatus = r.endReconcilingStatus(ctx, id, req.NamespacedName, resgroup.Spec, resgroup.Status, resgroup.Generation)
	if err := r.updateStatusKptGroup(ctx, resgroup, newStatus); err != nil {
		logger.Error(err, "failed to update")
		return ctrl.Result{Requeue: true}, err
	}

	logger.Info("finished reconciling")
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

func (r *reconciler) updateStatusKptGroup(ctx context.Context, resgroup *v1alpha1.ResourceGroup, newStatus v1alpha1.ResourceGroupStatus) error {
	newStatus.Conditions = adjustConditionOrder(newStatus.Conditions)
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if apiequality.Semantic.DeepEqual(resgroup.Status, newStatus) {
			return nil
		}
		resgroup.Status = newStatus
		// Use `r.Status().Update()` here instead of `r.Update()` to update only resgroup.Status.
		return r.Status().Update(ctx, resgroup)
	})
}

func (r *reconciler) startReconcilingStatus(status v1alpha1.ResourceGroupStatus) v1alpha1.ResourceGroupStatus {
	newStatus := v1alpha1.ResourceGroupStatus{
		ObservedGeneration: status.ObservedGeneration,
		ResourceStatuses:   status.ResourceStatuses,
		SubgroupStatuses:   status.SubgroupStatuses,
		Conditions: []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.TrueConditionStatus, StartReconciling, startReconcilingMsg),
			newStalledCondition(v1alpha1.FalseConditionStatus, "", ""),
		},
	}
	return newStatus
}

func (r *reconciler) endReconcilingStatus(
	ctx context.Context,
	id string,
	namespacedName types.NamespacedName,
	spec v1alpha1.ResourceGroupSpec,
	status v1alpha1.ResourceGroupStatus,
	generation int64,
) v1alpha1.ResourceGroupStatus {
	// reset newStatus to make sure the former setting of newStatus does not carry over
	newStatus := v1alpha1.ResourceGroupStatus{}
	startTime := time.Now()
	reconcileTimeout := getReconcileTimeOut(len(spec.Subgroups) + len(spec.Resources))

	finish := make(chan struct{})
	go func() {
		newStatus.ResourceStatuses = r.computeResourceStatuses(ctx, id, status, spec.Resources, namespacedName)
		newStatus.SubgroupStatuses = r.computeSubGroupStatuses(ctx, id, status, spec.Subgroups, namespacedName)
		close(finish)
	}()
	select {
	case <-finish:
		newStatus.ObservedGeneration = generation
		newStatus.Conditions = []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.FalseConditionStatus, FinishReconciling, finishReconcilingMsg),
			aggregateResourceStatuses(newStatus.ResourceStatuses),
		}
	case <-time.After(reconcileTimeout):
		newStatus.ObservedGeneration = status.ObservedGeneration
		newStatus.ResourceStatuses = status.ResourceStatuses
		newStatus.SubgroupStatuses = status.SubgroupStatuses
		newStatus.Conditions = []v1alpha1.Condition{
			newReconcilingCondition(v1alpha1.FalseConditionStatus, ExceedTimeout, exceedTimeoutMsg),
			newStalledCondition(v1alpha1.TrueConditionStatus, ExceedTimeout, exceedTimeoutMsg),
		}
	}

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

		cachedStatus := r.resMap.GetStatus(res)

		// Add status to cache, if not present.
		switch {
		case cachedStatus != nil:
			r.log.V(4).Info("found the cached resource status for", "namespace", res.Namespace, "name", res.Name)
			setResStatus(id, &resStatus, cachedStatus)
		default:
			resObj := new(unstructured.Unstructured)
			gvk, gvkFound := r.resolver.Resolve(schema.GroupKind(res.GroupKind))
			if !gvkFound {
				// If the resolver cache does not contain the server preferred GVK, then GVK returned
				// will be empty resulting in a GET error. An instance of this occurring is when the
				// resource type (CRD) does not exist.
				r.log.V(4).Info("unable to get object from API server to compute status as resource does not exist", "namespace", res.Namespace, "name", res.Name)
				resStatus.Status = v1alpha1.NotFound
				break
			}
			resObj.SetGroupVersionKind(gvk)
			r.log.Info("get the object from API server to compute status for", "namespace", res.Namespace, "name", res.Name)
			err := r.Get(ctx, types.NamespacedName{
				Namespace: res.Namespace,
				Name:      res.Name,
			}, resObj)
			if err != nil {
				if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
					resStatus.Status = v1alpha1.NotFound
				} else {
					resStatus.Status = v1alpha1.Unknown
				}
				r.log.V(4).Error(err, "unable to get object from API server to compute status", "namespace", res.Namespace, "name", res.Name)

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

		// Update the legacy status field based on the actuation, strategy and reconcile
		// statuses set by cli-utils. If the actuation is not successful, update the legacy
		// status field to be of unknown status.
		aStatus, exists := actuationStatuses[resStatus.ObjMetadata]
		if exists {
			resStatus.Actuation = aStatus.Actuation
			resStatus.Strategy = aStatus.Strategy
			resStatus.Reconcile = aStatus.Reconcile

			resStatus.Status = ActuationStatusToLegacy(resStatus)
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

// ActuationStatusToLegacy contains the logic/rules to convert from the actuation statuses
// to the legacy status field. If conversion is not needed, the original status field is returned
// instead.
func ActuationStatusToLegacy(s v1alpha1.ResourceStatus) v1alpha1.Status {
	if s.Status == v1alpha1.NotFound {
		return v1alpha1.NotFound
	}

	if s.Actuation != "" &&
		s.Actuation != v1alpha1.ActuationSucceeded {
		return v1alpha1.Unknown
	}

	if s.Actuation == v1alpha1.ActuationSucceeded && s.Reconcile == v1alpha1.ReconcileSucceeded {
		return v1alpha1.Current
	}
	return s.Status
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
		Client:   mgr.GetClient(),
		log:      logger,
		scheme:   mgr.GetScheme(),
		resolver: resolver,
		resMap:   resMap,
	}

	c, err := controller.New(v1alpha1.ResourceGroupKind, mgr, controller.Options{
		Reconciler: reconcile.Func(r.Reconcile),
	})

	if err != nil {
		return err
	}

	err = c.Watch(&source.Channel{Source: channel}, handler.NewThrottler(duration))
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
