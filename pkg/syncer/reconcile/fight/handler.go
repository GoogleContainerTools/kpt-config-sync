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

package fight

import (
	"sync"

	"github.com/elliotchance/orderedmap/v2"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
)

// Handler is the generic interface of the fight handler.
type Handler interface {
	AddFightError(core.ID, status.Error)
	RemoveFightError(core.ID)

	// FightErrors returns the fight errors (KNV2005) the remediator encounters.
	FightErrors() []status.Error
}

// handler implements Handler.
type handler struct {
	// mux guards the fightErrs
	mux sync.Mutex
	// fightErrs tracks all the controller fights (KNV2005) the remediator encounters,
	// and report to RootSync|RepoSync status.
	fightErrs *orderedmap.OrderedMap[core.ID, status.Error]
}

var _ Handler = &handler{}

// NewHandler instantiates a fight handler
func NewHandler() Handler {
	return &handler{
		fightErrs: orderedmap.NewOrderedMap[core.ID, status.Error](),
	}
}

func (h *handler) AddFightError(id core.ID, err status.Error) {
	h.mux.Lock()
	defer h.mux.Unlock()

	h.fightErrs.Set(id, err)
}

func (h *handler) RemoveFightError(id core.ID) {
	h.mux.Lock()
	defer h.mux.Unlock()

	if h.fightErrs.Delete(id) {
		klog.Infof("Fight error resolved for %s", id)
	}
}

func (h *handler) FightErrors() []status.Error {
	h.mux.Lock()
	defer h.mux.Unlock()

	// Return a copy
	var fightErrs []status.Error
	for e := h.fightErrs.Front(); e != nil; e = e.Next() {
		fightErrs = append(fightErrs, e.Value)
	}
	return fightErrs
}
