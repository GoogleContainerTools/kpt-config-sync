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
	"io"
	"strings"

	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
)

// ObjectStatus is a subset of actuation.ObjectStatus for tracking object status
// as a map value instead of a list value.
type ObjectStatus struct {
	// Strategy indicates the method of actuation (apply or delete) used or planned to be used.
	Strategy actuation.ActuationStrategy
	// Actuation indicates whether actuation has been performed yet and how it went.
	Actuation actuation.ActuationStatus
	// Reconcile indicates whether reconciliation has been performed yet and how it went.
	Reconcile actuation.ReconcileStatus
}

// ObjectStatusMap is a map of object IDs to ObjectStatus.
type ObjectStatusMap map[core.ID]*ObjectStatus

// infofLogger is a subest of klog.Verbose to make testing ObjectStatusMap.Log
// easier.
type infofLogger interface {
	Enabled() bool
	Infof(format string, args ...interface{})
}

// Log uses the specified logger to log object statuses.
// This produces multiple log entries, if the logger is enabled.
// Takes a minimal logger interface in order to make testing easier, but is
// designed for use with a leveled klog, like klog.V(3)
func (m ObjectStatusMap) Log(logger infofLogger) {
	if !logger.Enabled() {
		return
	}

	count := 0
	var b strings.Builder
	for i, status := range actuationStatuses {
		if i > 0 {
			b.WriteString(commaNewlineDelimiter)
		}
		ids := m.Filter(actuation.ActuationStrategyApply, status, -1)
		count += len(ids)
		writeStatus(&b, status, ids)
	}
	if count == 0 {
		logger.Infof("Apply Actuations (Total: %d)", count)
	} else {
		logger.Infof("Apply Actuations (Total: %d):\n%s", count, b.String())
	}

	count = 0
	b.Reset()
	for i, status := range reconcileStatuses {
		if i > 0 {
			b.WriteString(commaNewlineDelimiter)
		}
		ids := m.Filter(actuation.ActuationStrategyApply, -1, status)
		count += len(ids)
		writeStatus(&b, status, ids)
	}
	if count == 0 {
		logger.Infof("Apply Reconciles (Total: %d)", count)
	} else {
		logger.Infof("Apply Reconciles (Total: %d):\n%s", count, b.String())
	}

	count = 0
	b.Reset()
	for i, status := range actuationStatuses {
		if i > 0 {
			b.WriteString(commaNewlineDelimiter)
		}
		ids := m.Filter(actuation.ActuationStrategyDelete, status, -1)
		count += len(ids)
		writeStatus(&b, status, ids)
	}
	if count == 0 {
		logger.Infof("Prune Actuations (Total: %d)", count)
	} else {
		logger.Infof("Prune Actuations (Total: %d):\n%s", count, b.String())
	}

	count = 0
	b.Reset()
	for i, status := range reconcileStatuses {
		if i > 0 {
			b.WriteString(commaNewlineDelimiter)
		}
		ids := m.Filter(actuation.ActuationStrategyDelete, -1, status)
		count += len(ids)
		writeStatus(&b, status, ids)
	}
	if count == 0 {
		logger.Infof("Prune Reconciles (Total: %d)", count)
	} else {
		logger.Infof("Prune Reconciles (Total: %d):\n%s", count, b.String())
	}
}

func writeStatus(w io.Writer, status interface{ String() string }, ids []core.ID) {
	if len(ids) == 0 {
		fmt.Fprintf(w, "%s (%d)", status, len(ids))
	} else {
		fmt.Fprintf(w, "%s (%d): [%s]", status, len(ids), joinIDs(commaSpaceDelimiter, ids...))
	}
}

// Filter returns an unsorted list of IDs that satisfy the specified constraints.
// Use -1 to specify the constraint is not required.
func (m ObjectStatusMap) Filter(
	strategy actuation.ActuationStrategy,
	actuation actuation.ActuationStatus,
	reconcile actuation.ReconcileStatus,
) []core.ID {
	var ids []core.ID
	for id, status := range m {
		if status == nil {
			continue
		}
		if strategy > 0 && status.Strategy != strategy {
			continue
		}
		if actuation > 0 && status.Actuation != actuation {
			continue
		}
		if reconcile > 0 && status.Reconcile != reconcile {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// actuationStatuses is the list of ActuationStatus enums in order for logging.
var actuationStatuses = []actuation.ActuationStatus{
	// actuation.ActuationPending, // Don't log pending actuation. It doesn't emit for all objects.
	actuation.ActuationSkipped,
	actuation.ActuationSucceeded,
	actuation.ActuationFailed,
}

// reconcileStatuses is the list of ReconcileStatus enums in order for logging.
var reconcileStatuses = []actuation.ReconcileStatus{
	// actuation.ReconcilePending, // Don't log pending reconcile. It doesn't emit for all objects.
	actuation.ReconcileSkipped,
	actuation.ReconcileSucceeded,
	actuation.ReconcileFailed,
	actuation.ReconcileTimeout,
}
