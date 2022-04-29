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

package controller

import (
	"kpt.dev/configsync/pkg/syncer/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	genericreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	k8scontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const clusterConfigControllerName = "clusterconfig-resources"

// AddClusterConfig adds ClusterConfig sync controllers to the Manager.
func AddClusterConfig(mgr manager.Manager, decoder decode.Decoder,
	resourceTypes map[schema.GroupVersionKind]client.Object, mgrInitTime metav1.Time) error {
	genericClient := syncerclient.New(mgr.GetClient(), metrics.APICallDuration)
	applier, err := genericreconcile.NewApplier(mgr.GetConfig(), genericClient)
	if err != nil {
		return err
	}

	cpc, err := k8scontroller.New(clusterConfigControllerName, mgr, k8scontroller.Options{
		Reconciler: genericreconcile.NewClusterConfigReconciler(
			syncerclient.New(mgr.GetClient(), metrics.APICallDuration),
			applier,
			mgr.GetCache(),
			&cancelFilteringRecorder{mgr.GetEventRecorderFor(clusterConfigControllerName)},
			decoder,
			metav1.Now,
			extractGVKs(resourceTypes),
			mgrInitTime,
		),
	})
	if err != nil {
		return errors.Wrapf(err, "could not create %q controller", clusterConfigControllerName)
	}
	if err = cpc.Watch(&source.Kind{Type: &v1.ClusterConfig{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return errors.Wrapf(err, "could not watch ClusterConfigs in the %q controller", clusterConfigControllerName)
	}

	mapToClusterConfig := handler.EnqueueRequestsFromMapFunc(genericResourceToClusterConfig)
	// Set up a watch on all cluster-scoped resources defined in Syncs.
	// Look up the corresponding ClusterConfig for the changed resources.
	for gvk, t := range resourceTypes {
		if err := cpc.Watch(&source.Kind{Type: t}, mapToClusterConfig); err != nil {
			return errors.Wrapf(err, "could not watch %q in the %q controller", gvk, clusterConfigControllerName)
		}
	}
	return nil
}

// genericResourceToClusterConfig maps generic resources being watched,
// to reconciliation requests for the ClusterConfig potentially managing them.
func genericResourceToClusterConfig(_ client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			// There is only one ClusterConfig potentially managing generic resources.
			Name: v1.ClusterConfigName,
		},
	}}
}
