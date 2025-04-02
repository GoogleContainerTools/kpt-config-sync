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

package finalizer

import (
	"context"
	"fmt"

	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Finalizer handles finalizing a RootSync or RepoSync for the reconciler.
type Finalizer interface {
	Finalize(ctx context.Context, syncObj client.Object) error
	AddFinalizer(ctx context.Context, syncObj client.Object) (bool, error)
	RemoveFinalizer(ctx context.Context, syncObj client.Object) (bool, error)
}

// New constructs a new RootSyncFinalizer or RepoSyncFinalizer, depending on the
// specified scope.
func New(scope declared.Scope, destroyer applier.Destroyer, c client.Client, stopControllers context.CancelFunc, controllersStopped <-chan struct{}, applySetID string) Finalizer {
	if scope == declared.RootScope {
		return &RootSyncFinalizer{
			baseFinalizer: baseFinalizer{
				Destroyer:  destroyer,
				Client:     c,
				ApplySetID: applySetID,
			},
			StopControllers:    stopControllers,
			ControllersStopped: controllersStopped,
		}
	}
	return &RepoSyncFinalizer{
		baseFinalizer: baseFinalizer{
			Destroyer:  destroyer,
			Client:     c,
			ApplySetID: applySetID,
		},
		StopControllers:    stopControllers,
		ControllersStopped: controllersStopped,
	}
}

// addFinalizer adds the `configsync.gke.io/reconciler` finalizer to the
// specified object, locally.
// Returns true, if the object was modified.
func addFinalizer(syncObj client.Object) bool {
	return controllerutil.AddFinalizer(syncObj, metadata.ReconcilerFinalizer)
}

// removeFinalizer removes the `configsync.gke.io/reconciler` finalizer from the
// specified object, locally.
// Returns true, if the object was modified.
func removeFinalizer(syncObj client.Object) bool {
	return controllerutil.RemoveFinalizer(syncObj, metadata.ReconcilerFinalizer)
}

func objSummary(obj client.Object) string {
	return fmt.Sprintf("%T %s %s/%s",
		obj, obj.GetObjectKind().GroupVersionKind(),
		obj.GetNamespace(), obj.GetName())
}
