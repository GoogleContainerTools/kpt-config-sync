// Copyright 2023 Google LLC
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

package conflict

import (
	"context"
	"sync"

	"github.com/elliotchance/orderedmap/v2"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
)

// UnknownManager is used as a placeholder when the new or old manager is unknown.
const UnknownManager = "UNKNOWN"

// Handler is the generic interface of the conflict handler.
type Handler interface {
	AddConflictError(core.ID, status.ManagementConflictError)
	RemoveConflictError(core.ID)
	ClearConflictErrorsWithKind(gk schema.GroupKind)

	// ConflictErrors returns the management conflict errors (KNV1060) the remediator encounters.
	ConflictErrors() []status.ManagementConflictError
	// HasConflictErrors returns true when there are conflict errors
	HasConflictErrors() bool
	// HasConflictError returns true when there is a conflict for the specified object ID.
	HasConflictError(core.ID) bool
}

// Record a management conflict error, including log and metric.
func Record(ctx context.Context, handler Handler, err status.ManagementConflictError, commit string) {
	klog.Errorf("Remediator detected a management conflict: "+
		"reconciler %q received a watch event for object %q, which is managed by namespace reconciler %q. "+
		"To resolve the conflict, remove the object from one of the sources of truth "+
		"so that the object is only managed by one reconciler.",
		err.DesiredManager(), err.ObjectID(), err.CurrentManager())
	handler.AddConflictError(err.ObjectID(), err)
	// TODO: Use separate metrics for management conflicts vs resource conflicts
	metrics.RecordResourceConflict(ctx, commit)
}

// handler implements Handler.
type handler struct {
	// mux guards the conflictErrs
	mux sync.RWMutex
	// conflictErrs tracks all the conflict errors (KNV1060) the remediator encounters,
	// and report to RootSync|RepoSync status.
	conflictErrs *orderedmap.OrderedMap[core.ID, status.ManagementConflictError]
}

var _ Handler = &handler{}

// NewHandler instantiates a conflict handler
func NewHandler() Handler {
	return &handler{
		conflictErrs: orderedmap.NewOrderedMap[core.ID, status.ManagementConflictError](),
	}
}

func (h *handler) AddConflictError(id core.ID, newErr status.ManagementConflictError) {
	h.mux.Lock()
	defer h.mux.Unlock()

	// Ignore KptManagementConflictError if a ManagementConflictError was already reported.
	// KptManagementConflictError don't have a real ConflictingManager recorded.
	// TODO: Remove if cli-utils supports reporting the conflicting manager in InventoryOverlapError.
	if newErr.CurrentManager() == UnknownManager {
		if oldErr, found := h.conflictErrs.Get(id); found {
			if oldErr.CurrentManager() != UnknownManager {
				return
			}
		}
	}

	h.conflictErrs.Set(id, newErr)
}

// HasConflictError returns true when there is a conflict for the specified object ID.
func (h *handler) HasConflictError(id core.ID) bool {
	h.mux.RLock()
	defer h.mux.RUnlock()

	_, found := h.conflictErrs.Get(id)
	return found
}

func (h *handler) RemoveConflictError(id core.ID) {
	h.mux.Lock()
	defer h.mux.Unlock()

	if h.conflictErrs.Delete(id) {
		klog.Infof("Conflict error resolved for %s", id)
	}
}

func (h *handler) ClearConflictErrorsWithKind(gk schema.GroupKind) {
	h.mux.Lock()
	defer h.mux.Unlock()

	for pair := h.conflictErrs.Front(); pair != nil; pair = pair.Next() {
		if pair.Key.GroupKind == gk {
			h.conflictErrs.Delete(pair.Key)
		}
	}
}

func (h *handler) ConflictErrors() []status.ManagementConflictError {
	h.mux.RLock()
	defer h.mux.RUnlock()

	// Return a copy
	var result []status.ManagementConflictError
	for pair := h.conflictErrs.Front(); pair != nil; pair = pair.Next() {
		result = append(result, pair.Value)
	}
	return result
}

func (h *handler) HasConflictErrors() bool {
	h.mux.RLock()
	defer h.mux.RUnlock()

	return h.conflictErrs.Len() > 0
}
