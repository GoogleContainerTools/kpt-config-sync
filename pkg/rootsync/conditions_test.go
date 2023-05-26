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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const fakeConditionMessage = "Testing"

var initialNow = metav1.Date(1, time.February, 3, 4, 5, 6, 0, time.Local)
var updatedNow = metav1.Date(1, time.February, 3, 4, 5, 7, 0, time.Local)

func withConditions(conds ...v1beta1.RootSyncCondition) core.MetaMutator {
	return func(o client.Object) {
		rs := o.(*v1beta1.RootSync)
		rs.Status.Conditions = append(rs.Status.Conditions, conds...)
	}
}

func fakeCondition(condType v1beta1.RootSyncConditionType, status metav1.ConditionStatus, lastTransitionTime, lastUpdateTime metav1.Time, strs ...string) v1beta1.RootSyncCondition {
	rsc := v1beta1.RootSyncCondition{
		Type:               condType,
		Status:             status,
		Reason:             "Test",
		Message:            fakeConditionMessage,
		LastUpdateTime:     lastUpdateTime,
		LastTransitionTime: lastTransitionTime,
	}
	if condType == v1beta1.RootSyncStalled && status == metav1.ConditionTrue {
		rsc.ErrorSummary = singleErrorSummary
	}
	if len(strs) > 0 {
		rsc.Reason = strs[0]
	}
	if len(strs) > 1 {
		rsc.Message = strs[1]
	}
	return rsc
}

func TestIsReconciling(t *testing.T) {
	testCases := []struct {
		name string
		rs   *v1beta1.RootSync
		want bool
	}{
		{
			name: "Missing condition is false",
			rs:   fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			want: false,
		},
		{
			name: "False condition is false",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionFalse, initialNow, initialNow))),
			want: false,
		},
		{
			name: "True condition is true",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			want: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsReconciling(tc.rs)
			if got != tc.want {
				t.Errorf("got IsReconciling() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsStalled(t *testing.T) {
	testCases := []struct {
		name string
		rs   *v1beta1.RootSync
		want bool
	}{
		{
			name: "Missing condition is false",
			rs:   fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			want: false,
		},
		{
			name: "False condition is false",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			want: false,
		},
		{
			name: "True condition is true",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionFalse, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionTrue, initialNow, initialNow))),
			want: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsStalled(tc.rs)
			if got != tc.want {
				t.Errorf("got IsStalled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReconcilingMessage(t *testing.T) {
	testCases := []struct {
		name string
		rs   *v1beta1.RootSync
		want string
	}{
		{
			name: "Missing condition is empty",
			rs:   fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			want: "",
		},
		{
			name: "False condition is empty",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionFalse, initialNow, initialNow))),
			want: "",
		},
		{
			name: "True condition is its message",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			want: fakeConditionMessage,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReconcilingMessage(tc.rs)
			if got != tc.want {
				t.Errorf("got ReconcilingMessage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStalledMessage(t *testing.T) {
	testCases := []struct {
		name string
		rs   *v1beta1.RootSync
		want string
	}{
		{
			name: "Missing condition is empty",
			rs:   fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			want: "",
		},
		{
			name: "False condition is empty",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			want: "",
		},
		{
			name: "True condition is its message",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionFalse, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionTrue, initialNow, initialNow))),
			want: fakeConditionMessage,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := StalledMessage(tc.rs)
			if got != tc.want {
				t.Errorf("got StalledMessage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClearCondition(t *testing.T) {
	now = func() metav1.Time {
		return initialNow
	}
	testCases := []struct {
		name    string
		rs      *v1beta1.RootSync
		toClear v1beta1.RootSyncConditionType
		want    []v1beta1.RootSyncCondition
	}{
		{
			name: "Clear existing true condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionTrue, initialNow, initialNow))),
			toClear: v1beta1.RootSyncStalled,
			want: []v1beta1.RootSyncCondition{
				// No update or transition
				fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
				// Update and transition
				fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, updatedNow, updatedNow, "", ""),
			},
		},
		{
			name: "Ignore existing false condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			toClear: v1beta1.RootSyncStalled,
			want: []v1beta1.RootSyncCondition{
				// No update or transition
				fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
				// No update or transition
				fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow),
			},
		},
		{
			name:    "Handle empty conditions",
			rs:      fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			toClear: v1beta1.RootSyncStalled,
			want:    nil,
		},
	}
	now = func() metav1.Time {
		return updatedNow
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ClearCondition(tc.rs, tc.toClear)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestSetReconciling(t *testing.T) {
	now = func() metav1.Time {
		return initialNow
	}
	testCases := []struct {
		name             string
		rs               *v1beta1.RootSync
		reason           string
		message          string
		want             []v1beta1.RootSyncCondition
		wantUpdated      bool
		wantTransitioned bool
	}{
		{
			name: "Set new reconciling condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow))),
			reason:  "Test1",
			message: "This is test 1",
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, updatedNow, "Test1", "This is test 1"),
			},
			wantUpdated:      true,
			wantTransitioned: false,
		},
		{
			name: "Update existing reconciling condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			reason:  "Test2",
			message: "This is test 2",
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, updatedNow, "Test2", "This is test 2"),
				// No update or transition
				fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow),
			},
			wantUpdated:      true,
			wantTransitioned: false,
		},
	}
	now = func() metav1.Time {
		return updatedNow
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated, transitioned := SetReconciling(tc.rs, tc.reason, tc.message)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
			assert.Equal(t, tc.wantUpdated, updated, "updated")
			assert.Equal(t, tc.wantTransitioned, transitioned, "transitioned")
		})
	}
}

func TestSetStalled(t *testing.T) {
	now = func() metav1.Time {
		return initialNow
	}
	testCases := []struct {
		name             string
		rs               *v1beta1.RootSync
		reason           string
		err              error
		want             []v1beta1.RootSyncCondition
		wantUpdated      bool
		wantTransitioned bool
	}{
		{
			name:   "Set new stalled condition",
			rs:     fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			reason: "Error1",
			err:    errors.New("this is error 1"),
			want: []v1beta1.RootSyncCondition{
				// Update and transition
				fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionTrue, updatedNow, updatedNow, "Error1", "this is error 1"),
			},
			wantUpdated:      true,
			wantTransitioned: true,
		},
		{
			name: "Update existing stalled condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
					fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionFalse, initialNow, initialNow))),
			reason: "Error2",
			err:    errors.New("this is error 2"),
			want: []v1beta1.RootSyncCondition{
				// No update or transition
				fakeCondition(v1beta1.RootSyncReconciling, metav1.ConditionTrue, initialNow, initialNow),
				// Update and transition
				fakeCondition(v1beta1.RootSyncStalled, metav1.ConditionTrue, updatedNow, updatedNow, "Error2", "this is error 2"),
			},
			wantUpdated:      true,
			wantTransitioned: true,
		},
	}
	now = func() metav1.Time {
		return updatedNow
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated, transitioned := SetStalled(tc.rs, tc.reason, tc.err)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
			assert.Equal(t, tc.wantUpdated, updated, "updated")
			assert.Equal(t, tc.wantTransitioned, transitioned, "transitioned")
		})
	}
}

func TestSetSyncing(t *testing.T) {
	testCases := []struct {
		name             string
		rs               *v1beta1.RootSync
		status           bool
		reason           string
		message          string
		commit           string
		errorSources     []v1beta1.ErrorSource
		errorSummary     *v1beta1.ErrorSummary
		timestamp        metav1.Time
		want             []v1beta1.RootSyncCondition
		wantUpdated      bool
		wantTransitioned bool
	}{
		{
			name:         "Set new syncing condition without error",
			rs:           fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			status:       true,
			reason:       "Syncing",
			message:      "",
			commit:       "commit-1",
			errorSources: nil,
			errorSummary: &v1beta1.ErrorSummary{},
			timestamp:    updatedNow,
			want: []v1beta1.RootSyncCondition{
				// Update and transition
				{
					Type:               v1beta1.RootSyncSyncing,
					Status:             metav1.ConditionTrue,
					Reason:             "Syncing",
					Message:            "",
					Commit:             "commit-1",
					ErrorSourceRefs:    nil,
					ErrorSummary:       &v1beta1.ErrorSummary{},
					LastUpdateTime:     updatedNow,
					LastTransitionTime: updatedNow,
				},
			},
			wantUpdated:      true,
			wantTransitioned: true,
		},
		{
			name: "Update to add syncing error",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionTrue,
						Reason:             "Syncing",
						Message:            "",
						Commit:             "commit-1",
						ErrorSourceRefs:    nil,
						ErrorSummary:       &v1beta1.ErrorSummary{},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			status:  true,
			reason:  "Error1",
			message: "this is error 1",
			commit:  "commit-1",
			errorSources: []v1beta1.ErrorSource{
				"status.sync.errors",
			},
			errorSummary: &v1beta1.ErrorSummary{
				TotalCount: 1,
			},
			timestamp: updatedNow,
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				{
					Type:    v1beta1.RootSyncSyncing,
					Status:  metav1.ConditionTrue,
					Reason:  "Error1",
					Message: "this is error 1",
					Commit:  "commit-1",
					ErrorSourceRefs: []v1beta1.ErrorSource{
						"status.sync.errors",
					},
					ErrorSummary: &v1beta1.ErrorSummary{
						TotalCount: 1,
					},
					LastUpdateTime:     updatedNow,
					LastTransitionTime: initialNow,
				},
			},
			wantUpdated:      true,
			wantTransitioned: false,
		},
		{
			name: "Transition to completed",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:    v1beta1.RootSyncSyncing,
						Status:  metav1.ConditionTrue,
						Reason:  "Error1",
						Message: "this is error 1",
						Commit:  "commit-1",
						ErrorSourceRefs: []v1beta1.ErrorSource{
							"status.sync.errors",
						},
						ErrorSummary: &v1beta1.ErrorSummary{
							TotalCount: 1,
						},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			status:       false,
			reason:       "Synced",
			message:      "Sync Completed",
			commit:       "commit-2",
			errorSources: nil,
			errorSummary: &v1beta1.ErrorSummary{},
			timestamp:    updatedNow,
			want: []v1beta1.RootSyncCondition{
				// Update and transition
				{
					Type:               v1beta1.RootSyncSyncing,
					Status:             metav1.ConditionFalse,
					Reason:             "Synced",
					Message:            "Sync Completed",
					Commit:             "commit-2",
					ErrorSourceRefs:    nil,
					ErrorSummary:       &v1beta1.ErrorSummary{},
					LastUpdateTime:     updatedNow,
					LastTransitionTime: updatedNow,
				},
			},
			wantUpdated:      true,
			wantTransitioned: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated, transitioned := SetSyncing(tc.rs, tc.status, tc.reason, tc.message, tc.commit, tc.errorSources, tc.errorSummary, tc.timestamp)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
			assert.Equal(t, tc.wantUpdated, updated, "updated")
			assert.Equal(t, tc.wantTransitioned, transitioned, "transitioned")
		})
	}
}

func TestSetReconcilerFinalizing(t *testing.T) {
	now = func() metav1.Time {
		return initialNow
	}
	testCases := []struct {
		name        string
		rs          *v1beta1.RootSync
		reason      string
		message     string
		want        []v1beta1.RootSyncCondition
		wantUpdated bool
	}{
		{
			name:    "Set new finalizing condition without error",
			rs:      fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			reason:  "Finalizing",
			message: "",
			want: []v1beta1.RootSyncCondition{
				// Update and transition
				{
					Type:               v1beta1.RootSyncReconcilerFinalizing,
					Status:             metav1.ConditionTrue,
					Reason:             "Finalizing",
					Message:            "",
					LastUpdateTime:     updatedNow,
					LastTransitionTime: updatedNow,
				},
			},
			wantUpdated: true,
		},
		{
			name: "Update to add change message",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:               v1beta1.RootSyncReconcilerFinalizing,
						Status:             metav1.ConditionTrue,
						Reason:             "Finalizing",
						Message:            "",
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			reason:  "Finalizing",
			message: "Deleting managed objects",
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				{
					Type:               v1beta1.RootSyncReconcilerFinalizing,
					Status:             metav1.ConditionTrue,
					Reason:             "Finalizing",
					Message:            "Deleting managed objects",
					LastUpdateTime:     updatedNow,
					LastTransitionTime: initialNow,
				},
			},
			wantUpdated: true,
		},
	}
	now = func() metav1.Time {
		return updatedNow
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated := SetReconcilerFinalizing(tc.rs, tc.reason, tc.message)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
			assert.Equal(t, tc.wantUpdated, updated, "updated")
		})
	}
}

func TestSetReconcilerFinalizerFailure(t *testing.T) {
	deployment1 := fake.DeploymentObject()
	deployment1ID := core.IDOf(deployment1)

	now = func() metav1.Time {
		return initialNow
	}
	testCases := []struct {
		name        string
		rs          *v1beta1.RootSync
		errs        status.MultiError
		want        []v1beta1.RootSyncCondition
		wantUpdated bool
	}{
		{
			name: "Set new finalizer failure condition without error",
			rs:   fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			errs: nil,
			want: []v1beta1.RootSyncCondition{
				// Update and transition
				{
					Type:               v1beta1.RootSyncReconcilerFinalizerFailure,
					Status:             metav1.ConditionFalse,
					Reason:             "DestroySuccess",
					Message:            "Successfully deleted managed resource objects",
					LastUpdateTime:     updatedNow,
					LastTransitionTime: updatedNow,
				},
			},
			wantUpdated: true,
		},
		{
			name: "Set new finalizer failure condition with error",
			rs:   fake.RootSyncObjectV1Beta1(configsync.RootSyncName),
			errs: applier.DeleteErrorForResource(errors.New("fake error"), deployment1ID),
			want: []v1beta1.RootSyncCondition{
				// Update and transition
				{
					Type:    v1beta1.RootSyncReconcilerFinalizerFailure,
					Status:  metav1.ConditionTrue,
					Reason:  "DestroyFailure",
					Message: "Failed to delete managed resource objects",
					Errors: []v1beta1.ConfigSyncError{
						{
							Code:         "2009",
							ErrorMessage: "KNV2009: failed to delete Deployment.apps, /default-name: fake error\n\nFor more information, see https://g.co/cloud/acm-errors#knv2009",
						},
					},
					LastUpdateTime:     updatedNow,
					LastTransitionTime: updatedNow,
				},
			},
			wantUpdated: true,
		},
		{
			name: "Update to change status",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:    v1beta1.RootSyncReconcilerFinalizerFailure,
						Status:  metav1.ConditionTrue,
						Reason:  "DestroyFailure",
						Message: "Failed to delete managed resource objects",
						Errors: []v1beta1.ConfigSyncError{
							{
								Code:         "2009",
								ErrorMessage: "KNV2009: fake error message",
							},
						},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			errs: nil,
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				{
					Type:               v1beta1.RootSyncReconcilerFinalizerFailure,
					Status:             metav1.ConditionFalse,
					Reason:             "DestroySuccess",
					Message:            "Successfully deleted managed resource objects",
					LastUpdateTime:     updatedNow,
					LastTransitionTime: updatedNow,
				},
			},
			wantUpdated: true,
		},
		{
			name: "Update to change error message",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:    v1beta1.RootSyncReconcilerFinalizerFailure,
						Status:  metav1.ConditionTrue,
						Reason:  "DestroyFailure",
						Message: "fake error message",
						ErrorSummary: &v1beta1.ErrorSummary{
							TotalCount:                1,
							ErrorCountAfterTruncation: 1,
						},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			errs: applier.DeleteErrorForResource(errors.New("fake error"), deployment1ID),
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				{
					Type:    v1beta1.RootSyncReconcilerFinalizerFailure,
					Status:  metav1.ConditionTrue,
					Reason:  "DestroyFailure",
					Message: "Failed to delete managed resource objects",
					Errors: []v1beta1.ConfigSyncError{
						{
							Code:         "2009",
							ErrorMessage: "KNV2009: failed to delete Deployment.apps, /default-name: fake error\n\nFor more information, see https://g.co/cloud/acm-errors#knv2009",
						},
					},
					LastUpdateTime:     updatedNow,
					LastTransitionTime: initialNow,
				},
			},
			wantUpdated: true,
		},
	}
	now = func() metav1.Time {
		return updatedNow
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated := SetReconcilerFinalizerFailure(tc.rs, tc.errs)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
			assert.Equal(t, tc.wantUpdated, updated, "updated")
		})
	}
}

func TestRemoveCondition(t *testing.T) {
	now = func() metav1.Time {
		return initialNow
	}
	testCases := []struct {
		name        string
		rs          *v1beta1.RootSync
		condType    v1beta1.RootSyncConditionType
		want        []v1beta1.RootSyncCondition
		wantUpdated bool
	}{
		{
			name: "Remove Reconciling condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:               v1beta1.RootSyncReconciling,
						Status:             metav1.ConditionTrue,
						Reason:             "Reconciling",
						Message:            "",
						ErrorSummary:       &v1beta1.ErrorSummary{},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			condType:    v1beta1.RootSyncReconciling,
			want:        nil,
			wantUpdated: true,
		},
		{
			name: "Remove Syncing condition when Reconciling",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionTrue,
						Reason:             "Syncing",
						Message:            "",
						ErrorSummary:       &v1beta1.ErrorSummary{},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					},
					v1beta1.RootSyncCondition{
						Type:               v1beta1.RootSyncReconciling,
						Status:             metav1.ConditionTrue,
						Reason:             "Reconciling",
						Message:            "",
						ErrorSummary:       &v1beta1.ErrorSummary{},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			condType: v1beta1.RootSyncSyncing,
			want: []v1beta1.RootSyncCondition{
				{
					Type:               v1beta1.RootSyncReconciling,
					Status:             metav1.ConditionTrue,
					Reason:             "Reconciling",
					Message:            "",
					ErrorSummary:       &v1beta1.ErrorSummary{},
					LastUpdateTime:     initialNow,
					LastTransitionTime: initialNow,
				},
			},
			wantUpdated: true,
		},
		{
			name: "Remove missing condition",
			rs: fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
				withConditions(
					v1beta1.RootSyncCondition{
						Type:               v1beta1.RootSyncReconcilerFinalizing,
						Status:             metav1.ConditionTrue,
						Reason:             "Finalizing",
						Message:            "",
						ErrorSummary:       &v1beta1.ErrorSummary{},
						LastUpdateTime:     initialNow,
						LastTransitionTime: initialNow,
					})),
			condType: v1beta1.RootSyncSyncing,
			want: []v1beta1.RootSyncCondition{
				// Update but no transition
				{
					Type:               v1beta1.RootSyncReconcilerFinalizing,
					Status:             metav1.ConditionTrue,
					Reason:             "Finalizing",
					Message:            "",
					ErrorSummary:       &v1beta1.ErrorSummary{},
					LastUpdateTime:     initialNow,
					LastTransitionTime: initialNow,
				},
			},
			wantUpdated: false,
		},
	}
	now = func() metav1.Time {
		return updatedNow
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updated := RemoveCondition(tc.rs, tc.condType)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
			assert.Equal(t, tc.wantUpdated, updated, "updated")
		})
	}
}

func TestConditionHasNoErrors(t *testing.T) {
	testCases := []struct {
		name string
		cond v1beta1.RootSyncCondition
		want bool
	}{
		{
			"Errors is nil, ErrorSummary is nil",
			v1beta1.RootSyncCondition{},
			true,
		},
		{
			name: "Errors is not nil but empty, ErrorSummary is nil",
			cond: v1beta1.RootSyncCondition{
				Errors: []v1beta1.ConfigSyncError{},
			},
			want: true,
		},
		{
			name: "Errors is not nil and not empty, ErrorSummary is nil",
			cond: v1beta1.RootSyncCondition{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1061", ErrorMessage: "rendering-error-message"},
				},
			},
			want: false,
		},
		{
			name: "Errors is nil, ErrorSummary is not nil but empty",
			cond: v1beta1.RootSyncCondition{
				ErrorSummary: &v1beta1.ErrorSummary{},
			},
			want: true,
		},
		{
			name: "Errors is nil, ErrorSummary is not nil and not empty",
			cond: v1beta1.RootSyncCondition{
				ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1},
			},
			want: false,
		},
		{
			name: "Errors is not nil but empty, ErrorSummary is not nil but empty",
			cond: v1beta1.RootSyncCondition{
				Errors:       []v1beta1.ConfigSyncError{},
				ErrorSummary: &v1beta1.ErrorSummary{},
			},
			want: true,
		},
		{
			name: "Errors is not nil and not empty, ErrorSummary is not nil and not empty",
			cond: v1beta1.RootSyncCondition{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1061", ErrorMessage: "rendering-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1},
			},
			want: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConditionHasNoErrors(tc.cond)
			if got != tc.want {
				t.Errorf("ConditionHasNoErrors() got %v, want %v", got, tc.want)
			}
		})
	}
}
