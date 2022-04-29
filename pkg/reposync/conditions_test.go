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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testNs = "test"
const fakeConditionMessage = "Testing"

var testNow = metav1.Date(1, time.February, 3, 4, 5, 6, 7, time.Local)

func withConditions(conds ...v1beta1.RepoSyncCondition) core.MetaMutator {
	return func(o client.Object) {
		rs := o.(*v1beta1.RepoSync)
		rs.Status.Conditions = append(rs.Status.Conditions, conds...)
	}
}

func fakeCondition(condType v1beta1.RepoSyncConditionType, status metav1.ConditionStatus, strs ...string) v1beta1.RepoSyncCondition {
	rsc := v1beta1.RepoSyncCondition{
		Type:               condType,
		Status:             status,
		Reason:             "Test",
		Message:            fakeConditionMessage,
		LastUpdateTime:     testNow,
		LastTransitionTime: testNow,
	}
	if condType == v1beta1.RepoSyncReconciling && status == metav1.ConditionTrue {
		rsc.ErrorSummary = &v1beta1.ErrorSummary{}
	}
	if condType == v1beta1.RepoSyncStalled && status == metav1.ConditionTrue {
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
		rs   *v1beta1.RepoSync
		want bool
	}{
		{
			"Missing condition is false",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			false,
		},
		{
			"False condition is false",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionFalse))),
			false,
		},
		{
			"True condition is true",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			true,
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
		rs   *v1beta1.RepoSync
		want bool
	}{
		{
			"Missing condition is false",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			false,
		},
		{
			"False condition is false",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			false,
		},
		{
			"True condition is true",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionFalse), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionTrue))),
			true,
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
		rs   *v1beta1.RepoSync
		want string
	}{
		{
			"Missing condition is empty",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			"",
		},
		{
			"False condition is empty",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionFalse))),
			"",
		},
		{
			"True condition is its message",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			fakeConditionMessage,
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
		rs   *v1beta1.RepoSync
		want string
	}{
		{
			"Missing condition is empty",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			"",
		},
		{
			"False condition is empty",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			"",
		},
		{
			"True condition is its message",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionFalse), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionTrue))),
			fakeConditionMessage,
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
		return testNow
	}
	testCases := []struct {
		name    string
		rs      *v1beta1.RepoSync
		toClear v1beta1.RepoSyncConditionType
		want    []v1beta1.RepoSyncCondition
	}{
		{
			"Clear existing true condition",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionTrue))),
			v1beta1.RepoSyncStalled,
			[]v1beta1.RepoSyncCondition{
				fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue),
				fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse, "", ""),
			},
		},
		{
			"Ignore existing false condition",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			v1beta1.RepoSyncStalled,
			[]v1beta1.RepoSyncCondition{
				fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue),
				fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse),
			},
		},
		{
			"Handle empty conditions",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			v1beta1.RepoSyncStalled,
			nil,
		},
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
		return testNow
	}
	testCases := []struct {
		name    string
		rs      *v1beta1.RepoSync
		reason  string
		message string
		want    []v1beta1.RepoSyncCondition
	}{
		{
			"Set new reconciling condition",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			"Test1",
			"This is test 1",
			[]v1beta1.RepoSyncCondition{
				fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue, "Test1", "This is test 1"),
			},
		},
		{
			"Update existing reconciling condition",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionFalse), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			"Test2",
			"This is test 2",
			[]v1beta1.RepoSyncCondition{
				fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue, "Test2", "This is test 2"),
				fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SetReconciling(tc.rs, tc.reason, tc.message)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestSetStalled(t *testing.T) {
	testCases := []struct {
		name   string
		rs     *v1beta1.RepoSync
		reason string
		err    error
		want   []v1beta1.RepoSyncCondition
	}{
		{
			"Set new stalled condition",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName),
			"Error1",
			errors.New("this is error 1"),
			[]v1beta1.RepoSyncCondition{
				fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionTrue, "Error1", "this is error 1"),
			},
		},
		{
			"Update existing stalled condition",
			fake.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName, withConditions(fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue), fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionFalse))),
			"Error2",
			errors.New("this is error 2"),
			[]v1beta1.RepoSyncCondition{
				fakeCondition(v1beta1.RepoSyncReconciling, metav1.ConditionTrue),
				fakeCondition(v1beta1.RepoSyncStalled, metav1.ConditionTrue, "Error2", "this is error 2"),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SetStalled(tc.rs, tc.reason, tc.err)
			if diff := cmp.Diff(tc.want, tc.rs.Status.Conditions); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestConditionHasNoErrors(t *testing.T) {
	testCases := []struct {
		name string
		cond v1beta1.RepoSyncCondition
		want bool
	}{
		{
			"Errors is nil, ErrorSummary is nil",
			v1beta1.RepoSyncCondition{},
			true,
		},
		{
			"Errors is not nil but empty, ErrorSummary is nil",
			v1beta1.RepoSyncCondition{
				Errors: []v1beta1.ConfigSyncError{},
			},
			true,
		},
		{
			"Errors is not nil and not empty, ErrorSummary is nil",
			v1beta1.RepoSyncCondition{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1061", ErrorMessage: "rendering-error-message"},
				},
			},
			false,
		},
		{
			"Errors is nil, ErrorSummary is not nil but empty",
			v1beta1.RepoSyncCondition{
				ErrorSummary: &v1beta1.ErrorSummary{},
			},
			true,
		},
		{
			"Errors is nil, ErrorSummary is not nil and not empty",
			v1beta1.RepoSyncCondition{
				ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1},
			},
			false,
		},
		{
			"Errors is not nil but empty, ErrorSummary is not nil but empty",
			v1beta1.RepoSyncCondition{
				Errors:       []v1beta1.ConfigSyncError{},
				ErrorSummary: &v1beta1.ErrorSummary{},
			},
			true,
		},
		{
			"Errors is not nil and not empty, ErrorSummary is not nil and not empty",
			v1beta1.RepoSyncCondition{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1061", ErrorMessage: "rendering-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1},
			},
			false,
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
