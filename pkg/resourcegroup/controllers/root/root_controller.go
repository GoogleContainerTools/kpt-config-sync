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

package root

import (
	"context"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/handler"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcegroup"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/typeresolver"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/watch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

//nolint:revive // TODO: add comments for public constants and enable linting
const (
	KptGroup           = "kpt"
	DisableStatusKey   = "configsync.gke.io/status"
	DisableStatusValue = "disabled"
)

// contextKey is a custom type for wrapping context values to make them unique
// to this package
type contextKey string

const contextLoggerKey = contextKey("logger")

// Reconciler reconciles a ResourceGroup object
// It only accepts the Create, Update, Delete events of ResourceGroup objects.
type Reconciler struct {
	// cfg is the rest config associated with the reconciler
	cfg *rest.Config

	// Client is to get and update ResourceGroup object.
	client.Client

	// log is the logger of the reconciler.
	log logr.Logger

	// TODO: check if scheme is needed
	scheme *runtime.Scheme

	// resolver is the type resolver to find the server preferred
	// GVK for a GK
	resolver *typeresolver.TypeResolver

	// resMap is an instance of resourcemap which contains
	// the mapping from the existing ResourceGroups to their underlying resources
	// and reverse mapping.
	resMap *resourcemap.ResourceMap

	// channel accepts the events that are from
	// different watchers for GVKs.
	channel chan event.GenericEvent

	// watches contains the mapping from GVK to their watchers.
	watches *watch.Manager
}

// Reconcile implements reconcile.Reconciler. This function handles reconciliation
// for ResourceGroup objects.
// +kubebuilder:rbac:groups=kpt.dev,resources=resourcegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kpt.dev,resources=resourcegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (r *Reconciler) Reconcile(rootCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.log
	ctx := context.WithValue(rootCtx, contextLoggerKey, logger)
	logger.Info("starts reconciling")
	return r.reconcileKptGroup(ctx, logger, req)
}

func (r *Reconciler) reconcileKptGroup(ctx context.Context, logger logr.Logger, req ctrl.Request) (ctrl.Result, error) {
	var resgroup = &v1alpha1.ResourceGroup{}
	err := r.Get(ctx, req.NamespacedName, resgroup)
	if err != nil {
		if errors.IsNotFound(err) {
			// If the ResourceGroup has been deleted, update the resMap
			return r.reconcile(ctx, req.NamespacedName, []v1alpha1.ObjMetadata{}, true)
		}
		return ctrl.Result{}, err
	}

	// ResourceGroup CR is created from ConfigSync and set to disable the status
	if isStatusDisabled(resgroup) {
		return r.reconcileDisabledResourceGroup(ctx, req, resgroup)
	}

	// ResourceGroup is in the process of being deleted, clean up the cache for this ResourceGroup
	if resgroup.DeletionTimestamp != nil {
		return r.reconcile(ctx, req.NamespacedName, []v1alpha1.ObjMetadata{}, true)
	}

	resources := make([]v1alpha1.ObjMetadata, 0, len(resgroup.Spec.Resources)+len(resgroup.Spec.Subgroups))
	resources = append(resources, resgroup.Spec.Resources...)
	resources = append(resources, v1alpha1.ToObjMetadata(resgroup.Spec.Subgroups)...)
	if result, err := r.reconcile(ctx, req.NamespacedName, resources, false); err != nil {
		return result, err
	}

	// Push an event to the ResourceGroup event channel
	r.channel <- event.GenericEvent{Object: resgroup}
	logger.Info("finished reconciling")

	return ctrl.Result{}, nil
}

func (r *Reconciler) reconcile(ctx context.Context, name types.NamespacedName,
	resources []v1alpha1.ObjMetadata, deleteRG bool) (ctrl.Result, error) {
	gks := r.resMap.Reconcile(ctx, name, resources, deleteRG)
	if err := r.updateWatches(ctx, gks); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// updateWatches add new watches for GVKs when resgroup includes the first GVK resource(s),
// and delete watches for GVKs when no resource group includes GVK resources any more.
func (r *Reconciler) updateWatches(ctx context.Context, gks []schema.GroupKind) error {
	gvkMap := map[schema.GroupVersionKind]struct{}{}
	for _, gk := range gks {
		gvk, found := r.resolver.Resolve(gk)
		if found {
			gvkMap[gvk] = struct{}{}
		}
	}
	return r.watches.UpdateWatches(ctx, gvkMap)
}

func (r *Reconciler) reconcileDisabledResourceGroup(ctx context.Context, req ctrl.Request, resgroup *v1alpha1.ResourceGroup) (ctrl.Result, error) {
	// clean the existing .status field
	emptyStatus := v1alpha1.ResourceGroupStatus{}
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if apiequality.Semantic.DeepEqual(resgroup.Status, emptyStatus) {
			return nil
		}
		resgroup.Status = emptyStatus
		// Use `r.Status().Update()` here instead of `r.Update()` to update only resgroup.Status.
		return r.Status().Update(ctx, resgroup, client.FieldOwner(resourcegroup.FieldManager))
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	// update the resMap
	return r.reconcile(ctx, req.NamespacedName, []v1alpha1.ObjMetadata{}, true)
}

func isStatusDisabled(resgroup *v1alpha1.ResourceGroup) bool {
	annotations := resgroup.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, found := annotations[DisableStatusKey]
	return found && val == DisableStatusValue
}

// NewController creates a new Reconciler and registers it with the provided manager
func NewController(mgr manager.Manager, channel chan event.GenericEvent,
	logger logr.Logger, resolver *typeresolver.TypeResolver, group string, resMap *resourcemap.ResourceMap) error {
	cfg := mgr.GetConfig()
	httpClient := mgr.GetHTTPClient()
	watchOption, err := watch.DefaultOptions(cfg, httpClient)
	if err != nil {
		return err
	}
	watchManager, err := watch.NewManager(cfg, httpClient, resMap, channel, watchOption)
	if err != nil {
		return err
	}
	// Create the reconciler
	reconciler := &Reconciler{
		Client:   mgr.GetClient(),
		cfg:      cfg,
		log:      logger,
		scheme:   mgr.GetScheme(),
		resolver: resolver,
		resMap:   resMap,
		channel:  channel,
		watches:  watchManager,
	}

	_, err = ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ResourceGroup{}).
		Named(group+"Root").
		WithEventFilter(ResourceGroupPredicate{}).
		// skip the Generic events
		WithEventFilter(NoGenericEventPredicate{}).
		// only reconcile resource groups owned by Config Sync
		WithEventFilter(OwnedByConfigSyncPredicate{}).
		Watches(&apiextensionsv1.CustomResourceDefinition{}, &handler.CRDEventHandler{
			Mapping: resMap,
			Channel: channel,
			Log:     logger,
		}).
		Build(reconciler)

	if err != nil {
		return err
	}
	return nil
}

// NoGenericEventPredicate skips all the generic events
type NoGenericEventPredicate struct {
	predicate.Funcs
}

// Generic skips all generic events
func (NoGenericEventPredicate) Generic(event.GenericEvent) bool {
	return false
}

// ResourceGroupPredicate skips events where the new status is not changed by the old status.
type ResourceGroupPredicate struct {
	predicate.Funcs
}

// Update ensures only select ResourceGroup updates causes a reconciliation loop. This prevents
// the controller from generating an infinite loop of reconcilers.
func (ResourceGroupPredicate) Update(e event.UpdateEvent) bool {
	// Only allow ResourceGroup CR events.
	rgNew, ok := e.ObjectNew.(*v1alpha1.ResourceGroup)
	if !ok {
		return false
	}
	rgOld, ok := e.ObjectOld.(*v1alpha1.ResourceGroup)
	if !ok {
		return false
	}

	// Reconcile if the generation (spec) is updated, or the previous reconcile stalled and needs to be reconciled.
	if rgNew.Generation != rgOld.Generation || isConditionTrue(v1alpha1.Stalled, rgNew.Status.Conditions) {
		return true
	}

	// If a ResourceGroup has the status disabled annotation and it status field
	// is not empty, it should trigger a reconcile to remove reset the status.
	if isStatusDisabled(rgNew) {
		return rgNew.Status.Conditions != nil
	}

	// If a current reconcile loop is already acting on the ResourceGroup CR, it
	// should not trigger another reconcile.
	if isConditionTrue(v1alpha1.Reconciling, rgNew.Status.Conditions) {
		return false
	}

	// Check if the status field needs to be updated since the actuation field was externally updated.
	return statusNeedsUpdate(rgNew.Status.ResourceStatuses)
}

// statusNeedsUpdate checks each resource status to ensure the legacy status field
// aligns with the new actuation/reconcile status fields.
func statusNeedsUpdate(statuses []v1alpha1.ResourceStatus) bool {
	for _, s := range statuses {
		if resourcegroup.ActuationStatusToLegacy(s) != s.Status {
			return true
		}
	}

	return false
}

// isConditionTrue scans through a slice of conditions and returns whether the wanted condition
// type is true or false. Defaults to false if the condition type is not found.
func isConditionTrue(cType v1alpha1.ConditionType, conditions []v1alpha1.Condition) bool {
	for _, c := range conditions {
		if c.Type == cType {
			return c.Status == v1alpha1.TrueConditionStatus
		}
	}

	return false
}

// OwnedByConfigSyncPredicate filters events for objects that have the label "app.kubernetes.io/managed-by=configmanagement.gke.io"
type OwnedByConfigSyncPredicate struct{}

// Create implements predicate.Predicate
func (OwnedByConfigSyncPredicate) Create(e event.CreateEvent) bool {
	return !isResourceGroup(e.Object) || isOwnedByConfigSync(e.Object)
}

// Delete implements predicate.Predicate
func (OwnedByConfigSyncPredicate) Delete(e event.DeleteEvent) bool {
	return !isResourceGroup(e.Object) || isOwnedByConfigSync(e.Object)
}

// Update implements predicate.Predicate
func (OwnedByConfigSyncPredicate) Update(e event.UpdateEvent) bool {
	return !isResourceGroup(e.ObjectOld) || !isResourceGroup(e.ObjectNew) || isOwnedByConfigSync(e.ObjectOld) || isOwnedByConfigSync(e.ObjectNew)
}

// Generic implements predicate.Predicate
func (OwnedByConfigSyncPredicate) Generic(e event.GenericEvent) bool {
	return !isResourceGroup(e.Object) || isOwnedByConfigSync(e.Object)
}

func isResourceGroup(o client.Object) bool {
	return o.GetObjectKind().GroupVersionKind().Group == v1alpha1.SchemeGroupVersion.Group &&
		o.GetObjectKind().GroupVersionKind().Kind == v1alpha1.ResourceGroupKind
}

func isOwnedByConfigSync(o client.Object) bool {
	labels := o.GetLabels()
	owner, ok := labels[metadata.ManagedByKey]
	return ok && owner == metadata.ManagedByValue
}
