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

package namespaceconfig

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
)

type namespaceConfigEqualTestcase struct {
	name      string
	lhs       *v1.NamespaceConfig
	rhs       *v1.NamespaceConfig
	wantEqual bool
}

func (t *namespaceConfigEqualTestcase) Run(tt *testing.T) {
	equal, err := NamespaceConfigsEqual(fake.NewDecoder(nil), t.lhs, t.rhs)
	if err != nil {
		tt.Fatalf("unexpected error: %v", err)
	}

	if t.wantEqual == equal {
		return
	}

	diff := cmp.Diff(t.lhs, t.rhs, ncsIgnore...)
	if equal {
		tt.Errorf("wanted not equal, got equal: %s", diff)
	} else {
		tt.Errorf("wanted equal, got not equal: %s", diff)
	}
}

var time1 = time.Now()

var namespaceConfigEqualTestcases = []namespaceConfigEqualTestcase{
	{
		name: "basic",
		lhs: &v1.NamespaceConfig{
			Spec: v1.NamespaceConfigSpec{},
		},
		rhs: &v1.NamespaceConfig{
			Spec: v1.NamespaceConfigSpec{},
		},
		wantEqual: true,
	},
	{
		name: "different import token",
		lhs: &v1.NamespaceConfig{
			Spec: v1.NamespaceConfigSpec{
				Token: "1234567890123456789012345678901234567890",
			},
		},
		rhs: &v1.NamespaceConfig{
			Spec: v1.NamespaceConfigSpec{
				Token: "1234567890123456789012345678900000000000",
			},
		},
		wantEqual: true,
	},
	{
		name: "different import time",
		lhs: &v1.NamespaceConfig{
			Spec: v1.NamespaceConfigSpec{
				ImportTime: metav1.NewTime(time1),
			},
		},
		rhs: &v1.NamespaceConfig{
			Spec: v1.NamespaceConfigSpec{
				ImportTime: metav1.NewTime(time1.Add(time.Second)),
			},
		},
		wantEqual: true,
	},
}

func TestNamespaceConfigEqual(t *testing.T) {
	for _, tc := range namespaceConfigEqualTestcases {
		t.Run(tc.name, tc.Run)
	}
}
