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

	"sigs.k8s.io/cli-utils/pkg/apply/event"
)

// pruneEventStats tracks the stats for all the PruneType events
type pruneEventStats struct {
	// EventByOp tracks the number of PruneType events including no error by PruneEventOperation
	EventByOp map[event.PruneEventStatus]uint64
}

func (s pruneEventStats) string() string {
	var strs []string
	var keys []int
	for k := range s.EventByOp {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	total := uint64(0)
	for _, k := range keys {
		op := event.PruneEventStatus(k)
		if s.EventByOp[op] > 0 {
			total += s.EventByOp[op]
			strs = append(strs, fmt.Sprintf("%s: %d", op, s.EventByOp[op]))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("PruneEvents: %d (%s)", total, strings.Join(strs, ", "))
}

func (s pruneEventStats) empty() bool {
	return len(s.EventByOp) == 0
}

// applyEventStats tracks the stats for all the ApplyType events
type applyEventStats struct {
	// EventByOp tracks the number of ApplyType events including no error by ApplyEventOperation
	// Possible values: Created, Configured, Unchanged.
	EventByOp map[event.ApplyEventStatus]uint64
}

func (s applyEventStats) string() string {
	var strs []string
	var keys []int
	for k := range s.EventByOp {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	total := uint64(0)
	for _, k := range keys {
		op := event.ApplyEventStatus(k)
		if s.EventByOp[op] > 0 {
			total += s.EventByOp[op]
			strs = append(strs, fmt.Sprintf("%s: %d", op, s.EventByOp[op]))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("ApplyEvents: %d (%s)", total, strings.Join(strs, ", "))
}

func (s applyEventStats) empty() bool {
	return len(s.EventByOp) == 0
}

// waitEventStats tracks the stats for all the WaitType events
type waitEventStats struct {
	// EventByOp tracks the number of WaitType events including no error by WaitTypeOperation
	// Possible values: Pending, Successful, Skipped, Timeout, Failed
	EventByOp map[event.WaitEventStatus]uint64
}

func (s waitEventStats) string() string {
	var strs []string
	var keys []int
	for k := range s.EventByOp {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	total := uint64(0)
	for _, k := range keys {
		op := event.WaitEventStatus(k)
		if s.EventByOp[op] > 0 {
			total += s.EventByOp[op]
			strs = append(strs, fmt.Sprintf("%s: %d", op, s.EventByOp[op]))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("WaitEvents: %d (%s)", total, strings.Join(strs, ", "))
}

func (s waitEventStats) empty() bool {
	return len(s.EventByOp) == 0
}

// DisabledObjStats tracks the stats for dsiabled objects
type DisabledObjStats struct {
	// Total tracks the number of objects to be disabled
	Total uint64
	// Succeeded tracks how many ojbects were disabled successfully
	Succeeded uint64
}

func (s DisabledObjStats) string() string {
	if s.empty() {
		return ""
	}
	return fmt.Sprintf("disabled %d out of %d objects", s.Succeeded, s.Total)
}

func (s DisabledObjStats) empty() bool {
	return s.Total == 0
}

// ApplyStats tracks the stats for all the events
type ApplyStats struct {
	ApplyEvent  applyEventStats
	PruneEvent  pruneEventStats
	WaitEvent   waitEventStats
	DisableObjs DisabledObjStats
	// ErrorTypeEvents tracks the number of ErrorType events
	ErrorTypeEvents uint64
}

func (s ApplyStats) string() string {
	var strs []string
	if !s.ApplyEvent.empty() {
		strs = append(strs, s.ApplyEvent.string())
	}
	if !s.PruneEvent.empty() {
		strs = append(strs, s.PruneEvent.string())
	}
	if !s.WaitEvent.empty() {
		strs = append(strs, s.WaitEvent.string())
	}
	if !s.DisableObjs.empty() {
		strs = append(strs, s.DisableObjs.string())
	}
	if s.ErrorTypeEvents > 0 {
		strs = append(strs, fmt.Sprintf("ErrorEvents: %d", s.ErrorTypeEvents))
	}
	return strings.Join(strs, ", ")
}

func (s ApplyStats) empty() bool {
	return s.ErrorTypeEvents == 0 && s.PruneEvent.empty() && s.ApplyEvent.empty() && s.WaitEvent.empty() && s.DisableObjs.empty()
}

func newApplyStats() ApplyStats {
	return ApplyStats{
		ApplyEvent: applyEventStats{
			EventByOp: map[event.ApplyEventStatus]uint64{},
		},
		PruneEvent: pruneEventStats{
			EventByOp: map[event.PruneEventStatus]uint64{},
		},
		WaitEvent: waitEventStats{
			EventByOp: map[event.WaitEventStatus]uint64{},
		},
	}
}
