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

package rootsync

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// Local alias to enable unit test mocking.
var now = metav1.Now

// ClearCondition sets the specified condition to False if it is currently
// defined as True. If the condition is unspecified, then it is left that way.
func ClearCondition(rs *v1beta1.RootSync, condType v1beta1.RootSyncConditionType) {
	condition := GetCondition(rs.Status.Conditions, condType)
	if condition == nil {
		return
	}

	if condition.Status == metav1.ConditionFalse {
		return
	}

	time := now()
	condition.Status = metav1.ConditionFalse
	condition.Reason = ""
	condition.Message = ""
	condition.LastTransitionTime = time
	condition.LastUpdateTime = time
	condition.Errors = nil
	condition.ErrorSourceRefs = nil
	condition.ErrorSummary = nil
}

// IsReconciling returns true if the given RootSync has a True Reconciling condition.
func IsReconciling(rs *v1beta1.RootSync) bool {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RootSyncReconciling)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// IsStalled returns true if the given RootSync has a True Stalled condition.
func IsStalled(rs *v1beta1.RootSync) bool {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// ReconcilingMessage returns the message from a True Reconciling condition or
// an empty string if no True Reconciling condition was found.
func ReconcilingMessage(rs *v1beta1.RootSync) string {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RootSyncReconciling)
	if cond == nil || cond.Status == metav1.ConditionFalse {
		return ""
	}
	return cond.Message
}

// StalledMessage returns the message from a True Stalled condition or an empty
// string if no True Stalled condition was found.
func StalledMessage(rs *v1beta1.RootSync) string {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
	if cond == nil || cond.Status == metav1.ConditionFalse {
		return ""
	}
	return cond.Message
}

var singleErrorSummary = &v1beta1.ErrorSummary{
	TotalCount:                1,
	Truncated:                 false,
	ErrorCountAfterTruncation: 1,
}

// SetReconciling sets the Reconciling condition to True.
func SetReconciling(rs *v1beta1.RootSync, reason, message string) {
	if setCondition(rs, v1beta1.RootSyncReconciling, metav1.ConditionTrue, reason, message, "", nil, &v1beta1.ErrorSummary{}, now()) {
		// Only remove the Syncing condition when the Reconciling condition status is updated from false to true.
		removeCondition(rs, v1beta1.RootSyncSyncing)
	}
}

// SetStalled sets the Stalled condition to True.
func SetStalled(rs *v1beta1.RootSync, reason string, err error) {
	if setCondition(rs, v1beta1.RootSyncStalled, metav1.ConditionTrue, reason, err.Error(), "", nil, singleErrorSummary, now()) {
		// Only remove the Syncing condition when the Stalled condition status is updated from false to true.
		removeCondition(rs, v1beta1.RootSyncSyncing)
	}
}

// SetSyncing sets the Syncing condition.
func SetSyncing(rs *v1beta1.RootSync, status bool, reason, message, commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary, lastUpdate metav1.Time) {
	var conditionStatus metav1.ConditionStatus
	if status {
		conditionStatus = metav1.ConditionTrue
	} else {
		conditionStatus = metav1.ConditionFalse
	}
	setCondition(rs, v1beta1.RootSyncSyncing, conditionStatus, reason, message, commit, errorSources, errorSummary, lastUpdate)
}

// setCondition adds or updates the specified condition.
// It returns a boolean indicating if the condition status is transited.
func setCondition(rs *v1beta1.RootSync, condType v1beta1.RootSyncConditionType, status metav1.ConditionStatus, reason, message, commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary, lastUpdate metav1.Time) bool {
	conditionTransited := false
	condition := GetCondition(rs.Status.Conditions, condType)
	if condition == nil {
		i := len(rs.Status.Conditions)
		rs.Status.Conditions = append(rs.Status.Conditions, v1beta1.RootSyncCondition{Type: condType})
		condition = &rs.Status.Conditions[i]
	}

	if condition.Status != status {
		condition.Status = status
		condition.LastTransitionTime = lastUpdate
		conditionTransited = true
	}
	condition.Reason = reason
	condition.Message = message
	condition.Commit = commit
	condition.ErrorSourceRefs = errorSources
	condition.ErrorSummary = errorSummary
	condition.LastUpdateTime = lastUpdate
	return conditionTransited
}

// GetCondition returns the condition with the provided type.
func GetCondition(conditions []v1beta1.RootSyncCondition, condType v1beta1.RootSyncConditionType) *v1beta1.RootSyncCondition {
	for i, condition := range conditions {
		if condition.Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// removeCondition removes the RootSync condition with the provided type.
func removeCondition(rs *v1beta1.RootSync, condType v1beta1.RootSyncConditionType) {
	rs.Status.Conditions = filterOutCondition(rs.Status.Conditions, condType)
}

// filterOutCondition returns a new slice of RootSync conditions without conditions with the provided type.
func filterOutCondition(conditions []v1beta1.RootSyncCondition, condType v1beta1.RootSyncConditionType) []v1beta1.RootSyncCondition {
	var newConditions []v1beta1.RootSyncCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

// ConditionHasNoErrors returns true when `cond` has no errors, and returns false when `cond` has errors.
func ConditionHasNoErrors(cond v1beta1.RootSyncCondition) bool {
	if cond.ErrorSummary == nil {
		return len(cond.Errors) == 0
	}
	return cond.ErrorSummary.TotalCount == 0
}
