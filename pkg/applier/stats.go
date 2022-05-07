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

package applier

import (
	"fmt"
	"sort"
	"strings"

	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
)

// pruneEventStats tracks the stats for all the PruneType events
type pruneEventStats struct {
	// eventByOp tracks the number of PruneType events including no error by PruneEventOperation
	eventByOp map[event.PruneEventStatus]uint64
}

func (s pruneEventStats) string() string {
	var strs []string
	var keys []int
	for k := range s.eventByOp {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	total := uint64(0)
	for _, k := range keys {
		op := event.PruneEventStatus(k)
		if s.eventByOp[op] > 0 {
			total += s.eventByOp[op]
			strs = append(strs, fmt.Sprintf("%s: %d", op, s.eventByOp[op]))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("PruneEvents: %d (%s)", total, strings.Join(strs, ", "))
}

func (s pruneEventStats) empty() bool {
	return len(s.eventByOp) == 0
}

// applyEventStats tracks the stats for all the ApplyType events
type applyEventStats struct {
	// eventByOp tracks the number of ApplyType events including no error by ApplyEventOperation
	// Possible values: Created, Configured, Unchanged.
	eventByOp map[event.ApplyEventStatus]uint64
}

func (s applyEventStats) string() string {
	var strs []string
	var keys []int
	for k := range s.eventByOp {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	total := uint64(0)
	for _, k := range keys {
		op := event.ApplyEventStatus(k)
		if s.eventByOp[op] > 0 {
			total += s.eventByOp[op]
			strs = append(strs, fmt.Sprintf("%s: %d", op, s.eventByOp[op]))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("ApplyEvents: %d (%s)", total, strings.Join(strs, ", "))
}

func (s applyEventStats) empty() bool {
	return len(s.eventByOp) == 0
}

// disabledObjStats tracks the stats for dsiabled objects
type disabledObjStats struct {
	// total tracks the number of objects to be disabled
	total uint64
	// succeeded tracks how many ojbects were disabled successfully
	succeeded uint64
}

func (s disabledObjStats) string() string {
	if s.empty() {
		return ""
	}
	return fmt.Sprintf("disabled %d out of %d objects", s.succeeded, s.total)
}

func (s disabledObjStats) empty() bool {
	return s.total == 0
}

// applyStats tracks the stats for all the events
type applyStats struct {
	applyEvent  applyEventStats
	pruneEvent  pruneEventStats
	disableObjs disabledObjStats
	// errorTypeEvents tracks the number of ErrorType events
	errorTypeEvents uint64
	objsReconciled  map[core.ID]struct{}
}

func (s applyStats) string() string {
	var strs []string
	if !s.applyEvent.empty() {
		strs = append(strs, s.applyEvent.string())
	}
	if !s.pruneEvent.empty() {
		strs = append(strs, s.pruneEvent.string())
	}
	if !s.disableObjs.empty() {
		strs = append(strs, s.disableObjs.string())
	}
	if s.errorTypeEvents > 0 {
		strs = append(strs, fmt.Sprintf("ErrorEvents: %d", s.errorTypeEvents))
	}
	return strings.Join(strs, ", ")
}

func (s applyStats) empty() bool {
	return s.errorTypeEvents == 0 && s.pruneEvent.empty() && s.applyEvent.empty() && s.disableObjs.empty()
}

func newApplyStats() applyStats {
	return applyStats{
		applyEvent: applyEventStats{
			eventByOp: map[event.ApplyEventStatus]uint64{},
		},
		pruneEvent: pruneEventStats{
			eventByOp: map[event.PruneEventStatus]uint64{},
		},
		objsReconciled: map[core.ID]struct{}{},
	}
}
