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

package typeresolver

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// TypeResolver keeps the preferred GroupVersionKind for all the
// types in the cluster.
type TypeResolver struct {
	log         logr.Logger
	mu          sync.Mutex
	dc          *discovery.DiscoveryClient
	typeMapping map[schema.GroupKind]schema.GroupVersionKind
}

// Refresh refreshes the type mapping by querying the api server
func (r *TypeResolver) Refresh() {
	mapping := make(map[schema.GroupKind]schema.GroupVersionKind)
	apiResourcesList, err := discovery.ServerPreferredResources(r.dc)
	if err != nil {
		r.log.Error(err, "Unable to fetch api resources list by dynamic client")
		return
	}
	for _, resources := range apiResourcesList {
		groupversion := resources.GroupVersion
		gv, err := schema.ParseGroupVersion(groupversion)
		if err != nil {
			continue
		}
		for _, resource := range resources.APIResources {
			gk := schema.GroupKind{
				Group: gv.Group,
				Kind:  resource.Kind,
			}
			mapping[gk] = gk.WithVersion(gv.Version)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.typeMapping = mapping
}

// Resolve maps the provided GroupKind to a GroupVersionKind
func (r *TypeResolver) Resolve(gk schema.GroupKind) (schema.GroupVersionKind, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, found := r.typeMapping[gk]
	return item, found
}

// Reconcile implements reconciler.Reconciler. This function handles reconciliation
// for the type mapping.
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
func (r *TypeResolver) Reconcile(context.Context, ctrl.Request) (ctrl.Result, error) {
	logger := r.log
	logger.Info("refreshing type resolver")
	r.Refresh()
	return ctrl.Result{}, nil
}

// NewTypeResolver creates a new TypeResolver
func NewTypeResolver(mgr ctrl.Manager, logger logr.Logger) (*TypeResolver, error) {
	dc := discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig())
	r := &TypeResolver{
		log: logger,
		dc:  dc,
	}

	c, err := controller.New("TypeResolver", mgr, controller.Options{
		Reconciler: reconcile.Func(r.Reconcile),
	})

	if err != nil {
		return nil, err
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	return r, c.Watch(&source.Kind{Type: u}, &handler.EnqueueRequestForObject{})
}
