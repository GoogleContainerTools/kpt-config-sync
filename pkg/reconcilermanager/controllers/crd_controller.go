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

package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/util/customresource"
	utilwatch "kpt.dev/configsync/pkg/util/watch"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// CRDReconcileFunc is called by the CRDMetaController to handle CRD updates.
type CRDReconcileFunc func(context.Context, *apiextensionsv1.CustomResourceDefinition) error

// CRDController keeps track of CRDReconcileFuncs and calls them when the
// CRD changes. Only one reconciler is allowed per GroupKind.
type CRDController struct {
	lock        sync.RWMutex
	reconcilers map[schema.GroupKind]CRDReconcileFunc
}

// SetReconciler sets the reconciler for the specified CRD.
// The reconciler will be called when the CRD becomes established.
// If the reconciler errors, it will be retried with backoff until success.
// A new reconciler will replace any old reconciler set with the same GroupKind.
func (s *CRDController) SetReconciler(gk schema.GroupKind, crdHandler CRDReconcileFunc) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.reconcilers == nil {
		s.reconcilers = make(map[schema.GroupKind]CRDReconcileFunc)
	}
	s.reconcilers[gk] = crdHandler
}

// DeleteReconciler removes the reconciler for the specified CRD.
func (s *CRDController) DeleteReconciler(gk schema.GroupKind) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.reconcilers != nil {
		delete(s.reconcilers, gk)
	}
}

// Reconcile calls the CRDReconcileFunc registered for this CRD by GroupKind.
func (s *CRDController) Reconcile(ctx context.Context, gk schema.GroupKind, crd *apiextensionsv1.CustomResourceDefinition) error {
	// Let getReconciler handle locking to prevent deadlock in case the
	// reconciler calls SetReconciler/DeleteReconciler.
	reconciler := s.getReconciler(gk)
	if reconciler == nil {
		// No reconciler for this CRD
		return nil
	}
	if err := reconciler(ctx, crd); err != nil {
		return fmt.Errorf("reconciling CRD: %s: %w", gk, err)
	}
	return nil
}

func (s *CRDController) getReconciler(gk schema.GroupKind) CRDReconcileFunc {
	s.lock.RLock()
	defer s.lock.RUnlock()

	reconciler, found := s.reconcilers[gk]
	if !found {
		return nil
	}
	return reconciler
}

// CRDMetaController watches CRDs and delegates reconciliation to a CRDControllerManager.
type CRDMetaController struct {
	loggingController
	cache             cache.Cache
	mapper            utilwatch.ResettableRESTMapper
	delegate          *CRDController
	observedResources map[schema.GroupResource]schema.GroupKind
}

var _ reconcile.Reconciler = &CRDMetaController{}

// NewCRDMetaController constructs a new CRDMetaController.
func NewCRDMetaController(
	delegate *CRDController,
	cache cache.Cache,
	mapper utilwatch.ResettableRESTMapper,
	log logr.Logger,
) *CRDMetaController {
	return &CRDMetaController{
		loggingController: loggingController{
			log: log,
		},
		cache:             cache,
		mapper:            mapper,
		delegate:          delegate,
		observedResources: make(map[schema.GroupResource]schema.GroupKind),
	}
}

// Reconcile checks if the CRD exists and delegates to the CRDController to
// reconcile the update.
//
// Reconcile also handles auto-discovery and auto-invalidation of custom
// resources by calling Reset on the RESTMapper, as needed.
func (r *CRDMetaController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	crdName := req.Name
	ctx = r.setLoggerValues(ctx, "crd", crdName)

	// Established if CRD exists and .status.conditions[type="Established"].status = "True"
	var kind schema.GroupKind
	crdObj := &apiextensionsv1.CustomResourceDefinition{}
	if err := r.cache.Get(ctx, req.NamespacedName, crdObj); err != nil {
		switch {
		// Should never run into NoMatchFound, since CRD is a built-in resource.
		case apierrors.IsNotFound(err):
			// Lookup last known GroupKind for the CRD name
			resource := schema.ParseGroupResource(crdName)
			var found bool
			kind, found = r.observedResources[resource]
			if !found {
				// No retry possible
				return reconcile.Result{}, reconcile.TerminalError(
					fmt.Errorf("failed to handle CRD update for deleted resource: %w",
						&meta.NoResourceMatchError{PartialResource: resource.WithVersion("")}))
			}
			crdObj = nil
		default:
			// Retry with backoff
			return reconcile.Result{},
				fmt.Errorf("getting CRD from cache: %s: %w", crdName, err)
		}
	} else {
		kind = schema.GroupKind{
			Group: crdObj.Spec.Group,
			Kind:  crdObj.Spec.Names.Kind,
		}
		resource := schema.GroupResource{
			Group:    crdObj.Spec.Group,
			Resource: crdObj.Spec.Names.Plural,
		}
		// Cache last known mapping from GroupResource to GroupKind.
		// This lets us lookup the GroupKind using the CRD name.
		r.observedResources[resource] = kind
	}

	r.logger(ctx).Info("CRDMetaController handling CRD status update", "name", crdName)

	if customresource.IsEstablished(crdObj) {
		if err := discoverResourceForKind(r.mapper, kind); err != nil {
			// Retry with backoff
			return reconcile.Result{}, err
		}
	} else {
		if err := forgetResourceForKind(r.mapper, kind); err != nil {
			// Retry with backoff
			return reconcile.Result{}, err
		}
	}

	if err := r.delegate.Reconcile(ctx, kind, crdObj); err != nil {
		// Retry with backoff
		return reconcile.Result{}, err
	}

	r.logger(ctx).V(3).Info("CRDMetaController handled CRD status update", "name", crdName)

	return reconcile.Result{}, nil
}

// Register the CRDMetaController with the ReconcilerManager.
func (r *CRDMetaController) Register(mgr controllerruntime.Manager) error {
	return controllerruntime.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{}).
		Complete(r)
}

// discoverResourceForKind resets the RESTMapper if needed, to discover the
// resource that maps to the specified kind.
func discoverResourceForKind(mapper utilwatch.ResettableRESTMapper, gk schema.GroupKind) error {
	if _, err := mapper.RESTMapping(gk); err != nil {
		if meta.IsNoMatchError(err) {
			klog.Infof("Remediator resetting RESTMapper to discover resource: %v", gk)
			if err := mapper.Reset(); err != nil {
				return fmt.Errorf("remediator failed to reset RESTMapper: %w", err)
			}
		} else {
			return fmt.Errorf("remediator failed to map kind to resource: %w", err)
		}
	}
	// Else, mapper already up to date
	return nil
}

// forgetResourceForKind resets the RESTMapper if needed, to forget the resource
// that maps to the specified kind.
func forgetResourceForKind(mapper utilwatch.ResettableRESTMapper, gk schema.GroupKind) error {
	if _, err := mapper.RESTMapping(gk); err != nil {
		if !meta.IsNoMatchError(err) {
			return fmt.Errorf("remediator failed to map kind to resource: %w", err)
		}
		// Else, mapper already up to date
	} else {
		klog.Infof("Remediator resetting RESTMapper to forget resource: %v", gk)
		if err := mapper.Reset(); err != nil {
			return fmt.Errorf("remediator failed to reset RESTMapper: %w", err)
		}
	}
	return nil
}
