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
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/status"
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
// Returns whether the condition was updated (any change) or transitioned
// (status change).
// Removes the Syncing condition if the Reconciling condition transitioned.
func SetReconciling(rs *v1beta1.RootSync, reason, message string) (updated, transitioned bool) {
	updated, transitioned = setCondition(rs, v1beta1.RootSyncReconciling, metav1.ConditionTrue, reason, message, "", nil, nil, nil, now())
	if transitioned {
		RemoveCondition(rs, v1beta1.RootSyncSyncing)
	}
	return updated, transitioned
}

// SetStalled sets the Stalled condition to True.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
// Removes the Syncing condition if the Stalled condition transitioned.
func SetStalled(rs *v1beta1.RootSync, reason string, err error) (updated, transitioned bool) {
	updated, transitioned = setCondition(rs, v1beta1.RootSyncStalled, metav1.ConditionTrue, reason, err.Error(), "", nil, nil, singleErrorSummary, now())
	if transitioned {
		RemoveCondition(rs, v1beta1.RootSyncSyncing)
	}
	return updated, transitioned
}

// SetSyncing sets the Syncing condition.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
func SetSyncing(rs *v1beta1.RootSync, status bool, reason, message, commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary, timestamp metav1.Time) (updated, transitioned bool) {
	var conditionStatus metav1.ConditionStatus
	if status {
		conditionStatus = metav1.ConditionTrue
	} else {
		conditionStatus = metav1.ConditionFalse
	}
	return setCondition(rs, v1beta1.RootSyncSyncing, conditionStatus, reason, message, commit, nil, errorSources, errorSummary, timestamp)
}

// SetStabilizing sets the Stabilizing condition.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
func SetStabilizing(rs *v1beta1.RootSync, status metav1.ConditionStatus, reason, message, commit string, errs []v1beta1.ConfigSyncError, timestamp metav1.Time) (updated, transitioned bool) {
	var errorSummary *v1beta1.ErrorSummary
	if len(errs) > 0 {
		errorSummary = &v1beta1.ErrorSummary{
			TotalCount:                len(errs),
			Truncated:                 false,
			ErrorCountAfterTruncation: len(errs),
		}
	}
	return setCondition(rs, v1beta1.RootSyncStabilizing, status, reason, message, commit, errs, nil, errorSummary, timestamp)
}

// SetReconcilerFinalizing sets the ReconcilerFinalizing condition to True.
// Use RemoveCondition to remove this condition. It should never be set to False.
func SetReconcilerFinalizing(rs *v1beta1.RootSync, reason, message string) (updated bool) {
	updated, _ = setCondition(rs, v1beta1.RootSyncReconcilerFinalizing, metav1.ConditionTrue, reason, message, "", nil, nil, nil, now())
	return updated
}

// SetReconcilerFinalizerFailure sets the ReconcilerFinalizerFailure condition.
// If there are errors, the status is True, otherwise False.
// Use RemoveCondition to remove this condition when the finalizer is done.
func SetReconcilerFinalizerFailure(rs *v1beta1.RootSync, errs status.MultiError) (updated bool) {
	var conditionStatus metav1.ConditionStatus
	var reason, message string
	var csErrs []v1beta1.ConfigSyncError
	if errs != nil {
		conditionStatus = metav1.ConditionTrue
		reason = "DestroyFailure"
		message = "Failed to delete managed resource objects"
		csErrs = status.ToCSE(errs)
	} else {
		conditionStatus = metav1.ConditionFalse
		reason = "DestroySuccess"
		message = "Successfully deleted managed resource objects"
		csErrs = nil
	}
	updated, _ = setCondition(rs, v1beta1.RootSyncReconcilerFinalizerFailure,
		conditionStatus, reason, message, "", csErrs, nil, nil, now())
	return updated
}

// setCondition adds or updates the specified condition with a True status.
// Returns whether the condition was updated (any change) or transitioned
// (status change).
//
// Use Errors OR (ErrorSource & ErrorSummary).
// Errors should only be used if there isn't another status field to reference.
func setCondition(rs *v1beta1.RootSync, condType v1beta1.RootSyncConditionType, status metav1.ConditionStatus, reason, message, commit string, errs []v1beta1.ConfigSyncError, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary, timestamp metav1.Time) (updated, transitioned bool) {
	condition := GetCondition(rs.Status.Conditions, condType)
	if condition == nil {
		i := len(rs.Status.Conditions)
		rs.Status.Conditions = append(rs.Status.Conditions, v1beta1.RootSyncCondition{Type: condType})
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
		!equality.Semantic.DeepEqual(condition.Errors, errs) ||
		!equality.Semantic.DeepEqual(condition.ErrorSourceRefs, errorSources) ||
		!equality.Semantic.DeepEqual(condition.ErrorSummary, errorSummary) {
		updated = true
	} else {
		return updated, transitioned
	}
	condition.Reason = reason
	condition.Message = message
	condition.Commit = commit
	condition.Errors = errs
	condition.ErrorSourceRefs = errorSources
	condition.ErrorSummary = errorSummary
	condition.LastUpdateTime = timestamp

	return updated, transitioned
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

// RemoveCondition removes the RootSync condition with the provided type.
func RemoveCondition(rs *v1beta1.RootSync, condType v1beta1.RootSyncConditionType) (updated bool) {
	rs.Status.Conditions, updated = filterOutCondition(rs.Status.Conditions, condType)
	return updated
}

// filterOutCondition returns a new slice of RootSync conditions without conditions with the provided type.
func filterOutCondition(conditions []v1beta1.RootSyncCondition, condType v1beta1.RootSyncConditionType) ([]v1beta1.RootSyncCondition, bool) {
	var newConditions []v1beta1.RootSyncCondition
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
func ConditionHasNoErrors(cond v1beta1.RootSyncCondition) bool {
	if cond.ErrorSummary == nil {
		return len(cond.Errors) == 0
	}
	return cond.ErrorSummary.TotalCount == 0
}
