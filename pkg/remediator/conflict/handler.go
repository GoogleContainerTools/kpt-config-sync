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
	"sync"

	"github.com/elliotchance/orderedmap/v2"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
)

// Handler is the generic interface of the conflict handler.
type Handler interface {
	AddConflictError(queue.GVKNN, status.ManagementConflictError)
	RemoveConflictError(queue.GVKNN)
	RemoveAllConflictErrors(gvk schema.GroupVersionKind)

	// ConflictErrors returns the management conflict errors (KNV1060) the remediator encounters.
	ConflictErrors() []status.ManagementConflictError
}

// handler implements Handler.
type handler struct {
	// mux guards the conflictErrs
	mux sync.Mutex
	// conflictErrs tracks all the conflict errors (KNV1060) the remediator encounters,
	// and report to RootSync|RepoSync status.
	conflictErrs *orderedmap.OrderedMap[queue.GVKNN, status.ManagementConflictError]
}

var _ Handler = &handler{}

// NewHandler instantiates a conflict handler
func NewHandler() Handler {
	return &handler{
		conflictErrs: orderedmap.NewOrderedMap[queue.GVKNN, status.ManagementConflictError](),
	}
}

func (h *handler) AddConflictError(gvknn queue.GVKNN, e status.ManagementConflictError) {
	h.mux.Lock()
	defer h.mux.Unlock()

	h.conflictErrs.Set(gvknn, e)
}

func (h *handler) RemoveConflictError(gvknn queue.GVKNN) {
	h.mux.Lock()
	defer h.mux.Unlock()

	if h.conflictErrs.Delete(gvknn) {
		klog.Infof("Conflict error resolved for %s", gvknn)
	}
}

func (h *handler) RemoveAllConflictErrors(gvk schema.GroupVersionKind) {
	h.mux.Lock()
	defer h.mux.Unlock()

	for pair := h.conflictErrs.Front(); pair != nil; pair = pair.Next() {
		if pair.Key.GroupVersionKind() == gvk {
			h.conflictErrs.Delete(pair.Key)
		}
	}
}

func (h *handler) ConflictErrors() []status.ManagementConflictError {
	h.mux.Lock()
	defer h.mux.Unlock()

	// Return a copy
	var result []status.ManagementConflictError
	for pair := h.conflictErrs.Front(); pair != nil; pair = pair.Next() {
		result = append(result, pair.Value)
	}
	return result
}
