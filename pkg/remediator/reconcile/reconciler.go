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
	"time"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator/conflict"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type reconcilerInterface interface {
	Remediate(ctx context.Context, id core.ID, obj client.Object) status.Error
	GetClient() client.Client
}

// reconciler ensures objects are consistent with their declared state in the
// repository.
type reconciler struct {
	scope    declared.Scope
	syncName string
	// applier is where to write the declared configuration to.
	applier syncerreconcile.Applier
	// declared is the threadsafe in-memory representation of declared configuration.
	declared *declared.Resources

	conflictHandler conflict.Handler
	fightHandler    fight.Handler
}

// newReconciler instantiates a new reconciler.
func newReconciler(
	scope declared.Scope,
	syncName string,
	applier syncerreconcile.Applier,
	declared *declared.Resources,
	conflictHandler conflict.Handler,
	fightHandler fight.Handler,
) *reconciler {
	return &reconciler{
		scope:           scope,
		syncName:        syncName,
		applier:         applier,
		declared:        declared,
		conflictHandler: conflictHandler,
		fightHandler:    fightHandler,
	}
}

// Remediate takes a client.Object representing the object to update, and then
// ensures that the version on the server matches it.
func (r *reconciler) Remediate(ctx context.Context, id core.ID, obj client.Object) status.Error {
	start := time.Now()

	declU, commit, found := r.declared.Get(id)
	// Yes, this if block is necessary because Go is pedantic about nil interfaces.
	// 1) var decl client.Object = declU results in a panic.
	// 2) Using declU as a client.Object results in a panic.
	var decl client.Object
	if found {
		decl = declU
	}
	objDiff := diff.Diff{
		Declared: decl,
		Actual:   obj,
	}

	err := r.remediate(ctx, id, objDiff)

	// Record duration, even if there's an error
	metrics.RecordRemediateDuration(ctx, metrics.StatusTagKey(err), start)

	if err != nil {
		switch err.Code() {
		case syncerclient.ResourceConflictCode:
			// Record cache conflict (create/delete/update failure due to found/not-found/resource-version)
			metrics.RecordResourceConflict(ctx, commit)
		case status.ManagementConflictErrorCode:
			if mce, ok := err.(status.ManagementConflictError); ok {
				conflict.Record(ctx, r.conflictHandler, mce, commit)
			}
		case status.FightErrorCode:
			operation := objDiff.Operation(r.scope, r.syncName)
			metrics.RecordResourceFight(ctx, string(operation))
			r.fightHandler.AddFightError(id, err)
		}
		return err
	}

	r.conflictHandler.RemoveConflictError(id)
	r.fightHandler.RemoveFightError(id)
	return nil
}

// Remediate takes diff (declared & actual) and ensures the server matches the
// declared state.
func (r *reconciler) remediate(ctx context.Context, id core.ID, objDiff diff.Diff) status.Error {
	switch t := objDiff.Operation(r.scope, r.syncName); t {
	case diff.NoOp:
		return nil
	case diff.ManagementConflict:
		// Error logged by conflict.Record AND worker.Run. So don't log here.
		newManager := declared.ResourceManager(r.scope, r.syncName)
		return status.ManagementConflictErrorWrap(objDiff.Actual, newManager)
	case diff.Create:
		declared, err := objDiff.UnstructuredDeclared()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Remediator creating object: %v", id)
		return r.applier.Create(ctx, declared)
	case diff.Update:
		declared, err := objDiff.UnstructuredDeclared()
		if err != nil {
			return err
		}
		actual, err := objDiff.UnstructuredActual()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Remediator updating object: %v", id)
		return r.applier.Update(ctx, declared, actual)
	case diff.Delete:
		actual, err := objDiff.UnstructuredActual()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Remediator deleting object: %v", id)
		return r.applier.Delete(ctx, actual)
	case diff.Error:
		// This is the case where the annotation in the *repository* is invalid.
		// Should never happen as the Parser would have thrown an error.
		return nonhierarchical.IllegalManagementAnnotationError(
			objDiff.Declared,
			objDiff.Declared.GetAnnotations()[metadata.ResourceManagementKey],
		)
	case diff.Abandon:
		actual, err := objDiff.UnstructuredActual()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Remediator abandoning object %v", id)
		return r.applier.RemoveNomosMeta(ctx, actual, metrics.RemediatorController)
	default:
		// e.g. differ.DeleteNsConfig, which shouldn't be possible to get to any way.
		metrics.RecordInternalError(ctx, "remediator")
		return status.InternalErrorf("diff type not supported: %v", t)
	}
}

// GetClient returns the reconciler's underlying client.Client.
func (r *reconciler) GetClient() client.Client {
	return r.applier.GetClient()
}
