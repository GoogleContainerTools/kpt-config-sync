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

package policycontroller

import (
	"context"
	"reflect"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/policycontroller/constraint"
	"kpt.dev/configsync/pkg/policycontroller/constrainttemplate"
	"kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// crdReconciler handles CRD reconcile events and also implements constraint
// Watcher interface.
type crdReconciler struct {
	client client.Client
	// thr is a throttler which limits the frequency of restarts for a
	// RestartableManager
	thr *throttler
	// crdKinds is a map of CRD name to the kind of resource it defines.
	crdKinds map[string]schema.GroupVersionKind
	// constraintKinds is a map of resource kind to boolean indicator if it is
	// established (aka discovery knows about).
	constraintKinds map[schema.GroupVersionKind]bool
}

// newReconciler returns a crdReconciler that able to restart the given Manager
// whenever the set of watched Constraint kinds changes.
func newReconciler(ctx context.Context, mgr manager.Manager) (*crdReconciler, error) {
	rm, err := watch.NewManager(mgr, &builder{})
	if err != nil {
		return nil, err
	}

	thr := &throttler{make(chan map[schema.GroupVersionKind]bool)}
	go thr.start(ctx, rm)

	return &crdReconciler{
		client:          mgr.GetClient(),
		thr:             thr,
		crdKinds:        map[string]schema.GroupVersionKind{},
		constraintKinds: map[schema.GroupVersionKind]bool{},
	}, nil
}

// Reconcile handles Requests from the PolicyController CRD controller. This may
// update the reconciler's map of established kinds. If this results in a net
// change to which constraint kinds are both watched and established, then the
// RestartableManager will be restarted.
func (c *crdReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.client.Get(ctx, request.NamespacedName, crd); err != nil {
		if !errors.IsNotFound(err) {
			klog.Errorf("Error getting CustomResourceDefinition %q: %v", request.NamespacedName, err)
			return reconcile.Result{}, err
		}

		klog.Infof("CustomResourceDefinition %q was deleted", request.NamespacedName)
		if kind, ok := c.crdKinds[request.NamespacedName.String()]; ok {
			delete(c.constraintKinds, kind)
			delete(c.crdKinds, request.NamespacedName.String())
			c.thr.updateGVKs(c.establishedConstraints())
		}
		return reconcile.Result{}, nil
	}

	var gvk schema.GroupVersionKind
	if constrainttemplate.MatchesGK(crd) {
		gvk = constrainttemplate.GVK
	} else if constraint.MatchesGroup(crd) {
		gvk = constraint.GVK(crd.Spec.Names.Kind)
	} else {
		// If it's not a constraint CRD or the Gatekeeper ConstraintTemplate CRD, we
		// don't care about it.
		return reconcile.Result{}, nil
	}

	c.crdKinds[request.NamespacedName.String()] = gvk
	c.constraintKinds[gvk] = isEstablished(crd)
	c.thr.updateGVKs(c.establishedConstraints())
	return reconcile.Result{}, nil
}

// isEstablished returns true if the given CRD is established on the cluster,
// which indicates if discovery knows about it yet. For more info see
// https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/#create-a-customresourcedefinition
func isEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established {
			return condition.Status == apiextensionsv1.ConditionTrue
		}
	}
	return false
}

// establishedConstraints returns a map of GVKs for all constraints that have a
// corresponding CRD which is established on the cluster.
func (c *crdReconciler) establishedConstraints() map[schema.GroupVersionKind]bool {
	gvks := map[schema.GroupVersionKind]bool{}
	for gvk, established := range c.constraintKinds {
		if established {
			gvks[gvk] = true
		}
	}
	return gvks
}

// throttler limits the frequency of calls to RestartableManager.Restart().
type throttler struct {
	input chan map[schema.GroupVersionKind]bool
}

func (t *throttler) updateGVKs(gvks map[schema.GroupVersionKind]bool) {
	t.input <- gvks
}

func (t *throttler) start(ctx context.Context, mgr watch.RestartableManager) {
	//TODO: Add unit tests for throttler logic.
	var lastGVKs map[schema.GroupVersionKind]bool
	var gvks map[schema.GroupVersionKind]bool
	var dirty bool
	restartPeriod := 3 * time.Second
	restartTimer := time.NewTimer(restartPeriod)
	defer restartTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case gvks = <-t.input:
			dirty = !reflect.DeepEqual(lastGVKs, gvks)
		case <-restartTimer.C:
			if dirty {
				klog.Infof("Restarting manager with GVKs: %v", gvks)
				if _, err := mgr.Restart(gvks, false); err != nil {
					klog.Errorf("Failed to restart submanager: %v", err)
				} else {
					lastGVKs = gvks
					dirty = false
				}
			}
			restartTimer.Reset(restartPeriod)
		}
	}
}
