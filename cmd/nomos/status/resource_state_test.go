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

package status

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestResourceState(t *testing.T) {
	resources := []resourceState{
		{
			Group:     "apps",
			Kind:      "Deployment",
			Namespace: "bookstore",
			Name:      "test",
			Status:    "CURRENT",
		},
		{
			Group:     "",
			Kind:      "Service",
			Namespace: "bookstore",
			Name:      "test",
			Status:    "CURRENT",
		},
		{
			Kind:      "Service",
			Namespace: "gamestore",
			Name:      "test",
			Status:    "CURRENT",
		},
		{
			Group:  "rbac.authorization.k8s.io",
			Kind:   "ClusterRole",
			Name:   "test",
			Status: "CURRENT",
		},
	}
	sort.Sort(byNamespaceAndType(resources))

	expected := []resourceState{
		{
			Group:  "rbac.authorization.k8s.io",
			Kind:   "ClusterRole",
			Name:   "test",
			Status: "CURRENT",
		},
		{
			Group:     "apps",
			Kind:      "Deployment",
			Namespace: "bookstore",
			Name:      "test",
			Status:    "CURRENT",
		},
		{
			Group:     "",
			Kind:      "Service",
			Namespace: "bookstore",
			Name:      "test",
			Status:    "CURRENT",
		},
		{
			Kind:      "Service",
			Namespace: "gamestore",
			Name:      "test",
			Status:    "CURRENT",
		},
	}
	if diff := cmp.Diff(expected, resources); diff != "" {
		t.Error(diff)
	}
}
