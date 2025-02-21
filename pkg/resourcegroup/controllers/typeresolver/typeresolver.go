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
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TypeResolver keeps the preferred GroupVersionKind for all the
// types in the cluster.
type TypeResolver struct {
	*controllers.LoggingController

	mu          sync.Mutex
	dc          discovery.DiscoveryInterface
	typeMapping map[schema.GroupKind]schema.GroupVersionKind
}

// Refresh refreshes the type mapping by querying the api server
func (r *TypeResolver) Refresh(ctx context.Context) error {
	mapping := make(map[schema.GroupKind]schema.GroupVersionKind)
	apiResourcesList, err := discovery.ServerPreferredResources(r.dc)
	if err != nil {
		if isStaleGroupDiscoveryError(err) {
			// Log and continue, using the cached APIs.
			// Stale groups means one or more of the APIService backends are
			// unhealthy. This is especially common for metrics providers.
			// If the API provider of a managed resource remains unhealthy, it
			// will be surfaced as a resource-specific API error later, when
			// watched or listed, which are retried and won't prevent updating
			// the status of other managed resources.
			r.Logger(ctx).Error(err, "Failed to discover API resources; using cached results")
		} else {
			// Return error, where it will be logged by the controller manager and retried
			return fmt.Errorf("discovery of API resources failed: %w", err)
		}
	}
	for _, resources := range apiResourcesList {
		gv, err := schema.ParseGroupVersion(resources.GroupVersion)
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
	return nil
}

// isStaleGroupDiscoveryError returns true if the error is a
// ErrGroupDiscoveryFailed and all the errors it wraps are
// StaleGroupVersionError.
//
// Clusters with Aggregated Discovery enabled return this error when one
// or more of the APIService backends fails to respond. However, the
// cached APIs are still returned, with unhealthy groups marked as stale.
// So if this returns true, it should be safe to use the cached APIs.
func isStaleGroupDiscoveryError(err error) bool {
	if err == nil {
		return false
	}
	var groupDiscoErr *discovery.ErrGroupDiscoveryFailed
	if !errors.As(err, &groupDiscoErr) {
		return false
	}
	if len(groupDiscoErr.Groups) == 0 {
		// unlikely - improper use of ErrGroupDiscoveryFailed
		return false
	}
	for _, gvErr := range groupDiscoErr.Groups {
		var staleErr discovery.StaleGroupVersionError
		if !errors.As(gvErr, &staleErr) {
			// At least one of the group discovery errors is NOT a stale error
			return false
		}
	}
	// All errors are StaleGroupVersionErrors
	return true
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
func (r *TypeResolver) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	r.Logger(ctx).V(3).Info("Reconcile starting")
	return ctrl.Result{}, r.Refresh(ctx)
}

// NewTypeResolver constructs a new TypeResolver
func NewTypeResolver(dc discovery.DiscoveryInterface, logger logr.Logger) *TypeResolver {
	return &TypeResolver{
		LoggingController: controllers.NewLoggingController(logger),
		dc:                dc,
	}
}

// ForManager creates a new TypeResolver and registers it with the controller
// manager.
func ForManager(mgr ctrl.Manager, logger logr.Logger) (*TypeResolver, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}
	reconciler := NewTypeResolver(dc, logger)
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	_, err = ctrl.NewControllerManagedBy(mgr).
		Named("TypeResolver").
		For(uObj).
		Build(reconciler)
	return reconciler, err
}
