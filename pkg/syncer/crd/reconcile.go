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
	"context"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	syncercache "kpt.dev/configsync/pkg/syncer/cache"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/syncer/metrics"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/sync"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	versionV1        = "v1"
	versionV1beta1   = "v1beta1"
	reconcileTimeout = time.Minute * 5

	restartSignal = "crd"
)

var _ reconcile.Reconciler = &reconciler{}

// Reconciler reconciles CRD resources on the cluster.
// It restarts ClusterConfig and NamespaceConfig controllers if changes have been made to
// CustomResourceDefinitions on the cluster.
type reconciler struct {
	// client is used to update the ClusterConfig.Status
	client *syncerclient.Client
	// applier create/patches/deletes CustomResourceDefinitions.
	applier syncerreconcile.Applier
	// cache is a shared cache that is populated by informers in the scheme and used by all controllers / reconcilers in the
	// manager.
	cache    *syncercache.GenericCache
	recorder record.EventRecorder
	decoder  decode.Decoder
	now      func() metav1.Time
	// signal is a handle that is used to restart the ClusterConfig and NamespaceConfig controllers and
	// their manager.
	signal sync.RestartSignal

	// allCrds tracks the entire set of CRDs on the API server.
	// We need to restart the syncer if a CRD is Created/Updated/Deleted since
	// this will change the overall set of resources that the syncer can
	// be handling (in this case gatekeeper will add CRDs to the cluster and we
	// need to restart in order to have the syncer work properly).
	allCrds map[schema.GroupVersionKind]struct{}
	//initTime is the reconciler's instantiation time
	initTime metav1.Time
}

// NewReconciler returns a new Reconciler.
func newReconciler(client *syncerclient.Client, applier syncerreconcile.Applier,
	reader client.Reader, recorder record.EventRecorder, decoder decode.Decoder,
	now func() metav1.Time, signal sync.RestartSignal) *reconciler {
	return &reconciler{
		client:   client,
		applier:  applier,
		cache:    syncercache.NewGenericResourceCache(reader),
		recorder: recorder,
		decoder:  decoder,
		now:      now,
		signal:   signal,
		initTime: now(),
	}
}

// Reconcile implements Reconciler.
func (r *reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if request.Name != v1.CRDClusterConfigName {
		// We only handle the CRD ClusterConfig in this reconciler.
		return reconcile.Result{}, nil
	}

	start := r.now()
	metrics.ReconcileEventTimes.WithLabelValues("crd").Set(float64(start.Unix()))

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	err := r.reconcile(ctx, request.Name)
	metrics.ReconcileDuration.WithLabelValues("crd", metrics.StatusLabel(err)).Observe(time.Since(start.Time).Seconds())

	if err != nil {
		klog.Errorf("Could not reconcile CRD ClusterConfig: %v", err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *reconciler) listCrds(ctx context.Context) ([]apiextensionsv1.CustomResourceDefinition, error) {
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	if err := r.client.List(ctx, crdList); err != nil {
		return nil, err
	}
	return crdList.Items, nil
}

func (r *reconciler) toCrdSet(crds []apiextensionsv1.CustomResourceDefinition) map[schema.GroupVersionKind]struct{} {
	allCRDs := map[schema.GroupVersionKind]struct{}{}
	for _, crd := range crds {
		crdGk := schema.GroupKind{
			Group: crd.Spec.Group,
			Kind:  crd.Spec.Names.Kind,
		}

		for _, ver := range crd.Spec.Versions {
			allCRDs[crdGk.WithVersion(ver.Name)] = struct{}{}
		}
	}
	if len(allCRDs) == 0 {
		return nil
	}
	return allCRDs
}

func (r *reconciler) reconcile(ctx context.Context, name string) status.MultiError {
	var mErr status.MultiError

	clusterConfig := &v1.ClusterConfig{}
	if err := r.cache.Get(ctx, types.NamespacedName{Name: name}, clusterConfig); err != nil {
		if apierrors.IsNotFound(err) {
			// CRDs may be changing on the cluster, but we don't have any CRD ClusterConfig to reconcile with.
			return nil
		}
		err = errors.Wrapf(err, "could not retrieve ClusterConfig %q", v1.CRDClusterConfigName)
		klog.Error(err)
		mErr = status.Append(mErr, err)
		return mErr
	}
	clusterConfig.SetGroupVersionKind(kinds.ClusterConfig())

	grs, err := r.decoder.DecodeResources(clusterConfig.Spec.Resources)
	if err != nil {
		mErr = status.Append(mErr, errors.Wrap(err, "could not decode ClusterConfig"))
		return mErr
	}

	// Keep track of what version the declarations are in.
	// We only want to compute diffs between CRDs of the same declared/actual version.
	declVersions := make(map[string]string)
	declaredCRDsV1Beta1 := grs[kinds.CustomResourceDefinitionV1Beta1()]
	for _, decl := range declaredCRDsV1Beta1 {
		syncerreconcile.SyncedAt(decl, clusterConfig.Spec.Token)
		declVersions[decl.GetName()] = versionV1beta1
	}
	declaredCRDsV1 := grs[kinds.CustomResourceDefinitionV1()]
	for _, decl := range declaredCRDsV1 {
		syncerreconcile.SyncedAt(decl, clusterConfig.Spec.Token)
		declVersions[decl.GetName()] = versionV1
	}

	var syncErrs []v1.ConfigManagementError

	actualCRDs, err := r.cache.UnstructuredList(ctx, kinds.CustomResourceDefinitionV1())
	if err != nil {
		mErr = status.Append(mErr, status.APIServerErrorf(err, "failed to list from config controller for %q", kinds.CustomResourceDefinitionV1Beta1()))
		syncErrs = append(syncErrs, syncerreconcile.NewConfigManagementError(clusterConfig, err))
		mErr = status.Append(mErr, syncerreconcile.SetClusterConfigStatus(ctx, r.client, clusterConfig, r.initTime, r.now, syncErrs, nil))
		return mErr
	}

	var actualV1Beta1 []*unstructured.Unstructured
	for _, u := range actualCRDs {
		// Default to v1beta1 if the CRD is not declared.
		// We can't assume v1 as this is incompatible with Kubernetes <1.16
		if declVersions[u.GetName()] == versionV1 {
			continue
		}
		actualV1Beta1 = append(actualV1Beta1, u)
	}

	var actualV1 []*unstructured.Unstructured
	for _, u := range actualCRDs {
		// Only keep CRDs declares as v1.
		if declVersions[u.GetName()] == "v1" {
			actualV1 = append(actualV1, u)
		}
	}

	allDeclaredVersions := syncerreconcile.AllVersionNames(grs, kinds.CustomResourceDefinition())
	diffsV1Beta1 := differ.Diffs(declaredCRDsV1Beta1, actualV1Beta1, allDeclaredVersions)
	diffsV1 := differ.Diffs(declaredCRDsV1, actualV1, allDeclaredVersions)
	diffs := append(diffsV1Beta1, diffsV1...)

	var reconcileCount int
	for _, diff := range diffs {
		if updated, err := syncerreconcile.HandleDiff(ctx, r.applier, diff, r.recorder); err != nil {
			mErr = status.Append(mErr, err)
			syncErrs = append(syncErrs, err.ToCME())
		} else if updated {
			// TODO: Add unit tests for diff type logic.
			if diff.Type() != differ.Update {
				// We don't need to restart if an existing CRD was updated.
				reconcileCount++
			}
		}
	}

	var needRestart bool
	if reconcileCount > 0 {
		needRestart = true
		// We've updated CRDs on the cluster; restart the NamespaceConfig and ClusterConfig controllers.
		r.recorder.Eventf(clusterConfig, corev1.EventTypeNormal, v1.EventReasonReconcileComplete,
			"crd cluster config was successfully reconciled: %d changes", reconcileCount)
		klog.Info("Triggering restart due to repo CRD change")
	}

	crdList, err := r.listCrds(ctx)
	if err != nil {
		mErr = status.Append(mErr, err)
	} else {
		allCrds := r.toCrdSet(crdList)
		if !equality.Semantic.DeepEqual(r.allCrds, allCrds) {
			needRestart = true
			r.recorder.Eventf(clusterConfig, corev1.EventTypeNormal, v1.EventReasonCRDChange,
				"crds changed on the cluster restarting syncer controllers")
			klog.Info("Triggering restart due to external CRD change")
		}
		r.allCrds = allCrds
	}

	if needRestart {
		r.signal.Restart(restartSignal)
	}

	if err := syncerreconcile.SetClusterConfigStatus(ctx, r.client, clusterConfig, r.initTime, r.now, syncErrs, clusterConfig.Status.ResourceConditions); err != nil {
		r.recorder.Eventf(clusterConfig, corev1.EventTypeWarning, v1.EventReasonStatusUpdateFailed,
			"failed to update ClusterConfig status: %v", err)
		mErr = status.Append(mErr, err)
	}

	return mErr
}
