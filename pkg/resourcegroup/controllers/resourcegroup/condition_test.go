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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
)

func TestAdjustConditionOrder(t *testing.T) {
	reconciling := v1alpha1.Condition{
		Type: v1alpha1.Reconciling,
	}
	stalled := v1alpha1.Condition{
		Type: v1alpha1.Stalled,
	}
	testCond1 := v1alpha1.Condition{
		Type: v1alpha1.ConditionType("hello"),
	}
	testCond2 := v1alpha1.Condition{
		Type: v1alpha1.ConditionType("world"),
	}
	tests := map[string]struct {
		conditions         []v1alpha1.Condition
		expectedConditions []v1alpha1.Condition
	}{
		"should order the reconciling and stalled conditions correctly": {
			conditions:         []v1alpha1.Condition{stalled, reconciling},
			expectedConditions: []v1alpha1.Condition{reconciling, stalled},
		},
		"should handle empty condition slice correctly": {
			conditions: []v1alpha1.Condition{},
			expectedConditions: []v1alpha1.Condition{
				{Type: v1alpha1.Reconciling, Status: v1alpha1.UnknownConditionStatus},
				{Type: v1alpha1.Stalled, Status: v1alpha1.UnknownConditionStatus},
			},
		},
		"should order the remaining conditions correctly": {
			conditions:         []v1alpha1.Condition{testCond2, stalled, testCond1, reconciling},
			expectedConditions: []v1alpha1.Condition{reconciling, stalled, testCond1, testCond2},
		},
	}
	for name, tc := range tests {
		t.Run(fmt.Sprintf("adjustConditionOrder %s", name), func(t *testing.T) {
			gotConditions := adjustConditionOrder(tc.conditions)
			assert.Equal(t, len(tc.expectedConditions), len(gotConditions))
			for i := range gotConditions {
				assert.Equal(t, tc.expectedConditions[i].Type, gotConditions[i].Type)
				assert.Equal(t, tc.expectedConditions[i].Status, gotConditions[i].Status)
			}
		})
	}
}

func TestOwnershipCondition(t *testing.T) {
	tests := map[string]struct {
		id                string
		inv               string
		expectedCondition *v1alpha1.Condition
	}{
		"should return nil for empty inventory id and empty owning inventory": {
			id:                "",
			inv:               "",
			expectedCondition: nil,
		},
		"should return nil for matched inventory id and owning inventory": {
			id:                "id",
			inv:               "id",
			expectedCondition: nil,
		},
		"Should return unmatched message": {
			id:  "id",
			inv: "unmatched",
			expectedCondition: &v1alpha1.Condition{
				Message: "This resource is owned by another ResourceGroup unmatched. The status only reflects the specification for the current object in ResourceGroup unmatched.",
				Reason:  v1alpha1.OwnershipUnmatch,
				Status:  v1alpha1.TrueConditionStatus,
			},
		},
		"Should return not owned message": {
			id:  "id",
			inv: "",
			expectedCondition: &v1alpha1.Condition{
				Message: "This object is not owned by any inventory object. The status for the current object may not reflect the specification for it in current ResourceGroup.",
				Reason:  v1alpha1.OwnershipEmpty,
				Status:  v1alpha1.UnknownConditionStatus,
			},
		},
	}
	for name, tc := range tests {
		t.Run(fmt.Sprintf("ownershipCondition %s", name), func(t *testing.T) {
			c := ownershipCondition(tc.id, tc.inv)
			if tc.expectedCondition == nil {
				assert.Nil(t, c)
			} else {
				assert.NotNil(t, c)
				assert.Equal(t, tc.expectedCondition.Message, c.Message)
				assert.Equal(t, tc.expectedCondition.Reason, c.Reason)
				assert.Equal(t, tc.expectedCondition.Status, c.Status)
			}
		})
	}
}
