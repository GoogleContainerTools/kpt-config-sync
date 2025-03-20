// Copyright 2025 Google LLC
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

package stabilizing

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/util/log"
	"kpt.dev/configsync/pkg/util/mutate"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	logFieldController      = "controller"
	logFieldSyncKind        = "sync.kind"
	logFieldSyncName        = "sync.name"
	logFieldSyncNamespace   = "sync.namespace"
	logFieldObjectKind      = "metadata.kind"
	logFieldName            = "metadata.name"
	logFieldNamespace       = "metadata.namespace"
	logFieldResourceVersion = "metadata.resourceVersion"
	logFieldGeneration      = "metadata.generation"
	logFieldOperation       = "operation"
	logFieldOperationStatus = "operation.status"
)

// Reason indicates the reason for a particular Stabilizing status.
type Reason string

const (
	// ReasonPending indicates that Syncing is pending or in-progress
	ReasonPending Reason = "Pending"
	// ReasonBlocked indicates that Syncing failed
	ReasonBlocked Reason = "Blocked"
	// ReasonInProgress indicates that Stabilizing is in-progress
	ReasonInProgress Reason = "InProgress"
	// ReasonFailed indicates that Stabilizing has failed
	ReasonFailed Reason = "Failed"
	// ReasonSucceeded indicates that Stabilizing has succeeded
	ReasonSucceeded Reason = "Succeeded"
)

// Controller watches the ResourceGroup inventory and updates the Stabilizing
// status condition.
type Controller struct {
	*controllers.LoggingController
	client client.Client
	syncID core.ID
}

var _ reconcile.Reconciler = &Controller{}

// NewController constructs a new stabilizing.Controller.
func NewController(
	kubeClient client.Client,
	log logr.Logger,
	syncID core.ID,
) *Controller {
	return &Controller{
		LoggingController: controllers.NewLoggingController(log),
		client:            kubeClient,
		syncID:            syncID,
	}
}

// Reconcile checks the ResourceGroup status and updates the Stabilizing status
// condition on the RSync.
func (r *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = r.SetLoggerValues(ctx,
		logFieldController, "StabilizingController",
		logFieldSyncKind, r.syncID.Kind,
		logFieldSyncName, r.syncID.Name,
		logFieldSyncNamespace, r.syncID.Namespace)
	r.Logger(ctx).V(5).Info("Reconcile starting")

	rgObj := &v1alpha1.ResourceGroup{}
	if err := r.client.Get(ctx, req.NamespacedName, rgObj); err != nil {
		switch {
		case meta.IsNoMatchError(err):
			return reconcile.Result{},
				fmt.Errorf("ResourceGroup CRD not installed or established: %w", err)
		case apierrors.IsNotFound(err):
			r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: ResourceGroup object not found")
			return reconcile.Result{}, nil
		default:
			return reconcile.Result{},
				fmt.Errorf("getting ResourceGroup object from cache: %s: %w",
					req.NamespacedName, err)
		}
	}

	if rgObj.Status.ObservedGeneration != rgObj.Generation {
		r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: ResourceGroup status update pending")
		return reconcile.Result{}, nil
	}
	if rgObj.DeletionTimestamp != nil {
		r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: ResourceGroup being deleted")
		return reconcile.Result{}, nil
	}

	stabilizingCondition, err := computeStabilizingCondition(rgObj)
	if err != nil {
		return reconcile.Result{},
			fmt.Errorf("failed to compute stabilizing status: %w", err)
	}

	if err := r.applyStabilizingCondition(ctx, stabilizingCondition); err != nil {
		return reconcile.Result{},
			fmt.Errorf("failed to apply stabilizing status condition on object: %v, %w",
				r.syncID, err)
	}

	r.Logger(ctx).V(5).Info("Reconcile completed")
	return reconcile.Result{}, nil
}

func computeStabilizingCondition(rgObj *v1alpha1.ResourceGroup) (SyncCondition, error) {
	actuationStatusCounts := map[v1alpha1.Actuation]int{
		v1alpha1.ActuationFailed:    0,
		v1alpha1.ActuationSkipped:   0,
		v1alpha1.ActuationPending:   0,
		v1alpha1.ActuationSucceeded: 0,
	}
	reconcileStatusCounts := map[v1alpha1.Reconcile]int{
		v1alpha1.ReconcileFailed:    0,
		v1alpha1.ReconcileSkipped:   0,
		v1alpha1.ReconcilePending:   0,
		v1alpha1.ReconcileSucceeded: 0,
	}

	for i, objStatus := range rgObj.Status.ResourceStatuses {
		switch objStatus.Actuation {
		case v1alpha1.ActuationFailed, v1alpha1.ActuationSkipped, v1alpha1.ActuationPending, v1alpha1.ActuationSucceeded:
			actuationStatusCounts[objStatus.Actuation]++
		default:
			return SyncCondition{},
				fmt.Errorf("invalid ResourceGroup field value: status.resourceStatuses[%d].actuation: `%s`",
					i, objStatus.Actuation)
		}
		switch objStatus.Reconcile {
		case v1alpha1.ReconcileFailed, v1alpha1.ReconcileSkipped, v1alpha1.ReconcilePending, v1alpha1.ReconcileSucceeded:
			reconcileStatusCounts[objStatus.Reconcile]++
		default:
			return SyncCondition{},
				fmt.Errorf("invalid ResourceGroup field value: status.resourceStatuses[%d].actuation: `%s`",
					i, objStatus.Actuation)
		}
	}

	// Get the source commit from the inventory
	commit := core.GetAnnotation(rgObj, metadata.SourceCommitAnnotationKey)

	syncCondition := SyncCondition{
		Type:   SyncStabilizing,
		Commit: commit,
	}

	switch {
	case actuationStatusCounts[v1alpha1.ActuationFailed] > 0:
		syncCondition.Status = v1.ConditionFalse
		syncCondition.Reason = string(ReasonBlocked)
		syncCondition.Message = fmt.Sprintf("Stabilizing blocked: %d objects have failed actuation",
			actuationStatusCounts[v1alpha1.ActuationFailed])
	case reconcileStatusCounts[v1alpha1.ReconcileFailed] > 0:
		syncCondition.Status = v1.ConditionFalse
		syncCondition.Reason = string(ReasonFailed)
		syncCondition.Message = fmt.Sprintf("Stabilizing failed: %d objects have failed reconciliation",
			reconcileStatusCounts[v1alpha1.ReconcileFailed])
	case actuationStatusCounts[v1alpha1.ActuationPending] > 0:
		syncCondition.Status = v1.ConditionFalse
		syncCondition.Reason = string(ReasonPending)
		syncCondition.Message = fmt.Sprintf("Stabilizing pending: %d objects are pending actuation",
			actuationStatusCounts[v1alpha1.ActuationPending])
	case reconcileStatusCounts[v1alpha1.ReconcilePending] > 0:
		syncCondition.Status = v1.ConditionTrue
		syncCondition.Reason = string(ReasonInProgress)
		syncCondition.Message = fmt.Sprintf("Stabilizing in progress: %d objects are pending reconciliation",
			reconcileStatusCounts[v1alpha1.ReconcilePending])
	default:
		// We're ignoring skipped actuation/reconcile, because it doesn't
		// indicate success or failure. The applier gets to decide what skipped
		// means and whether to surface the error or not. For example, a skipped
		// object may just be getting deliberately abandoned/unmanaged.
		syncCondition.Status = v1.ConditionFalse
		syncCondition.Reason = string(ReasonSucceeded)
		syncCondition.Message = fmt.Sprintf("Stabilizing succeeded: %d objects have reconciled",
			reconcileStatusCounts[v1alpha1.ReconcileSucceeded])
	}

	return syncCondition, nil
}

// SyncConditionType is an enum of types of conditions for RootSyncs.
type SyncConditionType string

const (
	// SyncStabilizing is a mirror of the RootSync & RepoSync Stabilizing
	// condition types that can be used internally to represent either type,
	// since they are identical.
	SyncStabilizing SyncConditionType = "Stabilizing"
)

// SyncCondition is a mirror of the RootSync & RepoSync condition structs that
// can be used internally to represent either type, since they are identical.
type SyncCondition struct {
	// type of RepoSync condition.
	Type SyncConditionType `json:"type"`
	// status of the condition, one of True, False, Unknown.
	Status metav1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	// +nullable
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +nullable
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// hash of the source of truth. It can be a git commit hash, or an OCI image digest.
	// +optional
	Commit string `json:"commit,omitempty"`
	// errors is a list of errors that occurred in the process.
	// This field is used to track errors when the condition type is Reconciling or Stalled.
	// When the condition type is Syncing, the `errorSourceRefs` field is used instead to
	// avoid duplicating errors between `status.conditions` and `status.rendering|source|sync`.
	// +optional
	Errors []v1beta1.ConfigSyncError `json:"errors,omitempty"`
}

func (r *Controller) applyStabilizingCondition(ctx context.Context, condition SyncCondition) error {
	switch r.syncID.Kind {
	case configsync.RootSyncKind:
		if err := r.applyRootSyncStabilizingCondition(ctx, condition); err != nil {
			return err
		}
	case configsync.RepoSyncKind:
		if err := r.applyRepoSyncStabilizingCondition(ctx, condition); err != nil {
			return err
		}
	default:
		// Already validated by Register method
		return fmt.Errorf("invalid sync ID: %v", r.syncID)
	}
	return nil
}

func (r *Controller) applyRootSyncStabilizingCondition(ctx context.Context, condition SyncCondition) error {
	key := r.syncID.ObjectKey
	rsObj := &v1beta1.RootSync{}
	if err := r.client.Get(ctx, key, rsObj); err != nil {
		return fmt.Errorf("getting RootSync: %s: %w", key, err)
	}
	_, err := mutate.Status(ctx, r.client, rsObj, func() error {
		if rsObj.Status.ObservedGeneration != rsObj.Generation {
			r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: RootSync status update pending")
			return &mutate.NoUpdateError{}
		}
		if rsObj.DeletionTimestamp != nil {
			r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: RootSync being deleted")
			return &mutate.NoUpdateError{}
		}
		oldCondition := rootsync.GetCondition(rsObj.Status.Conditions, v1beta1.RootSyncConditionType(condition.Type))
		oldCondition = oldCondition.DeepCopy()
		now := metav1.Now()
		updated, transitioned := rootsync.SetStabilizing(rsObj, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.Errors, now)
		if !updated {
			return &mutate.NoUpdateError{}
		}
		newCondition := rootsync.GetCondition(rsObj.Status.Conditions, v1beta1.RootSyncConditionType(condition.Type))
		if r.Logger(ctx).V(1).Enabled() {
			// diff includes line-breaks, which breaks log parsing
			if r.Logger(ctx).V(5).Enabled() {
				ctx = r.SetLoggerValues(ctx,
					"diff", fmt.Sprintf("Diff (- Old, + New):\n%s",
						log.AsYAMLDiff(oldCondition, newCondition)))
			}
			r.Logger(ctx).Info("Updating sync status",
				logFieldOperation, "update",
				logFieldOperationStatus, computeOperationStatus(updated, transitioned),
				logFieldGeneration, rsObj.Generation,
				logFieldResourceVersion, rsObj.ResourceVersion)
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return fmt.Errorf("Sync status update failed: %w", err)
	}
	return nil
}

func (r *Controller) applyRepoSyncStabilizingCondition(ctx context.Context, condition SyncCondition) error {
	key := r.syncID.ObjectKey
	rsObj := &v1beta1.RepoSync{}
	if err := r.client.Get(ctx, key, rsObj); err != nil {
		return fmt.Errorf("getting RepoSync: %s: %w", key, err)
	}
	_, err := mutate.Status(ctx, r.client, rsObj, func() error {
		if rsObj.Status.ObservedGeneration != rsObj.Generation {
			r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: RepoSync status update pending")
			return &mutate.NoUpdateError{}
		}
		if rsObj.DeletionTimestamp != nil {
			r.Logger(ctx).V(1).Info("Skipping Stabilizing condition update: RepoSync being deleted")
			return &mutate.NoUpdateError{}
		}
		oldCondition := reposync.GetCondition(rsObj.Status.Conditions, v1beta1.RepoSyncConditionType(condition.Type))
		oldCondition = oldCondition.DeepCopy()
		now := metav1.Now()
		updated, transitioned := reposync.SetStabilizing(rsObj, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.Errors, now)
		if !updated {
			return &mutate.NoUpdateError{}
		}
		newCondition := reposync.GetCondition(rsObj.Status.Conditions, v1beta1.RepoSyncConditionType(condition.Type))
		if r.Logger(ctx).V(1).Enabled() {
			// diff includes line-breaks, which breaks log parsing
			if r.Logger(ctx).V(5).Enabled() {
				ctx = r.SetLoggerValues(ctx,
					"diff", fmt.Sprintf("Diff (- Old, + New):\n%s",
						log.AsYAMLDiff(oldCondition, newCondition)))
			}
			r.Logger(ctx).Info("Updating sync status",
				logFieldOperation, "update",
				logFieldOperationStatus, computeOperationStatus(updated, transitioned),
				logFieldGeneration, rsObj.Generation,
				logFieldResourceVersion, rsObj.ResourceVersion)
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return fmt.Errorf("Sync status update failed: %w", err)
	}
	return nil
}

func computeOperationStatus(updated, transitioned bool) string {
	var opStatus string
	switch {
	case transitioned:
		opStatus = "transition"
	case updated:
		opStatus = "update"
	default:
		opStatus = "skipped"
	}
	return opStatus
}

// Register the RGController with the ReconcilerManager.
func (r *Controller) Register(mgr controllerruntime.Manager) error {
	b := controllerruntime.NewControllerManagedBy(mgr).
		Named("StabilizingController").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		})
	// Watch a single RSync and reconcile its status (or at least part of it).
	switch r.syncID.Kind {
	case configsync.RootSyncKind:
		b = b.For(&v1beta1.RootSync{},
			builder.WithPredicates(newKeySelectorPredicate(r.syncID.ObjectKey)))
	case configsync.RepoSyncKind:
		b = b.For(&v1beta1.RepoSync{},
			builder.WithPredicates(newKeySelectorPredicate(r.syncID.ObjectKey)))
	default:
		return fmt.Errorf("invalid sync ID: %v", r.syncID)
	}
	// Watch a single ResourceGroup, the one that matches the RSync.
	// ResourceGroup ObjectKey == RSync ObjectKey.
	b = b.Watches(&v1alpha1.ResourceGroup{},
		handler.EnqueueRequestsFromMapFunc(mapResourceGroupToRSync),
		builder.WithPredicates(newKeySelectorPredicate(r.syncID.ObjectKey)))
	return b.Complete(r)
}

func newKeySelectorPredicate(key client.ObjectKey) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return client.ObjectKeyFromObject(obj) == key
	})
}

// mapResourceGroupToRSync maps ResourceGroup to RSync by ObjectKey.
func mapResourceGroupToRSync(_ context.Context, obj client.Object) []reconcile.Request {
	return []reconcile.Request{
		{NamespacedName: client.ObjectKeyFromObject(obj)},
	}
}
