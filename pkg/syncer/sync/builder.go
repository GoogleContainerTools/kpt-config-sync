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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/syncer/controller"
	"kpt.dev/configsync/pkg/syncer/decode"
	syncerscheme "kpt.dev/configsync/pkg/syncer/scheme"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// syncAwareBuilder creates controllers for managing resources with sync enabled.
type syncAwareBuilder struct {
	scoper discovery.Scoper
}

var _ watch.ControllerBuilder = &syncAwareBuilder{}

// newSyncAwareBuilder returns a new syncAwareBuilder.
func newSyncAwareBuilder() *syncAwareBuilder {
	return &syncAwareBuilder{discovery.Scoper{}}
}

// updateScheme updates the scheme with resources declared in Syncs.
// This is needed to generate informers/listers for resources that are sync enabled.
func (r *syncAwareBuilder) updateScheme(scheme *runtime.Scheme, gvks map[schema.GroupVersionKind]bool) error {
	if err := v1.AddToScheme(scheme); err != nil {
		return err
	}
	syncerscheme.AddToSchemeAsUnstructured(scheme, gvks)
	return nil
}

// StartControllers starts all the controllers watching sync-enabled resources.
func (r *syncAwareBuilder) StartControllers(mgr manager.Manager, gvks map[schema.GroupVersionKind]bool, mgrInitTime metav1.Time) error {
	if err := r.updateScheme(mgr.GetScheme(), gvks); err != nil {
		return errors.Wrap(err, "could not update the scheme")
	}

	namespace, cluster, err := syncerscheme.ResourceScopes(gvks, mgr.GetScheme(), r.scoper)
	if err != nil {
		return errors.Wrap(err, "could not get resource scope information from discovery API")
	}

	decoder := decode.NewGenericResourceDecoder(mgr.GetScheme())
	if err := controller.AddNamespaceConfig(mgr, decoder, namespace, mgrInitTime); err != nil {
		return errors.Wrap(err, "could not create NamespaceConfig controller")
	}
	if err := controller.AddClusterConfig(mgr, decoder, cluster, mgrInitTime); err != nil {
		return errors.Wrap(err, "could not create ClusterConfig controller")
	}

	return nil
}
