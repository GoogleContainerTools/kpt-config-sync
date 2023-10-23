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

package resourcegroup

import (
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
)

func newReconcilingCondition(status v1alpha1.ConditionStatus, reason, message string) v1alpha1.Condition {
	return v1alpha1.Condition{
		Type:               v1alpha1.Reconciling,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Time{Time: time.Now().UTC()},
	}
}

func newStalledCondition(status v1alpha1.ConditionStatus, reason, message string) v1alpha1.Condition {
	return v1alpha1.Condition{
		Type:               v1alpha1.Stalled,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Time{Time: time.Now().UTC()},
	}
}

// adjustConditionOrder adjusts the order of the conditions to make sure that
// the first condition in the slice is Reconciling;
// the second condition in the slice is Stalled;
// the remaining conditions are sorted alphabetically according their types.
//
// Returns:
//   - a new slice of conditions including the ordered conditions.
//
// The +kubebuilder:printcolumn markers on the ResourceGroup struct expect the type of the first
// Condition in the slice to be Reconciling, and the type of the second Condition to be Stalled.
func adjustConditionOrder(conditions []v1alpha1.Condition) []v1alpha1.Condition {
	var reconciling, stalled v1alpha1.Condition
	var others []v1alpha1.Condition
	for _, cond := range conditions {
		switch cond.Type {
		case v1alpha1.Reconciling:
			reconciling = cond
		case v1alpha1.Stalled:
			stalled = cond
		default:
			others = append(others, cond)
		}
	}

	// sort the conditions in `others`
	sort.Slice(others, func(i, j int) bool {
		return others[i].Type < others[j].Type
	})

	if reconciling.IsEmpty() {
		reconciling = newReconcilingCondition(v1alpha1.UnknownConditionStatus, "", "")
	}
	if stalled.IsEmpty() {
		stalled = newStalledCondition(v1alpha1.UnknownConditionStatus, "", "")
	}

	var result []v1alpha1.Condition
	result = append(result, reconciling, stalled)
	result = append(result, others...)
	return result
}
