// Copyright 2025 The kpt Authors
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

package live

import (
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// ActuationStatusToKstatus contains the logic/rules to convert from
// ResourceStatus to kstatus.Status.
// If conversion is not needed, the original status field is returned.
func ActuationStatusToKstatus(status actuation.ObjectStatus) kstatus.Status {
	if status.Actuation == actuation.ActuationSucceeded && status.Reconcile == actuation.ReconcileSucceeded {
		switch status.Strategy {
		case actuation.ActuationStrategyApply:
			return kstatus.CurrentStatus
		case actuation.ActuationStrategyDelete:
			return kstatus.NotFoundStatus
		}
	}
	// Terminating, InProgress, and Failed are unknowable without access to the
	// object spec & status, and sometimes its children.
	// TODO: add kstatus to the actuation.ObjectStatus, so this information isn't lost
	// For now, the ResourceGroup controller has to immediately update the
	// ResourceGroup status to add this extra info.
	return kstatus.UnknownStatus
}
