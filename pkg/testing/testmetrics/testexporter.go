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

package testmetrics

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opencensus.io/stats/view"
)

// TestExporter keeps exported metric view data in memory to aid in testing.
type TestExporter struct {
	rows []*view.Row
}

// RowSort implements sort.Interface based on the string representation of Row.
type RowSort []*view.Row

// ExportView records the view data.
func (e *TestExporter) ExportView(data *view.Data) {
	e.rows = data.Rows
}

// ValidateMetrics compares the exported view data with the expected metric data.
func (e *TestExporter) ValidateMetrics(v *view.View, want []*view.Row) string {
	view.Unregister(v)
	// Need to sort first because the exported row order is non-deterministic
	sort.Sort(RowSort(e.rows))
	sort.Sort(RowSort(want))
	return diff(e.rows, want)
}

// RegisterMetrics collects data for the given views and reports data to the TestExporter.
func RegisterMetrics(views ...*view.View) *TestExporter {
	_ = view.Register(views...)
	var e TestExporter
	view.RegisterExporter(&e)
	return &e
}

// diff compares the exported rows' Tags and data Value with the expected
// rows' Tags and data Value. It excludes the Start time field from the comparison.
func diff(r []*view.Row, other []*view.Row) string {
	for i := 0; i < len(r); i++ {
		if r[i] == other[i] {
			continue
		}
		if len(r[i].Tags) > 0 && len(other[i].Tags) > 0 && !reflect.DeepEqual(r[i].Tags, other[i].Tags) {
			return fmt.Sprintf("Unexpected metric tags, -got, +want: -%v\n+%v", r[i].Tags, other[i].Tags)
		}
		if !cmp.Equal(r[i].Data, other[i].Data, cmpopts.IgnoreTypes(time.Time{})) {
			return fmt.Sprintf("Unexpected metric value, -got, +want: -%v\n+%v", r[i].Data, other[i].Data)
		}
	}
	if len(other) > len(r) {
		return fmt.Sprintf("Unexpected metric value(s): %v", other[len(r):])
	}
	return ""
}

func (rs RowSort) Len() int           { return len(rs) }
func (rs RowSort) Swap(i, j int)      { rs[i], rs[j] = rs[j], rs[i] }
func (rs RowSort) Less(i, j int) bool { return fmt.Sprintf("%v", rs[i]) < fmt.Sprintf("%v", rs[j]) }
