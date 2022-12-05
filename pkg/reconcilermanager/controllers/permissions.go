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
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
)

// rolereference returns an intialized Role with apigroup, kind and name.
func rolereference(name, kind string) rbacv1.RoleRef {
	return rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     kind,
		Name:     name,
	}
}

// newSubject returns and initialized Subject with kind, name and namespace.
func newSubject(name, namespace, kind string) rbacv1.Subject {
	return rbacv1.Subject{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}
}

// findSubjectIndex returns the index of the expected subject in the list, or -1.
func findSubjectIndex(list []rbacv1.Subject, expected rbacv1.Subject) int {
	for i, found := range list {
		if found == expected {
			return i
		}
	}
	return -1
}

// addSubject adds a subject to the subject list.
// Subjects are sorted, if modified.
func addSubject(subjects []rbacv1.Subject, subject rbacv1.Subject) []rbacv1.Subject {
	if i := findSubjectIndex(subjects, subject); i < 0 {
		subjects = sortSubjects(append(subjects, subject)...)
	}
	return subjects
}

// removeSubject removes the subject from the subject list.
// Subjects are not re-sorted, but will remain sorted if sorted on insert, by
// addSubject.
func removeSubject(subjects []rbacv1.Subject, subject rbacv1.Subject) []rbacv1.Subject {
	if i := findSubjectIndex(subjects, subject); i >= 0 {
		if i == 0 {
			// remove first subject
			subjects = subjects[i+1:]
		} else if i == len(subjects)-1 {
			// remove last subject
			subjects = subjects[:i]
		} else {
			// remove middle subject
			subjects = append(subjects[:i], subjects[i+1:]...)
		}
	}
	return subjects
}

// sortSubjects returns the subjects as a new sorted list
func sortSubjects(subjects ...rbacv1.Subject) []rbacv1.Subject {
	if len(subjects) > 1 {
		sort.Slice(subjects, func(i, j int) bool {
			if subjects[i].APIGroup != subjects[j].APIGroup {
				return subjects[i].APIGroup < subjects[j].APIGroup
			}
			if subjects[i].Kind != subjects[j].Kind {
				return subjects[i].Kind < subjects[j].Kind
			}
			if subjects[i].Namespace != subjects[j].Namespace {
				return subjects[i].Namespace < subjects[j].Namespace
			}
			if subjects[i].Name != subjects[j].Name {
				return subjects[i].Name < subjects[j].Name
			}
			return false
		})
	}
	return subjects
}
