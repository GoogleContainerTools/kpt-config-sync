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

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
)

// Handler is the generic interface of the fight handler.
type Handler interface {
	AddFightError(core.ID, status.Error)
	RemoveFightError(core.ID)

	// FightErrors returns the fight errors (KNV2005) the remediator encounters.
	FightErrors() map[core.ID]status.Error
}

// handler implements Handler.
type handler struct {
	// mux guards the fightErrs
	mux sync.Mutex
	// fightErrs tracks all the controller fights (KNV2005) the remediator encounters,
	// and report to RootSync|RepoSync status.
	fightErrs map[core.ID]status.Error
}

var _ Handler = &handler{}

// NewHandler instantiates a fight handler
func NewHandler() Handler {
	return &handler{
		fightErrs: map[core.ID]status.Error{},
	}
}

func (h *handler) AddFightError(id core.ID, err status.Error) {
	h.mux.Lock()
	defer h.mux.Unlock()

	h.fightErrs[id] = err
}

func (h *handler) RemoveFightError(id core.ID) {
	h.mux.Lock()
	defer h.mux.Unlock()

	delete(h.fightErrs, id)
}

func (h *handler) FightErrors() map[core.ID]status.Error {
	h.mux.Lock()
	defer h.mux.Unlock()

	// Return a copy
	fightErrs := map[core.ID]status.Error{}
	for k, v := range h.fightErrs {
		fightErrs[k] = v
	}
	return fightErrs
}
