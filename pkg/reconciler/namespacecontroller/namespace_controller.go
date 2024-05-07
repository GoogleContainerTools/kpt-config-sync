// Copyright 2023 Google LLC
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

package namespacecontroller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// NamespaceController is a controller that watches for Namespace events and
// sets a flag to indicate whether the Reconciler thread needs to reconcile for
// this namespace event.
type NamespaceController struct {
	client client.Client
	state  *State
}

// New instantiates the namespace controller.
func New(cl client.Client, state *State) *NamespaceController {
	return &NamespaceController{
		client: cl,
		state:  state,
	}
}

// Reconcile processes the Namespace request to set the flag to indicate whether
// a new reconciliation needs to be executed by the Reconciler thread.
func (nc *NamespaceController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ns := &corev1.Namespace{}
	if err := nc.client.Get(ctx, req.NamespacedName, ns); err != nil {
		if apierrors.IsNotFound(err) {
			if nc.state.isSelectedNamespace(req.Name) {
				klog.Infof("A previously selected Namespace %s is deleted", req.Name)
				nc.state.setSyncPending()
			}
			// Ignore the event of the non-selected namespaces
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, status.APIServerError(err, "failed to get Namespace "+req.Name)
	}

	if nc.state.matchChanged(ns) {
		// The Namespace is either previously selected but no longer selected, or it
		// is not selected but its labels match at least one NamespaceSelector's labels.
		klog.Infof("The Namespace %s is either selected or unselected", req.Name)
		nc.state.setSyncPending()
	}
	// Ignore the event of the non-selected and non-matching namespaces
	return ctrl.Result{}, nil
}

// SetupWithManager registers the Namespace Controller.
func (nc *NamespaceController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("NamespaceController").
		Watches(&corev1.Namespace{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(UnmanagedNamespacePredicate{})).
		Complete(nc)

}
