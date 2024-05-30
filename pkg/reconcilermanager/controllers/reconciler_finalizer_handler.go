// Copyright 2024 Google LLC
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

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// reconcilerFinalizerHandler is a delegator that handles the
// `configsync.gke.io/reconciler` finalizer on behalf of the reconciler.
//
// The reconciler adds the finalizer when deletion propagation is set to
// `Foreground`, and removes it when R*Sync has a deletionTimestamp.
// If the reconciler is unable to update R*Sync to remove the finalizer, R*Sync
// would get stuck in the deletion process.
// To unblock the deletion, the reconciler-manager acts as a delegator to remove
// R*Sync's finalizer when needed.
type reconcilerFinalizerHandler interface {
	// handleReconcilerFinalizer handles the `configsync.gke.io/reconciler`
	// finalizer when the reconciler is not able to remove it from R*Sync.
	handleReconcilerFinalizer(context.Context, client.Object) (bool, error)
}

type rootReconcilerFinalizerHandler struct {
}

// handleReconcilerFinalizer waits for the root reconciler finalizer to finish.
// Note: it is possible RootSync is stuck in deletion because of the finalizer
// (e.g. root-reconciler is either deleted, or unable to remove the finalizer),
// but we decided to leave it as a known issue and brainstorm a better solution later.
func (r rootReconcilerFinalizerHandler) handleReconcilerFinalizer(context.Context, client.Object) (bool, error) {
	// Waiting for the root reconciler finalizer to finish
	return false, nil
}

type nsReconcilerFinalizerHandler struct {
	loggingController
	client client.Client
}

// handleReconcilerFinalizer removes the 'configsync.gke.io/reconciler'
// finalizer from the RepoSync object when the RepoSync Namespace is terminating.
//
// When the RepoSync's Namespace is terminating, Namespace controller will
// handle the deletion propagation of all resources managed by the RepoSync.
// Therefore, remove the RepoSync finalizer to avoid duplicated work.
// This also fixes an issue with a race when Namespace controller deletes
// the reconciler's RoleBinding needed for removing the finalizer.
//
// It returns whether the RepoSync finalizer has been removed.
func (r nsReconcilerFinalizerHandler) handleReconcilerFinalizer(ctx context.Context, syncObj client.Object) (bool, error) {
	syncObjNamespace := &corev1.Namespace{}
	syncObjNamespace.Name = syncObj.GetNamespace()
	key := client.ObjectKeyFromObject(syncObjNamespace)
	if err := r.client.Get(ctx, key, syncObjNamespace); err != nil {
		r.logger(ctx).Error(err, "Get sync object Namespace failed")
		return false, err
	}

	if syncObjNamespace.Status.Phase != corev1.NamespaceTerminating {
		// RepoSync's namespace is active, wait for the reconciler finalizer to complete.
		return false, nil
	}

	r.logger(ctx).Info("Removing the reconciler finalizer because RepoSync's namespace is terminating")
	if !controllerutil.RemoveFinalizer(syncObj, metadata.ReconcilerFinalizer) {
		r.logger(ctx).Info("Reconciler finalizer not found")
		return true, nil
	}
	if err := r.client.Update(ctx, syncObj, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
		err = status.APIServerError(err, "failed to update RepoSync to remove the reconciler finalizer")
		r.logger(ctx).Error(err, "Removal of reconciler finalizer removal failed")
		return false, err
	}
	r.logger(ctx).Info("Removal of reconciler finalizer succeeded")
	return true, nil
}
