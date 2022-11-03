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

package stats

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/cli-utils/pkg/apply/event"
)

// PruneEventStats tracks the stats for all the PruneType events
type PruneEventStats struct {
	// EventByOp tracks the number of PruneType events including no error by PruneEventOperation
	EventByOp map[event.PruneEventStatus]uint64
}

// Add records a new event
func (s *PruneEventStats) Add(status event.PruneEventStatus) {
	if s.EventByOp == nil {
		s.EventByOp = map[event.PruneEventStatus]uint64{}
	}
	s.EventByOp[status]++
}

// String returns the stats as a human readable string.
func (s PruneEventStats) String() string {
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

// Empty returns true if no events were recorded.
func (s *PruneEventStats) Empty() bool {
	return s == nil || len(s.EventByOp) == 0
}

// DeleteEventStats tracks the stats for all the DeleteType events
type DeleteEventStats struct {
	// EventByOp tracks the number of DeleteType events including no error by DeleteEventOperation
	EventByOp map[event.DeleteEventStatus]uint64
}

// Add records a new event
func (s *DeleteEventStats) Add(status event.DeleteEventStatus) {
	if s.EventByOp == nil {
		s.EventByOp = map[event.DeleteEventStatus]uint64{}
	}
	s.EventByOp[status]++
}

// String returns the stats as a human readable string.
func (s DeleteEventStats) String() string {
	var strs []string
	var keys []int
	for k := range s.EventByOp {
		keys = append(keys, int(k))
	}
	sort.Ints(keys)
	total := uint64(0)
	for _, k := range keys {
		op := event.DeleteEventStatus(k)
		if s.EventByOp[op] > 0 {
			total += s.EventByOp[op]
			strs = append(strs, fmt.Sprintf("%s: %d", op, s.EventByOp[op]))
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("DeleteEvents: %d (%s)", total, strings.Join(strs, ", "))
}

// Empty returns true if no events were recorded.
func (s *DeleteEventStats) Empty() bool {
	return s == nil || len(s.EventByOp) == 0
}

// ApplyEventStats tracks the stats for all the ApplyType events
type ApplyEventStats struct {
	// EventByOp tracks the number of ApplyType events including no error by ApplyEventOperation
	// Possible values: Created, Configured, Unchanged.
	EventByOp map[event.ApplyEventStatus]uint64
}

// Add records a new event
func (s *ApplyEventStats) Add(status event.ApplyEventStatus) {
	if s.EventByOp == nil {
		s.EventByOp = map[event.ApplyEventStatus]uint64{}
	}
	s.EventByOp[status]++
}

// String returns the stats as a human readable string.
func (s ApplyEventStats) String() string {
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

// Empty returns true if no events were recorded.
func (s *ApplyEventStats) Empty() bool {
	return s == nil || len(s.EventByOp) == 0
}

// WaitEventStats tracks the stats for all the WaitType events
type WaitEventStats struct {
	// EventByOp tracks the number of WaitType events including no error by WaitTypeOperation
	// Possible values: Pending, Successful, Skipped, Timeout, Failed
	EventByOp map[event.WaitEventStatus]uint64
}

// Add records a new event
func (s *WaitEventStats) Add(status event.WaitEventStatus) {
	if s.EventByOp == nil {
		s.EventByOp = map[event.WaitEventStatus]uint64{}
	}
	s.EventByOp[status]++
}

// String returns the stats as a human readable string.
func (s WaitEventStats) String() string {
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

// Empty returns true if no events were recorded.
func (s *WaitEventStats) Empty() bool {
	return s == nil || len(s.EventByOp) == 0
}

// DisabledObjStats tracks the stats for dsiabled objects
type DisabledObjStats struct {
	// Total tracks the number of objects to be disabled
	Total uint64
	// Succeeded tracks how many ojbects were disabled successfully
	Succeeded uint64
}

// String returns the stats as a human readable String.
func (s DisabledObjStats) String() string {
	if s.Empty() {
		return ""
	}
	return fmt.Sprintf("disabled %d out of %d objects", s.Succeeded, s.Total)
}

// Empty returns true if no events were recorded.
func (s *DisabledObjStats) Empty() bool {
	return s == nil || s.Total == 0
}

// SyncStats tracks the stats for all the events
type SyncStats struct {
	ApplyEvent  *ApplyEventStats
	PruneEvent  *PruneEventStats
	DeleteEvent *DeleteEventStats
	WaitEvent   *WaitEventStats
	DisableObjs *DisabledObjStats
	// ErrorTypeEvents tracks the number of ErrorType events
	ErrorTypeEvents uint64
}

// String returns the stats as a human readable string.
func (s SyncStats) String() string {
	var strs []string
	if !s.ApplyEvent.Empty() {
		strs = append(strs, s.ApplyEvent.String())
	}
	if !s.PruneEvent.Empty() {
		strs = append(strs, s.PruneEvent.String())
	}
	if !s.DeleteEvent.Empty() {
		strs = append(strs, s.DeleteEvent.String())
	}
	if !s.WaitEvent.Empty() {
		strs = append(strs, s.WaitEvent.String())
	}
	if !s.DisableObjs.Empty() {
		strs = append(strs, s.DisableObjs.String())
	}
	if s.ErrorTypeEvents > 0 {
		strs = append(strs, fmt.Sprintf("ErrorEvents: %d", s.ErrorTypeEvents))
	}
	return strings.Join(strs, ", ")
}

// Empty returns true if no events were recorded.
func (s *SyncStats) Empty() bool {
	return s == nil || s.ErrorTypeEvents == 0 && s.PruneEvent.Empty() && s.DeleteEvent.Empty() && s.ApplyEvent.Empty() && s.WaitEvent.Empty() && s.DisableObjs.Empty()
}

// NewSyncStats constructs a SyncStats with empty event maps.
func NewSyncStats() *SyncStats {
	return &SyncStats{
		ApplyEvent:  &ApplyEventStats{},
		PruneEvent:  &PruneEventStats{},
		DeleteEvent: &DeleteEventStats{},
		WaitEvent:   &WaitEventStats{},
		DisableObjs: &DisabledObjStats{},
	}
}
