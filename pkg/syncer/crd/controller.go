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

package crd

import (
	"github.com/pkg/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/syncer/metrics"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/sync"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8scontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const crdControllerName = "crd-resources"

// AddCRDController adds the CRD controller to the Manager.
func AddCRDController(mgr manager.Manager, signal sync.RestartSignal) error {
	if err := apiextensionsv1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	resourceClient := syncerclient.New(mgr.GetClient(), metrics.APICallDuration)
	applier, err := syncerreconcile.NewApplier(mgr.GetConfig(), resourceClient)
	if err != nil {
		return err
	}

	reconciler := newReconciler(
		resourceClient,
		applier,
		mgr.GetCache(),
		mgr.GetEventRecorderFor(crdControllerName),
		decode.NewGenericResourceDecoder(mgr.GetScheme()),
		metav1.Now,
		signal,
	)

	cpc, err := k8scontroller.New(crdControllerName, mgr, k8scontroller.Options{
		Reconciler: reconciler,
	})
	if err != nil {
		return errors.Wrapf(err, "could not create %q controller", crdControllerName)
	}

	if err = cpc.Watch(&source.Kind{Type: &v1.ClusterConfig{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return errors.Wrapf(err, "could not watch ClusterConfigs in the %q controller", crdControllerName)
	}

	mapToClusterConfig := handler.EnqueueRequestsFromMapFunc(crdToClusterConfig)
	if err = cpc.Watch(&source.Kind{Type: &apiextensionsv1.CustomResourceDefinition{}}, mapToClusterConfig); err != nil {
		return errors.Wrapf(err, "could not watch CustomResourceDefinitions in the %q controller", crdControllerName)
	}

	return nil
}

// genericResourceToClusterConfig maps generic resources being watched,
// to reconciliation requests for the ClusterConfig potentially managing them.
func crdToClusterConfig(_ client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			// There is only one ClusterConfig potentially managing generic resources.
			Name: v1.CRDClusterConfigName,
		},
	}}
}
