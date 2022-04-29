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

package reconcile

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	syncercache "kpt.dev/configsync/pkg/syncer/cache"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ reconcile.Reconciler = &clusterConfigReconciler{}

// clusterConfigReconciler reconciles a ClusterConfig object.
type clusterConfigReconciler struct {
	client   *syncerclient.Client
	applier  Applier
	cache    *syncercache.GenericCache
	recorder record.EventRecorder
	decoder  decode.Decoder
	toSync   []schema.GroupVersionKind
	now      func() metav1.Time
	//mgrInitTime is the submanager's instantiation time
	mgrInitTime metav1.Time
}

// NewClusterConfigReconciler returns a new clusterConfigReconciler.  ctx is the ambient context
// to use for all reconciler operations.
func NewClusterConfigReconciler(c *syncerclient.Client, applier Applier, reader client.Reader, recorder record.EventRecorder,
	decoder decode.Decoder, now func() metav1.Time, toSync []schema.GroupVersionKind, mgrInitTime metav1.Time) reconcile.Reconciler {
	return &clusterConfigReconciler{
		client:      c,
		applier:     applier,
		cache:       syncercache.NewGenericResourceCache(reader),
		recorder:    recorder,
		decoder:     decoder,
		toSync:      toSync,
		now:         now,
		mgrInitTime: mgrInitTime,
	}
}

// Reconcile is the Reconcile callback for clusterConfigReconciler.
func (r *clusterConfigReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if request.Name == v1.CRDClusterConfigName {
		// We handle the CRD Cluster Config in the CRD controller, so don't reconcile it here.
		return reconcile.Result{}, nil
	}

	start := r.now()
	metrics.ReconcileEventTimes.WithLabelValues("cluster").Set(float64(start.Unix()))

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	err := r.reconcileConfig(ctx, request.NamespacedName)
	metrics.ReconcileDuration.WithLabelValues("cluster", metrics.StatusLabel(err)).Observe(time.Since(start.Time).Seconds())

	return reconcile.Result{}, err
}

func (r *clusterConfigReconciler) reconcileConfig(ctx context.Context, name types.NamespacedName) error {
	clusterConfig := &v1.ClusterConfig{}
	err := r.cache.Get(ctx, name, clusterConfig)
	if err != nil {
		err = errors.Wrapf(err, "could not retrieve clusterconfig %q", name)
		klog.Error(err)
		return err
	}
	clusterConfig.SetGroupVersionKind(kinds.ClusterConfig())

	if name.Name != v1.ClusterConfigName {
		err := errors.Errorf("ClusterConfig resource has invalid name %q. To fix, delete the ClusterConfig.", name.Name)
		r.recorder.Eventf(clusterConfig, corev1.EventTypeWarning, v1.EventReasonInvalidClusterConfig, err.Error())
		klog.Warning(err)
		// Update the status on a best effort basis. We don't want to retry handling a ClusterConfig
		// we want to ignore and it's possible it has been deleted by the time we reconcile it.
		syncErrs := []v1.ConfigManagementError{NewConfigManagementError(clusterConfig, err)}
		if err2 := SetClusterConfigStatus(ctx, r.client, clusterConfig, r.mgrInitTime, r.now, syncErrs, nil); err2 != nil {
			r.recorder.Eventf(clusterConfig, corev1.EventTypeWarning, v1.EventReasonStatusUpdateFailed,
				"failed to update cluster config status: %v", err2)
		}
		return nil
	}

	rErr := r.manageConfigs(ctx, clusterConfig)
	// Filter out errors caused by a context cancellation. These errors are expected and uninformative.
	if filtered := filterContextCancelled(rErr); filtered != nil {
		klog.Errorf("Could not reconcile clusterconfig: %v", status.FormatSingleLine(filtered))
	}
	return rErr
}

func (r *clusterConfigReconciler) manageConfigs(ctx context.Context, config *v1.ClusterConfig) error {
	grs, err := r.decoder.DecodeResources(config.Spec.Resources)
	if err != nil {
		return errors.Wrapf(err, "could not process cluster config: %q", config.GetName())
	}

	var errBuilder status.MultiError
	var resConditions []v1.ResourceCondition
	reconcileCount := 0

	for _, gvk := range r.toSync {
		declaredInstances := grs[gvk]
		for _, decl := range declaredInstances {
			SyncedAt(decl, config.Spec.Token)
		}

		actualInstances, err := r.cache.UnstructuredList(ctx, gvk)
		if err != nil {
			errBuilder = status.Append(errBuilder, status.APIServerErrorf(err, "failed to list from config controller for %q", gvk))
			continue
		}

		for _, act := range actualInstances {
			annotations := act.GetAnnotations()
			if annotationsHaveResourceCondition(annotations) {
				resConditions = append(resConditions, makeResourceCondition(*act, config.Spec.Token))
			}
		}

		allDeclaredVersions := AllVersionNames(grs, gvk.GroupKind())
		diffs := differ.Diffs(declaredInstances, actualInstances, allDeclaredVersions)
		for _, diff := range diffs {
			if updated, err := HandleDiff(ctx, r.applier, diff, r.recorder); err != nil {
				errBuilder = status.Append(errBuilder, err)
			} else if updated {
				reconcileCount++
			}
		}
	}

	// There's two possibilities for reaching this case:
	// 1) We reach this case on a race condition where the reconciler is run before the
	// changes to Sync objects are picked up.  We exit early since there are resources we can't
	// properly handle which will cause status on the ClusterConfig to incorrectly report that
	// everything is fully synced.  We can need to skip the status update since
	// not all resources will be synced and we are expecting a restart shortly.
	// 2) Someone has added a gatekeeper ConstraintTemplate and Constraint in the same commit.
	// The ConstraintTemplate will be applied at this point, but the Constraint
	// will be skipped since it doesn't yet exist since Gatekeeper needs to create
	// a CRD for it.  Once gatekeeper creates the CRD, the CRD meta controller will
	// notice the new CRD and restart the cluster config controller which will
	// allow the constraint to get applied.
	if gks := resourcesWithoutSync(config.Spec.Resources, r.toSync); len(gks) != 0 {
		klog.Infof(
			"clusterConfigReconciler encountered "+
				"group-kind(s) %s that were not present in a sync, "+
				"skipping status update and waiting for reconciler restart",
			strings.Join(gks, ", "))
		return errBuilder
	}

	cmErrs := status.ToCME(errBuilder)
	if err := SetClusterConfigStatus(ctx, r.client, config, r.mgrInitTime, r.now, cmErrs, resConditions); err != nil {
		errBuilder = status.Append(errBuilder, err)
		r.recorder.Eventf(config, corev1.EventTypeWarning, v1.EventReasonStatusUpdateFailed,
			"failed to update cluster config status: %v", err)
	}

	if errBuilder == nil && reconcileCount > 0 {
		r.recorder.Eventf(config, corev1.EventTypeNormal, v1.EventReasonReconcileComplete,
			"cluster config was successfully reconciled: %d changes", reconcileCount)
	}
	return errBuilder
}

// NewConfigManagementError returns a ConfigManagementError corresponding to the given ClusterConfig and error.
func NewConfigManagementError(config *v1.ClusterConfig, err error) v1.ConfigManagementError {
	e := v1.ErrorResource{
		SourcePath:        config.GetAnnotations()[metadata.SourcePathAnnotationKey],
		ResourceName:      config.GetName(),
		ResourceNamespace: config.GetNamespace(),
		ResourceGVK:       config.GroupVersionKind(),
	}
	cme := v1.ConfigManagementError{
		ErrorMessage: err.Error(),
	}
	cme.ErrorResources = append(cme.ErrorResources, e)
	return cme
}
