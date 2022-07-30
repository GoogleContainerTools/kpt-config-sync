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

package diff

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIgnoreTimestampUpdates(t *testing.T) {
	nowish := metav1.Now()

	testCases := []struct {
		name        string
		left, right interface{}
		expected    bool
	}{
		{
			name: "unequal structs",
			left: struct {
				NotTime string
			}{
				NotTime: "left",
			},
			right: struct {
				NotTime string
			}{
				NotTime: "right",
			},
			expected: false,
		},
		{
			name: "equal structs",
			left: struct {
				NotTime string
			}{
				NotTime: "left",
			},
			right: struct {
				NotTime string
			}{
				NotTime: "left",
			},
			expected: true,
		},
		{
			name: "unequal structs with different times",
			left: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
				Time:    metav1.NewTime(time.Now()),
			},
			right: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "right",
				Time:    metav1.NewTime(time.Now().Add(1 * time.Minute)),
			},
			expected: false,
		},
		{
			name: "equal structs with different times",
			left: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
				Time:    metav1.NewTime(time.Now()),
			},
			right: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
				Time:    metav1.NewTime(time.Now().Add(1 * time.Minute)),
			},
			expected: true,
		},
		{
			name: "equal structs with unset and set times",
			left: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
			},
			right: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
				Time:    metav1.NewTime(time.Now().Add(1 * time.Minute)),
			},
			expected: false,
		},
		{
			name: "equal structs with equal times",
			left: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
				Time:    nowish,
			},
			right: struct {
				NotTime string
				Time    metav1.Time
			}{
				NotTime: "left",
				Time:    nowish,
			},
			expected: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := cmp.Equal(tc.left, tc.right, IgnoreTimestampUpdates)
			assert.Equal(t, tc.expected, result, "[%s]", tc.name)
		})
	}
}
