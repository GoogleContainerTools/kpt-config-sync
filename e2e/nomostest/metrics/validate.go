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

package metrics

import (
	"k8s.io/apimachinery/pkg/types"
)

// Operation is an enum of possible values for the "operation" metric tag.
type Operation string

const (
	// UpdateOperation is the value expected for the "operation" metric tag for
	// objects that have been included in the source and applied/updated/patched.
	UpdateOperation Operation = "update"

	// DeleteOperation is the value expected for the "operation" metric tag for
	// objects that have been removed from source and deleted.
	DeleteOperation Operation = "delete"

	// SkipOperation is used to filter out objects that are declared in source,
	// but not updated/deleted
	SkipOperation Operation = "skip"

	// OtelCollectorMetricsPort is the port where prometheus metrics are exposed
	OtelCollectorMetricsPort = 8675
)

// ObjectOperation is used for validating count aggregated metrics that have a
// "operation" tag (ex: `api_duration_seconds` & `apply_operations`).
type ObjectOperation struct {
	// Operation performed on the object
	Operation Operation
	// Count of the times the operation was performed on objects of the
	// specified kind
	Count int
}

// AppendOperations updates a list of ObjectOperations to add the specified
// operation
func AppendOperations(to []ObjectOperation, from ...ObjectOperation) []ObjectOperation {
loop:
	for _, fromOp := range from {
		for j, toOp := range to {
			if fromOp.Operation == toOp.Operation {
				to[j].Count += fromOp.Count
				continue loop // stop looking for matches for this operation
			}
		}
		// no matches found for this operation
		to = append(to, fromOp)
	}
	return to
}

// Summary describes the expected state for a set of standard metrics.
type Summary struct {
	// Sync is the name and namespace of the RootSync or RepoSync
	Sync types.NamespacedName
	// ObjectCount is the expected value of the declared_resources_view metric
	ObjectCount int
	// Operations is a list of operations expected to be represented by the
	// operations metrics: api_cal_duration_view, apply_operations_view, &
	// remediate_duration_view
	Operations []ObjectOperation
	// Errors describes the expected state of a set of standard error metrics.
	Errors ErrorSummary
	// Absolute indicates that the ObjectCount and Operations should not be
	// added to the values specified in the ExpectedObjects struct.
	Absolute bool
}

// ErrorSummary describes the expected state of a set of standard error metrics.
type ErrorSummary struct {
	// Fights is the expected value of the resource_fights_view metric.
	Fights int
	// Conflicts is the expected value of the resource_conflicts_view metric.
	Conflicts int
	// Internal is the expected value of the internal_errors_view metric.
	Internal int
	// Rendering is the expected value of the reconciler_errors_view metric,
	// when component=rendering.
	Rendering int
	// Source is the expected value of the reconciler_errors_view metric, when
	// component=source.
	Source int
	// Sync is the expected value of the reconciler_errors_view metric, when
	// component=sync.
	Sync int
}
