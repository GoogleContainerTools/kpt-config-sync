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

package differ

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/policycontroller"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestIsManageableSystem(t *testing.T) {
	for _, testcase := range []struct {
		name     string
		reserved bool
	}{
		{metav1.NamespaceDefault, true},
		{"foo-bar", false},
		{"kube-foo", false},
		{metav1.NamespacePublic, true},
		{metav1.NamespaceSystem, true},
		{policycontroller.NamespaceSystem, true},
		{configmanagement.ControllerNamespace, false},
	} {
		testcase := testcase
		t.Run(testcase.name, func(t *testing.T) {
			t.Parallel()
			reserved := IsManageableSystemNamespace(fake.NamespaceObject(testcase.name))
			if reserved != testcase.reserved {
				t.Errorf("Expected %v got %v", testcase.reserved, reserved)
			}
		})
	}
}
