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
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const reconcileTimeout = time.Minute * 5

var _ reconcile.Reconciler = &metaReconciler{}

// ClientFactory is a function used for creating new controller-runtime clients.
type clientFactory func() (client.Client, error)

// MetaReconciler reconciles Syncs. It responds to changes in Syncs and causes RestartableManager
// to stop and start controllers based on the resources that are presently sync-enabled.
type metaReconciler struct {
	// client is used to update Sync status fields and finalize Syncs.
	client *syncerclient.Client
	// syncReader is a shared cache of Syncs on the cluster.
	// It is populated by informers in the scheme and used by all controllers and
	// reconcilers in the manager.
	syncReader client.Reader
	// discoveryClient is used to look up versions on the cluster for the GroupKinds in the Syncs being reconciled.
	discoveryClient utildiscovery.ServerResourcer
	// builder is used to recreate controllers for watched GroupVersionKinds.
	builder *syncAwareBuilder
	// subManager is responsible for starting/restarting all controllers that depend on Syncs.
	subManager watch.RestartableManager
	// clientFactory returns a new dynamic client.
	clientFactory clientFactory
	now           func() metav1.Time
}

// NewMetaReconciler returns a new MetaReconciler that reconciles changes in Syncs.
func newMetaReconciler(mgr manager.Manager, dc discovery.DiscoveryInterface, clientFactory clientFactory, now func() metav1.Time) (*metaReconciler, error) {
	builder := newSyncAwareBuilder()
	sm, err := watch.NewManager(mgr, builder)
	if err != nil {
		return nil, err
	}

	return &metaReconciler{
		client:          syncerclient.New(mgr.GetClient(), metrics.APICallDuration),
		syncReader:      mgr.GetCache(),
		clientFactory:   clientFactory,
		discoveryClient: dc,
		builder:         builder,
		subManager:      sm,
		now:             now,
	}, nil
}

// Reconcile is the Reconcile callback for MetaReconciler.
// It looks at all Syncs in the cluster and restarts the SubManager if its internal state doesn't match the cluster
// state.
func (r *metaReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	start := r.now()
	metrics.ReconcileEventTimes.WithLabelValues("sync").Set(float64(start.Unix()))

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	err := r.reconcileSyncs(ctx, request)
	metrics.ReconcileDuration.WithLabelValues("sync", metrics.StatusLabel(err)).Observe(time.Since(start.Time).Seconds())

	if err != nil {
		klog.Errorf("Error while reconciling Sync resources: %v", err)
	}
	return reconcile.Result{}, err
}

func (r *metaReconciler) reconcileSyncs(ctx context.Context, request reconcile.Request) error {
	syncs := &v1.SyncList{}
	err := r.syncReader.List(ctx, syncs)
	if err != nil {
		panic(errors.Wrap(err, "could not list all Syncs"))
	}

	var toFinalize []*v1.Sync
	var enabled []*v1.Sync
	for idx, s := range syncs.Items {
		if s.GetDeletionTimestamp() != nil {
			// Check for finalizer then finalize if needed.
			toFinalize = append(toFinalize, &syncs.Items[idx])
		} else {
			// Anything not pending delete should be enabled in SyncAwareBuilder.
			enabled = append(enabled, s.DeepCopy())
		}
	}

	scoper := utildiscovery.Scoper{}
	sr, err := utildiscovery.GetResources(r.discoveryClient)
	if err != nil {
		return err
	}
	err = scoper.AddAPIResourceLists(sr)
	if err != nil {
		return err
	}

	apirs, err := utildiscovery.NewAPIInfo(sr)
	if err != nil {
		return err
	}
	r.builder.scoper = scoper

	eventTriggeredRestart := restartSubManager(request.Name)
	source := "sync"
	if eventTriggeredRestart {
		// For event triggered forced restarts, we store the source of the restart in the Namespace field.
		source = request.Namespace
	}

	attemptedRestart, err := r.subManager.Restart(apirs.GroupVersionKinds(enabled...), eventTriggeredRestart)
	if attemptedRestart {
		metrics.ControllerRestarts.WithLabelValues(source).Inc()
	}
	if err != nil {
		klog.Errorf("Sync reconciler failed to restart SubManager: %v", err)
		return err
	}

	var mErr status.MultiError
	// Finalize Syncs that have not already been finalized.
	for _, tf := range toFinalize {
		// Make sure to delete all Sync-managed resource before finalizing the Sync.
		gvksToFinalize := apirs.GroupVersionKinds(tf)
		mErr = status.Append(mErr, r.finalizeSync(ctx, tf, gvksToFinalize))
	}

	// Update status sub-resource for enabled Syncs, if we have not already done so.
	for _, sync := range enabled {
		var ss v1.SyncStatus
		ss.Status = v1.Syncing

		// Check if status changed before updating.
		if !reflect.DeepEqual(sync.Status, ss) {
			updateFn := func(obj client.Object) (client.Object, error) {
				s := obj.(*v1.Sync)
				s.Status = ss
				return s, nil
			}
			sync.SetGroupVersionKind(kinds.Sync())
			_, err := r.client.UpdateStatus(ctx, sync, updateFn)
			if err != nil {
				mErr = status.Append(mErr, status.APIServerError(err, "could not update sync status"))
			}
		}
	}

	return mErr
}

func (r *metaReconciler) finalizeSync(ctx context.Context, sync *v1.Sync, gvks map[schema.GroupVersionKind]bool) status.MultiError {
	var newFinalizers []string
	var needsFinalize bool
	for _, f := range sync.Finalizers {
		if f == v1.SyncFinalizer {
			needsFinalize = true
		} else {
			newFinalizers = append(newFinalizers, f)
		}
	}

	// Check if Syncer finalizer is present before finalize.
	if !needsFinalize {
		klog.V(2).Infof("Sync %s already finalized", sync.Name)
		return nil
	}

	sync = sync.DeepCopy()
	sync.Finalizers = newFinalizers
	klog.Infof("beginning Sync finalize for %s", sync.Name)
	if err := r.gcResources(ctx, sync, gvks); err != nil {
		return err
	}
	err := r.client.Upsert(ctx, sync)
	if err != nil {
		return err
	}
	klog.Infof("finalized Sync %s", sync.Name)
	return nil
}

func (r *metaReconciler) gcResources(ctx context.Context, sync *v1.Sync, gvks map[schema.GroupVersionKind]bool) status.MultiError {
	// It doesn't matter which version we choose when deleting.
	// Deletes to a resource of a particular version affect all versions with the same group and kind.
	if len(gvks) == 0 {
		klog.Warningf("Could not find a gvk for %s, CRD may have been deleted, skipping garbage collection.", sync.Name)
		return nil
	}
	var gvk schema.GroupVersionKind
	for k := range gvks {
		gvk = k
		break
	}
	klog.Infof("Sync reconciler garbage collecting all %s", gvk.GroupKind())
	var errBuilder status.MultiError
	// Create a new dynamic client since it's possible that the manager client is reading from the
	// cache.
	cl, err := r.clientFactory()
	if err != nil {
		errBuilder = status.Append(errBuilder,
			status.APIServerError(err, fmt.Sprintf("failed to create dynamic client during garbage collection of %s", gvk.GroupKind().String())))
		return errBuilder
	}
	gvk.Kind += "List"
	ul := &unstructured.UnstructuredList{}
	ul.SetGroupVersionKind(gvk)
	if err := cl.List(ctx, ul); err != nil {
		errBuilder = status.Append(errBuilder, status.APIServerErrorf(err, "failed to list resources of type %s for garbage collection", gvk))
		return errBuilder
	}
	for _, u := range ul.Items {
		annots := u.GetAnnotations()
		if v, ok := annots[metadata.ResourceManagementKey]; !ok || v != metadata.ResourceManagementEnabled {
			continue
		}
		if err := cl.Delete(ctx, &u); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.V(2).Infof("Failed to delete %s while garbage collecting", resourceDesc(&u))
				errBuilder = status.Append(errBuilder, status.APIServerErrorf(err, "failed to delete resource %s during garbage collection", resourceDesc(&u)))
			}
			continue
		}
		klog.Infof("garbage collected %s", resourceDesc(&u))
	}
	return errBuilder
}

func resourceDesc(u *unstructured.Unstructured) string {
	gvk := u.GroupVersionKind()
	ns := u.GetNamespace()
	n := u.GetName()
	if ns == "" {
		return fmt.Sprintf("[%s kind=%s name=%s]", gvk.GroupVersion(), gvk.Kind, n)
	}
	return fmt.Sprintf("[%s kind=%s namespace=%s name=%s]", gvk.GroupVersion(), gvk.Kind, ns, n)
}

// restartSubManager returns true if the reconcile request indicates that we need to restart all
// controllers that the Sync Controller manages.
func restartSubManager(name string) bool {
	return name == forceRestart
}
