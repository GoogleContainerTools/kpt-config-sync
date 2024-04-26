// Copyright 2024 Google LLC
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
	"context"
	"fmt"

	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/applier/stats"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Applier fakes applier.Applier.
//
// This is not in kpt.dev/configsync/pkg/testing/fake because that would cause
// a import loop (applier -> fake -> applier).
type Applier struct {
	ApplyCalls   int
	ApplyInputs  []ApplierInputs
	ApplyOutputs []ApplierOutputs
}

// ApplierInputs stores inputs for fake.Applier.Apply()
type ApplierInputs struct {
	Objects []client.Object
}

// ApplierOutputs stores outputs for fake.Applier.Apply()
type ApplierOutputs struct {
	Errors          []status.Error
	ObjectStatusMap applier.ObjectStatusMap
	SyncStats       *stats.SyncStats
}

// Apply fakes applier.Applier.Apply()
func (a *Applier) Apply(_ context.Context, eventHandler func(applier.Event), objects []client.Object) (applier.ObjectStatusMap, *stats.SyncStats) {
	a.ApplyInputs = append(a.ApplyInputs, ApplierInputs{
		Objects: objects,
	})
	if a.ApplyCalls >= len(a.ApplyOutputs) {
		panic(fmt.Sprintf("Expected only %d calls to Applier.Apply, but got more. Update Applier.Outputs if this is expected.", len(a.ApplyOutputs)))
	}
	outputs := a.ApplyOutputs[a.ApplyCalls]
	a.ApplyCalls++
	for _, err := range outputs.Errors {
		eventHandler(applier.ErrorEvent{
			Error: err,
		})
	}
	return outputs.ObjectStatusMap, outputs.SyncStats
}

var _ applier.Applier = &Applier{}
