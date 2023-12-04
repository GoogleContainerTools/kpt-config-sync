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
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// CRDHandler is called by the CRDReconciler to handle establishment of a CRD.
type CRDHandler func() error

var _ reconcile.Reconciler = &CRDReconciler{}

// CRDReconciler watches CRDs and calls handlers once they are established.
type CRDReconciler struct {
	loggingController

	registerLock sync.Mutex
	handlers     map[string]CRDHandler
	handledCRDs  map[string]struct{}
}

// NewCRDReconciler constructs a new CRDReconciler.
func NewCRDReconciler(log logr.Logger) *CRDReconciler {
	return &CRDReconciler{
		loggingController: loggingController{
			log: log,
		},
		handlers:    make(map[string]CRDHandler),
		handledCRDs: make(map[string]struct{}),
	}
}

// SetCRDHandler adds an handler for the specified CRD.
// The handler will be called when the CRD becomes established.
// If the handler errors, it will be retried with backoff until success.
// One the handler succeeds, it will not be called again, unless SetCRDHandler
// is called again.
func (r *CRDReconciler) SetCRDHandler(crdName string, crdHandler CRDHandler) {
	r.registerLock.Lock()
	defer r.registerLock.Unlock()

	r.handlers[crdName] = crdHandler
	delete(r.handledCRDs, crdName)
}

// Reconcile the otel ConfigMap and update the Deployment annotation.
func (r *CRDReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	crdName := req.Name
	ctx = r.setLoggerValues(ctx, "crd", crdName)

	r.registerLock.Lock()
	defer r.registerLock.Unlock()

	handler, found := r.handlers[crdName]
	if !found {
		// No handler for this CRD
		return reconcile.Result{}, nil
	}
	if _, handled := r.handledCRDs[crdName]; handled {
		// Already handled
		return reconcile.Result{}, nil
	}

	r.logger(ctx).V(3).Info("reconciling CRD", "crd", crdName)

	if err := handler(); err != nil {
		// Retry with backoff
		return reconcile.Result{},
			fmt.Errorf("reconciling CRD %s: %w", crdName, err)
	}
	// Mark CRD as handled
	r.handledCRDs[crdName] = struct{}{}

	r.logger(ctx).V(3).Info("reconciling CRD successful", "crd", crdName)

	return controllerruntime.Result{}, nil
}

// Register the CRD controller with reconciler-manager.
func (r *CRDReconciler) Register(mgr controllerruntime.Manager) error {
	return controllerruntime.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{},
			builder.WithPredicates(
				ignoreDeletesPredicate(),
				crdIsEstablishedPredicate())).
		Complete(r)
}

// ignoreDeletesPredicate returns a predicate that handles CREATE, UPDATE, and
// GENERIC events, but not DELETE events.
func ignoreDeletesPredicate() predicate.Predicate {
	return predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
}

// crdIsEstablishedPredicate returns a predicate that only processes events for
// established CRDs.
func crdIsEstablishedPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition); ok {
			return crdIsEstablished(crd)
		}
		return false
	})
}

// crdIsEstablished returns true if the given CRD is established on the cluster,
// which indicates if discovery knows about it yet. For more info see
// https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/#create-a-customresourcedefinition
func crdIsEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == v1.Established && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
