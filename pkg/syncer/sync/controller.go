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

package sync

import (
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const syncControllerName = "meta-sync-resources"

var unaryHandler = handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "item"}}}
})

// AddController adds the Sync controller to the manager.
func AddController(mgr manager.Manager, rc RestartChannel) error {
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return errors.Wrapf(err, "failed to create discoveryclient")
	}
	// Set up a meta controller that restarts GenericResource controllers when Syncs change.
	clientFactory := func() (client.Client, error) {
		cfg := mgr.GetConfig()
		mapper, err2 := apiutil.NewDynamicRESTMapper(cfg)
		if err2 != nil {
			return nil, errors.Wrapf(err2, "failed to create mapper during gc")
		}
		return client.New(cfg, client.Options{
			Scheme: scheme.Scheme,
			Mapper: mapper,
		})
	}
	reconciler, err := newMetaReconciler(mgr, dc, clientFactory, metav1.Now)
	if err != nil {
		return errors.Wrapf(err, "could not create %q reconciler", syncControllerName)
	}

	c, err := controller.New(syncControllerName, mgr, controller.Options{
		Reconciler: reconciler,
	})
	if err != nil {
		return errors.Wrapf(err, "could not create %q controller", syncControllerName)
	}

	// Watch all changes to Syncs.
	if err = c.Watch(&source.Kind{Type: &v1.Sync{}}, unaryHandler); err != nil {
		return errors.Wrapf(err, "could not watch Syncs in the %q controller", syncControllerName)
	}
	// Watch all changes to NamespaceConfigs.
	// There is a corner case, where a user creates a repo with only namespaces in it.
	// In order for the NamespaceConfig reconciler to start reconciling NamespaceConfigs,
	// a Sync needed to be created to cause us to start the NamespaceConfig controller.
	// We watch NamespaceConfigs so we can reconcile namespaces for this specific scenario.
	if err = c.Watch(&source.Kind{Type: &v1.NamespaceConfig{}}, unaryHandler); err != nil {
		return errors.Wrapf(err, "could not watch NamespaceConfigs in the %q controller", syncControllerName)
	}

	// Create a watch for forced restarts from other controllers like the CRD controller.
	managerRestartSource := &source.Channel{Source: rc}
	if err = c.Watch(managerRestartSource, &handler.EnqueueRequestForObject{}); err != nil {
		return errors.Wrapf(err, "could not watch manager initialization errors in the %q controller", syncControllerName)
	}

	return nil
}
