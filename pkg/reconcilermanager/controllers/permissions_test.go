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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

var (
	subject1 = rbacv1.Subject{
		APIGroup:  "rbac/v1",
		Kind:      "ServiceAccount",
		Name:      "reconciler-1",
		Namespace: "namespace-1",
	}
	subject2 = rbacv1.Subject{
		APIGroup:  "rbac/v1",
		Kind:      "ServiceAccount",
		Name:      "reconciler-2",
		Namespace: "namespace-1",
	}
	subject3 = rbacv1.Subject{
		APIGroup:  "rbac/v1",
		Kind:      "ServiceAccount",
		Name:      "reconciler-1",
		Namespace: "namespace-2",
	}
	subject4 = rbacv1.Subject{
		APIGroup:  "rbac/v1",
		Kind:      "ServiceAccount",
		Name:      "reconciler-2",
		Namespace: "namespace-2",
	}
)

func TestFindSubjectIndex(t *testing.T) {
	testCases := []struct {
		name          string
		list          []rbacv1.Subject
		entry         rbacv1.Subject
		expectedIndex int
	}{
		{
			name:          "size 0, no match",
			list:          []rbacv1.Subject{},
			entry:         subject1,
			expectedIndex: -1,
		},
		{
			name: "size 1, no match",
			list: []rbacv1.Subject{
				subject1,
			},
			entry:         subject2,
			expectedIndex: -1,
		},
		{
			name: "size 2, no match",
			list: []rbacv1.Subject{
				subject1,
				subject2,
			},
			entry:         subject3,
			expectedIndex: -1,
		},
		{
			name: "size 1, match",
			list: []rbacv1.Subject{
				subject1,
			},
			entry:         subject1,
			expectedIndex: 0,
		},
		{
			name: "size 2, match",
			list: []rbacv1.Subject{
				subject1,
				subject2,
			},
			entry:         subject2,
			expectedIndex: 1,
		},
		{
			name: "size 3, match",
			list: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
			entry:         subject2,
			expectedIndex: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			index := findSubjectIndex(tc.list, tc.entry)
			require.Equal(t, tc.expectedIndex, index)
		})
	}
}

func TestSortSubjects(t *testing.T) {
	testCases := []struct {
		name             string
		subjects         []rbacv1.Subject
		expectedSubjects []rbacv1.Subject
	}{
		{
			name:             "nil",
			subjects:         nil,
			expectedSubjects: nil,
		},
		{
			name:             "size 0",
			subjects:         []rbacv1.Subject{},
			expectedSubjects: []rbacv1.Subject{},
		},
		{
			name: "size 1",
			subjects: []rbacv1.Subject{
				subject1,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
			},
		},
		{
			name: "size 2, no change",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
		},
		{
			name: "size 2, changed",
			subjects: []rbacv1.Subject{
				subject2,
				subject1,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
		},
		{
			name: "size 3, no change",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
		{
			name: "size 3, swap 1 & 2",
			subjects: []rbacv1.Subject{
				subject2,
				subject1,
				subject3,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
		{
			name: "size 3, swap 1 & 3",
			subjects: []rbacv1.Subject{
				subject3,
				subject2,
				subject1,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
		{
			name: "size 3, swap 2 & 3",
			subjects: []rbacv1.Subject{
				subject1,
				subject3,
				subject2,
			},
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subjects := sortSubjects(tc.subjects...)
			testutil.AssertEqual(t, tc.expectedSubjects, subjects)
		})
	}
}

func TestAddSubject(t *testing.T) {
	testCases := []struct {
		name             string
		subjects         []rbacv1.Subject
		subject          rbacv1.Subject
		expectedSubjects []rbacv1.Subject
	}{
		{
			name:     "size 0, append",
			subjects: []rbacv1.Subject{},
			subject:  subject1,
			expectedSubjects: []rbacv1.Subject{
				subject1,
			},
		},
		{
			name: "size 1, append",
			subjects: []rbacv1.Subject{
				subject1,
			},
			subject: subject2,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
		},
		{
			name: "size 1, prepend",
			subjects: []rbacv1.Subject{
				subject2,
			},
			subject: subject1,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
		},
		{
			name: "size 2, append",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
			subject: subject3,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
		{
			name: "size 2, prepend",
			subjects: []rbacv1.Subject{
				subject2,
				subject3,
			},
			subject: subject1,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
		{
			name: "size 2, insert",
			subjects: []rbacv1.Subject{
				subject1,
				subject3,
			},
			subject: subject2,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subjects := addSubject(tc.subjects, tc.subject)
			testutil.AssertEqual(t, tc.expectedSubjects, subjects)
		})
	}
}

func TestRemoveSubject(t *testing.T) {
	testCases := []struct {
		name             string
		subjects         []rbacv1.Subject
		subject          rbacv1.Subject
		expectedSubjects []rbacv1.Subject
	}{
		{
			name:             "size 0",
			subjects:         []rbacv1.Subject{},
			subject:          subject1,
			expectedSubjects: []rbacv1.Subject{},
		},
		{
			name: "size 1, not found",
			subjects: []rbacv1.Subject{
				subject1,
			},
			subject: subject2,
			expectedSubjects: []rbacv1.Subject{
				subject1,
			},
		},
		{
			name: "size 1, found",
			subjects: []rbacv1.Subject{
				subject1,
			},
			subject:          subject1,
			expectedSubjects: []rbacv1.Subject{},
		},
		{
			name: "size 2, not found",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
			subject: subject3,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
		},
		{
			name: "size 2, first",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
			subject: subject1,
			expectedSubjects: []rbacv1.Subject{
				subject2,
			},
		},
		{
			name: "size 2, last",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
			subject: subject2,
			expectedSubjects: []rbacv1.Subject{
				subject1,
			},
		},
		{
			name: "size 3, not found",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
			subject: subject4,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
		},
		{
			name: "size 3, first",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
			subject: subject1,
			expectedSubjects: []rbacv1.Subject{
				subject2,
				subject3,
			},
		},
		{
			name: "size 3, second",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
			subject: subject2,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject3,
			},
		},
		{
			name: "size 3, third",
			subjects: []rbacv1.Subject{
				subject1,
				subject2,
				subject3,
			},
			subject: subject3,
			expectedSubjects: []rbacv1.Subject{
				subject1,
				subject2,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			subjects := removeSubject(tc.subjects, tc.subject)
			testutil.AssertEqual(t, tc.expectedSubjects, subjects)
		})
	}
}
