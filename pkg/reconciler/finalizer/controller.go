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

package finalizer

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Controller that watches a RootSync or RepoSync, injects a finalizer when
// deletion propagation is enabled via annotation, handles deletion propagation
// when the RSync is marked for deletion, and removes the finalizer when
// deletion propagation is complete.
//
// Use `configsync.gke.io/deletion-propagation-policy: Foreground` to enable
// deletion propagation.
//
// Use `configsync.gke.io/deletion-propagation-policy: Orphan` or remove the
// annotation to disable deletion propagation (default behavior).
//
// The `configsync.gke.io/reconciler` finalizer is used to block deletion until
// all the managed objects can be deleted.
type Controller struct {
	SyncScope declared.Scope
	SyncName  string
	Client    client.Client
	Mapper    meta.RESTMapper
	Scheme    *runtime.Scheme
	Finalizer Finalizer
}

// SetupWithManager registers the finalizer Controller with reconciler-manager.
func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	exampleObj := c.newExampleObject()
	exampleKey := client.ObjectKeyFromObject(exampleObj)

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		Named("Finalizer").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		For(exampleObj, builder.WithPredicates(
			// Only send one event for each ResourceVersion
			predicate.ResourceVersionChangedPredicate{},
			// Filter the watch down to a single object
			SingleObjectPredicate(exampleKey),
		))

	return controllerBuilder.Complete(c)
}

// newExampleObject returns new RootSync or RepoSync with name and namespace set.
func (c *Controller) newExampleObject() client.Object {
	if c.SyncScope == declared.RootReconciler {
		exampleObj := &v1beta1.RootSync{}
		exampleObj.Name = c.SyncName
		exampleObj.Namespace = configmanagement.ControllerNamespace
		return exampleObj
	}
	exampleObj := &v1beta1.RepoSync{}
	exampleObj.Name = c.SyncName
	exampleObj.Namespace = string(c.SyncScope)
	return exampleObj
}

// Reconcile responds to changes in the RootSync/RepoSync being watched.
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// Returned error triggers retry, so we rarely need to populate the result.
	var result reconcile.Result

	rs := c.newExampleObject()
	rsKey := client.ObjectKeyFromObject(rs)
	reqKey := req.NamespacedName

	// This should never happen, if the controller is configured correctly.
	if reqKey.Name != rsKey.Name || reqKey.Namespace != rsKey.Namespace {
		klog.Errorf("Controller misconfiguration: reconciler called for the wrong %T: expected %q, but got %q",
			rs, rsKey, reqKey)
		// Programmer error. Retry won't help, so don't return the error.
		return result, nil
	}

	// Get the latest RSync from the client cache
	if err := c.Client.Get(ctx, rsKey, rs); err != nil {
		if apierrors.IsNotFound(err) {
			// Do nothing. After the RSync is NotFound is too late to finalize.
			// The finalizer should have run already in OnUpdate when the
			// deletionTimestamp was added. If we tried to run the destroyer
			// now, it would probably fail, because the reconciler-manager will
			// probably have deleted the dependencies. And even if the
			// dependencies were still around, the reconciler will soon be
			// deleted by the reconciler-manager, if it hasn't already.
			return result, nil
		}
		return result, status.APIServerError(err, fmt.Sprintf("failed to get %s", objSummary(rs)))
	}

	if !rs.GetDeletionTimestamp().IsZero() {
		// Object being deleted.
		if controllerutil.ContainsFinalizer(rs, metadata.ReconcilerFinalizer) {
			if err := c.Finalizer.Finalize(ctx, rs); err != nil {
				return result, errors.Wrapf(err, "finalizing")
			}
		}
	} else {
		if err := c.reconcileFinalizer(ctx, rs); err != nil {
			return result, errors.Wrapf(err, "reconciling finalizer")
		}
	}
	return result, nil
}

// reconcileFinalizer adds or removes the `configsync.gke.io/reconciler`
// finalizer, depending on the existence and value of the
// `configsync.gke.io/deletion-propagation-policy` annotation.
func (c *Controller) reconcileFinalizer(ctx context.Context, obj client.Object) error {
	policyStr, found := obj.GetAnnotations()[metadata.DeletionPropagationPolicyAnnotationKey]
	policy := metadata.DeletionPropagationPolicy(policyStr)
	if !found {
		// Orphan is the default policy
		policy = metadata.DeletionPropagationPolicyOrphan
	}
	switch policy {
	case metadata.DeletionPropagationPolicyForeground:
		if _, err := c.Finalizer.AddFinalizer(ctx, obj); err != nil {
			return err
		}
	case metadata.DeletionPropagationPolicyOrphan:
		if _, err := c.Finalizer.RemoveFinalizer(ctx, obj); err != nil {
			return err
		}
	default:
		klog.Warningf("%T %s has an invalid value for the annotation %q: %q",
			obj, client.ObjectKeyFromObject(obj), metadata.DeletionPropagationPolicyAnnotationKey, policy)
		// User error. Retry won't help, so don't return the error.
	}
	return nil
}
