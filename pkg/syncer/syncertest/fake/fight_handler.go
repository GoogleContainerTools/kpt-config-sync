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

package fake

import (
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
)

// FightHandler is a fake implementation of fight.Handler.
type FightHandler struct{}

// AddFightError is a fake implementation of the AddFightError of fight.Handler.
func (h *FightHandler) AddFightError(core.ID, status.Error) {}

// RemoveFightError is a fake implementation of the RemoveFightError of fight.Handler.
func (h *FightHandler) RemoveFightError(core.ID) {
}

// FightErrors is a fake implementation of the FightErrors of fight.Handler.
func (h *FightHandler) FightErrors() map[core.ID]status.Error {
	return map[core.ID]status.Error{}
}

var _ fight.Handler = &FightHandler{}

// NewFightHandler initiates a fake implementation of fight.Handler.
func NewFightHandler() fight.Handler {
	return &FightHandler{}
}
