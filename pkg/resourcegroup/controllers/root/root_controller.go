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
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/resourcegroup"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/handler"
	resourcegroupcontroller "kpt.dev/configsync/pkg/resourcegroup/controllers/resourcegroup"
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
	KptGroup = "kpt"
)

// Reconciler reconciles a ResourceGroup object
// It only accepts the Create, Update, Delete events of ResourceGroup objects.
type Reconciler struct {
	*controllers.LoggingController

	// cfg is the rest config associated with the reconciler
	cfg *rest.Config

	// Client is to get and update ResourceGroup object.
	client client.Client

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
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = r.SetLoggerValues(ctx, "resourcegroup", req.NamespacedName)
	r.Logger(ctx).V(3).Info("Reconcile starting")

	var resgroup = &v1alpha1.ResourceGroup{}
	err := r.client.Get(ctx, req.NamespacedName, resgroup)
	if err != nil {
		if errors.IsNotFound(err) {
			// If the ResourceGroup has been deleted, update the resMap
			r.Logger(ctx).V(3).Info("Skipping update event: ResourceGroup not found")
			return r.reconcile(ctx, req.NamespacedName, []v1alpha1.ObjMetadata{}, true)
		}
		return ctrl.Result{}, err
	}

	// Skip ResourceGroup status updates if the status is disabled and has
	// already been removed.
	if resourcegroup.IsStatusDisabled(resgroup) {
		r.Logger(ctx).V(3).Info("Skipping update event: ResourceGroup status disabled")
		return r.reconcileDisabledResourceGroup(ctx, req, resgroup)
	}

	// ResourceGroup is in the process of being deleted, clean up the cache for this ResourceGroup
	if resgroup.DeletionTimestamp != nil {
		r.Logger(ctx).V(3).Info("Skipping update event: ResourceGroup being deleted")
		return r.reconcile(ctx, req.NamespacedName, []v1alpha1.ObjMetadata{}, true)
	}

	resources := make([]v1alpha1.ObjMetadata, 0, len(resgroup.Spec.Resources)+len(resgroup.Spec.Subgroups))
	resources = append(resources, resgroup.Spec.Resources...)
	resources = append(resources, v1alpha1.ToObjMetadata(resgroup.Spec.Subgroups)...)
	if result, err := r.reconcile(ctx, req.NamespacedName, resources, false); err != nil {
		return result, err
	}

	// Push an event to the ResourceGroup event channel
	r.Logger(ctx).V(3).Info("Sending update event")
	r.channel <- event.GenericEvent{Object: resgroup}

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
	if apiequality.Semantic.DeepEqual(resgroup.Status, emptyStatus) {
		return ctrl.Result{}, nil
	}
	resgroup.Status = emptyStatus
	// Use `r.Status().Update()` here instead of `r.Update()` to update only resgroup.Status.
	err := r.client.Status().Update(ctx, resgroup, client.FieldOwner(resourcegroupcontroller.FieldManager))
	if err != nil {
		return ctrl.Result{}, err
	}
	// update the resMap
	return r.reconcile(ctx, req.NamespacedName, []v1alpha1.ObjMetadata{}, true)
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
		LoggingController: controllers.NewLoggingController(logger),
		client:            mgr.GetClient(),
		cfg:               cfg,
		scheme:            mgr.GetScheme(),
		resolver:          resolver,
		resMap:            resMap,
		channel:           channel,
		watches:           watchManager,
	}

	_, err = ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ResourceGroup{}).
		Named(group+"Root").
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
