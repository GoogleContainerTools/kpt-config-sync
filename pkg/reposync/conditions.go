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

package reposync

import (
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// Local alias to enable unit test mocking.
var now = metav1.Now

// IsReconciling returns true if the given RepoSync has a True Reconciling condition.
func IsReconciling(rs *v1beta1.RepoSync) bool {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RepoSyncReconciling)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// IsStalled returns true if the given RepoSync has a True Stalled condition.
func IsStalled(rs *v1beta1.RepoSync) bool {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// ReconcilingMessage returns the message from a True Reconciling condition or
// an empty string if no True Reconciling condition was found.
func ReconcilingMessage(rs *v1beta1.RepoSync) string {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RepoSyncReconciling)
	if cond == nil || cond.Status == metav1.ConditionFalse {
		return ""
	}
	return cond.Message
}

// StalledMessage returns the message from a True Stalled condition or an empty
// string if no True Stalled condition was found.
func StalledMessage(rs *v1beta1.RepoSync) string {
	cond := GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
	if cond == nil || cond.Status == metav1.ConditionFalse {
		return ""
	}
	return cond.Message
}

// ClearCondition sets the specified condition to False if it is currently
// defined as True. If the condition is unspecified, then it is left that way.
func ClearCondition(rs *v1beta1.RepoSync, condType v1beta1.RepoSyncConditionType) {
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

var singleErrorSummary = &v1beta1.ErrorSummary{
	TotalCount:                1,
	Truncated:                 false,
	ErrorCountAfterTruncation: 1,
}

// SetReconciling sets the Reconciling condition to True.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
// Removes the Syncing condition if the Reconciling condition transitioned.
func SetReconciling(rs *v1beta1.RepoSync, reason, message string) (updated, transitioned bool) {
	updated, transitioned = setCondition(rs, v1beta1.RepoSyncReconciling, metav1.ConditionTrue, reason, message, "", nil, &v1beta1.ErrorSummary{}, now())
	if transitioned {
		removeCondition(rs, v1beta1.RepoSyncSyncing)
	}
	return updated, transitioned
}

// SetStalled sets the Stalled condition to True.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
// Removes the Syncing condition if the Stalled condition transitioned.
func SetStalled(rs *v1beta1.RepoSync, reason string, err error) (updated, transitioned bool) {
	updated, transitioned = setCondition(rs, v1beta1.RepoSyncStalled, metav1.ConditionTrue, reason, err.Error(), "", nil, singleErrorSummary, now())
	if transitioned {
		removeCondition(rs, v1beta1.RepoSyncSyncing)
	}
	return updated, transitioned
}

// SetSyncing sets the Syncing condition.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
func SetSyncing(rs *v1beta1.RepoSync, status bool, reason, message, commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary, timestamp metav1.Time) (updated, transitioned bool) {
	var conditionStatus metav1.ConditionStatus
	if status {
		conditionStatus = metav1.ConditionTrue
	} else {
		conditionStatus = metav1.ConditionFalse
	}
	return setCondition(rs, v1beta1.RepoSyncSyncing, conditionStatus, reason, message, commit, errorSources, errorSummary, timestamp)
}

// setCondition adds or updates the specified condition with a True status.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
func setCondition(rs *v1beta1.RepoSync, condType v1beta1.RepoSyncConditionType, status metav1.ConditionStatus, reason, message, commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary, timestamp metav1.Time) (updated, transitioned bool) {
	condition := GetCondition(rs.Status.Conditions, condType)
	if condition == nil {
		i := len(rs.Status.Conditions)
		rs.Status.Conditions = append(rs.Status.Conditions, v1beta1.RepoSyncCondition{Type: condType})
		condition = &rs.Status.Conditions[i]
	}

	if condition.Status != status {
		condition.Status = status
		condition.LastTransitionTime = timestamp
		transitioned = true
		updated = true
	} else if condition.Reason != reason ||
		condition.Message != message ||
		condition.Commit != commit ||
		!equality.Semantic.DeepEqual(condition.ErrorSourceRefs, errorSources) ||
		!equality.Semantic.DeepEqual(condition.ErrorSummary, errorSummary) {
		updated = true
	} else {
		return updated, transitioned
	}
	condition.Reason = reason
	condition.Message = message
	condition.Commit = commit
	condition.ErrorSourceRefs = errorSources
	condition.ErrorSummary = errorSummary
	condition.LastUpdateTime = timestamp

	return updated, transitioned
}

// GetCondition returns the condition with the provided type.
func GetCondition(conditions []v1beta1.RepoSyncCondition, condType v1beta1.RepoSyncConditionType) *v1beta1.RepoSyncCondition {
	for i, condition := range conditions {
		if condition.Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// removeCondition removes the RepoSync condition with the provided type.
func removeCondition(rs *v1beta1.RepoSync, condType v1beta1.RepoSyncConditionType) (updated bool) {
	rs.Status.Conditions, updated = filterOutCondition(rs.Status.Conditions, condType)
	return updated
}

// filterOutCondition returns a new slice of RepoSync conditions without conditions with the provided type.
func filterOutCondition(conditions []v1beta1.RepoSyncCondition, condType v1beta1.RepoSyncConditionType) ([]v1beta1.RepoSyncCondition, bool) {
	var newConditions []v1beta1.RepoSyncCondition
	updated := false
	for _, c := range conditions {
		if c.Type == condType {
			updated = true
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions, updated
}

// ConditionHasNoErrors returns true when `cond` has no errors, and returns false when `cond` has errors.
func ConditionHasNoErrors(cond v1beta1.RepoSyncCondition) bool {
	if cond.ErrorSummary == nil {
		return len(cond.Errors) == 0
	}
	return cond.ErrorSummary.TotalCount == 0
}
